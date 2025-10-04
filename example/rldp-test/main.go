package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
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

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

const (
	defaultConfigURL   = "https://ton-blockchain.github.io/global.config.json"
	defaultListenAddr  = "0.0.0.0:30000"
	defaultParallel    = 4
	defaultPayloadSize = 256 << 10 // 1 MiB
	dhtTTL             = 10 * time.Minute
)

type speedRequest struct {
	Size uint64 `tl:"long"`
}

type speedResponse struct {
	Payload []byte `tl:"bytes"`
}

func init() {
	tl.Register(speedRequest{}, "rldp.speedTestRequest size:long = rldp.speedTestRequest")
	tl.Register(speedResponse{}, "rldp.speedTestResponse payload:bytes = rldp.speedTestResponse")
}

func main() {
	configURL := flag.String("config", defaultConfigURL, "URL to global config for DHT")
	adnlIDHex := flag.String("adnl", "", "remote ADNL ID (hex), if empty runs in server mode")
	listenAddr := flag.String("listen", defaultListenAddr, "listen address for server mode")
	publicIP := flag.String("public", "", "public IPv4 address to publish in DHT (server mode)")
	payload := flag.Uint64("payload", defaultPayloadSize, "payload size in bytes for each request (client mode)")
	parallel := flag.Int("parallel", defaultParallel, "number of parallel queries (client mode)")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	flag.Parse()

	if *adnlIDHex == "" {
		runServer(*listenAddr, *publicIP, *configURL)
		return
	}

	runClient(*adnlIDHex, *payload, *parallel, *timeout, *configURL)
}

