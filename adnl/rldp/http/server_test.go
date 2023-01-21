package http

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"github.com/xssnick/tonutils-go/tl"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestServer_fetchPayload(t *testing.T) {
	_, tPrivKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal("failed to prepare test private key, err: ", err)
	}

	mx := http.NewServeMux()
	mx.HandleFunc("/hello", func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte("hop hey 777"))
	})

	tSrv := NewServer(tPrivKey, &MockDHT{}, mx)
	w := newDataStreamer()
	err = tSrv.fetchPayload(context.Background(), []byte("lol"), MockRDLP{
		close: nil,
		doQuery: func(ctx context.Context, maxAnswerSize int64, query, result tl.Serializable) error {
			switch q := query.(type) {
			case GetNextPayloadPart:
				if !bytes.Equal(q.ID, []byte("lol")) {
					t.Fatal("wrong id received in doQuery")
				}
				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(PayloadPart{
					Data:    []byte("test"),
					Trailer: nil,
					IsLast:  true,
				}))
			}
			return nil
		},
		setOnQuery:      nil,
		setOnDisconnect: nil,
		sendAnswer:      nil,
	}, w)
	if err != nil {
		t.Fatal("failed to execute ListenAndServe func")
	}
	time.Sleep(time.Second)
	if !bytes.Equal(<-w.parts, []byte("test")) {
		t.Error("invalid data in data stream parts")
	}

}

func TestServer_Stop(t *testing.T) {
	_, tPrivKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal("failed to prepare test private key, err: ", err)
	}

	mx := http.NewServeMux()
	mx.HandleFunc("/hello", func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte("hop hey 777"))
	})

	tSrv := NewServer(tPrivKey, &MockDHT{}, mx)

	err = tSrv.Stop()
	if err != nil {
		t.Fatal("failed to execute Stop func, err: ", err)
	}
	if tSrv.closed != true {
		t.Error("closed flag == ture after Stop method")
	}
	if _, ok := <-tSrv.closer; ok != false {
		t.Error("closer chan not closed after Stop method")

	}
}
