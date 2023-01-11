package http

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/ton/dns"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

type MockResolver struct {
}

func (m MockResolver) Resolve(ctx context.Context, domain string) (*dns.Domain, error) {
	records := cell.NewDict(256)
	h := sha256.New()
	h.Write([]byte("site"))

	_ = records.Set(cell.BeginCell().MustStoreSlice(h.Sum(nil), 256).EndCell(),
		cell.BeginCell().MustStoreRef(cell.BeginCell().
			MustStoreUInt(0xad01, 16).
			MustStoreUInt(0, 256).
			EndCell()).EndCell())

	return &dns.Domain{
		Records: records,
	}, nil
}

type MockDHT struct {
	ip   string
	port int
	pub  ed25519.PublicKey
}

func (m *MockDHT) StoreAddress(ctx context.Context, addresses address.List, ttl time.Duration, ownerKey ed25519.PrivateKey, copies int) (int, []byte, error) {
	return copies, nil, nil
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

func Test_ClientServer(t *testing.T) {
	srvPub, srvKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	exp := "hop hey 777"
	mx := http.NewServeMux()
	mx.HandleFunc("/hello", func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte(exp))
	})

	dht := &MockDHT{
		ip:   "127.0.0.1",
		port: 9076,
		pub:  srvPub,
	}

	s := NewServer(srvKey, dht, mx)

	go func() {
		if err = s.ListenAndServe("127.0.0.1:9076"); err != nil {
			t.Fatal(err)
		}
	}()
	time.Sleep(1 * time.Second)

	client := &http.Client{
		Transport: NewTransport(dht, &MockResolver{}),
	}

	t.Run("check handler", func(t *testing.T) {
		resp, err := client.Get("http://utils.ton/hello")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}

		if string(data) != exp {
			t.Fatal("incorrect response")
		}
	})

	t.Run("check no handler", func(t *testing.T) {
		resp, err := client.Get("http://utils.ton/aaaa")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 404 {
			t.Fatal("incorrect response:", resp.StatusCode)
		}
	})
}

func BenchmarkServer(b *testing.B) {
	srvPub, srvKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}

	mx := http.NewServeMux()
	mx.HandleFunc("/hello", func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte("hop hey 777"))
	})

	dht := &MockDHT{
		ip:   "127.0.0.1",
		port: 9076,
		pub:  srvPub,
	}

	s := NewServer(srvKey, dht, mx)

	go func() {
		if err = s.ListenAndServe("127.0.0.1:9076"); err != nil {
			b.Fatal(err)
		}
	}()
	time.Sleep(1 * time.Second)

	for i := 0; i < b.N; i++ {
		client := &http.Client{
			Transport: NewTransport(dht, &MockResolver{}),
		}

		resp, err := client.Get("http://utils.ton/hello")
		if err != nil {
			b.Fatal(err)
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			b.Fatal(err)
		}

		println(string(data))
	}
}
