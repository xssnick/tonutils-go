package rldp

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

func testTransferID(value string) (id [32]byte) {
	copy(id[:], value)
	return id
}

func TestMessagePartParseCopyModes(t *testing.T) {
	tests := []struct {
		name string
		new  func() tl.Serializable
	}{
		{
			name: "v1",
			new: func() tl.Serializable {
				return MessagePart{
					TransferID: bytes.Repeat([]byte{0xA5}, 32),
					FecType: FECRaptorQ{
						DataSize:     768,
						SymbolSize:   768,
						SymbolsCount: 1,
					},
					TotalSize: 768,
					Data:      bytes.Repeat([]byte{0x5A}, 768),
				}
			},
		},
		{
			name: "v2",
			new: func() tl.Serializable {
				return MessagePartV2{
					TransferID: bytes.Repeat([]byte{0xA5}, 32),
					FecType: FECRaptorQ{
						DataSize:     768,
						SymbolSize:   768,
						SymbolsCount: 1,
					},
					TotalSize: 768,
					Data:      bytes.Repeat([]byte{0x5A}, 768),
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := tl.Serialize(test.new(), true)
			if err != nil {
				t.Fatal(err)
			}

			payloadOffset := bytes.Index(encoded, bytes.Repeat([]byte{0x5A}, 768))
			if payloadOffset < 0 {
				t.Fatal("payload not found in serialized message part")
			}

			t.Run("owned", func(t *testing.T) {
				data := append([]byte(nil), encoded...)
				var parsed tl.Serializable
				if _, err = tl.Parse(&parsed, data, true); err != nil {
					t.Fatal(err)
				}

				transferID, payload := messagePartFields(t, parsed)
				data[4] = 0x11
				data[payloadOffset] = 0x22
				if transferID[0] == 0x11 || payload[0] == 0x22 {
					t.Fatal("Parse result aliases its input")
				}
			})

			t.Run("no_copy", func(t *testing.T) {
				data := append([]byte(nil), encoded...)
				var parsed tl.Serializable
				if _, err = tl.ParseNoCopy(&parsed, data, true); err != nil {
					t.Fatal(err)
				}

				transferID, payload := messagePartFields(t, parsed)
				data[4] = 0x11
				data[payloadOffset] = 0x22
				if transferID[0] != 0x11 || payload[0] != 0x22 {
					t.Fatal("ParseNoCopy result does not alias its input")
				}
				if cap(transferID) != len(transferID) || cap(payload) != len(payload) {
					t.Fatal("ParseNoCopy returned slices with writable capacity past their fields")
				}
			})

			t.Run("message_custom_owns_payload", func(t *testing.T) {
				data, err := tl.Serialize(adnl.MessageCustom{Data: test.new()}, true)
				if err != nil {
					t.Fatal(err)
				}

				payloadOffset := bytes.Index(data, bytes.Repeat([]byte{0x5A}, 768))
				if payloadOffset < 0 {
					t.Fatal("payload not found in serialized custom message")
				}

				var parsed adnl.MessageCustom
				if _, err = tl.ParseNoCopy(&parsed, data, true); err != nil {
					t.Fatal(err)
				}

				transferID, payload := messagePartFields(t, parsed.Data)
				transferOffset := bytes.Index(data, bytes.Repeat([]byte{0xA5}, 32))
				if transferOffset < 0 {
					t.Fatal("transfer id not found in serialized custom message")
				}
				data[transferOffset] = 0x11
				data[payloadOffset] = 0x22
				if transferID[0] == 0x11 || payload[0] == 0x22 {
					t.Fatal("MessageCustom ParseNoCopy leaked aliases to its input")
				}
			})
		})
	}
}

func messagePartFields(t *testing.T, value tl.Serializable) ([]byte, []byte) {
	t.Helper()

	switch part := value.(type) {
	case MessagePart:
		return part.TransferID, part.Data
	case MessagePartV2:
		return part.TransferID, part.Data
	default:
		t.Fatalf("unexpected message part type %T", value)
		return nil, nil
	}
}

type recordingFECDecoder struct {
	addCalls    int
	decodeCalls int
	addErr      error
	canDecode   bool
}

func (d *recordingFECDecoder) AddSymbol(uint32, []byte) (bool, error) {
	d.addCalls++
	if d.addErr != nil {
		err := d.addErr
		d.addErr = nil
		return false, err
	}
	return d.canDecode, nil
}

func (d *recordingFECDecoder) Decode() (bool, []byte, error) {
	d.decodeCalls++
	return false, nil, nil
}

func TestProcessStreamMessagePartSkipsDuplicateBeforeDecoder(t *testing.T) {
	tests := []struct {
		name string
		isV2 bool
	}{
		{name: "v1"},
		{name: "v2", isV2: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoder := &recordingFECDecoder{canDecode: true}
			var confirms []tl.Serializable
			client := &RLDP{
				adnl: MockADNL{
					sendCustomMessage: func(_ context.Context, value tl.Serializable) error {
						confirms = append(confirms, value)
						return nil
					},
				},
				stats: &clientStats{},
			}

			now := time.Now()
			stream := &decoderStream{
				totalSize: 1,
				activeParts: map[uint32]*decoderStreamPart{
					0: {
						decoder:         decoder,
						fecSymbolSize:   1,
						fecSymbolsCount: 1,
						lastConfirmAt:   now.Add(-time.Second),
					},
				},
				nextPartIndex: 1,
			}
			part := &MessagePart{
				TransferID: make([]byte, 32),
				Part:       0,
				TotalSize:  1,
				Seqno:      0,
				Data:       []byte{0x42},
			}

			if err := client.processStreamMessagePart(stream, part, now, test.isV2); err != nil {
				t.Fatal(err)
			}
			duplicateAt := now.Add(21 * time.Millisecond)
			if err := client.processStreamMessagePart(stream, part, duplicateAt, test.isV2); err != nil {
				t.Fatal(err)
			}

			current := stream.activeParts[0]
			if decoder.addCalls != 1 || decoder.decodeCalls != 1 {
				t.Fatalf("duplicate reached decoder: AddSymbol=%d Decode=%d", decoder.addCalls, decoder.decodeCalls)
			}
			if current.receivedNum != 1 || current.receivedBytes != 1 || current.receivedFastNum != 1 || current.receivedRepairNum != 0 {
				t.Fatalf("duplicate changed receive counters: %+v", current)
			}
			if !stream.lastMessageAt.Equal(duplicateAt) {
				t.Fatal("duplicate did not refresh stream activity")
			}
			if len(confirms) != 2 {
				t.Fatalf("confirmations=%d want=2", len(confirms))
			}

			for i, value := range confirms {
				if test.isV2 {
					confirm, ok := value.(ConfirmV2)
					if !ok {
						t.Fatalf("confirmation %d has type %T, want ConfirmV2", i, value)
					}
					if confirm.ReceivedCount != 1 || confirm.ReceivedMask != 1 || confirm.MaxSeqno != 1 {
						t.Fatalf("confirmation %d has duplicate-inflated state: %+v", i, confirm)
					}
					continue
				}

				confirm, ok := value.(Confirm)
				if !ok {
					t.Fatalf("confirmation %d has type %T, want Confirm", i, value)
				}
				if confirm.Seqno != 0 {
					t.Fatalf("confirmation %d has seqno=%d want=0", i, confirm.Seqno)
				}
			}
		})
	}
}

