package rldp

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
	"log"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func init() {
	tl.Register(testRequest{}, "http.request id:int256 method:string url:string http_version:string headers:(vector http.header) = http.Response")
	tl.Register(testResponse{}, "http.response http_version:string status_code:int reason:string headers:(vector http.header) no_payload:Bool = http.Response")
	tl.Register(testHeader{}, "")
}

type MockADNL struct {
	setCustomMessageHandler func(msg *adnl.MessageCustom) error
	setDisconnectHandler    func(addr string, key ed25519.PublicKey)
	sendCustomMessage       func(ctx context.Context, req tl.Serializable) error
	close                   func()
}

func (m MockADNL) GetCloserCtx() context.Context {
	return context.Background()
}

func (m MockADNL) GetID() []byte {
	v := sha256.Sum256([]byte("1.1.1.1:1234"))
	return v[:]
}

func (m MockADNL) RemoteAddr() string {
	return "1.1.1.1:1234"
}

func (m MockADNL) GetDisconnectHandler() func(addr string, key ed25519.PublicKey) {
	return nil
}

func (m MockADNL) SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error) {
}

func (m MockADNL) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
}

func (m MockADNL) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return m.sendCustomMessage(ctx, req)
}

func (m MockADNL) Close() {
}

type testRequest struct {
	ID      []byte       `tl:"int256"`
	Method  string       `tl:"string"`
	URL     string       `tl:"string"`
	Version string       `tl:"string"`
	Headers []testHeader `tl:"vector struct"`
	RespSz  uint64       `tl:"long"`
}

type testResponse struct {
	Version    string       `tl:"string"`
	StatusCode int32        `tl:"int"`
	Reason     string       `tl:"string"`
	Headers    []testHeader `tl:"vector struct"`
	NoPayload  bool         `tl:"bool"`
}

type testHeader struct {
	Name  string `tl:"string"`
	Value string `tl:"string"`
}

