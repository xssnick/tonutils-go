package liteclient

import (
	"bufio"
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
	"log"
	"math/big"
	"net"
	"sync/atomic"
	"time"
)

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

	// get timeout if exists
	till, ok := ctx.Deadline()
	if !ok {
		till = time.Now().Add(60 * time.Second)

		// if no timeout passed, we still set it to 60 sec, to not hang forever
		timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		ctx = timeoutCtx
	}

	conn, err := net.DialTimeout("tcp", addr, till.Sub(time.Now()))
	if err != nil {
		return err
	}

	// we need 160 random bytes from which we will construct encryption keys for packets
	dd := make([]byte, 160)
	if _, err := io.ReadFull(rand.Reader, dd); err != nil {
		return err
	}

	r := bufio.NewReader(conn)

	// build ciphers for incoming packets and for outgoing
	rCrypt, err := newCipherCtr(dd[:32], dd[64:80])
	if err != nil {
		return err
	}
	wCrypt, err := newCipherCtr(dd[32:64], dd[80:96])
	if err != nil {
		return err
	}

	hs, err := c.handshake(dd, privateKey, sKey)
	if err != nil {
		return err
	}

	// send handshake packet to establish connection
	_, err = conn.Write(hs)
	if err != nil {
		return err
	}

	connEvent := make(chan error, 1)

	var initialized bool

	go func() {
		// listen for incoming packets
		for {
			var sz uint32
			sz, err = c.readSize(r, rCrypt)
			if err != nil {
				break
			}

			// should at least have nonce (its 32 bytes) and something else
			if sz <= 32 {
				err = errors.New("too small size of packet")
				break
			}

			var data []byte
			data, err = c.readData(r, rCrypt, sz)
			if err != nil {
				break
			}

			checksum := data[len(data)-32:]
			data = data[:len(data)-32]

			err = c.validatePacket(data, checksum)
			if err != nil {
				break
			}

			// skip nonce
			data = data[32:]

			// response for handshake is empty packet, it means that connection established
			if len(data) == 0 {
				if !initialized {
					initialized = true
					connEvent <- nil
				}
				continue
			}

			var typeID int32
			var queryID string
			var payload []byte

			typeID, queryID, payload, err = c.parseServerResp(data)
			if err != nil {
				break
			}

			c.mx.RLock()
			ch := c.activeReqs[queryID]
			c.mx.RUnlock()

			if ch != nil {
				ch.RespChan <- &LiteResponse{
					TypeID: typeID,
					Data:   payload,
				}
			} else {
				// handle system
			}
		}

		if initialized {
			// deactivate connection
			atomic.AddInt32(&c.activeConnections, -1)
		}

		connEvent <- err
		_ = conn.Close()
	}()

	select {
	case err = <-connEvent:
		if err != nil {
			return err
		}

		// start pings
		go func() {
			for {
				time.Sleep(5 * time.Second)

				n, err := rand.Int(rand.Reader, new(big.Int).SetUint64(0xFFFFFFFFFFFFFF))
				if err != nil {
					log.Println("rand err", err)
					continue
				}

				err = c.ping(conn, wCrypt, n.Uint64())
				if err != nil {
					log.Println("ping err", err)
					continue
				}
			}
		}()

		// now we are ready to accept requests
		go func() {
			for {
				var req *LiteRequest

				select {
				case req = <-c.requester:
					if req == nil {
						// handle graceful shutdown
						return
					}
				case <-connEvent:
					// on this stage it can be only connection issue
					return
				}

				err := c.queryLiteServer(conn, wCrypt, req.QueryID, req.TypeID, req.Data)
				if err != nil {
					// TODO: put request back to pool to pickup by next connection

					req.RespChan <- &LiteResponse{
						err: err,
					}

					// err can happen only because of network error, anyway close it for some case
					_ = conn.Close()
					return
				}
			}
		}()

		atomic.AddInt32(&c.activeConnections, 1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) readSize(reader io.Reader, cryptor cipher.Stream) (uint32, error) {
	size := make([]byte, 4)
	_, err := reader.Read(size)
	if err != nil {
		return 0, err
	}

	// decrypt packet
	cryptor.XORKeyStream(size, size)

	sz := binary.LittleEndian.Uint32(size)

	if sz > 10<<20 {
		return 0, fmt.Errorf("too big size of packet: %s", hex.EncodeToString(size))
	}

	return sz, nil
}

func (c *Client) readData(reader io.Reader, cryptor cipher.Stream, sz uint32) ([]byte, error) {
	var result []byte

	// read exact number of bytes requested, blocking operation
	left := int(sz)
	for left > 0 {
		data := make([]byte, left)
		n, err := reader.Read(data)
		if err != nil {
			return nil, err
		}

		data = data[:n]
		cryptor.XORKeyStream(data, data)
		result = append(result, data...)

		left -= n
	}

	return result, nil
}

func (c *Client) send(w io.Writer, cryptor cipher.Stream, data []byte) error {
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
	cryptor.XORKeyStream(buf, buf)

	// write all
	for len(buf) > 0 {
		n, err := w.Write(buf)
		if err != nil {
			return err
		}

		buf = buf[n:]
	}

	return nil
}

func (c *Client) handshake(data []byte, ourKey ed25519.PrivateKey, serverKey ed25519.PublicKey) ([]byte, error) {
	hash := sha256.New()
	hash.Write(data)
	checksum := hash.Sum(nil)

	pub := ourKey.Public().(ed25519.PublicKey)

	kid, err := keyID(serverKey)
	if err != nil {
		return nil, err
	}

	key, err := sharedKey(ourKey, serverKey)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	// encrypt data
	ctr.XORKeyStream(data, data)

	var res []byte
	res = append(res, kid...)
	res = append(res, pub...)
	res = append(res, checksum...)
	res = append(res, data...)

	return res, nil
}

func (c *Client) ping(w io.Writer, cryptor cipher.Stream, qid uint64) error {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint32(data, uint32(TCPPing))
	binary.LittleEndian.PutUint64(data[4:], qid)

	return c.send(w, cryptor, data)
}

func (c *Client) queryADNL(w io.Writer, cryptor cipher.Stream, qid, payload []byte) error {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, uint32(ADNLQuery))

	data = append(data, qid...)
	if len(payload) >= 0xFE {
		ln := make([]byte, 4)
		binary.LittleEndian.PutUint32(data, uint32(len(payload)<<8)|0xFE)
		data = append(data, ln...)
	} else {
		data = append(data, byte(len(payload)))
	}
	data = append(data, payload...)

	left := len(data) % 4
	if left != 0 {
		data = append(data, make([]byte, 4-left)...)
	}

	return c.send(w, cryptor, data)
}

func (c *Client) queryLiteServer(w io.Writer, cryptor cipher.Stream, qid []byte, typeID int32, payload []byte) error {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, uint32(LiteServerQuery))

	if len(payload) >= 0xFE {
		ln := make([]byte, 4)
		binary.LittleEndian.PutUint32(data, uint32((len(payload)+4)<<8)|0xFE)
		data = append(data, ln...)
	} else {
		data = append(data, byte(len(payload)+4))
	}

	typData := make([]byte, 4)
	binary.LittleEndian.PutUint32(typData, uint32(typeID))

	data = append(data, typData...)
	data = append(data, payload...)

	left := len(data) % 4
	if left != 0 {
		data = append(data, make([]byte, 4-left)...)
	}

	return c.queryADNL(w, cryptor, qid, data)
}
