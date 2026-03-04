package ton

import (
	"context"
	"reflect"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tl"
)

type ValidationMock struct {
	CheckQuery func(payload tl.Serializable) error
	Response   tl.Serializable
}

func (m *ValidationMock) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	if err := m.CheckQuery(payload); err != nil {
		return err
	}
	// reflect to set result
	reflect.ValueOf(result).Elem().Set(reflect.ValueOf(m.Response))
	return nil
}

func (m *ValidationMock) StickyContext(ctx context.Context) context.Context {
	return ctx
}

func (m *ValidationMock) StickyContextNextNode(ctx context.Context) (context.Context, error) {
	return ctx, nil
}

func (m *ValidationMock) StickyContextNextNodeBalanced(ctx context.Context) (context.Context, error) {
	return ctx, nil
}

func (m *ValidationMock) StickyNodeID(ctx context.Context) uint32 {
	return 0
}

func TestGetOutMsgQueueSizes(t *testing.T) {
	mock := &ValidationMock{
		Response: OutMsgQueueSizes{
			ExtMsgQueueSizeLimit: 100,
			Shards: []OutMsgQueueSize{
				{
					ID:   &BlockIDExt{Workchain: 0, Shard: -9223372036854775808, SeqNo: 100},
					Size: 50,
				},
			},
		},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(GetOutMsgQueueSizes)
			if !ok {
				t.Fatalf("unexpected request type: %T", payload)
			}
			if req.Mode != 0 {
				t.Errorf("expected mode 0, got %d", req.Mode)
			}
			return nil
		},
	}

	client := NewAPIClient(mock)
	res, err := client.GetOutMsgQueueSizes(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if res.ExtMsgQueueSizeLimit != 100 {
		t.Errorf("expected limit 100, got %d", res.ExtMsgQueueSizeLimit)
	}
	if len(res.Shards) != 1 {
		t.Errorf("expected 1 shard, got %d", len(res.Shards))
	}
}

func TestGetOutMsgQueueSizes_SpecificMsg(t *testing.T) {
	wc := int32(-1)
	shard := int64(-9223372036854775808)

	mock := &ValidationMock{
		Response: OutMsgQueueSizes{},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(GetOutMsgQueueSizes)
			if !ok {
				t.Fatalf("unexpected request type: %T", payload)
			}
			if req.Mode != 1 {
				t.Errorf("expected mode 1, got %d", req.Mode)
			}
			if req.WC != wc || req.Shard != shard {
				t.Errorf("unexpected wc/shard: %d/%d", req.WC, req.Shard)
			}
			return nil
		},
	}

	client := NewAPIClient(mock)
	_, err := client.GetOutMsgQueueSizes(context.Background(), &wc, &shard)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetBlockOutMsgQueueSize(t *testing.T) {
	block := &BlockIDExt{
		Workchain: 0,
		Shard:     -9223372036854775808,
		SeqNo:     12345,
	}

	mock := &ValidationMock{
		Response: BlockOutMsgQueueSize{
			ID:   block,
			Size: 1024,
		},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(GetBlockOutMsgQueueSize)
			if !ok {
				t.Fatalf("unexpected request type: %T", payload)
			}
			if !reflect.DeepEqual(req.ID, block) {
				t.Errorf("unexpected block id in request")
			}
			return nil
		},
	}

	client := NewAPIClient(mock)
	res, err := client.GetBlockOutMsgQueueSize(context.Background(), block)
	if err != nil {
		t.Fatal(err)
	}

	if res.Size != 1024 {
		t.Errorf("expected size 1024, got %d", res.Size)
	}
}

func TestGetDispatchQueueInfo(t *testing.T) {
	block := &BlockIDExt{Workchain: 0, SeqNo: 100}
	addr := address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")

	mock := &ValidationMock{
		Response: DispatchQueueInfo{
			ID:       block,
			Complete: true,
			AccountDispatchQueues: []AccountDispatchQueueInfo{
				{
					Addr: addr.Data(),
					Size: 500,
				},
			},
		},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(GetDispatchQueueInfo)
			if !ok {
				t.Fatalf("unexpected request type: %T", payload)
			}
			if req.MaxAccounts != 10 {
				t.Errorf("expected max accounts 10, got %d", req.MaxAccounts)
			}
			if req.Mode&2 == 0 { // 1 << 1
				t.Error("expected after addr flag")
			}
			return nil
		},
	}

	client := NewAPIClient(mock)
	res, err := client.GetDispatchQueueInfo(context.Background(), block, addr, 10)
	if err != nil {
		t.Fatal(err)
	}

	if !res.Complete {
		t.Error("expected complete")
	}
}

func TestGetDispatchQueueMessages(t *testing.T) {
	block := &BlockIDExt{Workchain: 0, SeqNo: 100}
	addr := address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")

	mock := &ValidationMock{
		Response: DispatchQueueMessages{
			ID: block,
			Messages: []DispatchQueueMessage{
				{
					LT: 1000,
					Metadata: TransactionMetadata{
						Depth: 1,
						Initiator: AccountId{
							Workchain: 0,
							ID:        addr.Data(),
						},
					},
				},
			},
		},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(GetDispatchQueueMessages)
			if !ok {
				t.Fatalf("unexpected request type: %T", payload)
			}
			if req.AfterLT != 500 {
				t.Errorf("expected after lt 500, got %d", req.AfterLT)
			}
			if req.Mode&4 == 0 { // 1 << 2
				t.Error("expected messages boc flag")
			}
			return nil
		},
	}

	client := NewAPIClient(mock)
	// Using WithDispatchQueueMessagesBOC option
	res, err := client.GetDispatchQueueMessages(context.Background(), block, addr, 500, 5, WithDispatchQueueMessagesBOC())
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(res.Messages))
	}

	// Check helper methods
	if res.Messages[0].Metadata.InitiatorAddress().String() != addr.String() {
		t.Error("initiator address mismatch")
	}
}
