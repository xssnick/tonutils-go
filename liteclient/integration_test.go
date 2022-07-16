package liteclient

import (
	"context"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
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
		resp, err := client.Do(ctx, -1984567762, nil)
		if err != nil {
			t.Fatal("do err", err)
		}

		block := new(tlb.BlockInfo)
		_, err = block.Load(resp.Data)
		if err != nil {
			t.Fatal("load err", err)
		}

		if block.Workchain != -1 || block.Shard != -9223372036854775808 {
			t.Fatal("data err", *block)
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
