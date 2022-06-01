package liteclient

import (
	"context"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync/atomic"
	"time"
)

type connection struct {
	addr      string
	serverKey string

	connResult chan error

	conn   net.Conn
	rCrypt cipher.Stream
	wCrypt cipher.Stream

	client *Client
}

func (c *Client) Connect(ctx context.Context, addr, serverKey string) error {
	sKey, err := base64.StdEncoding.DecodeString(serverKey)
	if err != nil {
		return err
	}

	// generate new random client key
	_, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}

	cn := &connection{
		addr:       addr,
		serverKey:  serverKey,
		connResult: make(chan error, 1),
		client:     c,
	}

	// get timeout if exists
	till, ok := ctx.Deadline()
	if !ok {
		till = time.Now().Add(60 * time.Second)
	}

	cn.conn, err = net.DialTimeout("tcp", addr, till.Sub(time.Now()))
	if err != nil {
		return err
	}

	// we need 160 random bytes from which we will construct encryption keys for packets
	rnd := make([]byte, 160)
	if _, err := io.ReadFull(rand.Reader, rnd); err != nil {
		return err
	}

	// build ciphers for incoming packets and for outgoing
	cn.rCrypt, err = newCipherCtr(rnd[:32], rnd[64:80])
	if err != nil {
		return err
	}
	cn.wCrypt, err = newCipherCtr(rnd[32:64], rnd[80:96])
	if err != nil {
		return err
	}

	err = cn.handshake(rnd, privateKey, sKey)
	if err != nil {
		return err
	}

	connResult := make(chan error, 1)

	go cn.listen(connResult)

	select {
	case err = <-connResult:
		if err != nil {
			return err
		}

		// start pings
		go cn.startPings(5 * time.Second)

		// now we are ready to accept requests
		go cn.serve(connResult)

		atomic.AddInt32(&c.activeConnections, 1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (cn *connection) listen(connResult chan<- error) {
	var initialized bool

	var err error
	// listen for incoming packets
	for {
		var sz uint32
		sz, err = cn.readSize()
		if err != nil {
			break
		}

		// should at least have nonce (its 32 bytes) and something else
		if sz <= 32 {
			err = errors.New("too small size of packet")
			break
		}

		var data []byte
		data, err = cn.readData(sz)
		if err != nil {
			break
		}

		checksum := data[len(data)-32:]
		data = data[:len(data)-32]

		err = validatePacket(data, checksum)
		if err != nil {
			break
		}

		// skip nonce
		data = data[32:]

		// response for handshake is empty packet, it means that connection established
		if len(data) == 0 {
			if !initialized {
				initialized = true
				connResult <- nil
			}
			continue
		}

		var typeID int32
		var queryID string
		var payload []byte

		typeID, queryID, payload, err = parseServerResp(data)
		if err != nil {
			break
		}

		cn.client.mx.RLock()
		ch := cn.client.activeReqs[queryID]
		cn.client.mx.RUnlock()

		if ch != nil {
			ch.RespChan <- &LiteResponse{
				TypeID: typeID,
				Data:   payload,
			}
		}
	}

	// force close in case of error
	_ = cn.conn.Close()

	connResult <- err

	if initialized {
		// deactivate connection
		atomic.AddInt32(&cn.client.activeConnections, -1)

		cn.client.mx.RLock()
		dis := cn.client.onDisconnect
		cn.client.mx.RUnlock()

		if dis != nil {
			go dis(cn.addr, cn.serverKey)
		}
	}
}

func (cn *connection) serve(connResult <-chan error) {
	for {
		var req *LiteRequest

		select {
		case req = <-cn.client.requester:
			if req == nil {
				// handle graceful shutdown
				return
			}
		case <-connResult:
			// on this stage it can be only connection issue
			return
		}

		err := cn.queryLiteServer(req.QueryID, req.TypeID, req.Data)
		if err != nil {
			req.RespChan <- &LiteResponse{
				err: err,
			}

			// force close in case of error
			_ = cn.conn.Close()

			return
		}
	}
}

func (cn *connection) startPings(every time.Duration) {
	for {
		select {
		case <-time.After(every):
		}

		n, err := rand.Int(rand.Reader, new(big.Int).SetUint64(0xFFFFFFFFFFFFFF))
		if err != nil {
			continue
		}

		err = cn.ping(n.Uint64())
		if err != nil {
			// force close in case of error
			_ = cn.conn.Close()

			break
		}
	}
}

func (cn *connection) readSize() (uint32, error) {
	size := make([]byte, 4)
	_, err := cn.conn.Read(size)
	if err != nil {
		return 0, err
	}

	// decrypt packet
	cn.rCrypt.XORKeyStream(size, size)

	sz := binary.LittleEndian.Uint32(size)

	if sz > 10<<20 {
		return 0, fmt.Errorf("too big size of packet: %s", hex.EncodeToString(size))
	}

	return sz, nil
}

func (cn *connection) readData(sz uint32) ([]byte, error) {
	var result []byte

	// read exact number of bytes requested, blocking operation
	left := int(sz)
	for left > 0 {
		data := make([]byte, left)
		n, err := cn.conn.Read(data)
		if err != nil {
			return nil, err
		}

		data = data[:n]
		cn.rCrypt.XORKeyStream(data, data)
		result = append(result, data...)

		left -= n
	}

	return result, nil
}

func (cn *connection) send(data []byte) error {
	buf := make([]byte, 4)

	// ADNL packet should have nonce
	nonce := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}

	binary.LittleEndian.PutUint32(buf, uint32(64+len(data)))
	buf = append(buf, nonce...)
	buf = append(buf, data...)

	hash := sha256.New()
	hash.Write(buf[4:])
	checksum := hash.Sum(nil)

	buf = append(buf, checksum...)

	// encrypt data
	cn.wCrypt.XORKeyStream(buf, buf)

	// write all
	for len(buf) > 0 {
		n, err := cn.conn.Write(buf)
		if err != nil {
			return err
		}

		buf = buf[n:]
	}

	return nil
}

func (cn *connection) handshake(data []byte, ourKey ed25519.PrivateKey, serverKey ed25519.PublicKey) error {
	hash := sha256.New()
	hash.Write(data)
	checksum := hash.Sum(nil)

	pub := ourKey.Public().(ed25519.PublicKey)

	kid, err := keyID(serverKey)
	if err != nil {
		return err
	}

	key, err := sharedKey(ourKey, serverKey)
	if err != nil {
		return err
	}

	k := []byte{
		key[0], key[1], key[2], key[3], key[4], key[5], key[6], key[7],
		key[8], key[9], key[10], key[11], key[12], key[13], key[14], key[15],
		checksum[16], checksum[17], checksum[18], checksum[19], checksum[20], checksum[21], checksum[22], checksum[23],
		checksum[24], checksum[25], checksum[26], checksum[27], checksum[28], checksum[29], checksum[30], checksum[31],
	}

	iv := []byte{
		checksum[0], checksum[1], checksum[2], checksum[3], key[20], key[21], key[22], key[23],
		key[24], key[25], key[26], key[27], key[28], key[29], key[30], key[31],
	}

	ctr, err := newCipherCtr(k, iv)
	if err != nil {
		return err
	}

	// encrypt data
	ctr.XORKeyStream(data, data)

	var res []byte
	res = append(res, kid...)
	res = append(res, pub...)
	res = append(res, checksum...)
	res = append(res, data...)

	// send handshake packet to establish connection
	_, err = cn.conn.Write(res)
	if err != nil {
		return err
	}

	return nil
}

func (cn *connection) ping(qid uint64) error {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint32(data, uint32(TCPPing))
	binary.LittleEndian.PutUint64(data[4:], qid)

	return cn.send(data)
}

func (cn *connection) queryADNL(qid, payload []byte) error {
	// bypass compiler negative check
	t := ADNLQuery

	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, uint32(t))

	data = append(data, qid...)
	data = append(data, storableBytes(payload)...)

	return cn.send(data)
}

