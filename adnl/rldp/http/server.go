package http

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type ADNLServer interface {
	ListenAndServe(listenAddr string) (err error)
	Close() error
	SetConnectionHandler(func(client adnl.Client) error)
}

type Server struct {
	dht DHT

	handler        http.Handler
	rldpInfos      map[string]*rldpInfo
	activeRequests map[string]*payloadStream
	adnlServer     ADNLServer

	closer chan bool
	closed bool
	mx     sync.RWMutex

	Timeout time.Duration
}

type respWriter struct {
	server *Server
	writer *dataStreamer

	maxAnswerSz int64
	queryID     []byte
	requestID   []byte

	statusCode int

	client  RLDP
	headers http.Header

	hasPayload bool
	headerSent bool

	mx sync.Mutex
}

func HandleRequests(adnlServer ADNLServer, handler http.Handler) *Server {
	s := &Server{
		dht:            nil,
		handler:        handler,
		adnlServer:     adnlServer,
		rldpInfos:      map[string]*rldpInfo{},
		activeRequests: map[string]*payloadStream{},
		closer:         make(chan bool, 1),
		Timeout:        30 * time.Second,
	}

	adnlServer.SetConnectionHandler(func(client adnl.Client) error {
		rl := newRLDP(client)
		rl.SetOnQuery(s.handle(rl, client.RemoteAddr()))
		return nil
	})

	go func() {
		for {
			select {
			case <-s.closer:
				return
			default:
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

	return s
}

func (s *Server) Stop() error {
	s.mx.Lock()
	defer s.mx.Unlock()

	if !s.closed {
		close(s.closer)
		return s.adnlServer.Close()
	}

	return nil
}

func (s *Server) handle(client RLDP, addr string) func(msg *rldp.Query) error {
	return func(query *rldp.Query) error {
		switch req := query.Data.(type) {
		case Request:
			uri, err := url.Parse(req.URL)
			if err != nil {
				return fmt.Errorf("failed to parse url `%s`: %w", uri, err)
			}

			contentLen := int64(-1)
			headers := http.Header{}
			for _, header := range req.Headers {
				if header.Name == "Content-Length" {
					contentLen, err = strconv.ParseInt(header.Value, 10, 64)
					if err != nil {
						return fmt.Errorf("failed to parse content len `%s`: %w", header.Value, err)
					}

					if contentLen < 0 {
						return fmt.Errorf("failed to parse content len: should be >= 0")
					}
				}
				headers[header.Name] = append(headers[header.Name], header.Value)
			}

			w := &respWriter{
				server:      s,
				writer:      newDataStreamer(),
				maxAnswerSz: query.MaxAnswerSize,
				queryID:     query.ID,
				requestID:   req.ID,
				client:      client,
				headers:     map[string][]string{},
			}

			httpReq := &http.Request{
				Method:        req.Method,
				URL:           uri,
				Proto:         req.Version,
				ProtoMajor:    1,
				ProtoMinor:    1,
				Header:        headers,
				Body:          w.writer,
				ContentLength: contentLen,
				Host:          uri.Host,
				RemoteAddr:    addr,
				RequestURI:    uri.RequestURI(),
			}

			s.handler.ServeHTTP(w, httpReq)

			err = w.flush()
			if err != nil {
				return fmt.Errorf("failed to flush response for `%s`: %w", uri, err)
			}
			w.writer.Finish()
		case GetNextPayloadPart:
			s.mx.RLock()
			stream := s.activeRequests[hex.EncodeToString(req.ID)]
			s.mx.RUnlock()

			if stream == nil {
				return fmt.Errorf("unknown request id")
			}

			part, err := handleGetPart(req, stream)
			if err != nil {
				return fmt.Errorf("handle part err: %w", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), s.Timeout)
			err = client.SendAnswer(ctx, query.MaxAnswerSize, query.ID, part)
			cancel()
			if err != nil {
				return fmt.Errorf("failed to send answer: %w", err)
			}
		}

		return nil
	}
}

func (r *respWriter) Header() http.Header {
	return r.headers
}

func (r *respWriter) Write(bytes []byte) (int, error) {
	if len(bytes) == 0 {
		return 0, nil
	}

	r.hasPayload = true

	if err := r.flush(); err != nil {
		println("FLERR", err)

		return 0, err
	}

	return r.writer.Write(bytes)
}

func (r *respWriter) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}

func (r *respWriter) flush() error {
	r.mx.Lock()
	defer r.mx.Unlock()

	if !r.headerSent {
		r.headerSent = true

		if r.statusCode <= 0 {
			r.statusCode = 200
		}

		var headers []Header
		for k, v := range r.headers {
			for _, hdr := range v {
				headers = append(headers, Header{
					Name:  k,
					Value: hdr,
				})
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := r.client.SendAnswer(ctx, r.maxAnswerSz, r.queryID, Response{
			Version:    "HTTP/1.1",
			StatusCode: int32(r.statusCode),
			Reason:     http.StatusText(r.statusCode),
			Headers:    headers,
			NoPayload:  !r.hasPayload,
		})
		cancel()
		if err != nil {
			_ = r.writer.Close()
			return fmt.Errorf("failed to send response for %s query: %w", hex.EncodeToString(r.queryID), err)
		}

		if r.hasPayload {
			r.server.mx.Lock()
			r.server.activeRequests[hex.EncodeToString(r.requestID)] = &payloadStream{
				Data:      r.writer,
				ValidTill: time.Now().Add(r.server.Timeout),
			}
			r.server.mx.Unlock()
		}
	}

	return nil
}
