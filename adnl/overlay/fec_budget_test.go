package overlay

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"math"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

func TestEstimateFECBroadcastBudgetBytes(t *testing.T) {
	tests := []struct {
		name     string
		fec      rldp.FECRaptorQ
		partSize int
		want     int64
	}{
		{
			name:     "symbol sized relay part",
			fec:      rldp.FECRaptorQ{DataSize: 200, SymbolSize: 100, SymbolsCount: 2},
			partSize: 100,
			want:     27376,
		},
		{
			name:     "larger observed relay part",
			fec:      rldp.FECRaptorQ{DataSize: 200, SymbolSize: 100, SymbolsCount: 2},
			partSize: 250,
			want:     28576,
		},
		{
			name:     "negative observed size is ignored",
			fec:      rldp.FECRaptorQ{DataSize: 200, SymbolSize: 100, SymbolsCount: 2},
			partSize: -1,
			want:     27376,
		},
		{
			name:     "maximum fields saturate",
			fec:      rldp.FECRaptorQ{DataSize: math.MaxUint32, SymbolSize: math.MaxUint32, SymbolsCount: math.MaxUint32},
			partSize: int(^uint(0) >> 1),
			want:     maxFECBroadcastBudgetEstimate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimateFECBroadcastBudgetBytes(tt.fec, tt.partSize); got != tt.want {
				t.Fatalf("estimateFECBroadcastBudgetBytes() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEstimateRetainedFECBroadcastBudgetBytes(t *testing.T) {
	tests := []struct {
		name string
		fec  rldp.FECRaptorQ
		want int64
	}{
		{
			name: "small stream",
			fec:  rldp.FECRaptorQ{DataSize: 200, SymbolSize: 100, SymbolsCount: 2},
			want: 25176,
		},
		{
			name: "one MiB default symbols",
			fec:  rldp.FECRaptorQ{DataSize: 1 << 20, SymbolSize: 768, SymbolsCount: 1366},
			want: 5965312,
		},
		{
			name: "maximum fields saturate",
			fec:  rldp.FECRaptorQ{DataSize: math.MaxUint32, SymbolSize: math.MaxUint32, SymbolsCount: math.MaxUint32},
			want: maxFECBroadcastBudgetEstimate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimateRetainedFECBroadcastBudgetBytes(tt.fec); got != tt.want {
				t.Fatalf("estimateRetainedFECBroadcastBudgetBytes() = %d, want %d", got, tt.want)
			}
		})
	}

	fec := tests[1].fec
	retained := estimateRetainedFECBroadcastBudgetBytes(fec)
	predecode := estimateFECBroadcastBudgetBytes(fec, int(fec.SymbolSize))
	if retained <= broadcastFECTerminalBudgetBytes {
		t.Fatalf("relay-retained budget = %d, must not use terminal budget %d", retained, broadcastFECTerminalBudgetBytes)
	}
	if retained >= predecode {
		t.Fatalf("relay-retained budget = %d, want below predecode budget %d", retained, predecode)
	}
	if payload := int64(fec.DataSize); retained < payload*5 || retained > payload*6 {
		t.Fatalf("relay-retained budget = %d, want conservative 5-6x of %d-byte payload", retained, payload)
	}
}

func TestFECBroadcastBudgetArithmeticSaturates(t *testing.T) {
	if got := multiplyFECBroadcastBudgetEstimate(uint64(maxFECBroadcastBudgetEstimate), 1); got != maxFECBroadcastBudgetEstimate {
		t.Fatalf("exact multiply = %d, want %d", got, maxFECBroadcastBudgetEstimate)
	}
	if got := multiplyFECBroadcastBudgetEstimate(uint64(maxFECBroadcastBudgetEstimate), 2); got != maxFECBroadcastBudgetEstimate {
		t.Fatalf("overflowing multiply = %d, want saturation", got)
	}
	if got := addFECBroadcastBudgetEstimate(maxFECBroadcastBudgetEstimate-1, 1); got != maxFECBroadcastBudgetEstimate {
		t.Fatalf("exact add = %d, want %d", got, maxFECBroadcastBudgetEstimate)
	}
	if got := addFECBroadcastBudgetEstimate(maxFECBroadcastBudgetEstimate-1, 2); got != maxFECBroadcastBudgetEstimate {
		t.Fatalf("overflowing add = %d, want saturation", got)
	}
	if got := addFECBroadcastBudgetEstimate(1, -1); got != maxFECBroadcastBudgetEstimate {
		t.Fatalf("negative input = %d, want conservative saturation", got)
	}
}

func TestBroadcastFECPartLimitDoesNotWrap(t *testing.T) {
	tests := []struct {
		name    string
		symbols uint32
		want    uint64
	}{
		{name: "zero", symbols: 0, want: 4},
		{name: "ordinary", symbols: 2, want: 8},
		{name: "maximum", symbols: math.MaxUint32, want: uint64(math.MaxUint32)*2 + 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := broadcastFECPartLimit(tt.symbols); got != tt.want {
				t.Fatalf("broadcastFECPartLimit(%d) = %d, want %d", tt.symbols, got, tt.want)
			}
		})
	}

	if got := broadcastFECSeqnoLimit(math.MaxUint32); got != math.MaxUint32 {
		t.Fatalf("sender seqno limit = %d, want saturated %d", got, uint32(math.MaxUint32))
	}
	if uint64(math.MaxUint32) >= broadcastFECPartLimit(math.MaxUint32) {
		t.Fatal("maximum representable seqno must remain valid when the mathematical limit is higher")
	}
	if uint64(8) < broadcastFECPartLimit(2) {
		t.Fatal("seqno at the ordinary exclusive limit must be rejected")
	}
	protocolLimit := broadcastFECPartLimit(maxFECBroadcastSymbols)
	if uint64(uint32(protocolLimit-1)) >= protocolLimit {
		t.Fatal("last seqno for the largest accepted FEC type must remain valid")
	}
	if uint64(uint32(protocolLimit)) < protocolLimit || uint64(math.MaxUint32) < protocolLimit {
		t.Fatal("seqno at or above the largest accepted FEC limit must be rejected")
	}
}

func TestFECBroadcastBudgetCheckDoesNotOverflow(t *testing.T) {
	state := NewBroadcastFECRelayState()
	state.maxActiveBytes = maxFECBroadcastBudgetEstimate
	state.activeBytes = maxFECBroadcastBudgetEstimate - 4

	if state.hasBudgetLocked(1, 8) {
		t.Fatal("overflowing byte sum must not pass the budget check")
	}
	if !state.hasBudgetLocked(1, 4) {
		t.Fatal("exact remaining byte budget must pass")
	}
}

func TestBroadcastFECImmediatePeerID(t *testing.T) {
	tests := []struct {
		name  string
		id    []byte
		match bool
	}{
		{name: "empty", id: nil, match: true},
		{name: "short test ID", id: bytes.Repeat([]byte{0x11}, 3), match: true},
		{name: "protocol ID", id: bytes.Repeat([]byte{0x22}, 32), match: true},
		{name: "oversized invalid ID", id: bytes.Repeat([]byte{0x33}, 33), match: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := newBroadcastFECImmediatePeerID(tt.id)
			if got := stored.matches(tt.id); got != tt.match {
				t.Fatalf("matches() = %v, want %v", got, tt.match)
			}
			if len(tt.id) > 0 && stored.matches(tt.id[:len(tt.id)-1]) {
				t.Fatal("stored peer ID matched a different length")
			}
		})
	}
}

func TestFECBroadcastConcurrentReservationsRespectStreamLimit(t *testing.T) {
	const contenders = 64
	const streamBudget = int64(1024)

	state := NewBroadcastFECRelayState()
	state.maxActiveStreams = 1
	state.maxActiveBytes = contenders * streamBudget
	start := make(chan struct{})
	releaseWinner := make(chan struct{})
	results := make(chan bool, contenders)

	var wg sync.WaitGroup
	wg.Add(contenders)
	for range contenders {
		go func() {
			defer wg.Done()
			<-start

			state.mx.Lock()
			reserved := state.reserveLocked(time.Now(), streamBudget)
			state.mx.Unlock()
			results <- reserved
			if !reserved {
				return
			}

			<-releaseWinner
			state.mx.Lock()
			state.cancelReservationLocked(streamBudget)
			state.mx.Unlock()
		}()
	}
	close(start)

	reserved := 0
	for range contenders {
		if <-results {
			reserved++
		}
	}
	if reserved != 1 {
		t.Fatalf("concurrent reservations = %d, want exactly one", reserved)
	}

	state.mx.RLock()
	reservedStreams := state.reservedStreams
	activeBytes := state.activeBytes
	state.mx.RUnlock()
	if reservedStreams != 1 || activeBytes != streamBudget {
		t.Fatalf("held reservation state streams=%d bytes=%d", reservedStreams, activeBytes)
	}
	if stats := state.Stats(); stats.ActiveStreams != 1 || stats.ActiveBytes != streamBudget {
		t.Fatalf("held reservation stats are inconsistent: %#v", stats)
	}

	close(releaseWinner)
	wg.Wait()
	state.mx.RLock()
	reservedStreams = state.reservedStreams
	activeBytes = state.activeBytes
	state.mx.RUnlock()
	if reservedStreams != 0 || activeBytes != 0 {
		t.Fatalf("released reservation state streams=%d bytes=%d", reservedStreams, activeBytes)
	}
}

func TestProcessFECBroadcastRejectsInvalidFirstSymbolWithoutState(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0xB1}, 32), 4096, true, true)
	_, priv := keyPairFromSeed(71)

	part := newTestFECPart(t, priv, []byte{1, 2, 3}, bytes.Repeat([]byte{0x11}, 32))
	err := o.processFECBroadcast(part)
	if err == nil || !strings.Contains(err.Error(), "incorrect symbol size") {
		t.Fatalf("expected symbol size error, got %v", err)
	}
	if stats := o.FECBroadcastStats(); stats.ActiveStreams != 0 || stats.ActiveBytes != 0 || stats.DroppedTotal != 0 {
		t.Fatalf("invalid first symbol retained receiver state: %#v", stats)
	}
}