func runServer(listenAddr, publicIP, configURL string) {
	log.Println("Starting RLDP speed test server")
	sd := sha256.Sum256([]byte(listenAddr + publicIP))

	priv := ed25519.NewKeyFromSeed(sd[:])
	var err error

	gateway := adnl.NewGateway(priv)
	defer func() { _ = gateway.Close() }()
	if err = gateway.StartServer(listenAddr); err != nil {
		log.Fatalf("failed to start ADNL server: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var bytesSent atomic.Uint64

	gateway.SetConnectionHandler(func(peer adnl.Peer) error {
		conn := rldp.NewClientV2(peer)
		conn.SetOnQuery(func(transferID []byte, query *rldp.Query) error {
			req, ok := query.Data.(speedRequest)
			if !ok {
				return fmt.Errorf("unexpected query type %T", query.Data)
			}

			respSize := req.Size
			if respSize > query.MaxAnswerSize {
				respSize = query.MaxAnswerSize
			}

			if respSize > uint64(math.MaxInt) {
				return fmt.Errorf("requested size is too large: %d", respSize)
			}

			payload := make([]byte, int(respSize))

			sendCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()

			err := conn.SendAnswer(sendCtx, query.MaxAnswerSize, query.Timeout, query.ID, transferID, speedResponse{Payload: payload})
			if err != nil {
				return fmt.Errorf("failed to send answer: %w", err)
			}

			bytesSent.Add(respSize)
			return nil
		})
		return nil
	})

	go reportServerSpeed(ctx, &bytesSent)

	adnlID, err := tl.Hash(keys.PublicKeyED25519{Key: priv.Public().(ed25519.PublicKey)})
	if err != nil {
		log.Fatalf("failed to compute ADNL ID: %v", err)
	}
	log.Printf("Server ADNL ID: %s", hex.EncodeToString(adnlID))

	if err := publishAddress(ctx, gateway, priv, listenAddr, publicIP, configURL); err != nil {
		log.Printf("failed to publish address in DHT: %v", err)
	}

	<-ctx.Done()
	log.Println("Shutting down server")
}

func publishAddress(ctx context.Context, gateway *adnl.Gateway, priv ed25519.PrivateKey, listenAddr, publicIP, configURL string) error {
	host, portStr, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}

	if publicIP == "" {
		if host != "" && host != "0.0.0.0" && host != "::" {
			publicIP = host
		} else {
			return errors.New("public IP is required to publish address in DHT")
		}
	}

	ip := net.ParseIP(publicIP)
	if ip == nil {
		return fmt.Errorf("invalid public IP: %s", publicIP)
	}
	if ip.To4() == nil {
		return fmt.Errorf("only IPv4 is supported for publication: %s", publicIP)
	}

	port, err := net.LookupPort("udp", portStr)
	if err != nil {
		return fmt.Errorf("invalid listen port: %w", err)
	}

	gateway.SetAddressList([]*address.UDP{{
		IP:   ip,
		Port: int32(port),
	}})

	log.Println("Starting DHT client for publication")
	dhtClient, err := dht.NewClientFromConfigUrl(ctx, gateway, configURL)
	if err != nil {
		return fmt.Errorf("failed to create DHT client: %w", err)
	}

	go func() {
		defer dhtClient.Close()
		ticker := time.NewTicker(dhtTTL / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			_, _, err := dhtClient.StoreAddress(ctx, gateway.GetAddressList(), dhtTTL, priv, 5)
			if err != nil {
				log.Printf("DHT publish failed: %v", err)
			} else {
				log.Println("Address published to DHT")
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	return nil
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

func runClient(adnlIDHex string, payload uint64, parallel int, timeout time.Duration, configURL string) {
	if parallel <= 0 {
		log.Fatalf("parallel must be positive")
	}
	if payload == 0 {
		log.Fatalf("payload must be positive")
	}

	adnlID, err := hex.DecodeString(adnlIDHex)
	if err != nil || len(adnlID) != 32 {
		log.Fatalf("invalid ADNL ID: %v", err)
	}

	log.Println("Starting RLDP speed test client")

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Fatalf("failed to generate key: %v", err)
	}

	gateway := adnl.NewGateway(priv)
	defer func() { _ = gateway.Close() }()
	if err = gateway.StartClient(); err != nil {
		log.Fatalf("failed to start ADNL client: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	dhtClient, err := dht.NewClientFromConfigUrl(ctx, gateway, configURL)
	if err != nil {
		log.Fatalf("failed to create DHT client: %v", err)
	}
	defer dhtClient.Close()

	addrList, pubKey, err := dhtClient.FindAddresses(ctx, adnlID)
	if err != nil {
		log.Fatalf("failed to resolve address: %v", err)
	}

	if len(addrList.Addresses) == 0 {
		log.Fatalf("no addresses found for %s", adnlIDHex)
	}

	var peer adnl.Peer
	for _, addr := range addrList.Addresses {
		remote := addr.IP.String() + ":" + fmt.Sprint(addr.Port)
		log.Printf("Trying address %s", remote)
		peer, err = gateway.RegisterClient(remote, pubKey)
		if err != nil {
			log.Printf("failed to register peer %s: %v", remote, err)
			continue
		}
		break
	}

	if peer == nil {
		log.Fatalf("failed to connect to any resolved address")
	}

	defer peer.Close()

	conn := rldp.NewClientV2(peer)

	var totalReceived atomic.Uint64
	bytesCh := make(chan uint64, parallel*4)
	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker(ctx, conn, payload, timeout, bytesCh)
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

func worker(ctx context.Context, conn *rldp.RLDP, payload uint64, timeout time.Duration, bytesCh chan<- uint64) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var resp speedResponse
		req := speedRequest{Size: payload}

		maxAnswer := payload + 4096
		if maxAnswer < payload {
			maxAnswer = ^uint64(0)
		}

		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		err := conn.DoQuery(reqCtx, maxAnswer, req, &resp)
		cancel()
		if err != nil {
			log.Printf("query failed: %v", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		bytesCh <- uint64(len(resp.Payload))
	}
}

func aggregateClientSpeed(ctx context.Context, bytesCh <-chan uint64, total *atomic.Uint64) {
	ticker := time.NewTicker(1000 * time.Millisecond)
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
