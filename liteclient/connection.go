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
	"github.com/xssnick/tonutils-go/adnl/keys"
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

var packetBufferSizes = [...]int{
	512,
	1024,
	2 << 10,
	4 << 10,
	8 << 10,
	16 << 10,
	32 << 10,
	64 << 10,
	128 << 10,
}

const packetSerializeBufferSize = 4 << 10

var packetBufferPools [len(packetBufferSizes)]sync.Pool

type packetBuffer struct {
	data   []byte
	pooled []byte
}

func (p packetBuffer) release() {
	releasePacketBuffer(p.pooled)
}

func acquirePacketBuffer(size int) []byte {
	for i, bucketSize := range packetBufferSizes {
		if size <= bucketSize {
			if buf, ok := packetBufferPools[i].Get().([]byte); ok {
				return buf[:size]
			}
			return make([]byte, size, bucketSize)
		}
	}
	return make([]byte, size)
}

func releasePacketBuffer(buf []byte) {
	if buf == nil {
		return
	}

	for i, bucketSize := range packetBufferSizes {
		if cap(buf) == bucketSize {
			packetBufferPools[i].Put(buf[:bucketSize])
			return
		}
	}
}

func sameBackingBuffer(a, b []byte) bool {
	return len(a) > 0 && len(b) > 0 && &a[0] == &b[0]
}

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
	inflight     int64
	lastRespTime int64
	missedPings  int64

	pingMx      sync.Mutex
	activePings map[int64]time.Time

	registered bool
	closed     bool
	closeErr   error

	pool *ConnectionPool
}

const (
	_nodeMaxWeight          = int64(1000)
	_nodeInitialWeight      = int64(700)
	_nodeReqSuccessReward   = int64(35)
	_nodePingSuccessReward  = int64(12)
	_nodeReqTimeoutPenalty  = int64(150)
	_nodeReqSendPenalty     = int64(220)
	_nodePingTimeoutPenalty = int64(60)
	_nodeInflightPenalty    = int64(75)
	_nodeEWMAHistoryWeight  = int64(3) // 75% history, 25% new sample
	_nodeWarmupLatency      = 750 * time.Millisecond
	_nodeMaxMissedPings     = int64(3)
)

func (n *connection) effectiveWeight() int64 {
	return atomic.LoadInt64(&n.weight) - atomic.LoadInt64(&n.inflight)*_nodeInflightPenalty
}

func (n *connection) effectiveLatency() int64 {
	latency := atomic.LoadInt64(&n.lastRespTime)
	if latency <= 0 {
		return int64(_nodeWarmupLatency)
	}
	return latency
}

func (n *connection) adjustWeight(delta int64) {
	for {
		cur := atomic.LoadInt64(&n.weight)
		next := cur + delta
		if next < 0 {
			next = 0
		}
		if next > _nodeMaxWeight {
			next = _nodeMaxWeight
		}
		if atomic.CompareAndSwapInt64(&n.weight, cur, next) {
			return
		}
	}
}

func (n *connection) observeLatency(sample time.Duration) {
	if sample <= 0 {
		return
	}

	sampleNs := sample.Nanoseconds()
	for {
		curRaw := atomic.LoadInt64(&n.lastRespTime)
		if curRaw <= 0 {
			if atomic.CompareAndSwapInt64(&n.lastRespTime, curRaw, sampleNs) {
				return
			}
			continue
		}

		next := (curRaw*_nodeEWMAHistoryWeight + sampleNs) / (_nodeEWMAHistoryWeight + 1)
		if atomic.CompareAndSwapInt64(&n.lastRespTime, curRaw, next) {
			return
		}
	}
}

func (n *connection) requestStarted() {
	atomic.AddInt64(&n.inflight, 1)
}

func (n *connection) requestFinished() {
	if atomic.AddInt64(&n.inflight, -1) < 0 {
		atomic.StoreInt64(&n.inflight, 0)
	}
}

func (n *connection) requestSucceeded(took time.Duration) {
	n.requestFinished()
	atomic.StoreInt64(&n.missedPings, 0)
	n.observeLatency(took)
	n.adjustWeight(_nodeReqSuccessReward)
}

func (n *connection) requestTimedOut(took time.Duration) {
	n.requestFinished()
	if took >= 200*time.Millisecond {
		n.adjustWeight(-_nodeReqTimeoutPenalty)
	}
}

func (n *connection) requestSendFailed() {
	n.requestFinished()
	n.adjustWeight(-_nodeReqSendPenalty)
}

