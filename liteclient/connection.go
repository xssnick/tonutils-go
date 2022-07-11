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
	"hash/crc32"
	"io"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

type connection struct {
	id        uint32
	addr      string
	serverKey string

	connResult chan error

	tcp    net.Conn
	rCrypt cipher.Stream
	wCrypt cipher.Stream
	wLock  sync.Mutex

	reqs chan *LiteRequest

	pool *ConnectionPool
}

func (c *ConnectionPool) AddConnection(ctx context.Context, addr, serverKey string) error {
	sKey, err := base64.StdEncoding.DecodeString(serverKey)
	if err != nil {
		return err
	}

	// generate new random client key
	_, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}

	conn := &connection{
		addr:       addr,
		serverKey:  serverKey,
		connResult: make(chan error, 1),
		reqs:       make(chan *LiteRequest),
		pool:       c,
		id:         crc32.ChecksumIEEE([]byte(serverKey)),
	}

	// get timeout if exists
	till, ok := ctx.Deadline()
	if !ok {
		till = time.Now().Add(60 * time.Second)
	}

	conn.tcp, err = net.DialTimeout("tcp", addr, till.Sub(time.Now()))
	if err != nil {
		return err
	}

	// we need 160 random bytes from which we will construct encryption keys for packets
	rnd := make([]byte, 160)
	if _, err := io.ReadFull(rand.Reader, rnd); err != nil {
		return err
	}

	// build ciphers for incoming packets and for outgoing
	conn.rCrypt, err = newCipherCtr(rnd[:32], rnd[64:80])
	if err != nil {
		return err
	}
	conn.wCrypt, err = newCipherCtr(rnd[32:64], rnd[80:96])
	if err != nil {
		return err
	}

	err = conn.handshake(rnd, privateKey, sKey)
	if err != nil {
		return err
	}

	connResult := make(chan error, 1)

	// we are waiting for handshake response and after
	// connection established sending signal to connResult
	// and listen goroutine stays working after it to serve responses
	go conn.listen(connResult)

	select {
	case err = <-connResult:
		if err != nil {
			return err
		}

		go conn.startPings(5 * time.Second)

		c.nodesMx.Lock()
		c.activeNodes = append(c.activeNodes, conn)
		c.nodesMx.Unlock()

		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (n *connection) listen(connResult chan<- error) {
	var initialized bool

	var err error
	// listen for incoming packets
	for {
		var sz uint32
		sz, err = n.readSize()
		if err != nil {
			break
		}

		// should at least have nonce (its 32 bytes) and something else
		if sz <= 32 {
			err = errors.New("too small size of packet")
			break
		}

		var data []byte
		data, err = n.readData(sz)
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

		n.pool.reqMx.RLock()
		ch := n.pool.activeReqs[queryID]
		n.pool.reqMx.RUnlock()

		if ch != nil {
			ch.RespChan <- &LiteResponse{
				TypeID: typeID,
				Data:   payload,
			}
		}
	}

	// force close in case of error
	_ = n.tcp.Close()

	connResult <- err

	if initialized {
		// deactivate connection
		n.pool.nodesMx.Lock()
		for i := range n.pool.activeNodes {
			if n.pool.activeNodes[i] == n {
				// remove from list
				n.pool.activeNodes = append(n.pool.activeNodes[:i], n.pool.activeNodes[i+1:]...)
				break
			}
		}
		n.pool.nodesMx.Unlock()

		n.pool.reqMx.RLock()
		dis := n.pool.onDisconnect
		n.pool.reqMx.RUnlock()

		if dis != nil {
			go dis(n.addr, n.serverKey)
		}
	}
}

func (n *connection) startPings(every time.Duration) {
	for {
		select {
		case <-time.After(every):
		}

		num, err := rand.Int(rand.Reader, new(big.Int).SetUint64(0xFFFFFFFFFFFFFF))
		if err != nil {
			continue
		}

		err = n.ping(num.Uint64())
		if err != nil {
			// force close in case of error
			_ = n.tcp.Close()

			break
		}
	}
}

func (n *connection) readSize() (uint32, error) {
	size := make([]byte, 4)
	_, err := n.tcp.Read(size)
	if err != nil {
		return 0, err
	}

	// decrypt packet
	n.rCrypt.XORKeyStream(size, size)

	sz := binary.LittleEndian.Uint32(size)

	if sz > 10<<20 {
		return 0, fmt.Errorf("too big size of packet: %s", hex.EncodeToString(size))
	}

	return sz, nil
}

func (n *connection) readData(sz uint32) ([]byte, error) {
	var result []byte

	// read exact number of bytes requested, blocking operation
	left := int(sz)
	for left > 0 {
		data := make([]byte, left)
		num, err := n.tcp.Read(data)
		if err != nil {
			return nil, err
		}

		data = data[:num]
		n.rCrypt.XORKeyStream(data, data)
		result = append(result, data...)

		left -= num
	}

	return result, nil
}

func (n *connection) send(data []byte) error {
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

	n.wLock.Lock()
	defer n.wLock.Unlock()

	// encrypt data
	n.wCrypt.XORKeyStream(buf, buf)

	// write all
	for len(buf) > 0 {
		n, err := n.tcp.Write(buf)
		if err != nil {
			return err
		}

		buf = buf[n:]
	}

	return nil
}

func (n *connection) handshake(data []byte, ourKey ed25519.PrivateKey, serverKey ed25519.PublicKey) error {
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
	_, err = n.tcp.Write(res)
	if err != nil {
		return err
	}

	return nil
}

func (n *connection) ping(qid uint64) error {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint32(data, uint32(TCPPing))
	binary.LittleEndian.PutUint64(data[4:], qid)

	return n.send(data)
}

func (n *connection) queryADNL(qid, payload []byte) error {
	// bypass compiler negative check
	t := ADNLQuery

	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, uint32(t))

	data = append(data, qid...)
	data = append(data, tl.ToBytes(payload)...)

	return n.send(data)
}

func (n *connection) queryLiteServer(qid []byte, typeID int32, payload []byte) error {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, uint32(LiteServerQuery))

	typData := make([]byte, 4)
	binary.LittleEndian.PutUint32(typData, uint32(typeID))

	data = append(data, tl.ToBytes(append(typData, payload...))...)

	return n.queryADNL(qid, data)
}

func (c *ConnectionPool) DefaultReconnect(waitBeforeReconnect time.Duration, maxTries int) OnDisconnectCallback {
	var tries int

	var cb OnDisconnectCallback
	cb = func(addr, key string) {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()

		err := c.AddConnection(ctx, addr, key)
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
