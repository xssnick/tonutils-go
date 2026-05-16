package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

const (
	defaultListenAddr = "0.0.0.0:30000"
)

type DownloadRequest struct {
	Size uint64 `tl:"long"`
	Seed uint64 `tl:"long"`
}

type DownloadResponse struct {
	Data []byte `tl:"bytes"`
	Hash []byte `tl:"int256"`
}

type UploadRequest struct {
	Seed uint64 `tl:"long"`
	Data []byte `tl:"bytes"`
	Hash []byte `tl:"int256"`
}

type UploadResponse struct {
	Size uint64 `tl:"long"`
	Hash []byte `tl:"int256"`
	OK   bool   `tl:"bool"`
}

type ChatRequest struct {
	Text   string `tl:"string"`
	SentAt int64  `tl:"long"`
}

type ChatResponse struct {
	Text     string `tl:"string"`
	RecvAt   int64  `tl:"long"`
	RemoteID []byte `tl:"int256"`
}

func init() {
	tl.Register(DownloadRequest{}, "tonutils.rldpBench.download size:long seed:long = tonutils.rldpBench.Download")
	tl.Register(DownloadResponse{}, "tonutils.rldpBench.downloadResult data:bytes hash:int256 = tonutils.rldpBench.DownloadResult")
	tl.Register(UploadRequest{}, "tonutils.rldpBench.upload seed:long data:bytes hash:int256 = tonutils.rldpBench.Upload")
	tl.Register(UploadResponse{}, "tonutils.rldpBench.uploadResult size:long hash:int256 ok:Bool = tonutils.rldpBench.UploadResult")
	tl.Register(ChatRequest{}, "tonutils.rldpBench.chat text:string sent_at:long = tonutils.rldpBench.Chat")
	tl.Register(ChatResponse{}, "tonutils.rldpBench.chatResult text:string recv_at:long remote_id:int256 = tonutils.rldpBench.ChatResult")
}

type byteSize uint64

func (b *byteSize) Set(s string) error {
	v, err := parseByteSize(s)
	if err != nil {
		return err
	}
	*b = byteSize(v)
	return nil
}

func (b byteSize) String() string {
	return formatBytes(uint64(b))
}

type config struct {
	mode           string
	listen         string
	peerAddr       string
	peerPub        string
	keyHex         string
	keySeed        string
	text           string
	duration       time.Duration
	timeout        time.Duration
	reportInterval time.Duration
	size           byteSize
	maxRequestSize byteSize
	partSize       byteSize
	symbolSize     byteSize
	minRate        byteSize
	maxRate        byteSize
	rrLimit        byteSize
	parallel       int
	threads        int
	verify         bool
	multiFEC       bool
	debug          bool
}

type benchStats struct {
	name      string
	bytes     atomic.Uint64
	ok        atomic.Uint64
	fail      atomic.Uint64
	latencyNS atomic.Uint64
	maxNS     atomic.Uint64
}