func TestProcessFECBroadcastCancelsReservationWhenDecoderInitFails(t *testing.T) {
	const symbols = uint32(56404) // One above raptorq v1.5's supported K.

	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0xB2}, 32), symbols, true, true)
	o.SetFECBroadcastLimits(8, maxFECBroadcastBudgetEstimate)
	_, priv := keyPairFromSeed(74)
	part := &BroadcastFEC{
		Source:      ed25519Public(priv),
		Certificate: CertificateEmpty{},
		DataHash:    bytes.Repeat([]byte{0x33}, 32),
		DataSize:    symbols,
		Flags:       BroadcastFlagAnySender,
		Data:        []byte{1},
		Seqno:       0,
		FEC:         rldp.FECRaptorQ{DataSize: symbols, SymbolSize: 1, SymbolsCount: symbols},
		Date:        uint32(time.Now().Unix()),
	}
	if err := part.Sign(priv); err != nil {
		t.Fatalf("sign FEC part: %v", err)
	}

	err := o.processFECBroadcast(part)
	if err == nil || !strings.Contains(err.Error(), "failed to init raptorq decoder") {
		t.Fatalf("expected decoder init error, got %v", err)
	}
	state := o.activeFECState()
	state.mx.RLock()
	reservedStreams := state.reservedStreams
	state.mx.RUnlock()
	if stats := state.Stats(); stats.ActiveStreams != 0 || stats.ActiveBytes != 0 || reservedStreams != 0 {
		t.Fatalf("decoder init failure leaked reservation: stats=%#v reserved=%d", stats, reservedStreams)
	}
}

