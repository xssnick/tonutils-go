package rldp

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/rldp/roundrobin"
	"github.com/xssnick/tonutils-go/tl"
)

func TestCreateFECDecoderValidation(t *testing.T) {
	cases := []struct {
		name    string
		fec     FEC
		maxSize uint64
		wantOk  bool
	}{
		{"valid raptorq", FECRaptorQ{DataSize: 4096, SymbolSize: 768, SymbolsCount: 6}, 4096, true},
		{"valid max symbol size", FECRaptorQ{DataSize: 4096, SymbolSize: MaxSymbolSize, SymbolsCount: 2}, 4096, true},
		{"legacy symbols count ceil+1", FECRaptorQ{DataSize: 1536, SymbolSize: 768, SymbolsCount: 3}, 1536, true},
		{"valid round robin", FECRoundRobin{DataSize: 1024, SymbolSize: 512, SymbolsCount: 2}, 1024, true},
		{"zero symbol size", FECRaptorQ{DataSize: 4096, SymbolSize: 0, SymbolsCount: 6}, 4096, false},
		{"too big symbol size", FECRaptorQ{DataSize: 4096, SymbolSize: MaxSymbolSize + 1, SymbolsCount: 2}, 4096, false},
		{"zero data size", FECRaptorQ{DataSize: 0, SymbolSize: 768, SymbolsCount: 0}, 4096, false},
		{"data size over left transfer space", FECRaptorQ{DataSize: 4096, SymbolSize: 768, SymbolsCount: 6}, 4095, false},
		{"data size over limit", FECRaptorQ{DataSize: MaxFECDataSize + 1, SymbolSize: 768, SymbolsCount: 2731}, uint64(MaxFECDataSize) + 1, false},
		{"too small symbols count", FECRaptorQ{DataSize: 4096, SymbolSize: 768, SymbolsCount: 5}, 4096, false},
		{"too big symbols count", FECRaptorQ{DataSize: 4096, SymbolSize: 768, SymbolsCount: 8}, 4096, false},
		{"huge symbols count", FECRaptorQ{DataSize: 4096, SymbolSize: 768, SymbolsCount: 1 << 30}, 4096, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dec, err := createFECDecoder(c.fec, c.maxSize)
			if c.wantOk {
				if err != nil {
					t.Fatalf("expected decoder, got error: %v", err)
				}
				if dec == nil {
					t.Fatal("expected decoder instance")
				}
			} else if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestRLDP_handleMessageRejectsBadFECBeforeDecoder(t *testing.T) {
	transferID := make([]byte, 32)
	if _, err := rand.Read(transferID); err != nil {
		t.Fatal(err)
	}

	cli := NewClient(MockADNL{
		sendCustomMessage: func(ctx context.Context, req tl.Serializable) error {
			return nil
		},
	})

	part := MessagePart{
		TransferID: transferID,
		FecType: FECRaptorQ{
			DataSize:     4096,
			SymbolSize:   MaxSymbolSize + 1,
			SymbolsCount: 2,
		},
		Part:      0,
		TotalSize: 4096,
		Seqno:     0,
		Data:      make([]byte, MaxSymbolSize+1),
	}

	if err := cli.handleMessage(&adnl.MessageCustom{Data: part}); err != nil {
		t.Fatal(err)
	}

	cli.mx.RLock()
	stream := cli.recvStreams[string(transferID)]
	cli.mx.RUnlock()
	if stream == nil {
		t.Fatal("expected stream to be created")
	}

	stream.mx.Lock()
	defer stream.mx.Unlock()
	if stream.nextPartIndex != 0 || len(stream.activeParts) != 0 {
		t.Fatal("expected no decoder part created for invalid fec")
	}
}

func TestRLDP_handleMessageOutOfOrderParts(t *testing.T) {
	transferID := make([]byte, 32)
	if _, err := rand.Read(transferID); err != nil {
		t.Fatal(err)
	}
	messageID := make([]byte, 32)
	if _, err := rand.Read(messageID); err != nil {
		t.Fatal(err)
	}

	payload := make([]byte, 1800)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}

	data, err := tl.Serialize(Message{ID: messageID, Data: payload}, true)
	if err != nil {
		t.Fatal(err)
	}

	const symbolSize = 512
	p0, p1 := data[:1200], data[1200:]

	enc0, err := roundrobin.NewEncoder(p0, symbolSize)
	if err != nil {
		t.Fatal(err)
	}
	enc1, err := roundrobin.NewEncoder(p1, symbolSize)
	if err != nil {
		t.Fatal(err)
	}

	genPart := func(idx uint32, partData []byte, seqno uint32, sym []byte) *adnl.MessageCustom {
		return &adnl.MessageCustom{Data: MessagePart{
			TransferID: transferID,
			FecType: FECRoundRobin{
				DataSize:     uint32(len(partData)),
				SymbolSize:   symbolSize,
				SymbolsCount: uint32((len(partData) + symbolSize - 1) / symbolSize),
			},
			Part:      idx,
			TotalSize: uint64(len(data)),
			Seqno:     seqno,
			Data:      sym,
		}}
	}

	var completes []Complete
	var received []byte
	cli := NewClient(MockADNL{
		sendCustomMessage: func(ctx context.Context, req tl.Serializable) error {
			if c, ok := req.(Complete); ok {
				completes = append(completes, c)
			}
			return nil
		},
	})
	cli.SetOnMessage(func(id []byte, msg []byte) error {
		received = msg
		return nil
	})

	// first symbol of part 0 creates its decoder, 3 symbols are needed to decode it
	if err = cli.handleMessage(genPart(0, p0, 0, enc0.GenSymbol(0))); err != nil {
		t.Fatal(err)
	}

	// part 2 breaks creation order, must be ignored
	if err = cli.handleMessage(genPart(2, p1, 0, enc1.GenSymbol(0))); err != nil {
		t.Fatal(err)
	}

	cli.mx.RLock()
	stream := cli.recvStreams[string(transferID)]
	cli.mx.RUnlock()
	if stream == nil {
		t.Fatal("expected stream to be created")
	}

	stream.mx.Lock()
	if stream.nextPartIndex != 1 || len(stream.activeParts) != 1 {
		t.Fatalf("expected only part 0 to be created, got next %d, active %d", stream.nextPartIndex, len(stream.activeParts))
	}
	stream.mx.Unlock()

	// part 1 completes before part 0
	if err = cli.handleMessage(genPart(1, p1, 0, enc1.GenSymbol(0))); err != nil {
		t.Fatal(err)
	}
	if err = cli.handleMessage(genPart(1, p1, 1, enc1.GenSymbol(1))); err != nil {
		t.Fatal(err)
	}

	stream.mx.Lock()
	if len(stream.activeParts) != 1 || stream.activeParts[0] == nil {
		t.Fatalf("expected only part 0 to stay active, got %d active", len(stream.activeParts))
	}
	if stream.dataParts[1] == nil {
		t.Fatal("expected part 1 to be decoded")
	}
	// enable instant completion resend for the stale symbol below
	stream.lastCompleteAt = time.Now().Add(-time.Second)
	stream.mx.Unlock()

	if len(completes) != 1 || completes[0].Part != 1 {
		t.Fatalf("expected completion of part 1, got %v", completes)
	}
	if received != nil {
		t.Fatal("transfer must not be finished yet")
	}

	// stale symbol of decoded part 1 must trigger completion resend without state changes
	if err = cli.handleMessage(genPart(1, p1, 0, enc1.GenSymbol(0))); err != nil {
		t.Fatal(err)
	}
	if len(completes) != 2 || completes[1].Part != 1 {
		t.Fatalf("expected completion resend for part 1, got %v", completes)
	}

	// rest of part 0 finishes the transfer
	if err = cli.handleMessage(genPart(0, p0, 1, enc0.GenSymbol(1))); err != nil {
		t.Fatal(err)
	}
	if err = cli.handleMessage(genPart(0, p0, 2, enc0.GenSymbol(2))); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(received, payload) {
		t.Fatal("expected message handler call with full payload")
	}
	if completes[len(completes)-1].Part != 0 {
		t.Fatalf("expected completion of part 0, got %v", completes)
	}

	stream.mx.Lock()
	defer stream.mx.Unlock()
	if stream.finishedAt == nil {
		t.Fatal("expected stream to be finished")
	}
}
