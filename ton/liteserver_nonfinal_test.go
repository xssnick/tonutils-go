package ton

import (
	"context"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
)

func TestGetAllNonfinalValidatorGroups(t *testing.T) {
	mock := &ValidationMock{
		Response: NonfinalValidatorGroups{},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(NonfinalGetValidatorGroups)
			if !ok {
				t.Fatalf("unexpected request type: %T", payload)
			}
			if req.Mode != 0 {
				t.Fatalf("expected mode 0, got %d", req.Mode)
			}
			return nil
		},
	}

	_, err := NewAPIClient(mock).GetAllNonfinalValidatorGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetNonfinalValidatorGroups(t *testing.T) {
	wc := int32(-1)
	shard := int64(-9223372036854775808)
	mock := &ValidationMock{
		Response: NonfinalValidatorGroups{},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(NonfinalGetValidatorGroups)
			if !ok {
				t.Fatalf("unexpected request type: %T", payload)
			}
			if req.Mode != 1 {
				t.Fatalf("expected mode 1, got %d", req.Mode)
			}
			if req.WC != wc || req.Shard != shard {
				t.Fatalf("unexpected wc/shard: %d/%d", req.WC, req.Shard)
			}
			return nil
		},
	}

	_, err := NewAPIClient(mock).GetNonfinalValidatorGroups(context.Background(), wc, shard)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetAllNonfinalPendingShardBlocks(t *testing.T) {
	mock := &ValidationMock{
		Response: NonfinalPendingShardBlocks{},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(NonfinalGetPendingShardBlocks)
			if !ok {
				t.Fatalf("unexpected request type: %T", payload)
			}
			if req.Mode != 0 {
				t.Fatalf("expected mode 0, got %d", req.Mode)
			}
			return nil
		},
	}

	_, err := NewAPIClient(mock).GetAllNonfinalPendingShardBlocks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetNonfinalPendingShardBlocks(t *testing.T) {
	wc := int32(-1)
	shard := int64(-9223372036854775808)
	mock := &ValidationMock{
		Response: NonfinalPendingShardBlocks{},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(NonfinalGetPendingShardBlocks)
			if !ok {
				t.Fatalf("unexpected request type: %T", payload)
			}
			if req.Mode != 1 {
				t.Fatalf("expected mode 1, got %d", req.Mode)
			}
			if req.WC != wc || req.Shard != shard {
				t.Fatalf("unexpected wc/shard: %d/%d", req.WC, req.Shard)
			}
			return nil
		},
	}

	_, err := NewAPIClient(mock).GetNonfinalPendingShardBlocks(context.Background(), wc, shard)
	if err != nil {
		t.Fatal(err)
	}
}
