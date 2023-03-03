package liteclient

import (
	"context"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/ton"
	"testing"
	"time"
)

func Test_Conn(t *testing.T) {
	client := NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		t.Fatal("add connections err", err)
	}

	doReq := func(expErr error) {
		var resp tl.Serializable
		err := client.QueryLiteserver(ctx, ton.GetMasterchainInf{}, &resp)
		if err != nil {
			t.Fatal("do err", err)
		}

		switch tb := resp.(type) {
		case ton.MasterchainInfo:
			if tb.Last.Workchain != -1 || tb.Last.Shard != -9223372036854775808 {
				t.Fatal("data err", *tb.Last)
			}
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
