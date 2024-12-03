package liteclient

import (
	"bytes"
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
	"log"
	"math/big"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
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

	reqs chan *ADNLRequest

	authed  bool
	authEvt chan bool

	authLock sync.Mutex
	ourNonce []byte

	weight       int64
	lastRespTime int64

	pool *ConnectionPool
}

func (c *ConnectionPool) AddConnectionsFromConfig(ctx context.Context, config *GlobalConfig) error {
	if len(config.Liteservers) == 0 {
		return ErrNoConnections
	}

	fails := int32(0)
	result := make(chan error, len(config.Liteservers))

	timeout := 3 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		timeout = dl.Sub(time.Now())
	}

	for _, ls := range config.Liteservers {
		ip := intToIP4(ls.IP)
		conStr := fmt.Sprintf("%s:%d", ip, ls.Port)
		ls := ls

		go func() {
			// we need personal context for each call, because it gets cancelled on failure of one
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			err := c.AddConnection(ctx, conStr, ls.ID.Key)
			if err == nil {
				result <- nil
				return
			}

			// if everything failed
			if int(atomic.AddInt32(&fails, 1)) == len(config.Liteservers) {
				result <- err
			}
		}()
	}

	// return on first success connection
	return <-result
}

func (c *ConnectionPool) AddConnectionsFromConfigFile(configPath string) error {
	config, err := GetConfigFromFile(configPath)
	if err != nil {
		return err
	}

	return c.AddConnectionsFromConfig(context.Background(), config)
}

func (c *ConnectionPool) AddConnectionsFromConfigUrl(ctx context.Context, configUrl string) error {
	config, err := GetConfigFromUrl(ctx, configUrl)
	if err != nil {
		return err
	}

	return c.AddConnectionsFromConfig(ctx, config)
}

var ErrStopped = errors.New("connection pool is closed")