func TestRLDP_handleMessage(t *testing.T) {
	tId := make([]byte, 32)
	_, err := rand.Read(tId)
	if err != nil {
		t.Fatal(err)
	}

	tUrl, err := url.Parse("http://foundation.ton/")
	if err != nil {
		t.Fatal("failed to prepare test URL, err: ", err)
	}

	httpReq := http.Request{
		Method: http.MethodGet,
		URL:    tUrl,
		Host:   "foundation.ton",
	}

	req := testRequest{
		ID:      tId,
		Method:  httpReq.Method,
		URL:     httpReq.URL.String(),
		Version: "HTTP/1.1",
		Headers: []testHeader{
			{
				Name:  "Host",
				Value: httpReq.Host,
			},
		},
	}

	_ChunkSize := 1 << 17
	_RLDPMaxAnswerSize := 2*_ChunkSize + 1024
	tQuery := &Query{
		ID:            tId,
		MaxAnswerSize: uint64(_RLDPMaxAnswerSize),
		Timeout:       uint32(123456),
		Data:          req,
	}

	dataQuery, err := tl.Serialize(tQuery, true)
	if err != nil {
		t.Fatal("failed to serialize test query, err: ", err)
	}

	encQuery, err := raptorq.NewRaptorQ(DefaultSymbolSize).CreateEncoder(dataQuery)
	if err != nil {
		t.Fatal("failed to create test raptorq object encoder, err: ", err)
	}

	symbolsSent := uint32(0)

	tMsg := MessagePart{
		TransferID: tId,
		FecType: FECRaptorQ{
			DataSize:     uint32(len(dataQuery)),
			SymbolSize:   DefaultSymbolSize,
			SymbolsCount: encQuery.BaseSymbolsNum(),
		},
		Part:      uint32(0),
		TotalSize: uint64(len(dataQuery)),
		Seqno:     symbolsSent,
		Data:      encQuery.GenSymbol(symbolsSent),
	}

	msgQuery := &adnl.MessageCustom{
		Data: tMsg,
	}

	response := testResponse{
		Version:    "HTTP/1.1",
		StatusCode: int32(200),
		Reason:     "test ok",
		Headers:    []testHeader{{"test", "test"}},
		NoPayload:  true,
	}
	answer := Answer{
		tId,
		response,
	}

	dataAnswer, err := tl.Serialize(answer, true)
	if err != nil {
		t.Fatal("failed to serialize test query, err: ", err)
	}

	encAnswer, err := raptorq.NewRaptorQ(DefaultSymbolSize).CreateEncoder(dataAnswer)
	if err != nil {
		t.Fatal("failed to create test raptorq object encoder, err: ", err)
	}

	tMsg = MessagePart{
		TransferID: tId,
		FecType: FECRaptorQ{
			DataSize:     uint32(len(dataAnswer)),
			SymbolSize:   DefaultSymbolSize,
			SymbolsCount: encAnswer.BaseSymbolsNum(),
		},
		Part:      uint32(0),
		TotalSize: uint64(len(dataAnswer)),
		Seqno:     symbolsSent,
		Data:      encAnswer.GenSymbol(symbolsSent),
	}

	msgAnswer := &adnl.MessageCustom{
		Data: tMsg,
	}

	testsMsgPartCase := []struct {
		tstName    string
		tstSubName string
		msg        *adnl.MessageCustom
	}{
		{
			"message part case", "query case", msgQuery,
		},
		{
			"message part case", "answer case", msgAnswer,
		},
	}

	for _, test := range testsMsgPartCase {
		t.Run(test.tstName+" "+test.tstSubName, func(t *testing.T) {
			tAdnl := MockADNL{
				setCustomMessageHandler: nil,
				setDisconnectHandler:    nil,
				sendCustomMessage: func(ctx context.Context, req tl.Serializable) error {
					r, ok := req.(Complete)
					if !ok {
						t.Fatalf("want 'Complete' type, got '%s'", reflect.TypeOf(req).String())
					}
					if !bytes.Equal(r.TransferID, tId) {
						t.Error("wrong transfer id in 'Complete' message")
					}
					return nil
				},
				close: nil,
			}

			cli := NewClient(tAdnl)
			if test.tstSubName == "query case" {
				cli.onQuery = func(transferId []byte, query *Query) error {
					if !reflect.DeepEqual(query, tQuery) {
						t.Fatal("got wrong query in handler")
					}
					return nil
				}
			} else if test.tstSubName == "answer case" {
				queryId := string(tQuery.ID)
				tChan := make(chan AsyncQueryResult, 2)
				cli.activeRequests[queryId] = &activeRequest{
					deadline: time.Now().Add(time.Second * 10).Unix(),
					result:   tChan,
				}
			}

			err = cli.handleMessage(test.msg)
			if err != nil {
				t.Fatal("handleMessage execution failed, err: ", err)
			}

			if test.tstSubName == "answer case" {
				if len(cli.activeRequests) != 0 {
					t.Errorf("got '%d' actiive requests after handeling, want '0'", len(cli.activeRequests))
				}
			}
		})

		tComplete := Complete{
			TransferID: tId,
			Part:       0,
		}

		msgComplete := &adnl.MessageCustom{
			Data: tComplete,
		}

		t.Run("message part case: got packet for a finished stream", func(t *testing.T) {
			tAdnl := MockADNL{
				setCustomMessageHandler: nil,
				setDisconnectHandler:    nil,
				sendCustomMessage: func(ctx context.Context, req tl.Serializable) error {
					r, ok := req.(Complete)
					if !ok {
						t.Fatalf("want 'Complete' type, got '%s'", reflect.TypeOf(req).String())
					}
					if !bytes.Equal(r.TransferID, tId) {
						t.Error("wrong transfer id in 'Complete' message")
					}
					return nil
				},
				close: nil,
			}
			cli := NewClient(tAdnl)
			err := cli.handleMessage(msgQuery)
			if err != nil {
				t.Fatal("failed to execute handleMessage func, err: ", err)
			}
			// emulate repeated msg receiving
			err = cli.handleMessage(msgQuery)
			if err != nil {
				t.Fatal("failed to execute handleMessage func, err: ", err)
			}
			if cli.recvStreams[string(tId)].currentPart.lastCompleteAt.IsZero() {
				t.Error("got lastCompleteAt == nil, want != nil")
			}
		})

		t.Run("message complete case", func(t *testing.T) {
			tAdnl := MockADNL{
				setCustomMessageHandler: nil,
				setDisconnectHandler:    nil,
				sendCustomMessage: func(ctx context.Context, req tl.Serializable) error {
					return nil
				},
				close: nil,
			}
			cli := NewClient(tAdnl)

			td := &activeTransfer{
				id:   tId,
				data: make([]byte, 10),
			}
			td.currentPart.Store(&activeTransferPart{
				index: 0,
			})

			cli.activeTransfers[string(tId)] = td

			err := cli.handleMessage(msgComplete)
			if err != nil {
				t.Fatal("handleMessage execution failed, err: ", err)
			}

			if len(cli.activeTransfers) != 0 {
				t.Errorf("got '%d' active transfers after handeling, want '0'", len(cli.activeTransfers))
			}
		})
	}
}

