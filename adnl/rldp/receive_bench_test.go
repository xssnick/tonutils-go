package rldp

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"testing"

	"github.com/xssnick/raptorq"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

type noopADNL struct{}

func (noopADNL) RemoteAddr() string                                             { return "127.0.0.1:1" }
func (noopADNL) GetID() []byte                                                  { return make([]byte, 32) }
func (noopADNL) SetCustomMessageHandler(func(msg *adnl.MessageCustom) error)    {}
func (noopADNL) SetDisconnectHandler(func(addr string, key ed25519.PublicKey))  {}
func (noopADNL) GetDisconnectHandler() func(addr string, key ed25519.PublicKey) { return nil }
func (noopADNL) SendCustomMessage(context.Context, tl.Serializable) error       { return nil }
func (noopADNL) GetCloserCtx() context.Context                                  { return context.Background() }
func (noopADNL) Close()                                                         {}

// BenchmarkReceiveTransfer measures the pure cpu cost of the inbound message
// part processing path, without network, rate limiting and recovery timers.
func BenchmarkReceiveTransfer(b *testing.B) {
	payload := make([]byte, (2<<20)-256)
	msg, err := tl.Serialize(Message{ID: make([]byte, 32), Data: payload}, true)
	if err != nil {
		b.Fatal(err)
	}

	enc, err := raptorq.NewRaptorQ(DefaultSymbolSize).CreateEncoder(msg)
	if err != nil {
		b.Fatal(err)
	}

	fec := FECRaptorQ{
		DataSize:     uint32(len(msg)),
		SymbolSize:   DefaultSymbolSize,
		SymbolsCount: enc.BaseSymbolsNum(),
	}

	// all symbols needed to decode plus a few stragglers hitting the completed part path
	symbols := make([][]byte, enc.BaseSymbolsNum()+8)
	for i := range symbols {
		symbols[i] = enc.GenSymbol(uint32(i))
	}

	oldLimit := MaxUnexpectedTransferSize
	MaxUnexpectedTransferSize = 1 << 30
	defer func() {
		MaxUnexpectedTransferSize = oldLimit
	}()

	cli := NewClient(noopADNL{})
	cli.SetOnMessage(func(id []byte, data []byte) error { return nil })

	b.SetBytes(int64(len(msg)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		transferID := make([]byte, 32)
		binary.LittleEndian.PutUint64(transferID, uint64(i)+1)

		for seq := range symbols {
			p := MessagePart{
				TransferID: transferID,
				FecType:    fec,
				Part:       0,
				TotalSize:  uint64(len(msg)),
				Seqno:      uint32(seq),
				Data:       symbols[seq],
			}

			if err = cli.handleMessage(&adnl.MessageCustom{Data: p}); err != nil {
				b.Fatal(err)
			}
		}
	}
}
