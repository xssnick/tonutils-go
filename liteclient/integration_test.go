package liteclient

import (
	"context"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"testing"
	"time"
)

func init() {
	tl.Register(MasterchainInfo{}, "liteServer.masterchainInfo last:tonNode.blockIdExt state_root_hash:int256 init:tonNode.zeroStateIdExt = liteServer.MasterchainInfo")
	tl.Register(GetMasterchainInf{}, "liteServer.getMasterchainInfo = liteServer.MasterchainInfo")
}

type GetMasterchainInf struct{}

type BlockIDExt = tlb.BlockInfo
type MasterchainInfo struct {
	Last          *BlockIDExt     `tl:"struct"`
	StateRootHash []byte          `tl:"int256"`
	Init          *ZeroStateIDExt `tl:"struct"`
}
type ZeroStateIDExt struct {
	Workchain int32  `tl:"int"`
	RootHash  []byte `tl:"int256"`
	FileHash  []byte `tl:"int256"`
}

func Test_Conn(t *testing.T) {
	client := NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton.org/global.config.json")
	if err != nil {
		t.Fatal("add connections err", err)
	}

	doReq := func(expErr error) {
		var resp tl.Serializable
		err := client.QueryLiteserver(ctx, GetMasterchainInf{}, &resp)
		if err != nil {
			t.Fatal("do err", err)
		}

		switch tb := resp.(type) {
		case MasterchainInfo:
		default:
			t.Fatal("bad response", fmt.Sprint(tb))
		}
	}
	doReq(nil)

	// simulate network crash
	for _, n := range client.activeNodes {
		_ = n.tcp.Close()
	}

	// time to reconnect
	time.Sleep(5 * time.Second)

	doReq(nil)
}

func Test_ConnSticky(t *testing.T) {
	client := NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = client.StickyContext(ctx)

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton.org/global.config.json")
	if err != nil {
		t.Fatal("add connections err", err)
	}

	doReq := func(expErr error) {
		var resp tl.Serializable
		err := client.QueryLiteserver(ctx, GetMasterchainInf{}, &resp)
		if err != nil {
			t.Fatal("do err", err)
		}

		switch tb := resp.(type) {
		case MasterchainInfo:
		default:
			t.Fatal("bad response", fmt.Sprint(tb))
		}
	}
	doReq(nil)

	// simulate network crash
	for _, n := range client.activeNodes {
		_ = n.tcp.Close()
	}

	// time to reconnect
	time.Sleep(5 * time.Second)

	doReq(nil)
}