func TestRDLP_sendMessageParts(t *testing.T) {
	tId := make([]byte, 32)
	_, err := rand.Read(tId)
	if err != nil {
		t.Fatal(err)
	}

	tUrl, err := url.Parse("http://foundation.ton/")
	if err != nil {
		t.Fatal("failed to prepare test URL, err: ", err)
	}

	httpReq := http.Request{
		Method: http.MethodGet,
		URL:    tUrl,
		Host:   "foundation.ton",
	}

	req := testRequest{
		ID:      tId,
		Method:  httpReq.Method,
		URL:     httpReq.URL.String(),
		Version: "HTTP/1.1",
		Headers: []testHeader{
			{
				Name:  "Host",
				Value: httpReq.Host,
			},
		},
	}

	_ChunkSize := 1 << 17
	_RLDPMaxAnswerSize := 2*_ChunkSize + 1024
	tQuery := &Query{
		ID:            tId,
		MaxAnswerSize: uint64(_RLDPMaxAnswerSize),
		Timeout:       uint32(123456),
		Data:          req,
	}

	data, err := tl.Serialize(tQuery, true)
	if err != nil {
		t.Fatal("failed to serialize test query, err: ", err)
	}

	tAdnl := MockADNL{
		setCustomMessageHandler: nil,
		setDisconnectHandler:    nil,
		sendCustomMessage: func(ctx context.Context, req tl.Serializable) error {
			reqMsg, ok := req.(MessagePart)
			if !ok {
				t.Fatalf("want 'MessagePart' type, got '%s'", reflect.TypeOf(req).String())
			}

			tDecoder, err := raptorq.NewRaptorQ(DefaultSymbolSize).CreateDecoder(uint32(reqMsg.TotalSize))
			if err != nil {
				t.Fatal("failed to prepare test decoder, err: ", err)
			}
			added, err := tDecoder.AddSymbol(uint32(reqMsg.Seqno), reqMsg.Data)
			if err != nil || added != true {
				t.Fatal("failed to added symbol to test decoder, err: ", err)
			}

			decoded, receivData, err := tDecoder.Decode()
			if err != nil {
				t.Fatal("failed to decode received test data, err: ", err)
			}

			if decoded != true {
				return nil
			}

			if !bytes.Equal(data, receivData) {
				t.Fatal("bad data received in 'sendCustomMessage'")
			}
			return nil
		},
		close: nil,
	}

	cli := NewClient(tAdnl)

	t.Run("positive case (got true in chan activeTransfers)", func(t *testing.T) {
		go func() {
			time.Sleep(2 * time.Second)
			//for _, v := range cli.activeTransfers {
			//	v <- true
			//}
		}()
		err = cli.startTransfer(context.Background(), nil, data, int64(3*time.Second))
		if err != nil {
			t.Fatal("sendMessageParts execution failed, err: ", err)
		}
	})

	t.Run("negative case (deadline exceeded)", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)

		err = cli.startTransfer(ctx, nil, data, int64(3*time.Second))
		if !errors.Is(err, ctx.Err()) {
			t.Errorf("got '%s', want contex error", err.Error())
		}
	})
}

