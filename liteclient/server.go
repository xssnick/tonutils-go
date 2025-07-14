package liteclient

import (
	"context"
	"crypto/cipher"
	"crypto/ed25519"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

var Logger = log.Println

type Server struct {
	keys     map[string]ed25519.PrivateKey
	listener net.Listener

	messageHandler func(ctx context.Context, client *ServerClient, msg tl.Serializable) error
	disconnectHook func(client *ServerClient)
	connectHook    func(client *ServerClient) error
}

type ServerClient struct {
	conn      net.Conn
	wCrypt    cipher.Stream
	rCrypt    cipher.Stream
	serverKey ed25519.PublicKey

	port uint16
	ip   string
	mx   sync.Mutex
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

func (s *Server) SetMessageHandler(handler func(ctx context.Context, client *ServerClient, msg tl.Serializable) error) {
	s.messageHandler = handler
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
	s.listener = listener

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

		sc := &ServerClient{
			conn: conn,
			ip:   ip[:ipSplit],
			port: uint16(port),
		}

		if s.connectHook != nil {
			if err = s.connectHook(sc); err != nil {
				_ = conn.Close()
				continue
			}
		}

		go s.serve(sc)
	}
}

func (s *Server) serve(client *ServerClient) {
	clientCtx, stopClient := context.WithCancel(context.Background())
	defer func() {
		stopClient()
		_ = client.conn.Close()
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
			ln, err := client.conn.Read(buffer)
			if err != nil {
				Logger("["+client.conn.RemoteAddr().String()+"]", "failed to read from client:", err.Error())
				return
			}
			packet := buffer[:ln]

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

			if err = writeEncrypt(client.conn, client.wCrypt, buf); err != nil {
				Logger("["+client.conn.RemoteAddr().String()+"]", "cannot write handshake response packet:", err.Error())
				return
			}

			// remove timeout
			_ = client.conn.SetReadDeadline(time.Time{})

			continue
		}

		sz, err := readSize(client.conn, client.rCrypt)
		if err != nil {
			return
		}

		data, err := readData(client.conn, client.rCrypt, sz)
		if err != nil {
			return
		}

		checksum := data[len(data)-32:]
		data = data[:len(data)-32]

		if err = validatePacket(data, checksum); err != nil {
			return
		}

		// skip nonce
		data = data[32:]

		var msg tl.Serializable
		if _, err = tl.Parse(&msg, data, true); err != nil {
			Logger("failed to parse incoming message:", err.Error())
			return
		}

		if s.messageHandler == nil {
			Logger("failed to handle message: no handler set")
			return
		}

		if err = s.messageHandler(clientCtx, client, msg); err != nil {
			Logger("failed to handle message:", err.Error())
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

func (s *ServerClient) Send(msg tl.Serializable) error {
	data, err := tl.Serialize(msg, true)
	if err != nil {
		return err
	}

	buf, err := buildPacket(data)
	if err != nil {
		return err
	}

	s.mx.Lock()
	defer s.mx.Unlock()

	return writeEncrypt(s.conn, s.wCrypt, buf)
}

func (s *ServerClient) Close() {
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
