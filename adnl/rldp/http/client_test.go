package http

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/dns"
	"io"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"
)

var originalConnector = Connector
var originalNewRLDP = newRLDP

type testRequest struct {
	ID      []byte       `tl:"int256"`
	Method  string       `tl:"string"`
	URL     string       `tl:"string"`
	Version string       `tl:"string"`
	Headers []testHeader `tl:"vector struct"`
}

type testHeader struct {
	Name  string `tl:"string"`
	Value string `tl:"string"`
}

type MockADNL struct {
	query                   func(ctx context.Context, req, result tl.Serializable) error
	setDisconnectHandler    func(handler func(addr string, key ed25519.PublicKey))
	setCustomMessageHandler func(handler func(msg *adnl.MessageCustom) error)
	sendCustomMessage       func(ctx context.Context, req tl.Serializable) error
	close                   func()
}

func (m MockADNL) GetDisconnectHandler() func(addr string, key ed25519.PublicKey) {
	return nil
}

func (m MockADNL) GetID() []byte {
	//TODO implement me
	panic("implement me")
}

func (m MockADNL) RemoteAddr() string {
	//TODO implement me
	panic("implement me")
}

func (m MockADNL) GetQueryHandler() func(msg *adnl.MessageQuery) error {
	return nil
}

func (m MockADNL) SetQueryHandler(handler func(msg *adnl.MessageQuery) error) {
	return
}

func (m MockADNL) Answer(ctx context.Context, queryID []byte, result tl.Serializable) error {
	return nil
}

func (m MockADNL) Query(ctx context.Context, req, result tl.Serializable) error {
	return m.query(ctx, req, result)
}

func (m MockADNL) SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey)) {
}

func (m MockADNL) SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error) {
}

func (m MockADNL) SendCustomMessage(ctx context.Context, req tl.Serializable) error {
	return m.sendCustomMessage(ctx, req)
}

func (m MockADNL) Close() {}

type MockRDLP struct {
	close           func()
	doQuery         func(ctx context.Context, maxAnswerSize int64, query, result tl.Serializable) error
	setOnQuery      func(handler func(transferId []byte, query *rldp.Query) error)
	setOnDisconnect func(handler func())
	sendAnswer      func(ctx context.Context, maxAnswerSize int64, queryId, transferId []byte, answer tl.Serializable) error
}

func (m MockRDLP) Close() {}

func (m MockRDLP) DoQuery(ctx context.Context, maxAnswerSize int64, query, result tl.Serializable) error {
	return m.doQuery(ctx, maxAnswerSize, query, result)
}

func (m MockRDLP) SetOnQuery(handler func(transferId []byte, query *rldp.Query) error) {}

func (m MockRDLP) SetOnDisconnect(handler func()) {}

func (m MockRDLP) SendAnswer(ctx context.Context, maxAnswerSize int64, queryId, transferId []byte, answer tl.Serializable) error {
	return m.sendAnswer(ctx, maxAnswerSize, queryId, transferId, answer)
}

func TestTransport_removeRLDP(t *testing.T) {
	var Connector = func(ctx context.Context, addr string, peerKey ed25519.PublicKey, ourKey ed25519.PrivateKey) (ADNL, error) {
		return MockADNL{}, nil
	}
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal("failed to prepare test keys, err: ", err)
	}
	tAdnlConn, err := Connector(context.Background(), "1.2.3.4:12345", pub, nil)
	if err != nil {
		t.Fatal("failed to prepare test adnl connection, err: ", err)
	}
	var newRLDP = func(a ADNL) RLDP {
		return rldp.NewClient(a)
	}
	tRldp := newRLDP(tAdnlConn)

	tTransport := NewTransport(&MockDHT{}, MockResolver{})

	tTransport.rldpInfos["123"] = &rldpInfo{
		mx:             sync.RWMutex{},
		ActiveClient:   tRldp,
		ClientLastUsed: time.Time{},
		ID:             nil,
		Addr:           "",
		Resolved:       false,
	}

	tTransport.removeRLDP(tRldp, "123")()
	if tTransport.rldpInfos["123"].ActiveClient != nil {
		t.Error("got active client after removeRLDP")
	}
}