func TestRLDP_DoQuery(t *testing.T) {
	_ChunkSize := 1 << 17
	_RLDPMaxAnswerSize := 2*_ChunkSize + 1024

	tId := make([]byte, 32)
	_, err := rand.Read(tId)
	if err != nil {
		t.Fatal(err)
	}

	tUrl, err := url.Parse("http://foundation.ton/")
	if err != nil {
		t.Fatal("failed to prepare test URL, err: ", err)
	}

	httpReq := http.Request{
		Method: http.MethodGet,
		URL:    tUrl,
		Host:   "foundation.ton",
	}

	tReq := testRequest{
		ID:      tId,
		Method:  httpReq.Method,
		URL:     httpReq.URL.String(),
		Version: "HTTP/1.1",
		Headers: []testHeader{
			{
				Name:  "Host",
				Value: httpReq.Host,
			},
		},
	}

	response := testResponse{
		Version:    "HTTP/1.1",
		StatusCode: int32(200),
		Reason:     "test ok",
		Headers:    []testHeader{{"test", "test"}},
		NoPayload:  true,
	}
	answer := Answer{
		tId,
		response,
	}

	tAdnl := MockADNL{
		setCustomMessageHandler: nil,
		setDisconnectHandler:    nil,
		sendCustomMessage: func(ctx context.Context, req tl.Serializable) error {
			reqMsg, ok := req.(MessagePart)
			if !ok {
				t.Fatalf("want 'MessagePart' type, got '%s'", reflect.TypeOf(req).String())
			}

			tDecoder, err := raptorq.NewRaptorQ(DefaultSymbolSize).CreateDecoder(uint32(reqMsg.TotalSize))
			if err != nil {
				t.Fatal("failed to prepare test decoder, err: ", err)
			}
			added, err := tDecoder.AddSymbol(uint32(reqMsg.Seqno), reqMsg.Data)
			if err != nil || added != true {
				t.Fatal("failed to added symbol to test decoder, err: ", err)
			}

			decoded, receivData, err := tDecoder.Decode()
			if err != nil {
				t.Fatal("failed to decode received test data, err: ", err)
			}

			if decoded != true {
				return nil
			}

			var checkReq Query
			_, err = tl.Parse(&checkReq, receivData, true)
			if err != nil {
				t.Fatal("failed to parse query to request, err: ", err)
			}

			if !reflect.DeepEqual(checkReq.Data, tReq) {
				t.Fatal("bad data received in 'sendCustomMessage'")
			}
			return nil
		},
		close: nil,
	}
	cli := NewClient(tAdnl)

	t.Run("positive case", func(t *testing.T) {
		go func() {
			time.Sleep(300 * time.Millisecond)

			for _, v := range cli.activeRequests {
				v.result <- AsyncQueryResult{
					QueryID: answer.ID,
					Result:  answer.Data,
				}
			}
		}()
		var res testResponse
		err = cli.DoQuery(context.Background(), uint64(_RLDPMaxAnswerSize), tReq, &res)
		if err != nil {
			t.Fatal("DoQuery execution failed, err: ", err)
		}

		if !reflect.DeepEqual(res, answer.Data) {
			t.Error("got bad response")
		}
	})

	t.Run("negative case (deadline exceeded)", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), time.Second)

		var res Answer
		err = cli.DoQuery(ctx, uint64(_RLDPMaxAnswerSize), tReq, &res)
		if !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("got '%s', want contex error", err.Error())
		}
	})
}

