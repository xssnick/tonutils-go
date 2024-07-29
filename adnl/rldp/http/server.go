package http

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ADNLGateway interface {
	GetAddressList() address.List
	Close() error
	SetConnectionHandler(func(client adnl.Peer) error)
	SetExternalIP(ip net.IP)
	StartServer(listenAddr string) error
}

type Server struct {
	dht DHT

	id             []byte
	key            ed25519.PrivateKey
	handler        http.Handler
	rldpInfos      map[string]*rldpInfo
	activeRequests map[string]*payloadStream
	adnlServer     ADNLGateway
	externalIp     net.IP

	closer chan bool
	closed bool
	mx     sync.RWMutex

	Timeout time.Duration
}

type writerBuff struct {
	client RLDP

	server *Server
	stream *dataStreamer

	resp *respWriter

	headerSent bool
	handled    bool

	maxAnswerSz int64
	queryId     []byte
	requestId   []byte
	transferId  []byte

	mx sync.Mutex
}

type respWriter struct {
	writer     *bufio.Writer
	statusCode int
	headers    http.Header
}

var Logger = log.Println

var newServer = func(key ed25519.PrivateKey) ADNLGateway {
	return adnl.NewGateway(key)
}

func NewServer(key ed25519.PrivateKey, dht DHT, handler http.Handler) *Server {
	s := &Server{
		key:            key,
		dht:            dht,
		handler:        handler,
		rldpInfos:      map[string]*rldpInfo{},
		activeRequests: map[string]*payloadStream{},
		closer:         make(chan bool, 1),
		Timeout:        30 * time.Second,
		adnlServer:     newServer(key),
	}
	s.id, _ = tl.Hash(adnl.PublicKeyED25519{Key: s.key.Public().(ed25519.PublicKey)})
	return s
}

func (s *Server) SetExternalIP(ip net.IP) {
	s.adnlServer.SetExternalIP(ip)
}