func TestProcessStreamMessagePartDoesNotRecordRejectedSymbol(t *testing.T) {
	decoder := &recordingFECDecoder{addErr: errors.New("invalid symbol")}
	client := &RLDP{adnl: MockADNL{}, stats: &clientStats{}}
	now := time.Now()
	stream := &decoderStream{
		totalSize: 1,
		activeParts: map[uint32]*decoderStreamPart{
			0: {
				decoder:         decoder,
				fecSymbolSize:   1,
				fecSymbolsCount: 1,
				lastConfirmAt:   now.Add(time.Hour),
			},
		},
		nextPartIndex: 1,
	}
	part := &MessagePart{TransferID: make([]byte, 32), TotalSize: 1, Seqno: 5, Data: []byte{0x42}}

	if err := client.processStreamMessagePart(stream, part, now, true); err == nil {
		t.Fatal("expected decoder error")
	}
	current := stream.activeParts[0]
	if current.maxSeqno != 0 || current.receivedMask != 0 || current.receivedNum != 0 {
		t.Fatal("rejected symbol was recorded as received")
	}

	if err := client.processStreamMessagePart(stream, part, now, true); err != nil {
		t.Fatal(err)
	}
	if decoder.addCalls != 2 || current.maxSeqno != 5 || current.receivedMask != 1 || current.receivedNum != 1 {
		t.Fatal("corrected retransmission was not accepted")
	}
}

func TestProcessStreamMessagePartTracksReceiveWindowBoundary(t *testing.T) {
	decoder := &recordingFECDecoder{}
	client := &RLDP{adnl: MockADNL{}, stats: &clientStats{}}
	now := time.Now()
	current := &decoderStreamPart{
		decoder:         decoder,
		fecSymbolSize:   1,
		fecSymbolsCount: 2,
		maxSeqno:        32,
		receivedMask:    1,
		lastConfirmAt:   now.Add(time.Hour),
	}
	stream := &decoderStream{
		totalSize:     1,
		activeParts:   map[uint32]*decoderStreamPart{0: current},
		nextPartIndex: 1,
	}
	part := &MessagePart{TransferID: make([]byte, 32), TotalSize: 1, Seqno: 1, Data: []byte{0x42}}

	if err := client.processStreamMessagePart(stream, part, now, true); err != nil {
		t.Fatal(err)
	}
	if err := client.processStreamMessagePart(stream, part, now, true); err != nil {
		t.Fatal(err)
	}

	if decoder.addCalls != 1 || current.receivedNum != 1 {
		t.Fatalf("offset-31 duplicate reached decoder: AddSymbol=%d received=%d", decoder.addCalls, current.receivedNum)
	}
	if current.receivedMask != 0x80000001 {
		t.Fatalf("received mask=%032b want=%032b", current.receivedMask, uint32(0x80000001))
	}
}