func TestProcessFECBroadcastCompactsTerminalDecodeFailures(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		hash    []byte
		wantErr string
	}{
		{
			name:    "decoded hash mismatch",
			data:    []byte{1, 2, 3, 4},
			hash:    bytes.Repeat([]byte{0x22}, 32),
			wantErr: "incorrect data hash",
		},
		{
			name:    "decoded TL parse failure",
			data:    []byte{0xFF, 0xFF, 0xFF, 0xFF},
			hash:    hashForFECTest([]byte{0xFF, 0xFF, 0xFF, 0xFF}),
			wantErr: "failed to parse decoded broadcast message",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockADNL()
			w := CreateExtendedADNL(m)
			o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{byte(0xC1 + i)}, 32), 4096, true, true)
			_, priv := keyPairFromSeed(byte(72 + i))
			part := newTestFECPart(t, priv, tt.data, tt.hash)

			err := o.processFECBroadcast(part)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected %q, got %v", tt.wantErr, err)
			}
			assertTerminalFECStream(t, o.activeFECState(), part, err)

			replayErr := o.processFECBroadcast(part)
			if replayErr == nil || replayErr.Error() != err.Error() {
				t.Fatalf("terminal replay error = %v, want %v", replayErr, err)
			}
			if len(m.sendCustomCalls) != 0 {
				t.Fatalf("terminal stream must not acknowledge completion, got %d messages", len(m.sendCustomCalls))
			}
		})
	}
}

