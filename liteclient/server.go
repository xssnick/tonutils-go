package liteclient

import (
	"context"
	"crypto/cipher"
	"crypto/ed25519"
	"fmt"
	"io"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

var Logger = func(v ...any) {}

const (
	serverQueryQueuePerWorker = 10
	serverWriteDrainMax       = 16
)

// ServerClientSendQueueSize is a per-client write queue size for new server connections.
// Set it before starting the server to override the default.
var ServerClientSendQueueSize = 100

// ServerQueryWorkers is the number of goroutines used by server to handle queued queries.
// Set it before starting the server to override the default.
var ServerQueryWorkers = runtime.GOMAXPROCS(0) * 4

type Server struct {
	keys     map[string]ed25519.PrivateKey
	listener net.Listener

	queryHandler   func(ctx context.Context, client *ServerClient, query tl.Serializable) (tl.Serializable, error)
	disconnectHook func(client *ServerClient)
	connectHook    func(client *ServerClient) error

	queryWorkers int
	queryQueue   chan serverQueryTask
	workersOnce  sync.Once
}

type ServerClient struct {
	conn      net.Conn
	wCrypt    cipher.Stream
	rCrypt    cipher.Stream
	serverKey ed25519.PublicKey

	sendQueue chan packetBuffer
	ctx       context.Context
	cancel    context.CancelFunc

	port uint16
	ip   string
}

type serverQueryTask struct {
	ctx     context.Context
	client  *ServerClient
	queryID []byte
	query   tl.Serializable
}

func NewServer(keysList []ed25519.PrivateKey) *Server {
	list := map[string]ed25519.PrivateKey{}
	for _, k := range keysList {
		kid, err := tl.Hash(keys.PublicKeyED25519{Key: k.Public().(ed25519.PublicKey)})
		if err != nil {
			panic(err.Error())
		}

		list[string(kid)] = k
	}

	return &Server{
		keys: list,
	}
}

func (s *Server) SetQueryHandler(handler func(ctx context.Context, client *ServerClient, query tl.Serializable) (tl.Serializable, error)) {
	s.queryHandler = handler
}

func (s *Server) SetDisconnectHook(hook func(client *ServerClient)) {
	s.disconnectHook = hook
}

func (s *Server) SetConnectionHook(hook func(client *ServerClient) error) {
	s.connectHook = hook
}

func (s *Server) Close() error {
	if s.listener != nil {
		lis := s.listener
		s.listener = nil
		return lis.Close()
	}
	return nil
}

func (s *Server) Listen(addr string) error {
	if s.listener != nil {
		return fmt.Errorf("already started")
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	return s.listen(listener)
}

func (s *Server) listen(listener net.Listener) error {
	s.listener = listener

	s.startQueryWorkers()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if listener != s.listener {
				return nil
			}

			Logger("failed to accept connection:", err.Error())
			continue
		}

		var port uint64
		ip := conn.RemoteAddr().String()
		ipSplit := strings.LastIndex(conn.RemoteAddr().String(), ":")
		if ipSplit < 0 {
			ipSplit = len(ip)
		} else {
			port, _ = strconv.ParseUint(ip[ipSplit+1:], 10, 16)
		}

		clientCtx, cancelClient := context.WithCancel(context.Background())
		sc := &ServerClient{
			conn:      conn,
			sendQueue: make(chan packetBuffer, ServerClientSendQueueSize),
			ctx:       clientCtx,
			cancel:    cancelClient,
			ip:        ip[:ipSplit],
			port:      uint16(port),
		}

		if s.connectHook != nil {
			if err = s.connectHook(sc); err != nil {
				sc.Close()
				continue
			}
		}

		go s.serve(sc)
	}
}

func (s *Server) startQueryWorkers() {
	s.workersOnce.Do(func() {
		workers := s.queryWorkers
		if workers <= 0 {
			workers = ServerQueryWorkers
		}
		if workers <= 0 {
			workers = 1
		}
		if s.queryQueue == nil {
			s.queryQueue = make(chan serverQueryTask, workers*serverQueryQueuePerWorker)
		}

		for i := 0; i < workers; i++ {
			go s.queryWorker()
		}
	})
}

func (s *Server) queryWorker() {
	for task := range s.queryQueue {
		s.handleQueryTask(task)
	}
}

func (s *Server) handleQueryTask(task serverQueryTask) {
	select {
	case <-task.ctx.Done():
		return
	default:
	}

	resp, err := s.queryHandler(task.ctx, task.client, task.query)
	if err != nil {
		Logger("failed to handle query:", err.Error())
		return
	}
	if resp == nil {
		return
	}

	select {
	case <-task.ctx.Done():
	default:
		task.client.enqueue(adnl.MessageAnswer{ID: task.queryID, Data: resp})
	}
}

