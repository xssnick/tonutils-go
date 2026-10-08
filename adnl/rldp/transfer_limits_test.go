package rldp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/rldp/roundrobin"
)

type admissionADNL struct {
	MockADNL
	addr string
}

func (a admissionADNL) RemoteAddr() string { return a.addr }

func newAdmissionClient(t *testing.T, addr string) *RLDP {
	t.Helper()
	c := newInertClient()
	c.adnl = admissionADNL{MockADNL: c.adnl.(MockADNL), addr: addr}
	c.rateLimit = NewTokenBucket(InitialRateBytesSec, addr)
	c.rateCtrl = NewBBRv2Controller(c.rateLimit, BBRv2Options{})
	t.Cleanup(c.closeState)
	return c
}

func admissionPart(id uint64) *MessagePart {
	transferID := make([]byte, 32)
	binary.LittleEndian.PutUint64(transferID, id)
	return &MessagePart{
		TransferID: transferID,
		FecType:    FECRoundRobin{DataSize: 1024, SymbolSize: 512, SymbolsCount: 2},
		TotalSize:  1024,
		Data:       make([]byte, 512),
	}
}

func TestRLDPInvalidFirstPartDoesNotReserveTransfer(t *testing.T) {
	client := newAdmissionClient(t, "192.0.2.1:1000")
	for i := uint64(0); i < 5000; i++ {
		part := admissionPart(i)
		part.FecType = FECRaptorQ{DataSize: 0, SymbolSize: 768, SymbolsCount: 4096}
		part.TotalSize = 1
		if err := client.handleMessagePart(part, true); err == nil {
			t.Fatal("invalid first part was accepted")
		}
	}
	part := admissionPart(5001)
	part.Data = nil
	if err := client.handleMessagePart(part, true); err == nil {
		t.Fatal("invalid symbol length was accepted")
	}
	part = admissionPart(5002)
	part.Part = 1
	if err := client.handleMessagePart(part, true); err != nil {
		t.Fatal(err)
	}
	if got := client.Stats(); got.Active.InboundStreams != 0 || got.Inbound.TransfersStarted != 0 {
		t.Fatalf("invalid parts retained transfer state: %+v", got)
	}
	if err := client.handleMessagePart(admissionPart(5003), true); err != nil {
		t.Fatalf("valid transfer after invalid flood: %v", err)
	}
}

func TestRLDPInboundTransferLimitSharedByIP(t *testing.T) {
	first := newAdmissionClient(t, "192.0.2.2:1000")
	second := newAdmissionClient(t, "[::ffff:192.0.2.2]:2000")
	other := newAdmissionClient(t, "192.0.2.3:1000")
	for i := 0; i < maxInboundTransfersPerIP; i++ {
		client := first
		if i%2 != 0 {
			client = second
		}
		if err := client.handleMessagePart(admissionPart(uint64(i)), true); err != nil {
			t.Fatalf("transfer %d: %v", i, err)
		}
	}
	for _, client := range []*RLDP{first, second} {
		if err := client.handleMessagePart(admissionPart(2000), true); !errors.Is(err, ErrTooManyInboundTransfers) {
			t.Fatalf("transfer beyond shared IP limit: %v", err)
		}
	}
	if err := first.handleMessagePart(admissionPart(0), true); err != nil {
		t.Fatalf("existing transfer at the limit: %v", err)
	}
	if err := other.handleMessagePart(admissionPart(0), true); err != nil {
		t.Fatalf("independent IP: %v", err)
	}
	if got := first.Stats().Active.InboundStreams + second.Stats().Active.InboundStreams; got != maxInboundTransfersPerIP {
		t.Fatalf("retained streams = %d, want %d", got, maxInboundTransfersPerIP)
	}

	first.cleanupRecvStreams(time.Now().Add(_recvStreamIdleTimeout + time.Second))
	if err := second.handleMessagePart(admissionPart(2000), true); err != nil {
		t.Fatalf("admission after expiry: %v", err)
	}
	second.closeState()
	if err := first.handleMessagePart(admissionPart(2001), true); err != nil {
		t.Fatalf("admission after other connection closed: %v", err)
	}
}

func TestRLDPInboundTransferLimitConcurrent(t *testing.T) {
	clients := []*RLDP{
		newAdmissionClient(t, "192.0.2.4:1000"),
		newAdmissionClient(t, "192.0.2.4:2000"),
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Go(func() {
			for id := uint64(0); id < 1000; id++ {
				err := client.handleMessagePart(admissionPart(id), true)
				if err == nil {
					accepted.Add(1)
				} else if !errors.Is(err, ErrTooManyInboundTransfers) {
					t.Errorf("unexpected admission error: %v", err)
				}
			}
		})
	}
	wg.Wait()
	if got := accepted.Load(); got != maxInboundTransfersPerIP {
		t.Fatalf("accepted = %d, want %d", got, maxInboundTransfersPerIP)
	}
}

func TestRLDPSendFastSymbolsPublishesStartTime(t *testing.T) {
	client := newInertClient()
	client.rateLimit = NewTokenBucket(InitialRateBytesSec, "test")
	client.rateCtrl = NewBBRv2Controller(client.rateLimit, BBRv2Options{})
	for range 100 {
		encoder, err := roundrobin.NewEncoder(bytes.Repeat([]byte{1}, 16), 768)
		if err != nil {
			t.Fatal(err)
		}
		part := &activeTransferPart{
			encoder: encoder, fec: FECRoundRobin{DataSize: 16, SymbolSize: 768, SymbolsCount: 1},
			fecSymbolSize: 768, fecSymbolsCount: 1, fastSeqnoTill: 1, sendClock: NewSendClock(64),
		}
		transfer := &activeTransfer{id: make([]byte, 32), totalSize: 16}
		transfer.currentPart.Store(part)
		started := make(chan time.Time, 1)
		go func() {
			for !part.recoveryReady.Load() {
				runtime.Gosched()
			}
			started <- part.startedAt
		}()
		if err := client.sendFastSymbols(t.Context(), transfer); err != nil {
			t.Fatal(err)
		}
		if at := <-started; at.IsZero() {
			t.Fatal("recovery observed an uninitialized start time")
		}
	}
}