func TestTerminalFECBroadcastErrorClosesAdmissionAndReleasesBudget(t *testing.T) {
	state := NewBroadcastFECRelayState()
	errTerminal := errors.New("encoder failed")
	admissionDone := make(chan struct{})
	stream := &fecBroadcastStream{
		budgetBytes:    1 << 20,
		lastMessageAt:  time.Now(),
		partHashes:     map[uint32][32]byte{1: {}},
		parts:          map[uint32]broadcastFECRelayPart{1: {}},
		receivedPeers:  map[string]struct{}{"peer": {}},
		completedPeers: map[string]struct{}{"peer": {}},
		admissionDone:  admissionDone,
	}
	state.streams["id"] = stream
	state.activeBytes = stream.budgetBytes

	stream.mx.Lock()
	if got := terminalFECBroadcastErrorLocked(state, "id", stream, time.Now(), errTerminal); !errors.Is(got, errTerminal) {
		t.Fatalf("terminal error = %v, want %v", got, errTerminal)
	}

	select {
	case <-admissionDone:
	default:
		t.Fatal("terminal transition left admission waiter blocked")
	}
	if result := stream.waitAdmission(); !errors.Is(result.err, errTerminal) {
		t.Fatalf("waitAdmission() = %#v, want error %v", result, errTerminal)
	}
	if stats := state.Stats(); stats.ActiveStreams != 1 || stats.ActiveBytes != broadcastFECTerminalBudgetBytes {
		t.Fatalf("terminal state budget was not compacted: %#v", stats)
	}
}

func TestFECBroadcastAcceptRelayTransitionsToRetainedBudget(t *testing.T) {
	m := newMockADNL()
	m.id = bytes.Repeat([]byte{0xC1}, 32)
	w := CreateExtendedADNL(m)
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0xC2}, 32), 4096, true, true)
	state := NewBroadcastFECRelayState()
	o.EnableBroadcastFECRelay(bytes.Repeat([]byte{0xC3}, 32), mockBroadcastPeerSet{}, state)
	_, priv := keyPairFromSeed(75)
	sender, err := NewBroadcastFECSenderFromTL(
		priv,
		CertificateEmpty{},
		Message{Overlay: bytes.Repeat([]byte{0xC4}, 32)},
		BroadcastFlagAnySender,
	)
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	for seqno := uint32(0); seqno < sender.fec.SymbolsCount; seqno++ {
		part, partErr := sender.part(seqno)
		if partErr != nil {
			t.Fatalf("build part %d: %v", seqno, partErr)
		}
		if err = o.processFECBroadcast(part.full); err != nil {
			t.Fatalf("process part %d: %v", seqno, err)
		}
	}

	want := estimateRetainedFECBroadcastBudgetBytes(sender.fec)
	predecode := estimateFECBroadcastBudgetBytes(sender.fec, int(sender.fec.SymbolSize))
	if stats := state.Stats(); stats.ActiveStreams != 1 || stats.CompletedTotal != 1 || stats.ActiveBytes != want {
		t.Fatalf("accepted relay stream budget did not transition: stats=%#v want_bytes=%d", stats, want)
	}
	if want <= broadcastFECTerminalBudgetBytes || want >= predecode {
		t.Fatalf("invalid retained transition terminal=%d retained=%d predecode=%d", broadcastFECTerminalBudgetBytes, want, predecode)
	}

	state.mx.RLock()
	stream := state.streams[string(sender.BroadcastHash())]
	state.mx.RUnlock()
	if stream == nil {
		t.Fatal("accepted relay stream was not retained")
	}
	stream.mx.Lock()
	hasEncoder := stream.encoder != nil
	partHashes := len(stream.partHashes)
	queuedParts := len(stream.parts)
	streamBudget := stream.budgetBytes
	stream.mx.Unlock()
	if !hasEncoder || partHashes != int(sender.fec.SymbolsCount) || queuedParts != 0 || streamBudget != want {
		t.Fatalf("unexpected retained state encoder=%v hashes=%d queued=%d budget=%d", hasEncoder, partHashes, queuedParts, streamBudget)
	}
}

