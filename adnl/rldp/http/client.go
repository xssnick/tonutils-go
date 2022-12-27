package http

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/sigurn/crc16"
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

type DHT interface {
	StoreAddress(ctx context.Context, addresses address.List, ttl time.Duration, ownerKey ed25519.PrivateKey, copies int) ([]byte, error)
	FindAddresses(ctx context.Context, key []byte) (*address.List, ed25519.PublicKey, error)
	Close()
}

type Resolver interface {
	Resolve(ctx context.Context, domain string) (*dns.Domain, error)
}

type RLDP interface {
	Close()
	DoQuery(ctx context.Context, maxAnswerSize int64, query, result tl.Serializable) error
	SetOnQuery(handler func(query *rldp.Query) error)
	SetOnDisconnect(handler func())
	SendAnswer(ctx context.Context, maxAnswerSize int64, queryID []byte, answer tl.Serializable) error
}

type ADNL interface {
	Query(ctx context.Context, req, result tl.Serializable) error
	SetDisconnectHandler(handler func(addr string, key ed25519.PublicKey))
	SetCustomMessageHandler(handler func(msg *adnl.MessageCustom) error)
	SendCustomMessage(ctx context.Context, req tl.Serializable) error
	Close()
}

var Connector = func(ctx context.Context, addr string, peerKey ed25519.PublicKey, ourKey ed25519.PrivateKey) (ADNL, error) {
	return adnl.Connect(ctx, addr, peerKey, ourKey)
}

var newRLDP = func(a ADNL) RLDP {
	return rldp.NewClient(a)
}

type payloadStream struct {
	nextOffset int
	Data       *dataStreamer
	ValidTill  time.Time
}

type dataStreamer struct {
	buf []byte

	parts    chan []byte
	closer   chan bool
	finished bool
	closed   bool

	readerLock sync.Mutex
	writerLock sync.Mutex
	closerLock sync.Mutex
}

func newDataStreamer() *dataStreamer {
	return &dataStreamer{
		parts:  make(chan []byte, 1),
		closer: make(chan bool, 1),
	}
}

func (d *dataStreamer) Read(p []byte) (n int, err error) {
	d.readerLock.Lock()
	defer d.readerLock.Unlock()

	for {
		if len(d.buf) == 0 {
			select {
			case d.buf = <-d.parts:
				if d.buf == nil {
					return n, io.EOF
				}
			case <-d.closer:
				return n, io.ErrUnexpectedEOF
			}
		}

		if n == len(p) {
			return n, nil
		}

		copied := copy(p[n:], d.buf)
		d.buf = d.buf[copied:]

		n += copied
	}
}

func (d *dataStreamer) Close() error {
	d.closerLock.Lock()
	defer d.closerLock.Unlock()

	if !d.closed {
		d.closed = true
		close(d.closer)
	}

	return nil
}

func (d *dataStreamer) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	d.writerLock.Lock()
	defer d.writerLock.Unlock()

	if d.finished {
		return 0, io.ErrClosedPipe
	}

	tmp := make([]byte, len(data))
	copy(tmp, data)

	select {
	case d.parts <- tmp:
	case <-d.closer:
		return 0, io.ErrClosedPipe
	}

	return len(data), nil
}

func (d *dataStreamer) Finish() {
	d.writerLock.Lock()
	defer d.writerLock.Unlock()

	if !d.finished {
		d.finished = true
		close(d.parts)
	}
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

	rldpInfos map[string]*rldpInfo

	activeRequests map[string]*payloadStream
	mx             sync.RWMutex
}

func NewTransport(dht DHT, resolver Resolver) *Transport {
	t := &Transport{
		dht:            dht,
		resolver:       resolver,
		activeRequests: map[string]*payloadStream{},
		rldpInfos:      map[string]*rldpInfo{},
	}
	return t
}

func (t *Transport) connectRLDP(ctx context.Context, key ed25519.PublicKey, addr, id string) (RLDP, error) {
	a, err := Connector(ctx, addr, key, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to init adnl for rldp connection %s, err: %w", addr, err)
	}

	r := newRLDP(a)
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

func (t *Transport) getRLDPQueryHandler(r RLDP) func(query *rldp.Query) error {
	return func(query *rldp.Query) error {
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
			err = r.SendAnswer(ctx, query.MaxAnswerSize, query.ID, part)
			cancel()
			if err != nil {
				return fmt.Errorf("failed to send answer: %w", err)
			}

			return nil
		}
		return fmt.Errorf("unexpected query type %s", reflect.TypeOf(query.Data))
	}
}

func handleGetPart(req GetNextPayloadPart, stream *payloadStream) (*PayloadPart, error) {
	offset := int(req.Seqno * req.MaxChunkSize)
	if offset != stream.nextOffset {
		return nil, fmt.Errorf("incorrect offset %d, should be %d", offset, stream.nextOffset)
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

	if client == nil {
		panic(fmt.Sprintln(rlInfo.Resolved))
	}

	rlInfo.mx.Unlock()

	req := Request{
		ID:      qid,
		Method:  request.Method,
		URL:     request.URL.String(),
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
			// TODO: enable later, possible bug
			// dr.data = make([]byte, 0, httpResp.ContentLength)
		}

		go func() {
			seqno := int32(0)
			for withPayload {
				var part PayloadPart
				err = client.DoQuery(request.Context(), _RLDPMaxAnswerSize, GetNextPayloadPart{
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
	var adnlID []byte
	if strings.HasSuffix(host, ".adnl") {
		adnlID, err = ParseADNLAddress(host[:len(host)-5])
		if err != nil {
			return fmt.Errorf("failed to aprse adnl address %s, err: %w", host, err)
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

		adnlID = domain.GetSiteRecord()
	}

	addresses, pubKey, err := t.dht.FindAddresses(ctx, adnlID)
	if err != nil {
		return fmt.Errorf("failed to find address of %s (%s) in DHT, err: %w", host, hex.EncodeToString(adnlID), err)
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

var crc16table = crc16.MakeTable(crc16.CRC16_XMODEM)

func ParseADNLAddress(addr string) ([]byte, error) {
	if len(addr) != 55 {
		return nil, errors.New("wrong id length")
	}

	buf, err := base32.StdEncoding.DecodeString("F" + strings.ToUpper(addr))
	if err != nil {
		return nil, fmt.Errorf("failed to decode address: %w", err)
	}

	if buf[0] != 0x2d {
		return nil, errors.New("invalid first byte")
	}

	hash := binary.BigEndian.Uint16(buf[33:])
	calc := crc16.Checksum(buf[:33], crc16table)
	if hash != calc {
		return nil, errors.New("invalid address")
	}

	return buf[1:33], nil
}

func SerializeADNLAddress(addr []byte) (string, error) {
	a := append([]byte{0x2d}, addr...)

	crcBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(crcBytes, crc16.Checksum(a, crc16table))

	return strings.ToLower(base32.StdEncoding.EncodeToString(append(a, crcBytes...))[1:]), nil
}