func TestRLDP_SendAnswer(t *testing.T) {
	tId := make([]byte, 32)
	_, err := rand.Read(tId)
	if err != nil {
		t.Fatal(err)
	}

	response := testResponse{
		Version:    "HTTP/1.1",
		StatusCode: int32(200),
		Reason:     "test ok",
		Headers:    []testHeader{{"test", "test"}},
		NoPayload:  true,
	}

	tAdnl := MockADNL{
		setCustomMessageHandler: nil,
		setDisconnectHandler:    nil,
		sendCustomMessage: func(ctx context.Context, req tl.Serializable) error {
			reqAnswer, ok := req.(MessagePart)
			if !ok {
				t.Fatalf("want 'MessagePart' type, got '%s'", reflect.TypeOf(req).String())
			}

			tDecoder, err := raptorq.NewRaptorQ(DefaultSymbolSize).CreateDecoder(uint32(reqAnswer.TotalSize))
			if err != nil {
				t.Fatal("failed to prepare test decoder, err: ", err)
			}
			added, err := tDecoder.AddSymbol(reqAnswer.Seqno, reqAnswer.Data)
			if err != nil || added != true {
				t.Fatal("failed to added symbol to test decoder, err: ", err)
			}

			decoded, receivData, err := tDecoder.Decode()
			if err != nil {
				t.Fatal("failed to decode received test data, err: ", err)
			}

			if decoded != true {
				return nil
			}

			var checkAnswer Answer
			_, err = tl.Parse(&checkAnswer, receivData, true)
			if err != nil {
				t.Fatal("failed to parse test data, err:", err)
			}

			checkAnswerSer, err := tl.Serialize(checkAnswer.Data, true)
			if err != nil {
				t.Fatal("failed to serialize test data, err:", err)
			}

			checkResponse, err := tl.Serialize(response, true)
			if err != nil {
				t.Fatal("failed to serialize test data, err:", err)
			}

			if !bytes.Equal(checkResponse, checkAnswerSer) {
				t.Fatal("bad data received in 'sendCustomMessage'")
			}

			return nil
		},
		close: nil,
	}
	cli := NewClient(tAdnl)

	t.Run("positive case", func(t *testing.T) {
		go func() {
			time.Sleep(300 * time.Millisecond)
			for k, _ := range cli.activeTransfers {
				delete(cli.activeTransfers, k)
			}
			//for _, v := range cli.activeTransfers {
			// v <- true
			//}
		}()
		err := cli.SendAnswer(context.Background(), 100, uint32(time.Now().Add(10*time.Second).Unix()), tId, nil, response)
		if err != nil {
			t.Fatal("SendAnswer execution failed, err: ", err)
		}

		time.Sleep(time.Second)
		if len(cli.activeRequests) != 0 || len(cli.activeTransfers) != 0 {
			t.Error("invalid activeRequests and activeTransfers after response")
		}
	})
}

