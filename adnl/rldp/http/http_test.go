package http

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"github.com/xssnick/tonutils-go/adnl"
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
		ExpireAT:   int32(time.Now().Add(1 * time.Hour).Unix()),
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

	s := adnl.NewServer(srvKey)
	HandleRequests(s, mx)

	go func() {
		if err = s.ListenAndServe("127.0.0.1:9056"); err != nil {
			t.Fatal(err)
		}
	}()

	client := &http.Client{
		Transport: NewTransport(&MockDHT{
			ip:   "127.0.0.1",
			port: 9056,
			pub:  srvPub,
		}, &MockResolver{}),
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