func (n *connection) notePingSent(qid int64, sentAt time.Time) {
	n.pingMx.Lock()
	if n.activePings == nil {
		n.activePings = map[int64]time.Time{}
	}
	n.activePings[qid] = sentAt
	n.pingMx.Unlock()
}

func (n *connection) forgetPing(qid int64) {
	n.pingMx.Lock()
	delete(n.activePings, qid)
	n.pingMx.Unlock()
}

func (n *connection) notePong(qid int64) {
	n.pingMx.Lock()
	sentAt, ok := n.activePings[qid]
	if ok {
		delete(n.activePings, qid)
	}
	n.pingMx.Unlock()
	if !ok {
		return
	}

	atomic.StoreInt64(&n.missedPings, 0)
	n.observeLatency(time.Since(sentAt))
	n.adjustWeight(_nodePingSuccessReward)
}

func (n *connection) expirePingsBefore(deadline time.Time) int {
	n.pingMx.Lock()
	missed := 0
	for qid, sentAt := range n.activePings {
		if !sentAt.After(deadline) {
			delete(n.activePings, qid)
			missed++
		}
	}
	n.pingMx.Unlock()

	if missed > 0 {
		atomic.AddInt64(&n.missedPings, int64(missed))
		n.adjustWeight(-_nodePingTimeoutPenalty * int64(missed))
	}
	return missed
}

