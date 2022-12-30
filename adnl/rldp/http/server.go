package http

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ADNLServer interface {
	GetAddressList() address.List
	ListenAndServe(listenAddr string) (err error)
	Close() error
	SetConnectionHandler(func(client adnl.Client) error)
}

type Server struct {
	dht DHT

	initDate        int32
	id              []byte
	key             ed25519.PrivateKey
	handler         http.Handler
	rldpInfos       map[string]*rldpInfo
	activeRequests  map[string]*payloadStream
	adnlServer      ADNLServer
	virtualListener *httpListener

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

	client RLDP
}

var Logger = log.Println

var newServer = func(key ed25519.PrivateKey) ADNLServer {
	return adnl.NewServer(key)
}

func NewServer(key ed25519.PrivateKey, dht DHT, handler http.Handler) *Server {
	s := &Server{
		key:             key,
		dht:             dht,
		handler:         handler,
		initDate:        int32(time.Now().Unix()),
		adnlServer:      newServer(key),
		rldpInfos:       map[string]*rldpInfo{},
		activeRequests:  map[string]*payloadStream{},
		closer:          make(chan bool, 1),
		virtualListener: newVirtualHttpListener(),
		Timeout:         30 * time.Second,
	}

	s.adnlServer.SetConnectionHandler(func(client adnl.Client) error {
		rl := newRLDP(client)
		rl.SetOnQuery(s.handle(rl, client.RemoteAddr()))
		return nil
	})
	s.id, _ = adnl.ToKeyID(adnl.PublicKeyED25519{Key: s.key.Public().(ed25519.PublicKey)})

	return s
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
		wait := 2 * time.Second
		// refresh dht records
		for {
			select {
			case <-s.closer:
				return
			case <-time.After(wait):
			}

			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			err := s.updateDHT(ctx)
			cancel()

			if err != nil {
				// on err, retry sooner
				wait = 5 * time.Second
				continue
			}
			wait = 3 * time.Minute
		}
	}()

	// it will handle all http logic, like transfer encodings etc.
	// we will put it into mock network connected to adnl wrapper
	httpSrv := http.Server{
		Handler: s.handler,
	}

	go func() {
		_ = httpSrv.Serve(s.virtualListener)
		_ = s.Stop()
	}()

	if err := s.adnlServer.ListenAndServe(listenAddr); err != nil {
		_ = s.Stop()
		return err
	}
	return nil
}

func (s *Server) Address() []byte {
	return s.id
}

func (s *Server) updateDHT(ctx context.Context) error {
	addr := s.adnlServer.GetAddressList()

	id, err := s.dht.StoreAddress(ctx, addr, 10*time.Minute, s.key, 3)
	if err != nil {
		return err
	}

	// make sure it was saved
	_, _, err = s.dht.FindAddresses(ctx, id)
	if err != nil {
		return err
	}

	Logger("DHT address record for ADNL site was updated successfully to ", addr.Addresses[0].IP.String(), addr.Addresses[0].Port)
	return nil
}

func (s *Server) Stop() error {
	s.mx.Lock()
	defer s.mx.Unlock()

	if !s.closed {
		close(s.closer)
		s.virtualListener.Close()
		s.dht.Close()
		return s.adnlServer.Close()
	}

	return nil
}

func (s *Server) handle(client RLDP, addr string) func(msg *rldp.Query) error {
	netAddr := net.UDPAddrFromAddrPort(netip.MustParseAddrPort(addr))

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
			// force http server to close writer after request processing,
			// it will allow us to detect the end of the data stream
			headers["Connection"] = []string{"close"}

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
				ProtoMinor:    0,
				Header:        headers,
				Body:          reqBody,
				ContentLength: contentLen,
				Host:          uri.Host,
				RemoteAddr:    addr,
				RequestURI:    uri.RequestURI(),
			}

			virtualNetR, virtualNetW := newDataStreamer(), newDataStreamer()

			w := &respWriter{
				server:      s,
				writer:      newDataStreamer(),
				maxAnswerSz: query.MaxAnswerSize,
				queryID:     query.ID,
				requestID:   req.ID,
				client:      client,
			}

			// add connection to fake net of http server
			s.virtualListener.addConn(netAddr, virtualNetR, virtualNetW)
			defer virtualNetR.Close()
			defer virtualNetW.Close()

			err = httpReq.Write(virtualNetR)
			if err != nil {
				return fmt.Errorf("failed to write request to virtual net: %w", err)
			}
			// mark the end of request for server by freeing reader, so it can start processing
			virtualNetR.FlushReader()

			err = w.forwardResponse(virtualNetW)
			if err != nil {
				return fmt.Errorf("failed to forward response to target: %w", err)
			}
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
			err = client.SendAnswer(ctx, query.MaxAnswerSize, query.ID, part)
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

func (r *respWriter) forwardResponse(source *dataStreamer) error {
	var requestData []byte
	var headerSent bool

	var buf = make([]byte, 4096)
	finished := false
	for !finished {
		n, err := source.Read(buf)
		if err != nil {
			if err != io.EOF {
				r.writer.Close()
				return fmt.Errorf("failed to read resp of virtual net: %w", err)
			}
			finished = true
		}

		res := buf[:n]

		if !headerSent {
			requestData = append(requestData, res...)
			// find end of header
			pos := bytes.Index(requestData, []byte("\r\n\r\n"))
			if pos == -1 {
				continue
			}

			headerSent = true
			res = requestData[pos+4:]
			requestData = requestData[:pos]
			hasPayload := !finished || len(res) > 0

			if hasPayload {
				r.server.mx.Lock()
				r.server.activeRequests[hex.EncodeToString(r.requestID)] = &payloadStream{
					Data:      r.writer,
					ValidTill: time.Now().Add(r.server.Timeout),
				}
				r.server.mx.Unlock()
			}

			err = r.sendHeader(string(requestData), hasPayload)
			if err != nil {
				r.writer.Close()
				return fmt.Errorf("failed to send header: %w", err)
			}

			if len(res) == 0 {
				continue
			}
		}

		_, err = r.writer.Write(res)
		if err != nil {
			return fmt.Errorf("failed to writer resp to target stream: %w", err)
		}
	}
	r.writer.Finish()

	return nil
}

func (r *respWriter) sendHeader(data string, hasPayload bool) error {
	resp := Response{
		NoPayload: !hasPayload,
	}

	lines := strings.Split(data, "\r\n")
	for i, line := range lines {
		if i == 0 {
			str := strings.SplitN(line, " ", 3)

			if len(str) != 3 {
				return fmt.Errorf("invalid first header line: %s", line)
			}

			statusCode, err := strconv.Atoi(str[1])
			if err != nil {
				return fmt.Errorf("invalid status code")
			}

			resp.Version = str[0]
			resp.StatusCode = int32(uint16(statusCode))
			resp.Reason = str[2]
			continue
		}

		str := strings.SplitN(line, ": ", 2)
		if len(str) != 2 {
			return fmt.Errorf("invalid header at %d line: %s", i, line)
		}

		resp.Headers = append(resp.Headers, Header{
			Name:  str[0],
			Value: str[1],
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	err := r.client.SendAnswer(ctx, r.maxAnswerSz, r.queryID, resp)
	cancel()
	if err != nil {
		return fmt.Errorf("failed to send response for %s query: %w", hex.EncodeToString(r.queryID), err)
	}

	return nil
}