func TestProcessFECBroadcastAcceptsLowPartsAfterHighRepair(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0xD1}, 32), 4096, true, true)
	_, priv := keyPairFromSeed(76)
	payload := Broadcast{
		Source:      ed25519Public(priv),
		Certificate: CertificateEmpty{},
		Data:        bytes.Repeat([]byte{0xD2}, 768),
		Date:        1,
	}
	sender, err := NewBroadcastFECSenderFromTL(
		priv,
		CertificateEmpty{},
		payload,
		BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(16),
	)
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}

	const highSeqno = uint32(70)
	if sender.TotalParts() <= highSeqno {
		t.Fatalf("test stream has %d parts, need seqno %d", sender.TotalParts(), highSeqno)
	}
	highPart, err := sender.part(highSeqno)
	if err != nil {
		t.Fatalf("build high repair part: %v", err)
	}
	if err = o.processFECBroadcast(highPart.full); err != nil {
		t.Fatalf("process high repair part: %v", err)
	}

	state := o.activeFECState()
	state.mx.RLock()
	stream := state.streams[string(sender.BroadcastHash())]
	state.mx.RUnlock()
	if stream == nil {
		t.Fatal("high repair part did not create a stream")
	}
	stream.mx.Lock()
	highReceived := stream.receivedPart(highSeqno)
	lowReceived := stream.receivedPart(0)
	stream.mx.Unlock()
	if !highReceived || lowReceived {
		t.Fatalf("exact membership high=%v low=%v, want true/false", highReceived, lowReceived)
	}

	for seqno := uint32(0); seqno < sender.fec.SymbolsCount && state.Stats().CompletedTotal == 0; seqno++ {
		part, partErr := sender.part(seqno)
		if partErr != nil {
			t.Fatalf("build base part %d: %v", seqno, partErr)
		}
		if err = o.processFECBroadcast(part.full); err != nil {
			t.Fatalf("process base part %d: %v", seqno, err)
		}
	}
	if stats := state.Stats(); stats.CompletedTotal != 1 || stats.DeliveredBroadcasts != 1 {
		t.Fatalf("low base parts did not complete poisoned-order stream: %#v", stats)
	}
}

func TestProcessFECBroadcastRecontendsAfterCleanup(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0xD3}, 32), 4096, true, false)
	state := NewBroadcastFECRelayState()
	peers := newBlockingBroadcastPeerSet()
	o.EnableBroadcastFECRelay(bytes.Repeat([]byte{0xDA}, 32), peers, state)
	_, priv := keyPairFromSeed(77)
	sender, err := NewBroadcastFECSenderFromTL(
		priv,
		CertificateEmpty{},
		Message{Overlay: bytes.Repeat([]byte{0xD4}, 32)},
		BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(8),
	)
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}
	if sender.fec.SymbolsCount < 2 {
		t.Fatalf("test stream has only %d base symbol", sender.fec.SymbolsCount)
	}

	first, err := sender.part(0)
	if err != nil {
		t.Fatalf("build first part: %v", err)
	}
	if err = o.processFECBroadcast(first.full); err != nil {
		t.Fatalf("process first part: %v", err)
	}

	id := string(sender.BroadcastHash())
	state.mx.Lock()
	old := state.streams[id]
	if old == nil {
		state.mx.Unlock()
		t.Fatal("first part did not create stream")
	}
	old.lastMessageAt = time.Now().Add(-fecBroadcastStreamIdleTTL - time.Second)
	state.nextCleanupAt = time.Now().Add(time.Hour)
	state.mx.Unlock()

	second, err := sender.part(1)
	if err != nil {
		t.Fatalf("build second part: %v", err)
	}
	peers.armed.Store(true)
	result := make(chan error, 1)
	go func() {
		result <- o.processFECBroadcast(second.full)
	}()
	<-peers.entered

	state.mx.Lock()
	state.cleanupLocked(time.Now(), true)
	if state.streams[id] != nil {
		state.mx.Unlock()
		t.Fatal("forced cleanup did not remove stale stream")
	}
	state.mx.Unlock()
	close(peers.release)
	if err = <-result; err != nil {
		t.Fatalf("process after cleanup: %v", err)
	}

	state.mx.RLock()
	current := state.streams[id]
	state.mx.RUnlock()
	if current == nil || current == old {
		t.Fatalf("packet continued on orphan stream: current=%p old=%p", current, old)
	}
	old.mx.Lock()
	oldReceived := old.receivedPart(1)
	old.mx.Unlock()
	current.mx.Lock()
	currentReceived := current.receivedPart(1)
	current.mx.Unlock()
	if oldReceived || !currentReceived {
		t.Fatalf("second part membership old=%v current=%v, want false/true", oldReceived, currentReceived)
	}
}

