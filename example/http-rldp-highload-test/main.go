package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl/address"
	rldphttp "github.com/xssnick/tonutils-go/adnl/rldp/http"
	"io"
	"net"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"
)

func main() {
	srvPub, srvKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}

	mx := http.NewServeMux()
	mx.HandleFunc("/hello", func(writer http.ResponseWriter, request *http.Request) {
		//		writer.Header().Set("Transfer-Encoding", "chunked")
		_, _ = writer.Write([]byte("abc"))
		_, _ = writer.Write([]byte("2223"))
	})

	dht := &MockDHT{
		ip:   "127.0.0.1",
		port: 9056,
		pub:  srvPub,
	}

	s := rldphttp.NewServer(srvKey, dht, mx)

	go func() {
		if err = s.ListenAndServe("127.0.0.1:9056"); err != nil {
			panic(err)
		}
	}()

	var x int64
	numClients := 100

	// 100 clients, parallel requests
	for i := 0; i < numClients; i++ {
		go func() {
			// it uses fake DHT, but real adnl and rldp through network
			client := &http.Client{
				Transport: rldphttp.NewTransport(dht, nil),
				//Timeout:   300 * time.Millisecond,
			}

			for {
				resp, err := client.Get("http://ucvasyg57dnvgez2pct5v4z7ul5n3a6w7noenmxoh74uuo4djj5dfzv.adnl/hello")
				if err != nil {
					continue
				}

				dat, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					println("err body", err.Error())
					continue
				}
				_ = dat

				atomic.AddInt64(&x, 1)
			}
		}()
	}

	xOld := int64(0)

	// stats
	for {
		time.Sleep(100 * time.Millisecond)
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		// those stats include client's resources usage too, not so accurate
		fmt.Println("RPS:", (x-xOld)*10, "| Requests num:", x, "| Goroutines:", runtime.NumGoroutine()-(numClients*2), "| RAM Usage:", m.Alloc/1024/1024, "MB")

		xOld = x
	}
}

type MockDHT struct {
	ip   string
	port int
	pub  ed25519.PublicKey
}

func (m *MockDHT) StoreAddress(ctx context.Context, addresses address.List, ttl time.Duration, ownerKey ed25519.PrivateKey, copies int) ([]byte, error) {
	return nil, nil
}

func (m *MockDHT) Close() {}

func (m *MockDHT) FindAddresses(ctx context.Context, key []byte) (*address.List, ed25519.PublicKey, error) {
	return &address.List{
		Addresses: []*address.UDP{
			{
				IP:   net.ParseIP(m.ip),
				Port: int32(m.port),
			},
		},
		Version:    0,
		ReinitDate: 0,
		Priority:   0,
		ExpireAt:   int32(time.Now().Add(1 * time.Hour).Unix()),
	}, m.pub, nil
}