type serverStats struct {
	downloadBytes atomic.Uint64
	uploadBytes   atomic.Uint64
	downloadReqs  atomic.Uint64
	uploadReqs    atomic.Uint64
	chatReqs      atomic.Uint64
	fail          atomic.Uint64
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg := parseFlags()
	if err := applyTuning(cfg); err != nil {
		log.Fatal(err)
	}
	if cfg.debug {
		adnl.Logger = log.Println
		rldp.Logger = log.Println
		rldp.BBRLogger = log.Println
	}

	priv, err := loadKey(cfg.keyHex, cfg.keySeed)
	if err != nil {
		log.Fatalf("failed to prepare key: %v", err)
	}

	gateway := adnl.NewGateway(priv)
	defer func() { _ = gateway.Close() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch cfg.mode {
	case "serve":
		err = runServer(ctx, gateway, cfg)
	case "download", "upload", "bidi", "chat", "adnl-ping":
		err = runClient(ctx, gateway, cfg)
	default:
		err = fmt.Errorf("unsupported mode %q", cfg.mode)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func parseFlags() *config {
	cfg := &config{
		size:           byteSize(64 << 20),
		maxRequestSize: byteSize(1 << 30),
		partSize:       byteSize(rldp.PartSize),
		symbolSize:     byteSize(rldp.DefaultSymbolSize),
		minRate:        byteSize(rldp.MinRateBytesSec),
		maxRate:        byteSize(rldp.MaxRateBytesSec),
		rrLimit:        byteSize(rldp.RoundRobinFECLimit),
	}

	flag.StringVar(&cfg.mode, "mode", "serve", "serve, download, upload, bidi, chat, adnl-ping")
	flag.StringVar(&cfg.listen, "listen", defaultListenAddr, "listen UDP address for serve mode")
	flag.StringVar(&cfg.peerAddr, "peer", "", "remote UDP address host:port for client modes")
	flag.StringVar(&cfg.peerPub, "peer-pub", "", "remote ed25519 public key hex for client modes")
	flag.StringVar(&cfg.keyHex, "key", "", "ed25519 seed/private key hex; 32-byte seed or 64-byte private key")
	flag.StringVar(&cfg.keySeed, "key-seed", "", "derive deterministic ed25519 key from this text")
	flag.StringVar(&cfg.text, "text", "hello over RLDP", "chat text for -mode chat")
	flag.DurationVar(&cfg.duration, "duration", 20*time.Second, "benchmark duration; 0 means until interrupted")
	flag.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "per RLDP query timeout")
	flag.DurationVar(&cfg.reportInterval, "report", time.Second, "report interval")
	flag.Var(&cfg.size, "size", "payload size per query, accepts B/KiB/MiB/GiB suffixes")
	flag.Var(&cfg.maxRequestSize, "max-request-size", "server-side maximum accepted payload size")
	flag.Var(&cfg.partSize, "part-size", "RLDP part size")
	flag.Var(&cfg.symbolSize, "symbol-size", "RLDP FEC symbol size; keep below UDP MTU")
	flag.Var(&cfg.minRate, "min-rate", "RLDP BBR minimum pacing rate, bytes/sec")
	flag.Var(&cfg.maxRate, "max-rate", "RLDP BBR maximum pacing rate, bytes/sec")
	flag.Var(&cfg.rrLimit, "rr-limit", "payload limit for round-robin FEC when -multi-fec is enabled")
	flag.IntVar(&cfg.parallel, "parallel", runtime.NumCPU(), "parallel workers")
	flag.IntVar(&cfg.threads, "threads", runtime.NumCPU(), "ADNL packet processing goroutines")
	flag.BoolVar(&cfg.verify, "verify", true, "verify payload hashes")
	flag.BoolVar(&cfg.multiFEC, "multi-fec", false, "enable RLDP MultiFECMode")
	flag.BoolVar(&cfg.debug, "debug", false, "enable ADNL/RLDP debug logs")
	flag.Parse()

	return cfg
}

func applyTuning(cfg *config) error {
	if cfg.parallel <= 0 {
		return errors.New("parallel should be positive")
	}
	if cfg.threads <= 0 {
		return errors.New("threads should be positive")
	}
	if cfg.reportInterval <= 0 {
		return errors.New("report interval should be positive")
	}
	if cfg.timeout <= 0 {
		return errors.New("timeout should be positive")
	}
	if uint64(cfg.partSize) > math.MaxUint32 {
		return errors.New("part-size is too large")
	}
	if uint64(cfg.symbolSize) > math.MaxUint32 {
		return errors.New("symbol-size is too large")
	}
	if uint64(cfg.rrLimit) > math.MaxUint32 {
		return errors.New("rr-limit is too large")
	}
	if uint64(cfg.symbolSize) == 0 {
		return errors.New("symbol-size should be positive")
	}
	if uint64(cfg.size) > uint64(math.MaxInt) {
		return errors.New("size is too large for this process")
	}
	if uint64(cfg.maxRequestSize) > uint64(math.MaxInt) {
		return errors.New("max-request-size is too large for this process")
	}
	if uint64(cfg.symbolSize) > 1200 {
		log.Printf("warning: symbol-size=%s may exceed the current ADNL UDP read buffer budget", cfg.symbolSize)
	}

	rldp.PartSize = uint32(cfg.partSize)
	rldp.DefaultSymbolSize = uint32(cfg.symbolSize)
	rldp.MinRateBytesSec = int64(cfg.minRate)
	rldp.MaxRateBytesSec = int64(cfg.maxRate)
	rldp.MultiFECMode = cfg.multiFEC
	rldp.RoundRobinFECLimit = uint32(cfg.rrLimit)

	if uint64(cfg.maxRequestSize) > uint64(rldp.MaxUnexpectedTransferSize) {
		rldp.MaxUnexpectedTransferSize = uint64(cfg.maxRequestSize)
	}
	if uint64(cfg.partSize) > uint64(rldp.MaxFECDataSize) {
		rldp.MaxFECDataSize = uint32(cfg.partSize)
	}

	return nil
}

func runServer(ctx context.Context, gateway *adnl.Gateway, cfg *config) error {
	stats := &serverStats{}

	gateway.SetConnectionHandler(func(peer adnl.Peer) error {
		log.Printf("peer connected remote=%s pub=%s id=%s", peer.RemoteAddr(), hex.EncodeToString(peer.GetPubKey()), hex.EncodeToString(peer.GetID()))

		conn := rldp.NewClientV2(peer)
		conn.SetOnQuery(func(transferID []byte, query *rldp.Query) error {
			err := handleQuery(ctx, conn, cfg, stats, transferID, query)
			if err != nil {
				stats.fail.Add(1)
			}
			return err
		})

		return nil
	})

	if err := gateway.StartServer(cfg.listen, cfg.threads); err != nil {
		return fmt.Errorf("failed to start ADNL server: %w", err)
	}

	log.Printf("server ready listen=%s pub=%s adnl_id=%s", cfg.listen, hex.EncodeToString(gateway.GetPublicKey()), hex.EncodeToString(gateway.GetID()))
	log.Printf("tuning part=%s symbol=%s min_rate=%s/s max_rate=%s/s multi_fec=%v threads=%d max_request=%s",
		cfg.partSize, cfg.symbolSize, cfg.minRate, cfg.maxRate, cfg.multiFEC, cfg.threads, cfg.maxRequestSize)

	reportCtx, cancelReport := context.WithCancel(ctx)
	stopReport := reportServer(reportCtx, gateway, stats, cfg.reportInterval)
	defer func() {
		cancelReport()
		stopReport()
	}()

	if cfg.duration > 0 {
		timer := time.NewTimer(cfg.duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		return nil
	}

	<-ctx.Done()
	return nil
}

func handleQuery(ctx context.Context, conn *rldp.RLDP, cfg *config, stats *serverStats, transferID []byte, query *rldp.Query) error {
	switch req := query.Data.(type) {
	case DownloadRequest:
		if req.Size > uint64(cfg.maxRequestSize) {
			return fmt.Errorf("download request is too large: %s > %s", formatBytes(req.Size), cfg.maxRequestSize)
		}

		data := make([]byte, int(req.Size))
		hash := make([]byte, sha256.Size)
		if cfg.verify {
			fillPayload(data, req.Seed)
			sum := sha256.Sum256(data)
			copy(hash, sum[:])
		}
		resp := DownloadResponse{
			Data: data,
			Hash: hash,
		}

		sendCtx, cancel := answerContext(ctx, query.Timeout)
		defer cancel()

		if err := conn.SendAnswer(sendCtx, query.MaxAnswerSize, query.Timeout, query.ID, transferID, resp); err != nil {
			return fmt.Errorf("failed to send download answer: %w", err)
		}

		stats.downloadReqs.Add(1)
		stats.downloadBytes.Add(req.Size)
		return nil
	case UploadRequest:
		if uint64(len(req.Data)) > uint64(cfg.maxRequestSize) {
			return fmt.Errorf("upload request is too large: %s > %s", formatBytes(uint64(len(req.Data))), cfg.maxRequestSize)
		}

		hash := make([]byte, sha256.Size)
		ok := true
		if cfg.verify {
			sum := sha256.Sum256(req.Data)
			copy(hash, sum[:])
			ok = len(req.Hash) == sha256.Size && bytes.Equal(req.Hash, hash)
		}
		resp := UploadResponse{
			Size: uint64(len(req.Data)),
			Hash: hash,
			OK:   ok,
		}

		sendCtx, cancel := answerContext(ctx, query.Timeout)
		defer cancel()

		if err := conn.SendAnswer(sendCtx, query.MaxAnswerSize, query.Timeout, query.ID, transferID, resp); err != nil {
			return fmt.Errorf("failed to send upload answer: %w", err)
		}

		stats.uploadReqs.Add(1)
		stats.uploadBytes.Add(uint64(len(req.Data)))
		if !ok {
			return errors.New("upload hash mismatch")
		}
		return nil
	case ChatRequest:
		resp := ChatResponse{
			Text:     "echo: " + req.Text,
			RecvAt:   time.Now().UnixNano(),
			RemoteID: conn.GetADNL().GetID(),
		}

		sendCtx, cancel := answerContext(ctx, query.Timeout)
		defer cancel()

		if err := conn.SendAnswer(sendCtx, query.MaxAnswerSize, query.Timeout, query.ID, transferID, resp); err != nil {
			return fmt.Errorf("failed to send chat answer: %w", err)
		}

		stats.chatReqs.Add(1)
		return nil
	default:
		return fmt.Errorf("unexpected query type %T", query.Data)
	}
}

func runClient(ctx context.Context, gateway *adnl.Gateway, cfg *config) error {
	if cfg.peerAddr == "" {
		return errors.New("peer address is required")
	}
	if cfg.peerPub == "" {
		return errors.New("peer public key is required")
	}

	pub, err := hex.DecodeString(strings.TrimSpace(cfg.peerPub))
	if err != nil {
		return fmt.Errorf("invalid peer public key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid peer public key length %d", len(pub))
	}

	if err = gateway.StartClient(cfg.threads); err != nil {
		return fmt.Errorf("failed to start ADNL client: %w", err)
	}

	peer, err := gateway.RegisterClient(cfg.peerAddr, ed25519.PublicKey(pub))
	if err != nil {
		return fmt.Errorf("failed to register peer: %w", err)
	}
	defer peer.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	rtt, pingErr := peer.Ping(pingCtx)
	cancel()
	if pingErr != nil {
		log.Printf("ADNL ping before benchmark failed: %v", pingErr)
	} else {
		log.Printf("ADNL ping before benchmark: %s", rtt)
	}

	conn := rldp.NewClientV2(peer)
	log.Printf("client ready peer=%s peer_pub=%s local_pub=%s local_adnl_id=%s",
		cfg.peerAddr, cfg.peerPub, hex.EncodeToString(gateway.GetPublicKey()), hex.EncodeToString(gateway.GetID()))
	log.Printf("tuning part=%s symbol=%s min_rate=%s/s max_rate=%s/s multi_fec=%v parallel=%d threads=%d size=%s",
		cfg.partSize, cfg.symbolSize, cfg.minRate, cfg.maxRate, cfg.multiFEC, cfg.parallel, cfg.threads, cfg.size)

	switch cfg.mode {
	case "chat":
		return runChat(ctx, conn, cfg)
	case "adnl-ping":
		return runADNLPing(ctx, peer, cfg)
	case "download", "upload", "bidi":
		return runBenchmark(ctx, conn, cfg)
	default:
		return fmt.Errorf("unsupported client mode %q", cfg.mode)
	}
}

func runChat(ctx context.Context, conn *rldp.RLDP, cfg *config) error {
	reqCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	var resp ChatResponse
	started := time.Now()
	err := conn.DoQuery(reqCtx, 64<<10, ChatRequest{
		Text:   cfg.text,
		SentAt: started.UnixNano(),
	}, &resp)
	if err != nil {
		return err
	}

	log.Printf("chat response in %s text=%q server_recv_delta=%s remote_id=%s",
		time.Since(started), resp.Text, time.Duration(resp.RecvAt-started.UnixNano()), hex.EncodeToString(resp.RemoteID))
	return nil
}

func runADNLPing(ctx context.Context, peer adnl.Peer, cfg *config) error {
	benchCtx, cancel := benchmarkContext(ctx, cfg.duration)
	defer cancel()

	var count uint64
	var total time.Duration
	var max time.Duration
	ticker := time.NewTicker(cfg.reportInterval)
	defer ticker.Stop()

	for {
		reqCtx, cancel := context.WithTimeout(benchCtx, cfg.timeout)
		rtt, err := peer.Ping(reqCtx)
		cancel()
		if err != nil {
			if benchCtx.Err() != nil {
				if count > 0 {
					log.Printf("ADNL ping summary count=%d avg=%s max=%s", count, total/time.Duration(count), max)
				}
				return nil
			}
			log.Printf("ADNL ping failed: %v", err)
		} else {
			count++
			total += rtt
			if rtt > max {
				max = rtt
			}
		}

		select {
		case <-benchCtx.Done():
			if count > 0 {
				log.Printf("ADNL ping summary count=%d avg=%s max=%s", count, total/time.Duration(count), max)
			}
			return nil
		case <-ticker.C:
			if count > 0 {
				log.Printf("ADNL ping count=%d avg=%s max=%s", count, total/time.Duration(count), max)
			}
		default:
		}
	}
}

func runBenchmark(ctx context.Context, conn *rldp.RLDP, cfg *config) error {
	benchCtx, cancel := benchmarkContext(ctx, cfg.duration)
	defer cancel()

	var wg sync.WaitGroup
	var seed atomic.Uint64
	download := &benchStats{name: "download"}
	upload := &benchStats{name: "upload"}

	for i := 0; i < cfg.parallel; i++ {
		switch cfg.mode {
		case "download":
			wg.Add(1)
			go func() {
				defer wg.Done()
				downloadWorker(benchCtx, conn, cfg, download, &seed)
			}()
		case "upload":
			wg.Add(1)
			go func() {
				defer wg.Done()
				uploadWorker(benchCtx, conn, cfg, upload, &seed)
			}()
		case "bidi":
			wg.Add(1)
			if i%2 == 0 {
				go func() {
					defer wg.Done()
					downloadWorker(benchCtx, conn, cfg, download, &seed)
				}()
			} else {
				go func() {
					defer wg.Done()
					uploadWorker(benchCtx, conn, cfg, upload, &seed)
				}()
			}
		}
	}

	stopReport := reportClient(benchCtx, cfg.reportInterval, download, upload)
	wg.Wait()
	stopReport()

	printFinalStats(download)
	printFinalStats(upload)
	return nil
}

func downloadWorker(ctx context.Context, conn *rldp.RLDP, cfg *config, stats *benchStats, seed *atomic.Uint64) {
	for ctx.Err() == nil {
		n := seed.Add(1)
		reqCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
		started := time.Now()

		var resp DownloadResponse
		err := conn.DoQuery(reqCtx, uint64(cfg.size)+4096, DownloadRequest{
			Size: uint64(cfg.size),
			Seed: n,
		}, &resp)
		cancel()

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			stats.fail.Add(1)
			log.Printf("download failed: %v", err)
			continue
		}
		if len(resp.Data) != int(cfg.size) {
			stats.fail.Add(1)
			log.Printf("download size mismatch: got=%s want=%s", formatBytes(uint64(len(resp.Data))), cfg.size)
			continue
		}
		if cfg.verify {
			hash := sha256.Sum256(resp.Data)
			if len(resp.Hash) != sha256.Size || !bytes.Equal(resp.Hash, hash[:]) {
				stats.fail.Add(1)
				log.Printf("download hash mismatch")
				continue
			}
		}

		stats.observe(uint64(len(resp.Data)), time.Since(started))
	}
}

func uploadWorker(ctx context.Context, conn *rldp.RLDP, cfg *config, stats *benchStats, seed *atomic.Uint64) {
	data := make([]byte, int(cfg.size))
	hash := make([]byte, sha256.Size)
	if !cfg.verify {
		fillPayload(data, 1)
	}

	for ctx.Err() == nil {
		n := seed.Add(1)
		if cfg.verify {
			fillPayload(data, n)
			sum := sha256.Sum256(data)
			copy(hash, sum[:])
		}

		reqCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
		started := time.Now()

		var resp UploadResponse
		err := conn.DoQuery(reqCtx, 4096, UploadRequest{
			Seed: n,
			Data: data,
			Hash: hash,
		}, &resp)
		cancel()

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			stats.fail.Add(1)
			log.Printf("upload failed: %v", err)
			continue
		}
		if !resp.OK || resp.Size != uint64(len(data)) {
			stats.fail.Add(1)
			log.Printf("upload verification failed: ok=%v size=%s", resp.OK, formatBytes(resp.Size))
			continue
		}

		stats.observe(uint64(len(data)), time.Since(started))
	}
}

func (s *benchStats) observe(n uint64, latency time.Duration) {
	s.bytes.Add(n)
	s.ok.Add(1)
	ns := uint64(latency.Nanoseconds())
	s.latencyNS.Add(ns)
	for {
		prev := s.maxNS.Load()
		if ns <= prev || s.maxNS.CompareAndSwap(prev, ns) {
			return
		}
	}
}

func reportClient(ctx context.Context, interval time.Duration, stats ...*benchStats) func() {
	done := make(chan struct{})
	var once sync.Once

	go func() {
		defer close(done)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		lastBytes := make([]uint64, len(stats))
		lastOK := make([]uint64, len(stats))
		lastFail := make([]uint64, len(stats))
		lastLatency := make([]uint64, len(stats))
		lastTime := time.Now()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				elapsed := now.Sub(lastTime).Seconds()
				lastTime = now

				for i, st := range stats {
					if st == nil || st.name == "" {
						continue
					}

					curBytes := st.bytes.Load()
					curOK := st.ok.Load()
					curFail := st.fail.Load()
					curLatency := st.latencyNS.Load()

					diffBytes := curBytes - lastBytes[i]
					diffOK := curOK - lastOK[i]
					diffFail := curFail - lastFail[i]
					diffLatency := curLatency - lastLatency[i]

					lastBytes[i] = curBytes
					lastOK[i] = curOK
					lastFail[i] = curFail
					lastLatency[i] = curLatency

					if diffBytes == 0 && diffOK == 0 && diffFail == 0 {
						continue
					}

					avg := time.Duration(0)
					if diffOK > 0 {
						avg = time.Duration(diffLatency / diffOK)
					}

					log.Printf("%s rate=%s/s %.2f Mbit/s ok=%d fail=%d avg_latency=%s max_latency=%s total=%s",
						st.name,
						formatBytes(uint64(float64(diffBytes)/elapsed)),
						float64(diffBytes*8)/1e6/elapsed,
						diffOK,
						diffFail,
						avg,
						time.Duration(st.maxNS.Load()),
						formatBytes(curBytes))
				}
			}
		}
	}()

	return func() {
		once.Do(func() {
			<-done
		})
	}
}