func TestProcessStreamMessagePartAcceptsUntrackedOldSymbol(t *testing.T) {
	tests := []struct {
		name string
		isV2 bool
	}{
		{name: "v1"},
		{name: "v2", isV2: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoder := &recordingFECDecoder{}
			client := &RLDP{adnl: MockADNL{}, stats: &clientStats{}}
			now := time.Now()
			current := &decoderStreamPart{
				decoder:         decoder,
				fecSymbolSize:   1,
				fecSymbolsCount: 1,
				lastConfirmAt:   now.Add(time.Hour),
			}
			stream := &decoderStream{
				totalSize:     1,
				activeParts:   map[uint32]*decoderStreamPart{0: current},
				nextPartIndex: 1,
			}
			part := &MessagePart{TransferID: make([]byte, 32), TotalSize: 1, Seqno: 32, Data: []byte{0x42}}

			if err := client.processStreamMessagePart(stream, part, now, test.isV2); err != nil {
				t.Fatal(err)
			}
			part.Seqno = 0
			if err := client.processStreamMessagePart(stream, part, now, test.isV2); err != nil {
				t.Fatal(err)
			}

			if decoder.addCalls != 2 {
				t.Fatalf("AddSymbol calls=%d want=2", decoder.addCalls)
			}
			if current.receivedNum != 2 {
				t.Fatalf("received count=%d want=2", current.receivedNum)
			}
		})
	}
}

func TestHandleMessagePartRejectsInvalidTransferID(t *testing.T) {
	client := &RLDP{
		recvStreams:       map[[32]byte]*decoderStream{},
		expectedTransfers: map[[32]byte]*activeRequest{},
	}

	err := client.handleMessagePart(&MessagePart{TransferID: make([]byte, 31)}, false)
	if err == nil {
		t.Fatal("expected invalid transfer id error")
	}
}

func BenchmarkMessageCustomParseNoCopy(b *testing.B) {
	tests := []struct {
		name string
		part tl.Serializable
	}{
		{
			name: "v1",
			part: MessagePart{
				TransferID: make([]byte, 32),
				FecType:    FECRaptorQ{DataSize: 768, SymbolSize: 768, SymbolsCount: 1},
				TotalSize:  768,
				Data:       make([]byte, 768),
			},
		},
		{
			name: "v2",
			part: MessagePartV2{
				TransferID: make([]byte, 32),
				FecType:    FECRaptorQ{DataSize: 768, SymbolSize: 768, SymbolsCount: 1},
				TotalSize:  768,
				Data:       make([]byte, 768),
			},
		},
	}

	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			data, err := tl.Serialize(adnl.MessageCustom{Data: test.part}, true)
			if err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			for b.Loop() {
				var parsed adnl.MessageCustom
				if _, err = tl.ParseNoCopy(&parsed, data, true); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkHandleMessagePartV2ExistingStream(b *testing.B) {
	transferID := make([]byte, 32)
	finishedAt := time.Now()
	stream := &decoderStream{
		finishedAt:     &finishedAt,
		lastCompleteAt: finishedAt.Add(time.Hour),
		msgBuf:         NewQueue(1),
	}
	client := &RLDP{
		adnl:        noopADNL{},
		recvStreams: map[[32]byte]*decoderStream{[32]byte(transferID): stream},
		stats:       &clientStats{},
	}
	part := MessagePartV2{TransferID: transferID}

	b.ReportAllocs()
	for b.Loop() {
		message := &adnl.MessageCustom{Data: part}
		if err := client.handleMessage(message); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProcessStreamMessagePartV2Duplicate(b *testing.B) {
	decoder := &recordingFECDecoder{canDecode: true}
	now := time.Now()
	current := &decoderStreamPart{
		decoder:              decoder,
		fecSymbolSize:        1,
		fecSymbolsCount:      1,
		receivedMask:         1,
		receivedNum:          1,
		receivedFastNum:      1,
		receivedNumConfirmed: 1,
		receivedBytes:        1,
		lastConfirmAt:        now.Add(time.Hour),
	}
	stream := &decoderStream{
		totalSize:     1,
		activeParts:   map[uint32]*decoderStreamPart{0: current},
		nextPartIndex: 1,
	}
	part := &MessagePart{TransferID: make([]byte, 32), TotalSize: 1, Data: []byte{0x42}}
	client := &RLDP{adnl: noopADNL{}, stats: &clientStats{}}

	b.ReportAllocs()
	for b.Loop() {
		if err := client.processStreamMessagePart(stream, part, now, true); err != nil {
			b.Fatal(err)
		}
	}
}