func TestProcessFECBroadcastShortDoesNotContinueAfterCleanup(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0xD5}, 32), 4096, true, false)
	state := NewBroadcastFECRelayState()
	peers := newBlockingBroadcastPeerSet()
	o.EnableBroadcastFECRelay(bytes.Repeat([]byte{0xD6}, 32), peers, state)
	_, priv := keyPairFromSeed(78)
	sender, err := NewBroadcastFECSenderFromTL(
		priv,
		CertificateEmpty{},
		Message{Overlay: bytes.Repeat([]byte{0xD7}, 32)},
		BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(256),
	)
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}
	for seqno := uint32(0); seqno < sender.fec.SymbolsCount && state.Stats().CompletedTotal == 0; seqno++ {
		part, partErr := sender.part(seqno)
		if partErr != nil {
			t.Fatalf("build base part %d: %v", seqno, partErr)
		}
		if err = o.processFECBroadcast(part.full); err != nil {
			t.Fatalf("process base part %d: %v", seqno, err)
		}
	}
	if state.Stats().CompletedTotal != 1 {
		t.Fatal("test stream did not complete")
	}

	id := string(sender.BroadcastHash())
	state.mx.Lock()
	old := state.streams[id]
	if old == nil {
		state.mx.Unlock()
		t.Fatal("completed relay stream was not retained")
	}
	old.lastMessageAt = time.Now().Add(-fecBroadcastStreamIdleTTL - time.Second)
	state.nextCleanupAt = time.Now().Add(time.Hour)
	state.mx.Unlock()

	repairSeqno := sender.fec.SymbolsCount
	repair, err := sender.part(repairSeqno)
	if err != nil {
		t.Fatalf("build repair part: %v", err)
	}
	peers.armed.Store(true)
	result := make(chan error, 1)
	go func() {
		result <- o.processFECBroadcastShort(repair.short)
	}()
	<-peers.entered

	state.mx.Lock()
	state.cleanupLocked(time.Now(), true)
	state.mx.Unlock()
	close(peers.release)
	if err = <-result; err != nil {
		t.Fatalf("process short part after cleanup: %v", err)
	}

	old.mx.Lock()
	orphanReceived := old.receivedPart(repairSeqno)
	old.mx.Unlock()
	if orphanReceived {
		t.Fatal("short part was committed to an evicted stream")
	}
	state.mx.RLock()
	registered := state.streams[id]
	state.mx.RUnlock()
	if registered == old {
		t.Fatal("forced cleanup left old stream registered")
	}
}

