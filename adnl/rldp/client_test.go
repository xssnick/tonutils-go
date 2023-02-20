package rldp

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/rldp/raptorq"
	"github.com/xssnick/tonutils-go/tl"
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

}

type MockADNL struct {
	setCustomMessageHandler func(msg *adnl.MessageCustom) error
	setDisconnectHandler    func(addr string, key ed25519.PublicKey)
	sendCustomMessage       func(ctx context.Context, req tl.Serializable) error
	close                   func()
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
		MaxAnswerSize: int64(_RLDPMaxAnswerSize),
		Timeout:       int32(123456),
		Data:          req,
	}

	dataQuery, err := tl.Serialize(tQuery, true)
	if err != nil {
		t.Fatal("failed to serialize test query, err: ", err)
	}

	encQuery, err := raptorq.NewRaptorQ(_SymbolSize).CreateEncoder(dataQuery)
	if err != nil {
		t.Fatal("failed to create test raptorq object encoder, err: ", err)
	}

	symbolsSent := uint32(0)

	tMsg := MessagePart{
		TransferID: tId,
		FecType: FECRaptorQ{
			DataSize:     int32(len(dataQuery)),
			SymbolSize:   _SymbolSize,
			SymbolsCount: int32(encQuery.BaseSymbolsNum()),
		},
		Part:      int32(0),
		TotalSize: int64(len(dataQuery)),
		Seqno:     int32(symbolsSent),
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

	encAnswer, err := raptorq.NewRaptorQ(_SymbolSize).CreateEncoder(dataAnswer)
	if err != nil {
		t.Fatal("failed to create test raptorq object encoder, err: ", err)
	}

	tMsg = MessagePart{
		TransferID: tId,
		FecType: FECRaptorQ{
			DataSize:     int32(len(dataAnswer)),
			SymbolSize:   _SymbolSize,
			SymbolsCount: int32(encAnswer.BaseSymbolsNum()),
		},
		Part:      int32(0),
		TotalSize: int64(len(dataAnswer)),
		Seqno:     int32(symbolsSent),
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
				queryId := hex.EncodeToString(tQuery.ID)
				tChan := make(chan any, 2)
				cli.activeRequests[queryId] = tChan
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
			if cli.recvStreams[hex.EncodeToString(tId)].lastCompleteAt.IsZero() {
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

			tChan := make(chan bool, 1)
			cli.activeTransfers[hex.EncodeToString(tId)] = tChan

			err := cli.handleMessage(msgComplete)
			if err != nil {
				t.Fatal("handleMessage execution failed, err: ", err)
			}

			if len(cli.activeTransfers) != 0 {
				t.Errorf("got '%d' actiive transfers after handeling, want '0'", len(cli.activeTransfers))
			}

			if <-tChan != false {
				t.Error("got 'true' in chan, want 'false'")
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
		MaxAnswerSize: int64(_RLDPMaxAnswerSize),
		Timeout:       int32(123456),
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

			tDecoder, err := raptorq.NewRaptorQ(_SymbolSize).CreateDecoder(uint32(reqMsg.TotalSize))
			if err != nil {
				t.Fatal("failed to prepare test decoder, err: ", err)
			}
			added, err := tDecoder.AddSymbol(uint32(reqMsg.Seqno), reqMsg.Data)
			if err != nil || added != true {
				t.Fatal("failed to added symbol to test decoder, err: ", err)
			}

			decoded, receivData, err := tDecoder.Decode()
			if err != nil || decoded != true {
				t.Fatal("failed to decode received test data, err: ", err)
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
			for _, v := range cli.activeTransfers {
				v <- true
			}
		}()
		err = cli.sendMessageParts(context.Background(), nil, data)
		if err != nil {
			t.Fatal("sendMessageParts execution failed, err: ", err)
		}
	})

	t.Run("negative case (deadline exceeded)", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)

		err = cli.sendMessageParts(ctx, nil, data)
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

			tDecoder, err := raptorq.NewRaptorQ(_SymbolSize).CreateDecoder(uint32(reqMsg.TotalSize))
			if err != nil {
				t.Fatal("failed to prepare test decoder, err: ", err)
			}
			added, err := tDecoder.AddSymbol(uint32(reqMsg.Seqno), reqMsg.Data)
			if err != nil || added != true {
				t.Fatal("failed to added symbol to test decoder, err: ", err)
			}

			decoded, receivData, err := tDecoder.Decode()
			if err != nil || decoded != true {
				t.Fatal("failed to decode received test data, err: ", err)
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
			time.Sleep(2 * time.Second)

			for _, v := range cli.activeRequests {
				v <- answer
			}
		}()
		var res Answer
		err = cli.DoQuery(context.Background(), int64(_RLDPMaxAnswerSize), tReq, &res)
		if err != nil {
			t.Fatal("DoQuery execution failed, err: ", err)
		}

		if !reflect.DeepEqual(res, answer) {
			t.Error("got bad response")
		}

		time.Sleep(time.Second)
		if len(cli.activeRequests) != 0 || len(cli.activeTransfers) != 0 {
			t.Error("invalid activeRequests and activeTransfers after response")
		}

	})

	t.Run("negative case (deadline exceeded)", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), time.Second)

		var res Answer
		err = cli.DoQuery(ctx, int64(_RLDPMaxAnswerSize), tReq, &res)
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

			tDecoder, err := raptorq.NewRaptorQ(_SymbolSize).CreateDecoder(uint32(reqAnswer.TotalSize))
			if err != nil {
				t.Fatal("failed to prepare test decoder, err: ", err)
			}
			added, err := tDecoder.AddSymbol(uint32(reqAnswer.Seqno), reqAnswer.Data)
			if err != nil || added != true {
				t.Fatal("failed to added symbol to test decoder, err: ", err)
			}

			decoded, receivData, err := tDecoder.Decode()
			if err != nil || decoded != true {
				t.Fatal("failed to decode received test data, err: ", err)
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
			time.Sleep(2 * time.Second)
			for _, v := range cli.activeTransfers {
				v <- true
			}
		}()
		err := cli.SendAnswer(context.Background(), 100, tId, nil, response)
		if err != nil {
			t.Fatal("SendAnswer execution failed, err: ", err)
		}

		time.Sleep(time.Second)
		if len(cli.activeRequests) != 0 || len(cli.activeTransfers) != 0 {
			t.Error("invalid activeRequests and activeTransfers after response")
		}
	})

	t.Run("negative case (deadline exceeded)", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), time.Second)

		err := cli.SendAnswer(ctx, 100, tId, nil, response)
		if !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("got '%s', want contex error", err.Error())
		}
	})
}