func (s *Server) serve(client *ServerClient) {
	defer func() {
		client.Close()
		if s.disconnectHook != nil {
			s.disconnectHook(client)
		}
		Logger("["+client.conn.RemoteAddr().String()+"]", "connection was closed with a client")
	}()

	for {
		if client.wCrypt == nil {
			var buffer = make([]byte, 256)

			// 10 sec timeout for handshake
			_ = client.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			_, err := io.ReadFull(client.conn, buffer)
			if err != nil {
				Logger("["+client.conn.RemoteAddr().String()+"]", "failed to read from client:", err.Error())
				return
			}
			packet := buffer

			client.serverKey, client.wCrypt, client.rCrypt, err = s.processHandshake(packet)
			if err != nil {
				Logger("["+client.conn.RemoteAddr().String()+"]", "invalid handshake packet:", err.Error())
				return
			}
			Logger("["+client.conn.RemoteAddr().String()+"]", "handshake done")

			buf, err := buildPacket(nil)
			if err != nil {
				Logger("["+client.conn.RemoteAddr().String()+"]", "cannot build handshake response packet:", err.Error())
				return
			}

			if err = writeEncrypt(client.conn, client.wCrypt, buf.data); err != nil {
				buf.release()
				Logger("["+client.conn.RemoteAddr().String()+"]", "cannot write handshake response packet:", err.Error())
				return
			}
			buf.release()

			// remove timeout
			_ = client.conn.SetReadDeadline(time.Time{})

			go client.writeLoop(client.ctx)

			continue
		}

		sz, err := readSize(client.conn, client.rCrypt)
		if err != nil {
			return
		}
		if sz <= 32 {
			return
		}

		packet, err := readData(client.conn, client.rCrypt, sz)
		if err != nil {
			return
		}
		data := packet

		checksum := data[len(data)-32:]
		data = data[:len(data)-32]

		if err = validatePacket(data, checksum); err != nil {
			releasePacketBuffer(packet)
			return
		}

		// skip nonce
		data = data[32:]

		var msg tl.Serializable
		if _, err = tl.Parse(&msg, data, true); err != nil {
			releasePacketBuffer(packet)
			Logger("failed to parse incoming message:", err.Error())
			return
		}
		releasePacketBuffer(packet)

		switch m := msg.(type) {
		case adnl.MessageQuery:
			if s.queryHandler == nil {
				Logger("failed to handle query: no handler set")
				return
			}

			query := m.Data
			if q, ok := m.Data.(LiteServerQuery); ok {
				query = q.Data
			}

			select {
			case s.queryQueue <- serverQueryTask{ctx: client.ctx, client: client, queryID: m.ID, query: query}:
			case <-client.ctx.Done():
				return
			}
		case TCPAuthenticate:
			client.enqueue(TCPAuthenticationNonce{Nonce: make([]byte, 32)})
		case TCPAuthenticationComplete:
		case TCPPing:
			client.enqueue(TCPPong{RandomID: m.RandomID})
		default:
			Logger("failed to handle message: unsupported type")
			return
		}
	}
}

func (s *Server) processHandshake(packet []byte) (ed25519.PublicKey, cipher.Stream, cipher.Stream, error) {
	if len(packet) != 256 {
		return nil, nil, nil, fmt.Errorf("invalid packet len: %d", len(packet))
	}

	serverKey := s.keys[string(packet[:32])]
	if serverKey == nil {
		return nil, nil, nil, fmt.Errorf("incorrect server key in packet")
	}

	key, err := keys.SharedKey(serverKey, packet[32:64])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to calc shared key: %w", err)
	}

	checksum := packet[64:96]

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
		return nil, nil, nil, fmt.Errorf("failed to calc cipher for rnd: %w", err)
	}

	rnd := packet[96:]
	// decrypt data
	ctr.XORKeyStream(rnd, rnd)

	// build ciphers for incoming packets and for outgoing
	w, err := keys.NewCipherCtr(rnd[:32], rnd[64:80])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to calc cipher for w crypt: %w", err)
	}
	r, err := keys.NewCipherCtr(rnd[32:64], rnd[80:96])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to calc cipher for r crypt: %w", err)
	}

	return serverKey.Public().(ed25519.PublicKey), w, r, nil
}

func (s *ServerClient) enqueue(msg tl.Serializable) bool {
	if len(s.sendQueue) >= cap(s.sendQueue) {
		return false
	}

	packet, err := buildPacketSerialized(msg)
	if err != nil {
		Logger("failed to build response packet:", err.Error())
		return false
	}

	select {
	case s.sendQueue <- packet:
		return true
	default:
		packet.release()
		return false
	}
}

func (s *ServerClient) writeLoop(ctx context.Context) {
	defer s.releaseQueuedPackets()

	for {
		select {
		case <-ctx.Done():
			return
		case packet := <-s.sendQueue:
			var batch [serverWriteDrainMax]packetBuffer
			batch[0] = packet
			batchSize := 1

		drain:
			for batchSize < serverWriteDrainMax {
				select {
				case batch[batchSize] = <-s.sendQueue:
					batchSize++
				default:
					break drain
				}
			}

			if err := s.writeBatch(batch[:batchSize]); err != nil {
				Logger("failed to write message:", err.Error())
				s.Close()
				return
			}
		}
	}
}

func (s *ServerClient) writeBatch(packets []packetBuffer) error {
	defer releasePacketBatch(packets)

	var buffersRaw [serverWriteDrainMax][]byte
	var total int64
	for i, packet := range packets {
		s.wCrypt.XORKeyStream(packet.data, packet.data)
		buffersRaw[i] = packet.data
		total += int64(len(packet.data))
	}

	_ = s.conn.SetWriteDeadline(time.Now().Add(7 * time.Second))

	buffers := net.Buffers(buffersRaw[:len(packets)])
	written, err := buffers.WriteTo(s.conn)
	if err != nil {
		_ = s.conn.Close()
		return NetworkErr{err}
	}
	if written != total {
		_ = s.conn.Close()
		return NetworkErr{io.ErrShortWrite}
	}

	return nil
}

func releasePacketBatch(packets []packetBuffer) {
	for _, packet := range packets {
		packet.release()
	}
}

func (s *ServerClient) releaseQueuedPackets() {
	for {
		select {
		case packet := <-s.sendQueue:
			packet.release()
		default:
			return
		}
	}
}

func (s *ServerClient) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	_ = s.conn.Close()
}

func (s *ServerClient) IP() string {
	return s.ip
}

func (s *ServerClient) Port() uint16 {
	return s.port
}

func (s *ServerClient) ServerKey() ed25519.PublicKey {
	return s.serverKey
}