func TestFECBroadcastCleanupKeepsStreamDuringAdmission(t *testing.T) {
	w := CreateExtendedADNL(newMockADNL())
	o := w.CreateOverlayWithSettings(bytes.Repeat([]byte{0xD8}, 32), 4096, true, true)
	_, priv := keyPairFromSeed(79)
	sender, err := NewBroadcastFECSenderFromTL(
		priv,
		CertificateEmpty{},
		Message{Overlay: bytes.Repeat([]byte{0xD9}, 32)},
		BroadcastFlagAnySender,
		WithBroadcastFECSymbolSize(256),
	)
	if err != nil {
		t.Fatalf("sender init failed: %v", err)
	}
	part, err := sender.part(0)
	if err != nil {
		t.Fatalf("build part: %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	o.SetBroadcastHandlerWithInfo(func(tl.Serializable, BroadcastInfo) BroadcastDisposition {
		close(entered)
		<-release
		return BroadcastDispositionAcceptAndRelay
	})
	result := make(chan error, 1)
	go func() {
		result <- o.processFECBroadcast(part.full)
	}()
	<-entered

	state := o.activeFECState()
	id := string(sender.BroadcastHash())
	state.mx.Lock()
	stream := state.streams[id]
	state.cleanupLocked(time.Now().Add(fecBroadcastFinishedTTL+time.Second), true)
	retained := state.streams[id] == stream && stream != nil
	state.mx.Unlock()
	if !retained {
		close(release)
		<-result
		t.Fatal("cleanup evicted stream while application admission was in progress")
	}

	close(release)
	if err = <-result; err != nil {
		t.Fatalf("finish admitted broadcast: %v", err)
	}
}

func BenchmarkEstimateFECBroadcastBudgetBytes(b *testing.B) {
	fec := rldp.FECRaptorQ{DataSize: 1 << 20, SymbolSize: 768, SymbolsCount: 1366}
	var result int64
	b.ReportAllocs()
	for b.Loop() {
		result = estimateFECBroadcastBudgetBytes(fec, 768)
	}
	_ = result
}

func BenchmarkEstimateRetainedFECBroadcastBudgetBytes(b *testing.B) {
	fec := rldp.FECRaptorQ{DataSize: 1 << 20, SymbolSize: 768, SymbolsCount: 1366}
	var result int64
	b.ReportAllocs()
	for b.Loop() {
		result = estimateRetainedFECBroadcastBudgetBytes(fec)
	}
	_ = result
}

func BenchmarkMeasureRetainedFECBroadcastHeap(b *testing.B) {
	const streamsCount = 8
	if b.N != 1 {
		b.Skip("run retained heap measurement with -benchtime=1x")
	}
	fec := rldp.FECRaptorQ{DataSize: 1 << 20, SymbolSize: 768, SymbolsCount: 1366}

	b.StopTimer()
	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	streams := make([]*fecBroadcastStream, 0, streamsCount)
	for range streamsCount {
		encoder, err := raptorq.NewRaptorQ(fec.SymbolSize).CreateEncoder(make([]byte, fec.DataSize))
		if err != nil {
			b.Fatalf("create encoder: %v", err)
		}

		maxParts := int(broadcastFECPartLimit(fec.SymbolsCount))
		partHashes := make(map[uint32][32]byte, maxParts)
		receivedPeers := make(map[string]struct{}, maxParts)
		completedPeers := make(map[string]struct{}, maxParts)
		for seqno := range uint32(maxParts) {
			partHashes[seqno] = [32]byte{byte(seqno), byte(seqno >> 8), byte(seqno >> 16), byte(seqno >> 24)}
			peerID := [32]byte{byte(seqno), byte(seqno >> 8), byte(seqno >> 16), byte(seqno >> 24)}
			id := string(peerID[:])
			receivedPeers[id] = struct{}{}
			completedPeers[id] = struct{}{}
		}
		streams = append(streams, &fecBroadcastStream{
			encoder:        encoder,
			partHashes:     partHashes,
			receivedPeers:  receivedPeers,
			completedPeers: completedPeers,
		})
	}

	runtime.GC()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(streams)
	if after.HeapAlloc <= before.HeapAlloc {
		b.Fatalf("retained heap did not grow: before=%d after=%d", before.HeapAlloc, after.HeapAlloc)
	}

	actual := float64(after.HeapAlloc-before.HeapAlloc) / streamsCount
	estimate := float64(estimateRetainedFECBroadcastBudgetBytes(fec))
	if actual > estimate {
		b.Fatalf("measured retained heap %.0f exceeds estimate %.0f", actual, estimate)
	}
	b.ReportMetric(actual, "retained-B/stream")
	b.ReportMetric(estimate, "budget-B/stream")
	b.ReportMetric(estimate/actual, "budget/actual")
}

