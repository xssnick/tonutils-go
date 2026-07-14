package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/quic"
	"github.com/xssnick/tonutils-go/tl"
)

const (
	defaultListenAddr  = "0.0.0.0:30000"
	defaultParallel    = 4
	defaultPayloadSize = 256 << 10
)

type SpeedRequest struct {
	Size uint64 `tl:"long"`
}

type SpeedResponse struct {
	Payload []byte `tl:"bytes"`
}

type SpeedMessage struct {
	Text   string `tl:"string"`
	SentAt int64  `tl:"long"`
}

func init() {
	tl.Register(SpeedRequest{}, "tonutils.quicTest.speedRequest size:long = tonutils.quicTest.SpeedRequest")
	tl.Register(SpeedResponse{}, "tonutils.quicTest.speedResponse payload:bytes = tonutils.quicTest.SpeedResponse")
	tl.Register(SpeedMessage{}, "tonutils.quicTest.speedMessage text:string sent_at:long = tonutils.quicTest.SpeedMessage")
}

func main() {
	listenAddr := flag.String("listen", defaultListenAddr, "listen address for server mode")
	addr := flag.String("addr", "", "remote QUIC address in client mode")
	peerKey := flag.String("peer", "", "remote Ed25519 public key, base64 or hex (client mode)")
	key := flag.String("key", "", "local Ed25519 private key or seed, base64 or hex")
	payload := flag.Uint64("payload", defaultPayloadSize, "payload size in bytes for each request (client mode)")
	parallel := flag.Int("parallel", defaultParallel, "number of parallel queries (client mode)")
	timeout := flag.Duration("timeout", 30*time.Second, "dial and per-request timeout")
	flag.Parse()

	priv, err := loadPrivateKey(*key)
	if err != nil {
		log.Fatalf("invalid local key: %v", err)
	}

	if *addr == "" && *peerKey == "" {
		runServer(*listenAddr, priv)
		return
	}
	if *addr == "" || *peerKey == "" {
		log.Fatalf("-addr and -peer must be set together in client mode")
	}

	remotePub, err := parsePublicKey(*peerKey)
	if err != nil {
		log.Fatalf("invalid peer key: %v", err)
	}

	runClient(*addr, remotePub, priv, *payload, *parallel, *timeout)
}

