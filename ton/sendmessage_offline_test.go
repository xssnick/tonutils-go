package ton

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestSendExternalMessageToAllNodesOffline(t *testing.T) {
	client := liteclient.NewOfflineClient()
	ctx := context.Background()

	// Check selection first so a regression cannot leave broadcast enumerating forever.
	if _, err := client.StickyContextNextNodeBalanced(ctx); !errors.Is(err, liteclient.ErrOfflineMode) {
		t.Fatalf("expected offline mode selection error, got %v", err)
	}

	msg := &tlb.ExternalMessage{
		DstAddr: address.NewAddress(0, 0, make([]byte, 32)),
		Body:    cell.BeginCell().EndCell(),
	}
	err := NewAPIClient(client).SendExternalMessageToAllNodes(ctx, msg)
	if !errors.Is(err, liteclient.ErrOfflineMode) {
		t.Fatalf("expected offline mode broadcast error, got %v", err)
	}
}

func TestCurrentMasterchainInfoOfflineWithTimeout(t *testing.T) {
	api := NewAPIClient(liteclient.NewOfflineClient()).WithTimeout(time.Second)

	_, err := api.CurrentMasterchainInfo(context.Background())
	if !errors.Is(err, liteclient.ErrOfflineMode) {
		t.Fatalf("expected offline mode error, got %v", err)
	}
}
