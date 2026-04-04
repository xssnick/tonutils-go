package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os/signal"
	"syscall"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
)

const (
	defaultConfigURL  = "https://ton-blockchain.github.io/global.config.json"
	defaultListenAddr = "0.0.0.0:30000"
	publishTTL        = 10 * time.Minute
)

func main() {
	configURL := flag.String("config", defaultConfigURL, "URL to global config for DHT bootstrap")
	listenAddr := flag.String("listen", defaultListenAddr, "UDP listen address")
	publicIP := flag.String("public", "", "public IP address to publish in DHT")
	seedHex := flag.String("seed", "", "32-byte hex seed for persistent server identity")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	key, err := loadOrGenerateKey(*seedHex)
	if err != nil {
		log.Fatalf("failed to prepare key: %v", err)
	}

	gateway := adnl.NewGateway(key)
	defer func() { _ = gateway.Close() }()

	if err = gateway.StartServer(*listenAddr); err != nil {
		log.Fatalf("failed to start ADNL gateway: %v", err)
	}

	publishedAddr, err := resolvePublishedAddress(*listenAddr, *publicIP)
	if err != nil {
		log.Fatalf("failed to determine published address: %v", err)
	}
	gateway.SetAddressList([]address.Address{publishedAddr})

	cfg, err := liteclient.GetConfigFromUrl(ctx, *configURL)
	if err != nil {
		log.Fatalf("failed to fetch global config: %v", err)
	}

	store := &loggingStore{
		inner: dht.NewMemoryValueStore(300000),
	}

	server, err := dht.NewServerFromConfig(gateway, key, cfg, store)
	if err != nil {
		log.Fatalf("failed to start DHT server: %v", err)
	}
	defer func() { _ = server.Close() }()

	pub := key.Public().(ed25519.PublicKey)
	adnlID, err := tl.Hash(keys.PublicKeyED25519{Key: pub})
	if err != nil {
		log.Fatalf("failed to calc ADNL ID: %v", err)
	}

	log.Printf("DHT server started")
	log.Printf("ADNL ID: %s", hex.EncodeToString(adnlID))
	publishedDial, err := address.DialString(publishedAddr)
	if err != nil {
		log.Fatalf("failed to format published address: %v", err)
	}
	log.Printf("Published address: %s", publishedDial)

	if err = publishLoop(ctx, server, gateway.GetAddressList(), key); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("publisher stopped with error: %v", err)
	}
}

type loggingStore struct {
	inner dht.ValueStore
}

func (l *loggingStore) Get(keyID []byte) (*dht.Value, error) {
	log.Printf("[store] Get key=%s", hex.EncodeToString(keyID))
	value, err := l.inner.Get(keyID)
	if err != nil {
		log.Printf("[store] Get key=%s err=%v", hex.EncodeToString(keyID), err)
		return nil, err
	}
	log.Printf("[store] Get key=%s value=%s", hex.EncodeToString(keyID), describeValue(value))
	return value, nil
}

func (l *loggingStore) Put(keyID []byte, value *dht.Value) error {
	log.Printf("[store] Put key=%s value=%s", hex.EncodeToString(keyID), describeValue(value))
	err := l.inner.Put(keyID, value)
	if err != nil {
		log.Printf("[store] Put key=%s err=%v", hex.EncodeToString(keyID), err)
	}
	return err
}

func (l *loggingStore) Delete(keyID []byte) error {
	log.Printf("[store] Delete key=%s", hex.EncodeToString(keyID))
	err := l.inner.Delete(keyID)
	if err != nil {
		log.Printf("[store] Delete key=%s err=%v", hex.EncodeToString(keyID), err)
	}
	return err
}

func (l *loggingStore) ForEach(fn func(keyID []byte, value *dht.Value) error) error {
	return l.inner.ForEach(func(keyID []byte, value *dht.Value) error {
		return fn(keyID, value)
	})
}

func (l *loggingStore) Close() error {
	return l.inner.Close()
}

func describeValue(value *dht.Value) string {
	if value == nil {
		return "<nil>"
	}

	dataPrefix := value.Data
	if len(dataPrefix) > 24 {
		dataPrefix = dataPrefix[:24]
	}

	return fmt.Sprintf(
		"name=%q idx=%d ttl=%d rule=%T data_len=%d data_prefix=%s",
		string(value.KeyDescription.Key.Name),
		value.KeyDescription.Key.Index,
		value.TTL,
		value.KeyDescription.UpdateRule,
		len(value.Data),
		hex.EncodeToString(dataPrefix),
	)
}

func loadOrGenerateKey(seedHex string) (ed25519.PrivateKey, error) {
	if seedHex == "" {
		_, key, err := ed25519.GenerateKey(nil)
		if err == nil {
			log.Printf("generated ephemeral key, pass -seed for persistent identity")
		}
		return key, err
	}

	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode seed: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("seed must be %d bytes", ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func resolvePublishedAddress(listenAddr, publicIP string) (address.Address, error) {
	host, portStr, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid listen address: %w", err)
	}

	if publicIP == "" {
		if host == "" || host == "0.0.0.0" || host == "::" {
			return nil, errors.New("public IP is required when listening on wildcard address")
		}
		publicIP = host
	}

	ip := net.ParseIP(publicIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid public IP: %s", publicIP)
	}

	port, err := net.LookupPort("udp", portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid listen port: %w", err)
	}

	return address.NewAddress(ip, int32(port))
}

func publishLoop(ctx context.Context, server *dht.Server, addrList address.List, key ed25519.PrivateKey) error {
	ticker := time.NewTicker(publishTTL / 2)
	defer ticker.Stop()

	for {
		storeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		_, _, err := server.StoreAddress(storeCtx, addrList, publishTTL, key)
		cancel()
		if err != nil {
			log.Printf("DHT address publish failed: %v", err)
		} else {
			log.Printf("address published to DHT")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