func runServer(listenAddr string, priv ed25519.PrivateKey) {
	log.Println("Starting QUIC speed test server")

	gateway, err := quic.NewGateway(priv)
	if err != nil {
		log.Fatalf("failed to create QUIC gateway: %v", err)
	}
	defer func() { _ = gateway.Close() }()

	var bytesSent atomic.Uint64
	gateway.SetConnectionHandler(func(peer *quic.Peer) error {
		log.Printf("Peer connected: %s", hex.EncodeToString(peer.PeerID()))

		peer.SetQueryHandler(func(ctx context.Context, payload []byte) ([]byte, error) {
			var req SpeedRequest
			rest, err := tl.ParseNoCopy(&req, payload, true)
			if err != nil {
				return nil, fmt.Errorf("failed to parse request: %w", err)
			}
			if len(rest) != 0 {
				return nil, fmt.Errorf("unexpected %d trailing request bytes", len(rest))
			}

			respSize := req.Size
			maxPayload := uint64(quic.DefaultMaxObjectSize - 4096)
			if respSize > maxPayload {
				respSize = maxPayload
			}
			if respSize > uint64(math.MaxInt) {
				return nil, fmt.Errorf("requested size is too large: %d", respSize)
			}

			resp, err := tl.Serialize(SpeedResponse{Payload: make([]byte, int(respSize))}, true)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize response: %w", err)
			}

			bytesSent.Add(respSize)
			return resp, nil
		})

		peer.SetMessageHandler(func(ctx context.Context, payload []byte) {
			var msg SpeedMessage
			rest, err := tl.ParseNoCopy(&msg, payload, true)
			if err != nil {
				log.Printf("failed to parse message from %s: %v", hex.EncodeToString(peer.PeerID()), err)
				return
			}
			if len(rest) != 0 {
				log.Printf("message from %s has %d trailing bytes", hex.EncodeToString(peer.PeerID()), len(rest))
				return
			}
			log.Printf("Message from %s: %q", hex.EncodeToString(peer.PeerID()), msg.Text)
		})

		peer.SetDisconnectHandler(func(peer *quic.Peer) {
			log.Printf("Peer disconnected: %s", hex.EncodeToString(peer.PeerID()))
		})
		return nil
	})

	pc, err := net.ListenPacket("udp", listenAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer pc.Close()

	pub := priv.Public().(ed25519.PublicKey)
	adnlID, err := publicKeyID(pub)
	if err != nil {
		log.Fatalf("failed to compute ADNL ID: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go reportServerSpeed(ctx, &bytesSent)
	go func() {
		<-ctx.Done()
		_ = gateway.Close()
	}()

	log.Printf("Listening on %s", listenAddr)
	log.Printf("Server public key: %s", base64.StdEncoding.EncodeToString(pub))
	log.Printf("Server ADNL ID: %s", hex.EncodeToString(adnlID))

	if err = gateway.Serve(pc); err != nil && ctx.Err() == nil {
		log.Fatalf("QUIC server failed: %v", err)
	}
	log.Println("Shutting down server")
}

func runClient(addr string, remotePub ed25519.PublicKey, priv ed25519.PrivateKey, payload uint64, parallel int, timeout time.Duration) {
	if parallel <= 0 {
		log.Fatalf("parallel must be positive")
	}
	if payload == 0 {
		log.Fatalf("payload must be positive")
	}

	remoteID, err := publicKeyID(remotePub)
	if err != nil {
		log.Fatalf("failed to compute peer ADNL ID: %v", err)
	}

	log.Printf("Starting QUIC speed test client")
	log.Printf("Remote ADNL ID: %s", hex.EncodeToString(remoteID))

	gateway, err := quic.NewGateway(priv)
	if err != nil {
		log.Fatalf("failed to create QUIC gateway: %v", err)
	}
	defer func() { _ = gateway.Close() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	dialCtx, cancelDial := context.WithTimeout(ctx, timeout)
	peer, err := gateway.DialDefault(dialCtx, remotePub, addr)
	cancelDial()
	if err != nil {
		log.Fatalf("failed to dial QUIC peer: %v", err)
	}

	msg, err := tl.Serialize(SpeedMessage{
		Text:   "hello from tonutils-go QUIC test",
		SentAt: time.Now().UnixNano(),
	}, true)
	if err != nil {
		log.Fatalf("failed to serialize message: %v", err)
	}
	if err = peer.SendMessage(ctx, msg); err != nil {
		log.Printf("failed to send startup message: %v", err)
	}

	if payload > uint64(quic.DefaultMaxObjectSize-4096) {
		log.Printf("Payload is above default QUIC object payload budget; server will cap responses near %d bytes", quic.DefaultMaxObjectSize-4096)
	}

	var totalReceived atomic.Uint64
	bytesCh := make(chan uint64, parallel*4)
	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker(ctx, peer, payload, timeout, bytesCh)
		}()
	}

	go func() {
		wg.Wait()
		close(bytesCh)
	}()

	go aggregateClientSpeed(ctx, bytesCh, &totalReceived)

	<-ctx.Done()
	log.Println("Client shutting down")
}

func worker(ctx context.Context, peer *quic.Peer, payload uint64, timeout time.Duration, bytesCh chan<- uint64) {
	req, err := tl.Serialize(SpeedRequest{Size: payload}, true)
	if err != nil {
		log.Printf("failed to serialize request: %v", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		respPayload, err := peer.Query(reqCtx, req, maxAnswerSize(payload))
		cancel()
		if err != nil {
			log.Printf("query failed: %v", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		var resp SpeedResponse
		rest, err := tl.ParseNoCopy(&resp, respPayload, true)
		if err != nil {
			log.Printf("failed to parse response: %v", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if len(rest) != 0 {
			log.Printf("response has %d trailing bytes", len(rest))
			time.Sleep(500 * time.Millisecond)
			continue
		}

		bytesCh <- uint64(len(resp.Payload))
	}
}

func maxAnswerSize(payload uint64) int64 {
	maxPayload := uint64(quic.DefaultMaxObjectSize - 4096)
	if payload > maxPayload {
		return quic.DefaultMaxObjectSize
	}
	return int64(payload + 4096)
}

func reportServerSpeed(ctx context.Context, total *atomic.Uint64) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var last uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := total.Load()
			diff := current - last
			last = current
			mbps := float64(diff*8) / 1e6
			mib := float64(diff) / (1024 * 1024)
			log.Printf("Server throughput: %.2f MiB/s (%.2f Mbit/s)", mib, mbps)
		}
	}
}

func aggregateClientSpeed(ctx context.Context, bytesCh <-chan uint64, total *atomic.Uint64) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var last uint64
	lastTime := time.Now()
	ctxDone := false

	for {
		select {
		case b, ok := <-bytesCh:
			if !ok {
				return
			}
			total.Add(b)
		case <-ticker.C:
			current := total.Load()
			diff := current - last
			now := time.Now()
			elapsed := now.Sub(lastTime).Seconds()
			if elapsed == 0 {
				elapsed = 1e-9
			}
			if !ctxDone {
				mib := float64(diff) / (1024 * 1024) / elapsed
				mbits := float64(diff*8) / 1e6 / elapsed
				log.Printf("Client throughput: %.2f MiB/s (%.2f Mbit/s)", mib, mbits)
			}
			last = current
			lastTime = now
		case <-ctx.Done():
			ctxDone = true
		}
	}
}

func loadPrivateKey(raw string) (ed25519.PrivateKey, error) {
	if raw == "" {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		return priv, err
	}

	key, err := decodeKey(raw)
	if err != nil {
		return nil, err
	}

	switch len(key) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(key), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(key), nil
	default:
		return nil, fmt.Errorf("expected %d-byte seed or %d-byte private key, got %d bytes",
			ed25519.SeedSize, ed25519.PrivateKeySize, len(key))
	}
}

func parsePublicKey(raw string) (ed25519.PublicKey, error) {
	key, err := decodeKey(raw)
	if err != nil {
		return nil, err
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("expected %d-byte public key, got %d bytes", ed25519.PublicKeySize, len(key))
	}
	return ed25519.PublicKey(key), nil
}

func decodeKey(raw string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return b, nil
	}
	if b, err := hex.DecodeString(raw); err == nil {
		return b, nil
	}
	return nil, errors.New("key must be base64 or hex")
}

func publicKeyID(pub ed25519.PublicKey) ([]byte, error) {
	return tl.Hash(keys.PublicKeyED25519{Key: pub})
}