func (c *ConnectionPool) AddConnectionsFromConfig(ctx context.Context, config *GlobalConfig) error {
	if len(config.Liteservers) == 0 {
		return ErrNoConnections
	}

	const attempts = 2
	const attemptTimeout = 8 * time.Second

	fails := int32(0)
	totalAttempts := int32(len(config.Liteservers) * attempts)
	result := make(chan error, 1)
	var reportOnce sync.Once

	report := func(err error) {
		reportOnce.Do(func() {
			result <- err
		})
	}

	for _, ls := range config.Liteservers {
		ip := intToIP4(ls.IP)
		conStr := fmt.Sprintf("%s:%d", ip, ls.Port)
		ls := ls

		go func() {
			for i := 0; i < attempts; i++ {
				// Keep background dialing tied to the pool lifetime, not the caller's wait context.
				ctx, cancel := context.WithTimeout(c.globalCtx, attemptTimeout)
				err := c.AddConnection(ctx, conStr, ls.ID.Key)
				cancel()
				if err == nil {
					report(nil)
					return
				}

				// if everything failed
				if atomic.AddInt32(&fails, 1) == totalAttempts {
					report(err)
				}
			}
		}()
	}

	// return on first success connection
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.globalCtx.Done():
		return ErrStopped
	}
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
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	}

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
		weight:     _nodeInitialWeight,
	}

	till, _ := ctx.Deadline()

	conn.tcp, err = net.DialTimeout("tcp", addr, till.Sub(time.Now()))
	if err != nil {
		return err
	}
	registered := false
	defer func() {
		if !registered && conn.tcp != nil {
			_ = conn.tcp.Close()
		}
	}()

	// we need 160 random bytes from which we will construct encryption keys for packets
	rnd := make([]byte, 160)
	if _, err := io.ReadFull(rand.Reader, rnd); err != nil {
		return err
	}

	// build ciphers for incoming packets and for outgoing
	conn.rCrypt, err = keys.NewCipherCtr(rnd[:32], rnd[64:80])
	if err != nil {
		return err
	}
	conn.wCrypt, err = keys.NewCipherCtr(rnd[32:64], rnd[80:96])
	if err != nil {
		return err
	}

	err = conn.handshake(rnd, privateKey, sKey)
	if err != nil {
		return err
	}

	handshakeResult := make(chan error, 1)
	listenResult := make(chan error, 1)

	if c.authKey != nil {
		conn.authEvt = make(chan bool, 1)
	}

	// we are waiting for handshake response and after
	// connection established sending signal to handshakeResult
	// and listen goroutine stays working after it to serve responses
	go conn.listen(handshakeResult, listenResult)

	select {
	case <-c.globalCtx.Done():
		return ErrStopped
	case err = <-handshakeResult:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	if c.authKey != nil {
		err = conn.authRequest()
		if err != nil {
			return fmt.Errorf("failed to send auth: %w", err)
		}

		select {
		case <-conn.authEvt:
			// auth completed
		case err = <-listenResult:
			if err == nil {
				err = io.ErrClosedPipe
			}
			return err
		case <-c.globalCtx.Done():
			return ErrStopped
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	select {
	case <-c.globalCtx.Done():
		return ErrStopped
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err = c.registerConnection(conn); err != nil {
		return err
	}
	registered = true

	return nil
}

func (c *ConnectionPool) registerConnection(conn *connection) error {
	c.nodesMx.Lock()
	defer c.nodesMx.Unlock()

	if conn.closed {
		if conn.closeErr != nil {
			return conn.closeErr
		}
		return io.ErrClosedPipe
	}

	conn.registered = true
	c.activeNodes = append(c.activeNodes, conn)

	return nil
}

func sendListenResult(ch chan<- error, err error) {
	select {
	case ch <- err:
	default:
	}
}

func (n *connection) listen(handshakeResult, listenResult chan<- error) {
	var initialized bool

	var err error
	// listen for incoming packets
listenLoop:
	for {
		var sz uint32
		sz, err = readSize(n.tcp, n.rCrypt)
		if err != nil {
			break listenLoop
		}

		// should at least have nonce (its 32 bytes) and something else
		if sz <= 32 {
			err = errors.New("too small size of packet")
			break listenLoop
		}

		var packet []byte
		packet, err = readData(n.tcp, n.rCrypt, sz)
		if err != nil {
			break listenLoop
		}
		data := packet

		checksum := data[len(data)-32:]
		data = data[:len(data)-32]

		if err = validatePacket(data, checksum); err != nil {
			releasePacketBuffer(packet)
			break listenLoop
		}

		// skip nonce
		data = data[32:]

		// response for handshake is empty packet, it means that connection established
		if len(data) == 0 {
			if !initialized {
				initialized = true
				sendListenResult(handshakeResult, nil)
			}
			releasePacketBuffer(packet)
			continue
		}

		var resp tl.Serializable
		_, err = tl.Parse(&resp, data, true)
		releasePacketBuffer(packet)
		if err != nil {
			log.Println("failed to parse message:", err.Error())
			break listenLoop
		}

		switch t := resp.(type) {
		case TCPPong:
			n.notePong(t.RandomID)
		case TCPAuthenticationNonce:
			if n.pool.authKey == nil {
				err = errors.New("server wants authorization, but we dont have a key")
				log.Println(err.Error())
				break listenLoop
			}

			err = n.authSignComplete(t.Nonce)
			if err != nil {
				log.Println("failed to sign and send auth message:", err.Error())
				break listenLoop
			}
		case adnl.MessageAnswer:
			n.pool.reqMx.RLock()
			ch := n.pool.activeReqs[string(t.ID)]
			n.pool.reqMx.RUnlock()

			if ch != nil {
				select {
				case ch.RespChan <- &ADNLResponse{
					Data: t.Data,
				}:
				default:
				}
			}
		default:
			log.Println("unknown ls msg:", reflect.TypeOf(t).String())
		}
	}

	// force close in case of error
	_ = n.tcp.Close()

	n.pool.nodesMx.Lock()
	n.closed = true
	n.closeErr = err
	registered := n.registered
	if registered {
		for i := range n.pool.activeNodes {
			if n.pool.activeNodes[i] == n {
				// remove from list
				n.pool.activeNodes = append(n.pool.activeNodes[:i], n.pool.activeNodes[i+1:]...)
				break
			}
		}
	}
	n.pool.nodesMx.Unlock()

	if !initialized {
		sendListenResult(handshakeResult, err)
	} else {
		sendListenResult(listenResult, err)
	}

	if initialized && registered {
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

func (c *ConnectionPool) startPings(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	var nodes []*connection
	for {
		select {
		case <-c.globalCtx.Done():
			return
		case <-ticker.C:
		}
		nodes = nodes[:0]

		c.nodesMx.RLock()
		nodes = append(nodes, c.activeNodes...)
		c.nodesMx.RUnlock()

		var wg sync.WaitGroup
		for _, node := range nodes {
			wg.Add(1)
			go func(n *connection) {
				defer wg.Done()
				n.expirePingsBefore(time.Now().Add(-2 * every))
				if atomic.LoadInt64(&n.missedPings) >= _nodeMaxMissedPings {
					_ = n.tcp.Close()
					return
				}

				num, err := rand.Int(rand.Reader, new(big.Int).SetInt64(0xFFFFFFFFFFFFFFF))
				if err != nil {
					return
				}

				qid := num.Int64()
				n.notePingSent(qid, time.Now())
				if err := n.ping(qid); err != nil {
					n.forgetPing(qid)
					// force close on error
					_ = n.tcp.Close()
				}
			}(node)
		}
		wg.Wait()
	}
}

func readSize(conn net.Conn, crypt cipher.Stream) (uint32, error) {
	var size [4]byte
	_, err := io.ReadFull(conn, size[:])
	if err != nil {
		return 0, err
	}

	// decrypt packet
	crypt.XORKeyStream(size[:], size[:])

	sz := binary.LittleEndian.Uint32(size[:])

	if sz > 16<<20 {
		return 0, fmt.Errorf("too big size of packet: %s", hex.EncodeToString(size[:]))
	}

	return sz, nil
}

func readData(conn net.Conn, crypt cipher.Stream, sz uint32) ([]byte, error) {
	if sz > 16<<20 {
		return nil, fmt.Errorf("too big packet")
	}

	result := acquirePacketBuffer(int(sz))

	// read exact number of bytes requested, blocking operation
	read := 0
	for read < len(result) {
		num, err := conn.Read(result[read:])
		if err != nil {
			releasePacketBuffer(result)
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

func buildPacket(data []byte) (packetBuffer, error) {
	raw := acquirePacketBuffer(4 + 64 + len(data))
	buf := raw[:4+32]
	binary.LittleEndian.PutUint32(buf, uint32(64+len(data)))

	// nonce
	if _, err := io.ReadFull(rand.Reader, buf[4:4+32]); err != nil {
		releasePacketBuffer(raw)
		return packetBuffer{}, err
	}
	buf = append(buf, data...)

	checksum := sha256.Sum256(buf[4:])

	buf = append(buf, checksum[:]...)
	return packetBuffer{data: buf, pooled: raw}, nil
}

func buildPacketSerialized(msg tl.Serializable) (packetBuffer, error) {
	raw := acquirePacketBuffer(4 + 32 + packetSerializeBufferSize + 32)
	buf := raw[:4+32]

	if _, err := io.ReadFull(rand.Reader, buf[4:4+32]); err != nil {
		releasePacketBuffer(raw)
		return packetBuffer{}, err
	}

	writer := bytes.NewBuffer(buf)
	if _, err := tl.Serialize(msg, true, writer); err != nil {
		releasePacketBuffer(raw)
		return packetBuffer{}, err
	}

	buf = writer.Bytes()
	pooled := raw
	if !sameBackingBuffer(buf, raw) {
		releasePacketBuffer(raw)
		pooled = nil
	}

	payloadLen := len(buf) - (4 + 32)
	binary.LittleEndian.PutUint32(buf, uint32(64+payloadLen))

	checksum := sha256.Sum256(buf[4:])
	buf = append(buf, checksum[:]...)
	if pooled != nil && !sameBackingBuffer(buf, pooled) {
		releasePacketBuffer(pooled)
		pooled = nil
	}

	return packetBuffer{data: buf, pooled: pooled}, nil
}

func (n *connection) send(data []byte) error {
	buf, err := buildPacket(data)
	if err != nil {
		return err
	}
	defer buf.release()

	n.wLock.Lock()
	defer n.wLock.Unlock()

	return writeEncrypt(n.tcp, n.wCrypt, buf.data)
}

func writeEncrypt(conn net.Conn, crypt cipher.Stream, buf []byte) error {
	// encrypt data
	crypt.XORKeyStream(buf, buf)

	return writeFull(conn, buf)
}

func writeFull(conn net.Conn, buf []byte) error {
	// write timeout in case of stuck socket, to reconnect
	_ = conn.SetWriteDeadline(time.Now().Add(7 * time.Second))
	// write all
	for len(buf) > 0 {
		num, err := conn.Write(buf)
		if err != nil {
			_ = conn.Close()
			return NetworkErr{err}
		}
		if num == 0 {
			_ = conn.Close()
			return NetworkErr{io.ErrShortWrite}
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

	kid, err := tl.Hash(keys.PublicKeyED25519{Key: serverKey})
	if err != nil {
		return err
	}

	key, err := keys.SharedKey(ourKey, serverKey)
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

	ctr, err := keys.NewCipherCtr(k, iv)
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
	if err = writeFull(n.tcp, res); err != nil {
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
	var cb OnDisconnectCallback
	cb = func(addr, key string) {
		tries := 0
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
	}

	return cb
}

func validatePacket(data []byte, recvChecksum []byte) error {
	if len(data) < 32 {
		return errors.New("too small packet")
	}

	checksum := sha256.Sum256(data)

	if !bytes.Equal(recvChecksum, checksum[:]) {
		return errors.New("checksum packet")
	}

	return nil
}
