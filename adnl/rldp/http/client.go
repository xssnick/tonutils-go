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
	FindAddresses(ctx context.Context, key []byte) (*address.List, ed25519.PublicKey, error)
}

type Resolver interface {
	Resolve(ctx context.Context, domain string) (*dns.Domain, error)
}

type payloadStream struct {
	Data      []byte
	StartTime time.Time
}

type dataReader struct {
	finished bool
	data     []byte
	mx       sync.Mutex
}

func (d *dataReader) Read(p []byte) (n int, err error) {
	d.mx.Lock()
	defer d.mx.Unlock()

	if d.finished && len(d.data) == 0 {
		return -1, io.EOF
	}

	n = copy(p, d.data)
	d.data = d.data[n:]
	return n, nil
}

func (d *dataReader) Close() error {
	d.mx.Lock()
	defer d.mx.Unlock()

	d.finished = true
	d.data = nil
	return nil
}

func (d *dataReader) pushData(data []byte, last bool) {
	d.mx.Lock()
	defer d.mx.Unlock()

	if d.finished {
		return
	}

	if data != nil {
		d.data = append(d.data, data...)
	}

	d.finished = last
}

type rldpInfo struct {
	mx           sync.RWMutex
	ActiveClient *rldp.RLDP

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

	mx2 sync.Mutex
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

func (t *Transport) connectRLDP(ctx context.Context, key ed25519.PublicKey, addr, id string) (*rldp.RLDP, error) {
	println("CONNECT", addr, id)
	a, err := adnl.NewADNL(key)
	if err != nil {
		return nil, fmt.Errorf("failed to init adnl for rldp connection %s, err: %w", addr, err)
	}

	if err = a.Connect(ctx, addr); err != nil {
		return nil, fmt.Errorf("failed to connect adnl for rldp connection %s, err: %w", addr, err)
	}

	r := rldp.NewRLDP(a, id)

	// save rldp client to reuse for next requests
	// t.mx.Lock()
	// t.rldpClients[id] = r
	// t.mx.Unlock()

	r.SetOnQuery(t.getRLDPQueryHandler(r))
	r.SetOnDisconnect(t.removeRLDP)

	return r, nil
}

func (t *Transport) removeRLDP(rl *rldp.RLDP, id string) {
	t.mx.RLock()
	r := t.rldpInfos[id]
	t.mx.RUnlock()

	if r != nil {
		r.destroyClient(rl)
	}
}

func (r *rldpInfo) destroyClient(rl *rldp.RLDP) {
	rl.Close()

	r.mx.Lock()
	if r.ActiveClient == rl {
		r.ActiveClient = nil
	}
	r.mx.Unlock()
}

func (t *Transport) getRLDPQueryHandler(r *rldp.RLDP) func(query *rldp.Query) error {
	return func(query *rldp.Query) error {
		switch req := query.Data.(type) {
		case GetNextPayloadPart:
			t.mx.Lock()
			stream := t.activeRequests[hex.EncodeToString(req.ID)]
			t.mx.Unlock()

			if stream == nil {
				return fmt.Errorf("unknown request id")
			}

			offset := int(req.Seqno * req.MaxChunkSize)
			if offset >= len(stream.Data) {
				return fmt.Errorf("too big offset for strea data size %d", offset)
			}

			till := len(stream.Data)
			if offset+int(req.MaxChunkSize) < till {
				till = offset + int(req.MaxChunkSize)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := r.SendAnswer(ctx, query.MaxAnswerSize, query.ID, PayloadPart{
				Data:    stream.Data[offset:till],
				Trailer: nil, // TODO: trailer
				IsLast:  till == len(stream.Data),
			})
			cancel()
			if err != nil {
				return fmt.Errorf("failed to send answer: %w", err)
			}

			return nil
		}
		return fmt.Errorf("unexpected query type %s", reflect.TypeOf(query.Data))
	}
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

	var client *rldp.RLDP
	rlInfo.mx.Lock()

	client = rlInfo.ActiveClient
	if rlInfo.Resolved && client == nil {
		client, err = t.connectRLDP(request.Context(), rlInfo.ID, rlInfo.Addr, request.Host)
		if err != nil {
			// resolve again
			rlInfo.Resolved = false
		}
		rlInfo.ActiveClient = client
	}

	if !rlInfo.Resolved {
		err = t.resolveRLDP(request.Context(), rlInfo, request.Host)
		if err != nil {
			rlInfo.mx.Unlock()
			return nil, err
		}
		client = rlInfo.ActiveClient
	}
	rlInfo.mx.Unlock()

	// TODO: async stream body for req
	if request.Body != nil {
		defer request.Body.Close()
	}

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
		// TODO: stream
		data, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}

		t.mx.Lock()
		t.activeRequests[hex.EncodeToString(qid)] = &payloadStream{
			Data:      data,
			StartTime: time.Now(),
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

	dr := &dataReader{}
	httpResp.Body = dr

	if withPayload {
		go func() {
			if httpResp.ContentLength > 0 && httpResp.ContentLength < (1<<22) {
				dr.data = make([]byte, 0, httpResp.ContentLength)
			}

			seqno := int32(0)
			for withPayload {
				//httpResp.Write(os.Stdout)
				// println("P PART", seqno, httpResp.StatusCode, httpResp.ContentLength)

				var part PayloadPart
				err = client.DoQuery(request.Context(), _RLDPMaxAnswerSize, GetNextPayloadPart{
					ID:           qid,
					Seqno:        seqno,
					MaxChunkSize: _ChunkSize,
				}, &part)
				if err != nil {
					_ = dr.Close()
					// client.Close()
					return
				}

				for _, tr := range part.Trailer {
					httpResp.Trailer[tr.Name] = []string{tr.Value}
				}

				withPayload = !part.IsLast
				dr.pushData(part.Data, part.IsLast)

				seqno++
			}

			// client.Close()
			//rlInfo.mx.Lock()
			//rlInfo.ActiveClients.push(client)
			//rlInfo.mx.Unlock()
		}()
	} else {
		dr.pushData(nil, true)
		// client.Close()
		//rlInfo.mx.Lock()
		//rlInfo.ActiveClients.push(client)
		//rlInfo.mx.Unlock()
	}

	return httpResp, nil
}

func (t *Transport) resolveRLDP(ctx context.Context, info *rldpInfo, host string) (err error) {
	var adnlID []byte
	if strings.HasSuffix(host, ".adnl") {
		adnlID, err = parseADNLAddress(host[:len(host)-5])
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

		// find working rld node addr
		client, err := t.connectRLDP(ctx, pubKey, addr, host)
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

func parseADNLAddress(addr string) ([]byte, error) {
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
	calc := crc16.Checksum(buf[:33], crc16.MakeTable(crc16.CRC16_XMODEM))
	if hash != calc {
		return nil, errors.New("invalid address")
	}

	return buf[:32], nil
}