func TestRLDP_ClientServer(t *testing.T) {
	srvPub, srvKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, cliKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	s := adnl.NewGateway(srvKey)
	err = s.StartServer("127.0.0.1:19155")
	if err != nil {
		t.Fatal(err)
	}

	s.SetConnectionHandler(func(client adnl.Peer) error {
		conn := NewClientV2(client)
		conn.SetOnQuery(func(transferId []byte, query *Query) error {
			q := query.Data.(testRequest)

			res := testResponse{
				Version:    "HTTP/1.1",
				StatusCode: int32(200),
				Reason:     q.URL,
				Headers:    []testHeader{{"test", "test"}},
				NoPayload:  true,
			}

			println("QUERY RECEIVED")
			if err = conn.SendAnswer(context.Background(), query.MaxAnswerSize, query.Timeout, query.ID, transferId, res); err != nil {
				println("ANSWER SEND ERROR", err.Error())
				t.Fatal(err.Error())
			}
			println("ANSWER SENT")

			return nil
		})
		return nil
	})

	time.Sleep(1 * time.Second)

	clg := adnl.NewGateway(cliKey)

	err = clg.StartClient()
	if err != nil {
		t.Fatal(err)
	}

	cli, err := clg.RegisterClient("127.0.0.1:19155", srvPub)
	if err != nil {
		t.Fatal(err)
	}
	cr := NewClientV2(cli)

	t.Run("small", func(t *testing.T) {
		u := "abc"
		var resp testResponse
		err := cr.DoQuery(context.Background(), 4096, testRequest{
			ID:      make([]byte, 32),
			Method:  "GET",
			URL:     u,
			Version: "1",
		}, &resp)
		if err != nil {
			t.Fatal("bad client execution, err: ", err)
		}

		if resp.Reason != "test ok:"+hex.EncodeToString(make([]byte, 32))+u {
			t.Fatal("bad response data")
		}
	})

	Logger = log.Println
	t.Run("big multipart 10mb", func(t *testing.T) {
		old := MaxUnexpectedTransferSize
		MaxUnexpectedTransferSize = 1 << 30
		defer func() {
			MaxUnexpectedTransferSize = old
		}()

		u := strings.Repeat("a", 10*1024*1024)

		var resp testResponse
		err := cr.DoQuery(context.Background(), 4096+uint64(len(u)), testRequest{
			ID:      make([]byte, 32),
			Method:  "GET",
			URL:     u,
			Version: "1",
		}, &resp)
		if err != nil {
			t.Fatal("bad client execution, err: ", err)
		}

		if resp.Reason != "test ok:"+hex.EncodeToString(make([]byte, 32))+u {
			t.Fatal("bad response data")
		}
	})

}

func BenchmarkRLDP_ClientServer(b *testing.B) {
	srvPub, srvKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}

	_, cliKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}

	s := adnl.NewGateway(srvKey)
	if err := s.StartServer("127.0.0.1:19155"); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_ = s.Close()
	})

	s.SetConnectionHandler(func(client adnl.Peer) error {
		conn := NewClientV2(client)
		conn.SetOnQuery(func(transferId []byte, query *Query) error {
			q := query.Data.(testRequest)
			res := testResponse{
				Version:    "HTTP/1.1",
				StatusCode: 200,
				Reason:     string(make([]byte, q.RespSz)),
				Headers:    []testHeader{{"test", "test"}},
				NoPayload:  true,
			}
			return conn.SendAnswer(context.Background(), query.MaxAnswerSize, query.Timeout, query.ID, transferId, res)
		})
		return nil
	})

	clg := adnl.NewGateway(cliKey)
	if err := clg.StartClient(); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = clg.Close() })

	cli, err := clg.RegisterClient("127.0.0.1:19155", srvPub)
	if err != nil {
		b.Fatal(err)
	}
	cr := NewClientV2(cli)

	sizes := []uint64{1 << 10, 256 << 10, 1 << 20, 4 << 20, 10 << 20} // 1KB, 256KB, 1MB, 4MB, 10 MB

	for _, sz := range sizes {
		b.Run(fmt.Sprintf("resp=%dKB", sz>>10), func(b *testing.B) {
			b.SetBytes(int64(sz))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				var resp testResponse
				if err := cr.DoQuery(context.Background(), 1<<30, testRequest{
					ID:      make([]byte, 32),
					Method:  "GET",
					URL:     "123",
					Version: "1",
					RespSz:  sz,
				}, &resp); err != nil {
					b.Fatalf("client exec err: %v", err)
				}
			}
		})

		b.Run(fmt.Sprintf("resp=%dKB/parallel", sz>>10), func(b *testing.B) {
			b.SetBytes(int64(sz))
			b.ReportAllocs()
			b.SetParallelism(8)

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					var resp testResponse
					if err := cr.DoQuery(context.Background(), 1<<30, testRequest{
						ID:      make([]byte, 32),
						Method:  "GET",
						URL:     "123",
						Version: "1",
						RespSz:  sz,
					}, &resp); err != nil {
						b.Fatalf("client exec err: %v", err)
					}
				}
			})
		})
	}
}