func (s *Server) ListenAndServe(listenAddr string) error {
	go func() {
		for {
			select {
			case <-s.closer:
				return
			case <-time.After(5 * time.Second):
			}

			now := time.Now()

			s.mx.Lock()
			for k, stream := range s.activeRequests {
				if stream.ValidTill.Before(now) {
					delete(s.activeRequests, k)
					_ = stream.Data.Close()
				}
			}
			s.mx.Unlock()
		}
	}()

	go func() {
		wait := 1 * time.Second
		// refresh dht records
		for {
			select {
			case <-s.closer:
				return
			case <-time.After(wait):
			}

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
			err := s.updateDHT(ctx)
			cancel()

			if err != nil {
				Logger("DHT ADNL address record update failed: ", err, ". We will retry in 5 sec")

				// on err, retry sooner
				wait = 5 * time.Second
				continue
			}
			wait = 1 * time.Minute
		}
	}()

	s.adnlServer.SetConnectionHandler(func(client adnl.Peer) error {
		adnlAddr, err := SerializeADNLAddress(client.GetID())
		if err != nil {
			return err
		}

		previousHandler := client.GetQueryHandler()
		client.SetQueryHandler(func(query *adnl.MessageQuery) error {
			switch query.Data.(type) {
			case GetCapabilities:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := client.Answer(ctx, query.ID, &Capabilities{Value: 0}) // CapabilityRLDP2
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

		rl := newRLDP(client, false) // server supports both v2 and v1 by default
		rl.SetOnQuery(s.handle(rl, adnlAddr, client.RemoteAddr()))
		return nil
	})

	err := s.adnlServer.StartServer(listenAddr)
	if err != nil {
		_ = s.Stop()
		return err
	}

	<-s.closer
	return nil
}

func (s *Server) Address() []byte {
	return s.id
}

func (s *Server) updateDHT(ctx context.Context) error {
	addr := s.adnlServer.GetAddressList()

	ctxStore, cancel := context.WithTimeout(ctx, 80*time.Second)
	stored, id, err := s.dht.StoreAddress(ctxStore, addr, 15*time.Minute, s.key, 5)
	cancel()
	if err != nil && stored == 0 {
		return err
	}

	// make sure it was saved
	_, _, err = s.dht.FindAddresses(ctx, id)
	if err != nil {
		return err
	}

	Logger("DHT ADNL address record for TON Site was refreshed successfully on", stored,
		"nodes to ip", addr.Addresses[0].IP.String(), "with port", addr.Addresses[0].Port)
	return nil
}

func (s *Server) Stop() (err error) {
	s.mx.Lock()
	defer s.mx.Unlock()

	if !s.closed {
		s.closed = true
		close(s.closer)
		s.dht.Close()

		if s.adnlServer != nil {
			err = s.adnlServer.Close()
		}
	}
	return
}

func (s *Server) handle(client RLDP, adnlId, addr string) func(transferId []byte, msg *rldp.Query) error {
	netAddr := net.UDPAddrFromAddrPort(netip.MustParseAddrPort(addr))

	return func(transferId []byte, query *rldp.Query) error {
		switch req := query.Data.(type) {
		case Request:
			uri, err := url.Parse(req.URL)
			if err != nil {
				return fmt.Errorf("failed to parse url `%s`: %w", uri, err)
			}
			uri.Scheme = "http"

			contentLen := int64(-1)
			headers := http.Header{}
			for _, header := range req.Headers {
				name := http.CanonicalHeaderKey(header.Name)
				if name == "Host" {
					uri.Host = header.Value
				} else if name == "Content-Length" {
					contentLen, err = strconv.ParseInt(header.Value, 10, 64)
					if err != nil {
						return fmt.Errorf("failed to parse content len `%s`: %w", header.Value, err)
					}

					if contentLen < 0 {
						return fmt.Errorf("failed to parse content len: should be >= 0")
					}
				}

				headers[name] = append(headers[name], header.Value)
			}
			headers.Set("X-Adnl-Ip", netAddr.IP.String())
			headers.Set("X-Adnl-Id", adnlId)

			ctx, cancel := context.WithTimeout(context.Background(), s.Timeout)
			defer cancel()

			reqBody := newDataStreamer()
			if req.Method == "CONNECT" ||
				len(headers["Content-Length"]) > 0 ||
				len(headers["Transfer-Encoding"]) > 0 {
				// request should have payload, fetch it in parallel and write to stream
				go func() {
					err = s.fetchPayload(ctx, req.ID, client, reqBody)
					if err != nil {
						reqBody.Close()
						return
					}
					reqBody.Finish()
				}()
			} else {
				reqBody.Finish()
			}

			httpReq := &http.Request{
				Method:        req.Method,
				URL:           uri,
				Proto:         req.Version,
				ProtoMajor:    1,
				ProtoMinor:    1,
				Header:        headers,
				Body:          reqBody,
				ContentLength: contentLen,
				Host:          uri.Host,
				RemoteAddr:    netAddr.IP.String(),
				RequestURI:    uri.RequestURI(),
			}

			stream := newDataStreamer()

			wb := &writerBuff{
				client:      client,
				server:      s,
				stream:      stream,
				maxAnswerSz: query.MaxAnswerSize,
				queryId:     query.ID,
				requestId:   req.ID,
				transferId:  transferId,
			}

			w := &respWriter{
				writer:  bufio.NewWriterSize(wb, 4096),
				headers: map[string][]string{},
			}
			wb.resp = w

			s.handler.ServeHTTP(w, httpReq)
			wb.handled = true
			// flush write buffer, to commit data
			err = w.writer.Flush()

			// if no data was committed - it will send empty response
			err = wb.flush(nil)
			if err != nil {
				return fmt.Errorf("failed to flush response for `%s`: %w", uri, err)
			}
			stream.Finish()
		case GetNextPayloadPart:
			s.mx.RLock()
			stream := s.activeRequests[hex.EncodeToString(req.ID)]
			s.mx.RUnlock()

			if stream == nil {
				return fmt.Errorf("unknown request id %s", hex.EncodeToString(req.ID))
			}

			part, err := handleGetPart(req, stream)
			if err != nil {
				return fmt.Errorf("handle part err: %w", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), s.Timeout)
			err = client.SendAnswer(ctx, query.MaxAnswerSize, query.ID, transferId, part)
			cancel()
			if err != nil {
				return fmt.Errorf("failed to send answer: %w", err)
			}

			if part.IsLast {
				s.mx.Lock()
				delete(s.activeRequests, hex.EncodeToString(req.ID))
				s.mx.Unlock()
				_ = stream.Data.Close()
			}
		}
		return nil
	}
}

func (s *Server) fetchPayload(ctx context.Context, requestID []byte, client RLDP, w io.Writer) error {
	var seqno int32 = 0
	last := false
	for !last {
		var part PayloadPart
		err := client.DoQuery(ctx, _RLDPMaxAnswerSize, GetNextPayloadPart{
			ID:           requestID,
			Seqno:        seqno,
			MaxChunkSize: _ChunkSize,
		}, &part)
		if err != nil {
			return err
		}

		last = part.IsLast
		_, err = w.Write(part.Data)
		if err != nil {
			return err
		}

		seqno++
	}
	return nil
}

func (r *respWriter) Header() http.Header {
	return r.headers
}

func (r *respWriter) Write(bytes []byte) (int, error) {
	return r.writer.Write(bytes)
}

func (r *respWriter) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}

func (w *writerBuff) Write(bytes []byte) (n int, err error) {
	if len(bytes) == 0 {
		return 0, nil
	}

	w.mx.Lock()
	defer w.mx.Unlock()

	if err := w.flush(bytes); err != nil {
		return 0, err
	}

	return w.stream.Write(bytes)
}

func (w *writerBuff) flush(payload []byte) error {
	if w.headerSent {
		return nil
	}
	w.headerSent = true

	if w.handled {
		// if it is first and last write - we can define content length
		if !strings.Contains(strings.ToLower(w.resp.headers.Get("Transfer-Encoding")), "chunked") {
			w.resp.headers.Set("Content-Length", fmt.Sprint(len(payload)))
		}
	} else {
		if w.resp.headers.Get("Content-Length") == "" && w.resp.headers.Get("Transfer-Encoding") == "" {
			// if it is not last write (flush inside handler), we use chunked transfer
			w.resp.headers.Set("Transfer-Encoding", "chunked")
		}
	}

	if w.resp.statusCode <= 0 {
		w.resp.statusCode = 200
	}

	var headers []Header
	for k, v := range w.resp.headers {
		for _, hdr := range v {
			headers = append(headers, Header{
				Name:  k,
				Value: hdr,
			})
		}
	}

	if len(payload) > 0 {
		w.server.mx.Lock()
		w.server.activeRequests[hex.EncodeToString(w.requestId)] = &payloadStream{
			Data:      w.stream,
			ValidTill: time.Now().Add(w.server.Timeout),
		}
		w.server.mx.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	err := w.client.SendAnswer(ctx, w.maxAnswerSz, w.queryId, w.transferId, Response{
		Version:    "HTTP/1.1",
		StatusCode: int32(w.resp.statusCode),
		Reason:     http.StatusText(w.resp.statusCode),
		Headers:    headers,
		NoPayload:  len(payload) == 0,
	})
	cancel()
	if err != nil {
		_ = w.stream.Close()
		return fmt.Errorf("failed to send response for %s query: %w", hex.EncodeToString(w.queryId), err)
	}

	return nil
}