func BenchmarkFECBroadcastReserveCancel(b *testing.B) {
	state := NewBroadcastFECRelayState()
	state.maxActiveStreams = 1
	state.maxActiveBytes = 1 << 30
	now := time.Now()

	b.ReportAllocs()
	for b.Loop() {
		state.mx.Lock()
		if !state.reserveLocked(now, 1<<20) {
			state.mx.Unlock()
			b.Fatal("reservation unexpectedly rejected")
		}
		state.cancelReservationLocked(1 << 20)
		state.mx.Unlock()
	}
}

func BenchmarkFECBroadcastReceivedPart(b *testing.B) {
	stream := fecBroadcastStream{partHashes: make(map[uint32][32]byte, 2048)}
	for seqno := uint32(0); seqno < 2048; seqno++ {
		stream.partHashes[seqno] = [32]byte{byte(seqno)}
	}

	b.ReportAllocs()
	for b.Loop() {
		if !stream.receivedPart(1024) || stream.receivedPart(4096) {
			b.Fatal("incorrect exact membership")
		}
	}
}

func BenchmarkFECBroadcastAddRelayPart(b *testing.B) {
	stream := fecBroadcastStream{parts: make(map[uint32]broadcastFECRelayPart, 1)}
	full := &BroadcastFEC{}
	broadcastHash := bytes.Repeat([]byte{0x11}, 32)
	partDataHash := bytes.Repeat([]byte{0x22}, 32)
	immediatePeerID := bytes.Repeat([]byte{0x33}, 32)

	b.ReportAllocs()
	for b.Loop() {
		stream.addRelayPart(1, full, broadcastHash, partDataHash, immediatePeerID)
		delete(stream.parts, 1)
	}
}

type blockingBroadcastPeerSet struct {
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingBroadcastPeerSet() *blockingBroadcastPeerSet {
	return &blockingBroadcastPeerSet{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *blockingBroadcastPeerSet) Peers() []BroadcastPeer {
	if s.armed.Load() {
		s.once.Do(func() {
			close(s.entered)
		})
		<-s.release
	}
	return nil
}

func newTestFECPart(t *testing.T, priv ed25519.PrivateKey, data, dataHash []byte) *BroadcastFEC {
	t.Helper()

	part := &BroadcastFEC{
		Source:      ed25519Public(priv),
		Certificate: CertificateEmpty{},
		DataHash:    dataHash,
		DataSize:    4,
		Flags:       BroadcastFlagAnySender,
		Data:        data,
		Seqno:       0,
		FEC:         rldp.FECRaptorQ{DataSize: 4, SymbolSize: 4, SymbolsCount: 1},
		Date:        uint32(time.Now().Unix()),
	}
	if err := part.Sign(priv); err != nil {
		t.Fatalf("sign FEC part: %v", err)
	}
	return part
}

func hashForFECTest(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:]
}

func assertTerminalFECStream(t *testing.T, state *BroadcastFECRelayState, part *BroadcastFEC, wantErr error) {
	t.Helper()

	id, err := part.CalcID()
	if err != nil {
		t.Fatalf("calculate broadcast ID: %v", err)
	}
	state.mx.RLock()
	stream := state.streams[string(id)]
	activeBytes := state.activeBytes
	reservedStreams := state.reservedStreams
	state.mx.RUnlock()
	if stream == nil {
		t.Fatal("terminal stream was removed, allowing repeated decode work")
	}

	stream.mx.Lock()
	defer stream.mx.Unlock()
	if stream.decoder != nil || stream.encoder != nil || stream.partHashes != nil || stream.parts != nil ||
		stream.receivedPeers != nil || stream.completedPeers != nil {
		t.Fatal("terminal stream retained heavy decode or relay state")
	}
	if stream.finishedAt == nil || stream.admissionErr == nil || stream.admissionErr.Error() != wantErr.Error() {
		t.Fatalf("unexpected terminal disposition: finished=%v err=%v", stream.finishedAt, stream.admissionErr)
	}
	if stream.budgetBytes != broadcastFECTerminalBudgetBytes || activeBytes != broadcastFECTerminalBudgetBytes {
		t.Fatalf("terminal budget stream=%d active=%d, want %d", stream.budgetBytes, activeBytes, broadcastFECTerminalBudgetBytes)
	}
	if reservedStreams != 0 {
		t.Fatalf("terminal stream left %d pending reservations", reservedStreams)
	}
}