func reportServer(ctx context.Context, gateway *adnl.Gateway, stats *serverStats, interval time.Duration) func() {
	done := make(chan struct{})
	var once sync.Once

	go func() {
		defer close(done)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var lastDL, lastUL, lastDLReq, lastULReq, lastChat, lastFail uint64
		lastTime := time.Now()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				elapsed := now.Sub(lastTime).Seconds()
				lastTime = now

				dl := stats.downloadBytes.Load()
				ul := stats.uploadBytes.Load()
				dlReq := stats.downloadReqs.Load()
				ulReq := stats.uploadReqs.Load()
				chat := stats.chatReqs.Load()
				fail := stats.fail.Load()

				log.Printf("server peers=%d download_out=%s/s %.2f Mbit/s req=%d upload_in=%s/s %.2f Mbit/s req=%d chat=%d fail=%d total_out=%s total_in=%s",
					len(gateway.GetActivePeers()),
					formatBytes(uint64(float64(dl-lastDL)/elapsed)),
					float64((dl-lastDL)*8)/1e6/elapsed,
					dlReq-lastDLReq,
					formatBytes(uint64(float64(ul-lastUL)/elapsed)),
					float64((ul-lastUL)*8)/1e6/elapsed,
					ulReq-lastULReq,
					chat-lastChat,
					fail-lastFail,
					formatBytes(dl),
					formatBytes(ul))

				lastDL = dl
				lastUL = ul
				lastDLReq = dlReq
				lastULReq = ulReq
				lastChat = chat
				lastFail = fail
			}
		}
	}()

	return func() {
		once.Do(func() {
			<-done
		})
	}
}

