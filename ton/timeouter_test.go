package ton

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tl"
)

type timeoutRetryMock struct {
	calls         int
	nextNodeCalls int
	response      tl.Serializable
}

func (m *timeoutRetryMock) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	m.calls++
	if m.calls == 1 {
		<-ctx.Done()
		return ctx.Err()
	}

	reflect.ValueOf(result).Elem().Set(reflect.ValueOf(m.response))
	return nil
}

func (m *timeoutRetryMock) StickyContext(ctx context.Context) context.Context {
	return ctx
}

func (m *timeoutRetryMock) StickyContextNextNode(ctx context.Context) (context.Context, error) {
	m.nextNodeCalls++
	return ctx, nil
}

func (m *timeoutRetryMock) StickyContextNextNodeBalanced(ctx context.Context) (context.Context, error) {
	m.nextNodeCalls++
	return ctx, nil
}

func (m *timeoutRetryMock) StickyNodeID(ctx context.Context) uint32 {
	return 0
}

func TestAPIClient_WithRetryTimeout_RetriesAttemptTimeout(t *testing.T) {
	mock := &timeoutRetryMock{
		response: MasterchainInfo{
			Last: &BlockIDExt{SeqNo: 123},
		},
	}

	api := NewAPIClient(mock).WithRetryTimeout(0, 20*time.Millisecond)

	block, err := api.GetMasterchainInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if block.SeqNo != 123 {
		t.Fatalf("unexpected block seqno: %d", block.SeqNo)
	}

	if mock.calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", mock.calls)
	}

	if mock.nextNodeCalls != 1 {
		t.Fatalf("expected 1 failover, got %d", mock.nextNodeCalls)
	}
}

func TestAPIClient_WithRetryTimeout_DoesNotMaskParentDeadline(t *testing.T) {
	mock := &timeoutRetryMock{
		response: MasterchainInfo{
			Last: &BlockIDExt{SeqNo: 123},
		},
	}

	api := NewAPIClient(mock).WithRetryTimeout(0, 200*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := api.GetMasterchainInfo(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}

	if errors.Is(err, liteclient.ErrADNLReqTimeout) {
		t.Fatalf("parent deadline must not become retryable timeout: %v", err)
	}

	if mock.calls != 1 {
		t.Fatalf("expected 1 attempt, got %d", mock.calls)
	}

	if mock.nextNodeCalls != 0 {
		t.Fatalf("expected no failover, got %d", mock.nextNodeCalls)
	}
}
