package rldp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

type deliveredRLDPMessage struct {
	id   []byte
	data []byte
}

func TestRLDP_SendMessageWireFraming(t *testing.T) {
	closerCtx, closeClient := context.WithCancel(context.Background())
	defer closeClient()

	var (
		captureMx  sync.Mutex
		decoder    fecDecoder
		wire       []byte
		transferID []byte
	)
	transport := MockADNL{
		closerCtx: closerCtx,
		sendCustomMessage: func(_ context.Context, req tl.Serializable) error {
			captureMx.Lock()
			defer captureMx.Unlock()

			part, ok := req.(MessagePartV2)
			if !ok {
				return fmt.Errorf("got wire part %T, want MessagePartV2", req)
			}
			if wire != nil {
				return nil
			}
			if decoder == nil {
				var err error
				decoder, err = createFECDecoder(part.FecType, part.TotalSize)
				if err != nil {
					return err
				}
				transferID = append([]byte(nil), part.TransferID...)
			}

			canDecode, err := decoder.AddSymbol(part.Seqno, part.Data)
			if err != nil {
				return err
			}
			if !canDecode {
				return nil
			}

			decoded, data, err := decoder.Decode()
			if err != nil {
				return err
			}
			if decoded {
				wire = append([]byte(nil), data...)
			}
			return nil
		},
	}
	client := NewClientV2(transport)
	defer client.closeState()

	payload := []byte("fire-and-forget")
	if err := client.SendMessage(context.Background(), payload); err != nil {
		t.Fatalf("send message: %v", err)
	}

	captureMx.Lock()
	gotWire := append([]byte(nil), wire...)
	gotTransferID := append([]byte(nil), transferID...)
	captureMx.Unlock()

	if len(gotTransferID) != 32 || bytes.Equal(gotTransferID, make([]byte, 32)) {
		t.Fatalf("transfer id = %x, want a random int256", gotTransferID)
	}
	if len(gotWire) == 0 {
		t.Fatal("message transfer did not produce a decodable wire payload")
	}

	var decoded any
	rest, err := tl.ParseNoCopy(&decoded, gotWire, true)
	if err != nil {
		t.Fatalf("parse message wire payload: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("message wire payload has %d trailing bytes", len(rest))
	}

	message, ok := decoded.(Message)
	if !ok {
		t.Fatalf("decoded wire type = %T, want Message value", decoded)
	}
	if len(message.ID) != 32 || bytes.Equal(message.ID, make([]byte, 32)) {
		t.Fatalf("message id = %x, want a random int256", message.ID)
	}
	if !bytes.Equal(message.Data, payload) {
		t.Fatalf("message payload = %x, want %x", message.Data, payload)
	}

	client.mx.RLock()
	transfer := client.activeTransfers[string(gotTransferID)]
	client.mx.RUnlock()
	if transfer == nil {
		t.Fatal("message transfer was not registered")
	}
	remaining := time.Until(time.UnixMilli(transfer.timeoutAt))
	if remaining < 8*time.Second || remaining > 11*time.Second {
		t.Fatalf("default transfer timeout remaining = %v, want about 10s", remaining)
	}
}

func TestRLDP_SendMessageDeliversToPeer(t *testing.T) {
	senderCloser, closeSender := context.WithCancel(context.Background())
	receiverCloser, closeReceiver := context.WithCancel(context.Background())
	defer closeSender()
	defer closeReceiver()

	var sender, receiver *RLDP
	senderTransport := MockADNL{
		closerCtx: senderCloser,
		sendCustomMessage: func(_ context.Context, req tl.Serializable) error {
			return receiver.handleMessage(&adnl.MessageCustom{Data: req})
		},
	}
	receiverTransport := MockADNL{
		closerCtx: receiverCloser,
		sendCustomMessage: func(_ context.Context, req tl.Serializable) error {
			return sender.handleMessage(&adnl.MessageCustom{Data: req})
		},
	}

	receiver = NewClientV2(receiverTransport)
	sender = NewClientV2(senderTransport)
	defer receiver.closeState()
	defer sender.closeState()

	delivered := make(chan deliveredRLDPMessage, 1)
	receiver.SetOnMessage(func(id []byte, data []byte) error {
		delivered <- deliveredRLDPMessage{
			id:   append([]byte(nil), id...),
			data: append([]byte(nil), data...),
		}
		return nil
	})

	payload := []byte("delivered over rldp2")
	if err := sender.SendMessage(context.Background(), payload); err != nil {
		t.Fatalf("send message: %v", err)
	}

	select {
	case message := <-delivered:
		if len(message.id) != 32 {
			t.Fatalf("delivered message id length = %d, want 32", len(message.id))
		}
		if !bytes.Equal(message.data, payload) {
			t.Fatalf("delivered payload = %x, want %x", message.data, payload)
		}
	case <-time.After(time.Second):
		t.Fatal("message was not delivered")
	}
}

func TestRLDP_SendMessagePreservesSubsecondDeadline(t *testing.T) {
	closerCtx, closeClient := context.WithCancel(context.Background())
	defer closeClient()

	var (
		captureMx  sync.Mutex
		transferID []byte
	)
	client := NewClientV2(MockADNL{
		closerCtx: closerCtx,
		sendCustomMessage: func(_ context.Context, req tl.Serializable) error {
			part, ok := req.(MessagePartV2)
			if !ok {
				return fmt.Errorf("got wire part %T, want MessagePartV2", req)
			}

			captureMx.Lock()
			if transferID == nil {
				transferID = append([]byte(nil), part.TransferID...)
			}
			captureMx.Unlock()
			return nil
		},
	})
	defer client.closeState()

	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	deadline, _ := ctx.Deadline()

	if err := client.SendMessage(ctx, []byte("short deadline")); err != nil {
		t.Fatalf("send message: %v", err)
	}

	captureMx.Lock()
	gotTransferID := append([]byte(nil), transferID...)
	captureMx.Unlock()
	if len(gotTransferID) != 32 {
		t.Fatalf("transfer id length = %d, want 32", len(gotTransferID))
	}

	client.mx.RLock()
	transfer := client.activeTransfers[string(gotTransferID)]
	client.mx.RUnlock()
	if transfer == nil {
		t.Fatal("subsecond deadline produced no transfer")
	}
	if transfer.timeoutAt != deadline.UnixMilli() {
		t.Fatalf("transfer timeout = %d, want %d", transfer.timeoutAt, deadline.UnixMilli())
	}
}

func TestRLDP_SendMessageCancellation(t *testing.T) {
	closerCtx, closeClient := context.WithCancel(context.Background())
	defer closeClient()

	var sends atomic.Int32
	client := NewClientV2(MockADNL{
		closerCtx: closerCtx,
		sendCustomMessage: func(_ context.Context, _ tl.Serializable) error {
			sends.Add(1)
			return nil
		},
	})
	defer client.closeState()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.SendMessage(canceled, []byte{1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled send error = %v, want context.Canceled", err)
	}
	if err := client.startTransfer(
		context.Background(),
		make([]byte, 32),
		[]byte{1},
		time.Now().Add(-time.Millisecond).UnixMilli(),
	); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired transfer error = %v, want context.DeadlineExceeded", err)
	}

	if got := sends.Load(); got != 0 {
		t.Fatalf("transport sends = %d, want 0", got)
	}

	client.mx.RLock()
	active := len(client.activeTransfers)
	client.mx.RUnlock()
	if active != 0 {
		t.Fatalf("active transfers = %d, want 0", active)
	}
}

func TestRLDP_SendMessageAcceptsExtendedTLBytes(t *testing.T) {
	closerCtx, closeClient := context.WithCancel(context.Background())
	defer closeClient()

	var sends atomic.Int32
	client := NewClientV2(MockADNL{
		closerCtx: closerCtx,
		sendCustomMessage: func(_ context.Context, _ tl.Serializable) error {
			sends.Add(1)
			return nil
		},
	})
	defer client.closeState()

	if err := client.SendMessage(context.Background(), make([]byte, 1<<24)); err != nil {
		t.Fatalf("extended TL bytes message: %v", err)
	}
	if got := sends.Load(); got == 0 {
		t.Fatal("extended TL bytes message produced no transport sends")
	}

	client.mx.RLock()
	active := len(client.activeTransfers)
	client.mx.RUnlock()
	if active != 1 {
		t.Fatalf("active transfers = %d, want 1", active)
	}
}