func (c *ConnectionPool) AddConnection(ctx context.Context, addr, serverKey string, clientKey ...ed25519.PrivateKey) error {
	select {
	case <-c.globalCtx.Done():
		return ErrStopped
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	sKey, err := base64.StdEncoding.DecodeString(serverKey)
	if err != nil {
		return err
	}

	var privateKey ed25519.PrivateKey
	if len(clientKey) == 0 || clientKey[0] == nil {
		// generate new random client key
		_, privateKey, err = ed25519.GenerateKey(nil)
		if err != nil {
			return err
		}
	} else {
		privateKey = clientKey[0]
	}

	conn := &connection{
		addr:       addr,
		serverKey:  serverKey,
		connResult: make(chan error, 1),
		reqs:       make(chan *ADNLRequest),
		pool:       c,
		id:         crc32.ChecksumIEEE([]byte(serverKey)),
		weight:     1000,
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
	conn.rCrypt, err = adnl.NewCipherCtr(rnd[:32], rnd[64:80])
	if err != nil {
		return err
	}
	conn.wCrypt, err = adnl.NewCipherCtr(rnd[32:64], rnd[80:96])
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

	if c.authKey != nil {
		conn.authEvt = make(chan bool, 1)
		err = conn.authRequest()
		if err != nil {
			return fmt.Errorf("failed to send auth: %w", err)
		}

		select {
		case <-conn.authEvt:
			// auth completed
		case <-c.globalCtx.Done():
			return ErrStopped
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	select {
	case <-c.globalCtx.Done():
		return ErrStopped
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
		sz, err = readSize(n.tcp, n.rCrypt)
		if err != nil {
			break
		}

		// should at least have nonce (its 32 bytes) and something else
		if sz <= 32 {
			err = errors.New("too small size of packet")
			break
		}

		var data []byte
		data, err = readData(n.tcp, n.rCrypt, sz)
		if err != nil {
			break
		}

		checksum := data[len(data)-32:]
		data = data[:len(data)-32]

		if err = validatePacket(data, checksum); err != nil {
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

		var resp tl.Serializable
		_, err = tl.Parse(&resp, data, true)
		if err != nil {
			log.Println("failed to parse message:", err.Error())
			break
		}

		switch t := resp.(type) {
		case TCPPong:
			// TODO: check ping
		case TCPAuthenticationNonce:
			if n.pool.authKey == nil {
				log.Println("server wants authorization, but we dont have a key:", err.Error())
				break
			}

			err = n.authSignComplete(t.Nonce)
			if err != nil {
				log.Println("failed to sign and send auth message:", err.Error())
				break
			}
		case adnl.MessageAnswer:
			n.pool.reqMx.RLock()
			ch := n.pool.activeReqs[string(t.ID)]
			n.pool.reqMx.RUnlock()

			if ch != nil {
				ch.RespChan <- &ADNLResponse{
					Data: t.Data,
				}
			}
		default:
			log.Println("unknown ls msg:", reflect.TypeOf(t).String())
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

		select {
		case <-n.pool.globalCtx.Done():
			return
		default:
			n.pool.reqMx.RLock()
			dis := n.pool.onDisconnect
			n.pool.reqMx.RUnlock()

			if dis != nil {
				go dis(n.addr, n.serverKey)
			}
		}
	}
}

func (n *connection) startPings(every time.Duration) {
	for {
		select {
		case <-n.pool.globalCtx.Done():
			return
		case <-time.After(every):
		}

		num, err := rand.Int(rand.Reader, new(big.Int).SetInt64(0xFFFFFFFFFFFFFFF))
		if err != nil {
			continue
		}

		err = n.ping(num.Int64())
		if err != nil {
			// force close in case of error
			_ = n.tcp.Close()

			break
		}
	}
}

func readSize(conn net.Conn, crypt cipher.Stream) (uint32, error) {
	size := make([]byte, 4)
	_, err := conn.Read(size)
	if err != nil {
		return 0, err
	}

	// decrypt packet
	crypt.XORKeyStream(size, size)

	sz := binary.LittleEndian.Uint32(size)

	if sz > 10<<20 {
		return 0, fmt.Errorf("too big size of packet: %s", hex.EncodeToString(size))
	}

	return sz, nil
}

func readData(conn net.Conn, crypt cipher.Stream, sz uint32) ([]byte, error) {
	if sz > 8<<20 {
		return nil, fmt.Errorf("too big packet")
	}

	var result = make([]byte, sz)

	// read exact number of bytes requested, blocking operation
	read := 0
	for read < cap(result) {
		num, err := conn.Read(result[read:])
		if err != nil {
			return nil, err
		}

		data := result[read : read+num] // pointer
		crypt.XORKeyStream(data, data)
		read += num
	}

	return result, nil
}

type NetworkErr struct {
	error
}

func (e NetworkErr) Is(err error) bool {
	if _, ok := err.(NetworkErr); ok {
		return true
	}
	return false
}

func (e NetworkErr) Unwrap() error {
	return e.error
}

func buildPacket(data []byte) ([]byte, error) {
	buf := make([]byte, 4+32, 4+64+len(data))
	binary.LittleEndian.PutUint32(buf, uint32(64+len(data)))

	// nonce
	if _, err := io.ReadFull(rand.Reader, buf[4:4+32]); err != nil {
		return nil, err
	}
	buf = append(buf, data...)

	hash := sha256.New()
	hash.Write(buf[4:])
	checksum := hash.Sum(nil)

	buf = append(buf, checksum...)
	return buf, nil
}

func (n *connection) send(data []byte) error {
	buf, err := buildPacket(data)
	if err != nil {
		return err
	}

	n.wLock.Lock()
	defer n.wLock.Unlock()

	return writeEncrypt(n.tcp, n.wCrypt, buf)
}

func writeEncrypt(conn net.Conn, crypt cipher.Stream, buf []byte) error {
	// encrypt data
	crypt.XORKeyStream(buf, buf)

	// write timeout in case of stuck socket, to reconnect
	_ = conn.SetWriteDeadline(time.Now().Add(7 * time.Second))
	// write all
	for len(buf) > 0 {
		num, err := conn.Write(buf)
		if err != nil {
			_ = conn.Close()
			return NetworkErr{err}
		}

		buf = buf[num:]
	}
	return nil
}

func (n *connection) handshake(data []byte, ourKey ed25519.PrivateKey, serverKey ed25519.PublicKey) error {
	hash := sha256.New()
	hash.Write(data)
	checksum := hash.Sum(nil)

	pub := ourKey.Public().(ed25519.PublicKey)

	kid, err := tl.Hash(adnl.PublicKeyED25519{Key: serverKey})
	if err != nil {
		return err
	}

	key, err := adnl.SharedKey(ourKey, serverKey)
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

	ctr, err := adnl.NewCipherCtr(k, iv)
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

func (n *connection) ping(qid int64) error {
	payload, err := tl.Serialize(TCPPing{qid}, true)
	if err != nil {
		return fmt.Errorf("failed to serialize request, err: %w", err)
	}

	return n.send(payload)
}

func (n *connection) queryAdnl(qid []byte, req tl.Serializable) (string, error) {
	payload, err := tl.Serialize(adnl.MessageQuery{
		ID:   qid,
		Data: req,
	}, true)
	if err != nil {
		return "", fmt.Errorf("failed to serialize request, err: %w", err)
	}
	return n.addr, n.send(payload)
}

func (c *ConnectionPool) DefaultReconnect(waitBeforeReconnect time.Duration, maxTries int) OnDisconnectCallback {
	var tries int

	var cb OnDisconnectCallback
	cb = func(addr, key string) {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
			err := c.AddConnection(ctx, addr, key)
			cancel()

			if err != nil && (tries < maxTries || maxTries == -1) {
				tries++
				time.Sleep(waitBeforeReconnect)
				continue
			}
			break
		}
		tries = 0
	}

	return cb
}

func validatePacket(data []byte, recvChecksum []byte) error {
	if len(data) < 32 {
		return errors.New("too small packet")
	}

	hash := sha256.New()
	hash.Write(data)
	checksum := hash.Sum(nil)

	if !bytes.Equal(recvChecksum, checksum) {
		return errors.New("checksum packet")
	}

	return nil
}