func printFinalStats(st *benchStats) {
	ok := st.ok.Load()
	fail := st.fail.Load()
	total := st.bytes.Load()
	if ok == 0 && fail == 0 && total == 0 {
		return
	}

	avg := time.Duration(0)
	if ok > 0 {
		avg = time.Duration(st.latencyNS.Load() / ok)
	}

	log.Printf("%s summary total=%s ok=%d fail=%d avg_latency=%s max_latency=%s",
		st.name, formatBytes(total), ok, fail, avg, time.Duration(st.maxNS.Load()))
}

func benchmarkContext(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	if duration <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, duration)
}

func answerContext(ctx context.Context, timeoutAt uint32) (context.Context, context.CancelFunc) {
	deadline := time.Unix(int64(timeoutAt), 0)
	if deadline.Before(time.Now().Add(time.Second)) {
		deadline = time.Now().Add(time.Second)
	}
	return context.WithDeadline(ctx, deadline)
}

func loadKey(hexKey, seedText string) (ed25519.PrivateKey, error) {
	if hexKey != "" {
		raw, err := hex.DecodeString(strings.TrimSpace(hexKey))
		if err != nil {
			return nil, err
		}
		switch len(raw) {
		case ed25519.SeedSize:
			return ed25519.NewKeyFromSeed(raw), nil
		case ed25519.PrivateKeySize:
			return ed25519.PrivateKey(raw), nil
		default:
			return nil, fmt.Errorf("invalid key length %d", len(raw))
		}
	}

	if seedText != "" {
		seed := sha256.Sum256([]byte(seedText))
		return ed25519.NewKeyFromSeed(seed[:]), nil
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	return priv, err
}

func fillPayload(buf []byte, seed uint64) {
	x := seed + 0x9e3779b97f4a7c15
	for i := 0; i < len(buf); i += 8 {
		x = splitmix64(x)
		var tmp [8]byte
		binary.LittleEndian.PutUint64(tmp[:], x)
		copy(buf[i:], tmp[:])
	}
}

func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	z := x
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func parseByteSize(s string) (uint64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, errors.New("empty byte size")
	}

	mul := float64(1)
	suffixes := []struct {
		suffix string
		mul    float64
	}{
		{"gib", 1 << 30},
		{"gb", 1e9},
		{"g", 1 << 30},
		{"mib", 1 << 20},
		{"mb", 1e6},
		{"m", 1 << 20},
		{"kib", 1 << 10},
		{"kb", 1e3},
		{"k", 1 << 10},
		{"b", 1},
	}
	for _, suf := range suffixes {
		if strings.HasSuffix(s, suf.suffix) {
			mul = suf.mul
			s = strings.TrimSpace(strings.TrimSuffix(s, suf.suffix))
			break
		}
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, errors.New("byte size cannot be negative")
	}
	if v*mul > math.MaxUint64 {
		return 0, errors.New("byte size is too large")
	}
	return uint64(v * mul), nil
}

func formatBytes(v uint64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d B", v)
	}

	div, exp := uint64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.2f %ciB", float64(v)/float64(div), "KMGTPE"[exp])
}
