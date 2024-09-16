package http

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/ton/dns"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

const _ChunkSize = 1 << 17
const _RLDPMaxAnswerSize = 2*_ChunkSize + 1024

var ErrSiteUsesStorage = fmt.Errorf("requested site is static and uses ton storage, you can files using storage.Downloader")

type DHT interface {
	StoreAddress(ctx context.Context, addresses address.List, ttl time.Duration, ownerKey ed25519.PrivateKey, copies int) (int, []byte, error)
	FindAddresses(ctx context.Context, key []byte) (*address.List, ed25519.PublicKey, error)
	Close()
}

type Resolver interface {
	Resolve(ctx context.Context, domain string) (*dns.Domain, error)
}

type RLDP interface {
	Close()
	DoQuery(ctx context.Context, maxAnswerSize int64, query, result tl.Serializable) error
	SetOnQuery(handler func(transferId []byte, query *rldp.Query) error)
	SetOnDisconnect(handler func())
	SendAnswer(ctx context.Context, maxAnswerSize int64, queryId, transferId []byte, answer tl.Serializable) error
}

type ADNL interface {
	GetID() []byte
	RemoteAddr() string
	Query(ctx context.Context, req, result tl.Serializable) error
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	GetDisconnectHandler() func(addr string, key ed25519.PublicKey)
	SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error)
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
	SetQueryHandler(handler func(msg *adnl.MessageQuery) error)
	GetQueryHandler() func(msg *adnl.MessageQuery) error
	Answer(ctx context.Context, queryID []byte, result tl.Serializable) error
	Close()
}

var Connector = func(ctx context.Context, addr string, peerKey ed25519.PublicKey, ourKey ed25519.PrivateKey) (ADNL, error) {
	return adnl.Connect(ctx, addr, peerKey, ourKey)
}

var newRLDP = func(a ADNL, v2 bool) RLDP {
	if v2 {
		return rldp.NewClientV2(a)
	}
	return rldp.NewClient(a)
}

type rldpInfo struct {
	mx             sync.RWMutex
	ActiveClient   RLDP
	ClientLastUsed time.Time

	ID   ed25519.PublicKey
	Addr string

	Resolved bool
}

type Transport struct {
	dht      DHT
	resolver Resolver

	adnlKey ed25519.PrivateKey

	rldpInfos map[string]*rldpInfo

	activeRequests map[string]*payloadStream
	mx             sync.RWMutex
}

func NewTransport(dht DHT, resolver Resolver, adnlKey ...ed25519.PrivateKey) *Transport {
	t := &Transport{
		dht:            dht,
		resolver:       resolver,
		activeRequests: map[string]*payloadStream{},
		rldpInfos:      map[string]*rldpInfo{},
	}
	if len(adnlKey) > 0 && adnlKey[0] != nil {
		t.adnlKey = adnlKey[0]
	}
	return t
}

func (t *Transport) connectRLDP(ctx context.Context, key ed25519.PublicKey, addr, id string) (RLDP, error) {
	a, err := Connector(ctx, addr, key, t.adnlKey)
	if err != nil {
		return nil, fmt.Errorf("failed to init adnl for rldp connection %s, err: %w", addr, err)
	}

	rCap := GetCapabilities{
		Capabilities: 0,
	}

	var caps Capabilities
	err = a.Query(ctx, rCap, &caps)
	if err != nil {
		return nil, fmt.Errorf("failed to query http caps: %w", err)
	}

	previousHandler := a.GetQueryHandler()
	a.SetQueryHandler(func(query *adnl.MessageQuery) error {
		switch query.Data.(type) {
		case GetCapabilities:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := a.Answer(ctx, query.ID, &Capabilities{Value: 0})
			cancel()
			if err != nil {
				return fmt.Errorf("failed to send capabilities answer: %w", err)
			}
			return nil
		}
		if previousHandler != nil {
			return previousHandler(query)
		}
		return fmt.Errorf("unexpected query type %s", reflect.TypeOf(query.Data))
	})

	r := newRLDP(a, caps.Value&CapabilityRLDP2 != 0)
	r.SetOnQuery(t.getRLDPQueryHandler(r))
	r.SetOnDisconnect(t.removeRLDP(r, id))

	return r, nil
}

func (t *Transport) removeRLDP(rl RLDP, id string) func() {
	return func() {
		t.mx.RLock()
		r := t.rldpInfos[id]
		t.mx.RUnlock()

		if r != nil {
			r.destroyClient(rl)
		}
	}
}

func (r *rldpInfo) destroyClient(rl RLDP) {
	rl.Close()

	r.mx.Lock()
	if r.ActiveClient == rl {
		r.ActiveClient = nil
	}
	r.mx.Unlock()
}