func (cn *connection) queryLiteServer(qid []byte, typeID int32, payload []byte) error {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, uint32(LiteServerQuery))

	typData := make([]byte, 4)
	binary.LittleEndian.PutUint32(typData, uint32(typeID))

	data = append(data, storableBytes(append(typData, payload...))...)

	return cn.queryADNL(qid, data)
}

func storableBytes(buf []byte) []byte {
	var data []byte

	// store buf length
	if len(buf) >= 0xFE {
		ln := make([]byte, 4)
		binary.LittleEndian.PutUint32(ln, uint32(len(buf)<<8)|0xFE)
		data = append(data, ln...)
	} else {
		data = append(data, byte(len(buf)))
	}

	data = append(data, buf...)

	// adjust actual length to fit % 4 = 0
	if round := len(data) % 4; round != 0 {
		data = append(data, make([]byte, 4-round)...)
	}

	return data
}

func (c *Client) DefaultReconnect(waitBeforeReconnect time.Duration, maxTries int) OnDisconnectCallback {
	var tries int

	var cb OnDisconnectCallback
	cb = func(addr, key string) {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()

		err := c.Connect(ctx, addr, key)
		if err != nil {
			if tries < maxTries {
				time.Sleep(waitBeforeReconnect)
				tries++

				cb(addr, key)
			}

			return
		}

		tries = 0
	}

	return cb
}