func TestTransport_getRLDPQueryHandler(t *testing.T) {
	var Connector = func(ctx context.Context, addr string, peerKey ed25519.PublicKey, ourKey ed25519.PrivateKey) (ADNL, error) {
		return MockADNL{}, nil
	}
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal("failed to prepare test keys, err: ", err)
	}
	tAdnlConn, err := Connector(context.Background(), "1.2.3.4:12345", pub, nil)
	if err != nil {
		t.Fatal("failed to prepare test adnl connection, err: ", err)
	}

	tQueryId := make([]byte, 32)
	_, err = rand.Read(tQueryId)
	if err != nil {
		t.Fatal("failed to prepare test query id, err: ", err)
	}

	tQuery := &rldp.Query{
		ID:            tQueryId,
		MaxAnswerSize: int64(_RLDPMaxAnswerSize),
		Timeout:       int32(123456),
		Data: GetNextPayloadPart{
			ID:           tQueryId,
			Seqno:        0,
			MaxChunkSize: _ChunkSize,
		},
	}

	var newRLDP = func(a ADNL) RLDP {
		return MockRDLP{
			close:           nil,
			doQuery:         nil,
			setOnQuery:      nil,
			setOnDisconnect: nil,
			sendAnswer: func(ctx context.Context, maxAnswerSize int64, queryId, transferId []byte, answer tl.Serializable) error {
				if !bytes.Equal(queryId, tQueryId) {
					t.Fatal("received wrong query id")
				}
				return nil
			},
		}
	}
	tRldp := newRLDP(tAdnlConn)

	t.Run("not last part", func(t *testing.T) {
		tTransport := NewTransport(&MockDHT{}, MockResolver{})
		dataStream := newDataStreamer()
		dataStream.closed = false

		tTransport.activeRequests[hex.EncodeToString(tQueryId)] = &payloadStream{
			Data:      dataStream,
			ValidTill: time.Now().Add(15 * time.Second),
		}
		go func() {
			dataStream.parts <- []byte("123")
		}()
		go func() {
			dataStream.parts <- nil
		}()

		err = tTransport.getRLDPQueryHandler(tRldp)(nil, tQuery)
		if err != nil {
			t.Fatal("failed to execute getRLDPQueryHandler func, err: ", err)
		}
		if _, ok := tTransport.activeRequests[hex.EncodeToString(tQueryId)]; !ok {
			t.Error("active request is closed before last part sent")
		}
	})

	t.Run("last part", func(t *testing.T) {
		tTransport := NewTransport(&MockDHT{}, MockResolver{})
		dataStream := newDataStreamer()
		dataStream.closed = false

		tTransport.activeRequests[hex.EncodeToString(tQueryId)] = &payloadStream{
			Data:      dataStream,
			ValidTill: time.Now().Add(15 * time.Second),
		}
		go func() {
			dataStream.finished = true
			dataStream.parts <- nil
		}()
		err = tTransport.getRLDPQueryHandler(tRldp)(nil, tQuery)
		if err != nil {
			t.Fatal("failed to execute getRLDPQueryHandler func, err: ", err)
		}
		if _, ok := tTransport.activeRequests[hex.EncodeToString(tQueryId)]; ok {
			t.Error("find active request after last part sent")
		}
	})
}

func Test_handleGetPart(t *testing.T) {
	tQueryId := make([]byte, 32)
	_, err := rand.Read(tQueryId)
	if err != nil {
		t.Fatal("failed to prepare test query id, err: ", err)
	}

	tReqest := GetNextPayloadPart{
		ID:           tQueryId,
		Seqno:        0,
		MaxChunkSize: _ChunkSize,
	}

	t.Run("not last part", func(t *testing.T) {
		dataStream := newDataStreamer()
		go func() {
			dataStream.parts <- []byte{0xFF, 0xFF} // random bytes
			dataStream.parts <- nil
		}()

		tTransport := NewTransport(&MockDHT{}, MockResolver{})
		tTransport.activeRequests[hex.EncodeToString(tQueryId)] = &payloadStream{
			Data:      dataStream,
			ValidTill: time.Now().Add(15 * time.Second),
		}
		payLoadStr, _ := tTransport.activeRequests[hex.EncodeToString(tQueryId)]

		payLoad, err := handleGetPart(tReqest, payLoadStr)
		if err != nil {
			t.Fatal("failed to execute handleGetPart func, err: ", err)
		}

		if !bytes.Equal(payLoad.Data, []byte{0xFF, 0xFF}) {
			t.Error("got wrong bytes in pay load")
		}
		if payLoad.IsLast != false {
			t.Error("got wrong payLoad.IsLast status")
		}
	})

	t.Run("last part", func(t *testing.T) {
		dataStream := newDataStreamer()
		go func() {
			dataStream.finished = true
			dataStream.parts <- nil
		}()

		tTransport := NewTransport(&MockDHT{}, MockResolver{})
		tTransport.activeRequests[hex.EncodeToString(tQueryId)] = &payloadStream{
			Data:      dataStream,
			ValidTill: time.Now().Add(15 * time.Second),
		}
		payLoadStr, _ := tTransport.activeRequests[hex.EncodeToString(tQueryId)]

		payLoad, err := handleGetPart(tReqest, payLoadStr)
		if err != nil {
			t.Fatal("failed to execute handleGetPart func, err: ", err)
		}
		if len(payLoad.Data) != 0 {
			t.Error("got wrong bytes in pay load")
		}
		if payLoad.IsLast != true {
			t.Error("got wrong payLoad.IsLast status")
		}
	})
}