func (t *Transport) getRLDPQueryHandler(r RLDP) func(transferId []byte, query *rldp.Query) error {
	return func(transferId []byte, query *rldp.Query) error {
		switch req := query.Data.(type) {
		case GetNextPayloadPart:
			t.mx.RLock()
			stream := t.activeRequests[hex.EncodeToString(req.ID)]
			t.mx.RUnlock()

			if stream == nil {
				return fmt.Errorf("unknown request id %s", hex.EncodeToString(req.ID))
			}

			part, err := handleGetPart(req, stream)
			if err != nil {
				return fmt.Errorf("handle part err: %w", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err = r.SendAnswer(ctx, query.MaxAnswerSize, query.ID, transferId, part)
			cancel()
			if err != nil {
				return fmt.Errorf("failed to send answer: %w", err)
			}

			if part.IsLast {
				t.mx.Lock()
				delete(t.activeRequests, hex.EncodeToString(req.ID))
				t.mx.Unlock()
				_ = stream.Data.Close()
			}
			return nil
		}
		return fmt.Errorf("unexpected query type %s", reflect.TypeOf(query.Data))
	}
}

func handleGetPart(req GetNextPayloadPart, stream *payloadStream) (*PayloadPart, error) {
	stream.mx.Lock()
	defer stream.mx.Unlock()

	offset := int(req.Seqno * req.MaxChunkSize)
	if offset != stream.nextOffset {
		return nil, fmt.Errorf("failed to get part for stream %s, incorrect offset %d, should be %d", hex.EncodeToString(req.ID), offset, stream.nextOffset)
	}

	var last bool
	data := make([]byte, req.MaxChunkSize)
	n, err := stream.Data.Read(data)
	if err != nil {
		if err != io.EOF {
			return nil, fmt.Errorf("failed to read chunk %d, err: %w", req.Seqno, err)
		}
		last = true
	}
	stream.nextOffset += n

	return &PayloadPart{
		Data:    data[:n],
		Trailer: nil, // TODO: trailer
		IsLast:  last,
	}, nil
}

func (t *Transport) RoundTrip(request *http.Request) (_ *http.Response, err error) {
	qid := make([]byte, 32)
	_, err = rand.Read(qid)
	if err != nil {
		return nil, err
	}

	t.mx.Lock()
	rlInfo := t.rldpInfos[request.Host]
	if rlInfo == nil {
		rlInfo = &rldpInfo{}
		t.rldpInfos[request.Host] = rlInfo
	}
	t.mx.Unlock()

	var client RLDP
	rlInfo.mx.Lock()
	if rlInfo.ActiveClient != nil && rlInfo.ClientLastUsed.Add(15*time.Second).Before(time.Now()) {
		// if last used more than 10 seconds ago,
		// we have a chance of stuck udp socket,
		// so we just reinit connection
		go rlInfo.ActiveClient.Close() // close async because of lock

		// set it nil now to reassign
		rlInfo.ActiveClient = nil
	}

	client = rlInfo.ActiveClient
	if rlInfo.Resolved && client == nil {
		client, err = t.connectRLDP(request.Context(), rlInfo.ID, rlInfo.Addr, request.Host)
		if err != nil {
			// resolve again
			rlInfo.Resolved = false
		}
		rlInfo.ActiveClient = client
		rlInfo.ClientLastUsed = time.Now()
	}

	if !rlInfo.Resolved {
		err = t.resolveRLDP(request.Context(), rlInfo, request.Host)
		if err != nil {
			rlInfo.mx.Unlock()
			return nil, err
		}
		client = rlInfo.ActiveClient
		rlInfo.ClientLastUsed = time.Now()
	}

	rlInfo.mx.Unlock()

	req := Request{
		ID:      qid,
		Method:  request.Method,
		URL:     request.URL.RequestURI(),
		Version: "HTTP/1.1",
		Headers: []Header{
			{
				Name:  "Host",
				Value: request.Host,
			},
		},
	}

	if request.ContentLength > 0 {
		req.Headers = append(req.Headers, Header{
			Name:  "Content-Length",
			Value: fmt.Sprint(request.ContentLength),
		})
	}

	for k, v := range request.Header {
		for _, hdr := range v {
			req.Headers = append(req.Headers, Header{
				Name:  k,
				Value: hdr,
			})
		}
	}

	if request.Body != nil {
		stream := newDataStreamer()

		// chunked stream reader
		go func() {
			defer request.Body.Close()

			var n int
			for {
				buf := make([]byte, 4096)
				n, err = request.Body.Read(buf)
				if err != nil {
					if errors.Is(err, io.EOF) {
						_, err = stream.Write(buf[:n])
						if err == nil {
							stream.Finish()
							break
						}
					}
					_ = stream.Close()
					break
				}

				_, err = stream.Write(buf[:n])
				if err != nil {
					_ = stream.Close()
					break
				}
			}
		}()

		t.mx.Lock()
		t.activeRequests[hex.EncodeToString(qid)] = &payloadStream{
			Data:      stream,
			ValidTill: time.Now().Add(15 * time.Second),
		}
		t.mx.Unlock()

		defer func() {
			t.mx.Lock()
			delete(t.activeRequests, hex.EncodeToString(qid))
			t.mx.Unlock()
		}()
	}

	var res Response
	err = client.DoQuery(request.Context(), _RLDPMaxAnswerSize, req, &res)
	if err != nil {
		return nil, fmt.Errorf("failed to query http over rldp: %w", err)
	}

	rlInfo.mx.Lock()
	if rlInfo.ActiveClient == client {
		rlInfo.ClientLastUsed = time.Now()
	}
	rlInfo.mx.Unlock()

	httpResp := &http.Response{
		Status:        res.Reason,
		StatusCode:    int(res.StatusCode),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        map[string][]string{},
		ContentLength: -1,
		Trailer:       map[string][]string{},
		Request:       request,
	}

	for _, header := range res.Headers {
		httpResp.Header[header.Name] = []string{header.Value}
	}

	if ln, ok := request.Header["Content-Length"]; ok && len(ln) > 0 {
		httpResp.ContentLength, err = strconv.ParseInt(ln[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse content length: %w", err)
		}
	}

	withPayload := !res.NoPayload && (httpResp.StatusCode < 300 || httpResp.StatusCode >= 400)

	dr := newDataStreamer()
	httpResp.Body = dr

	if withPayload {
		if httpResp.ContentLength > 0 && httpResp.ContentLength < (1<<22) {
			dr.buf = make([]byte, 0, httpResp.ContentLength)
		}

		go func() {
			seqno := int32(0)
			for withPayload {
				var part PayloadPart
				err := client.DoQuery(request.Context(), _RLDPMaxAnswerSize, GetNextPayloadPart{
					ID:           qid,
					Seqno:        seqno,
					MaxChunkSize: _ChunkSize,
				}, &part)
				if err != nil {
					_ = dr.Close()
					return
				}

				for _, tr := range part.Trailer {
					httpResp.Trailer[tr.Name] = []string{tr.Value}
				}

				withPayload = !part.IsLast
				_, err = dr.Write(part.Data)
				if err != nil {
					_ = dr.Close()
					return
				}

				if part.IsLast {
					dr.Finish()
				}

				seqno++

				rlInfo.mx.Lock()
				if rlInfo.ActiveClient == client {
					rlInfo.ClientLastUsed = time.Now()
				}
				rlInfo.mx.Unlock()
			}
		}()
	} else {
		dr.Finish()
	}

	return httpResp, nil
}

func (t *Transport) resolveRLDP(ctx context.Context, info *rldpInfo, host string) (err error) {
	var id []byte
	var inStorage bool
	if strings.HasSuffix(host, ".adnl") {
		id, err = ParseADNLAddress(host[:len(host)-5])
		if err != nil {
			return fmt.Errorf("failed to parse adnl address %s, err: %w", host, err)
		}
	} else {
		var domain *dns.Domain
		for i := 0; i < 3; i++ {
			domain, err = t.resolver.Resolve(ctx, host)
			if err != nil {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			break
		}
		if err != nil {
			return fmt.Errorf("failed to resolve host %s, err: %w", host, err)
		}

		id, inStorage = domain.GetSiteRecord()
		if inStorage {
			return ErrSiteUsesStorage
		}
	}

	addresses, pubKey, err := t.dht.FindAddresses(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to find address of %s (%s) in DHT, err: %w", host, hex.EncodeToString(id), err)
	}

	var triedAddresses []string
	for _, v := range addresses.Addresses {
		addr := fmt.Sprintf("%s:%d", v.IP.String(), v.Port)

		var client RLDP
		// find working rldp node addr
		client, err = t.connectRLDP(ctx, pubKey, addr, host)
		if err != nil {
			triedAddresses = append(triedAddresses, addr)
			continue
		}

		info.ActiveClient = client

		info.Resolved = true
		info.ID = pubKey
		info.Addr = addr

		break
	}
	if err != nil {
		return fmt.Errorf("failed to connect to rldp servers %s of host %s, err: %w", triedAddresses, host, err)
	}
	return nil
}