func TestTransport_RoundTripUnit(t *testing.T) {
	Connector = func(ctx context.Context, addr string, peerKey ed25519.PublicKey, ourKey ed25519.PrivateKey) (ADNL, error) {
		return MockADNL{
				query: func(ctx context.Context, req, result tl.Serializable) error {
					return nil
				},
				setDisconnectHandler: func(handler func(addr string, key ed25519.PublicKey)) {
				},
				setCustomMessageHandler: func(handler func(msg *adnl.MessageCustom) error) {
				},
				sendCustomMessage: func(ctx context.Context, req tl.Serializable) error {
					fmt.Println(123)
					return nil
				},
				close: func() {
				},
			},
			nil
	}

	newRLDP = func(a ADNL, is2 bool) RLDP {
		return &MockRDLP{
			close: func() {
			},
			doQuery: func(ctx context.Context, maxAnswerSize int64, query, result tl.Serializable) error {
				reflect.ValueOf(result).Elem().Set(reflect.ValueOf(Response{
					Version:    "HTTP/1.1",
					StatusCode: int32(200),
					Reason:     "test ok",
					Headers:    []Header{{"test", "test"}},
					NoPayload:  true,
				}))
				return nil
			},
			setOnQuery: func(handler func(transferId []byte, query *rldp.Query) error) {
			},
			setOnDisconnect: func(handler func()) {
			},
			sendAnswer: func(ctx context.Context, maxAnswerSize int64, queryId, transferId []byte, answer tl.Serializable) error {
				return nil
			},
		}
	}
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal("failed to prepare test keys, err: ", err)
	}
	tAdnlConn, err := Connector(context.Background(), "1.2.3.4:12345", pub, nil)
	if err != nil {
		t.Fatal("failed to prepare test adnl connection, err: ", err)
	}

	tRldpCli := newRLDP(tAdnlConn, false)
	tTransport := NewTransport(&MockDHT{}, MockResolver{})

	req, err := http.NewRequest(http.MethodGet, "http://foundation.ton/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = 1
	req.Header["for test1"] = []string{"for test1.1", "for test1.2"}
	req.Header["for test2"] = []string{"for test2.1", "for test2.2"}
	req.Body = io.NopCloser(bytes.NewBufferString("body for testing"))

	tTransport.rldpInfos[req.Host] = &rldpInfo{
		mx:             sync.RWMutex{},
		ActiveClient:   tRldpCli,
		ClientLastUsed: time.Now().Add(-16 * time.Second),
		ID:             nil,
		Addr:           "",
		Resolved:       true,
	}
	response, err := tTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != 200 {
		t.Errorf("got response code '%d', want '200'", response.StatusCode)
	}
}

func Test_parseADNLAddress(t *testing.T) {
	res, err := ParseADNLAddress("ui52b4urpcoigi26kfwp7vt2cgs2b5ljudwigvra35nhvymdqvqlfsa")
	if err != nil {
		t.Fatal(err)
	}

	if hex.EncodeToString(res) != "11dd07948bc4e4191af28b67feb3d08d2d07ab4d07641ab106fad3d70c1c2b05" {
		t.Fatal("incorrect result", hex.EncodeToString(res))
	}
}

func TestTransport_RoundTripIntegration(t *testing.T) {
	Connector = originalConnector
	newRLDP = originalNewRLDP

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	gateway := adnl.NewGateway(priv)
	err = gateway.StartClient()
	if err != nil {
		t.Fatal(err)
	}

	dhtClient, err := dht.NewClientFromConfigUrl(context.Background(), gateway, "https://tonutils.com/global.config.json")
	if err != nil {
		t.Fatal(err)
	}

	transport := NewTransport(dhtClient, getDNSResolver())

	req, err := http.NewRequest(http.MethodGet, "http://utils.ton/", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Errorf("got response code '%d', want '200'", response.StatusCode)
	}
}

func getDNSResolver() *dns.Client {
	client := liteclient.NewConnectionPool()

	// connect to testnet lite server
	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://tonutils.com/global.config.json")
	if err != nil {
		panic(err)
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client).WithRetry()

	// get root dns address from network config
	root, err := dns.RootContractAddr(api)
	if err != nil {
		panic(err)
	}

	return dns.NewDNSClient(api, root)
}

func Test_parseSerializeADNLAddress(t *testing.T) {
	val := make([]byte, 32)
	val[7] = 0xDA

	addr, err := SerializeADNLAddress(val)
	if err != nil {
		t.Fatal(err)
	}

	addrParsed, err := ParseADNLAddress(addr)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(addrParsed, val) {
		t.Log(hex.EncodeToString(addrParsed))
		t.Log(hex.EncodeToString(val))
		t.Fatal("incorrect addr")
	}
}
