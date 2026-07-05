package liteclient

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(MasterchainInfo{}, "liteServer.masterchainInfo last:tonNode.blockIdExt state_root_hash:int256 init:tonNode.zeroStateIdExt = liteServer.MasterchainInfo")
	tl.Register(GetMasterchainInf{}, "liteServer.getMasterchainInfo = liteServer.MasterchainInfo")
	tl.Register(BlockIDExt{}, "tonNode.blockIdExt workchain:int shard:long seqno:int root_hash:int256 file_hash:int256 = tonNode.BlockIdExt")
	tl.Register(ZeroStateIDExt{}, "tonNode.zeroStateIdExt workchain:int root_hash:int256 file_hash:int256 = tonNode.ZeroStateIdExt")
}

type GetMasterchainInf struct{}

type BlockIDExt struct {
	Workchain int32  `tl:"int"`
	Shard     int64  `tl:"long"`
	SeqNo     uint32 `tl:"int"`
	RootHash  []byte `tl:"int256"`
	FileHash  []byte `tl:"int256"`
}

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

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton-blockchain.github.io/global.config.json")
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
	for _, n := range client.activeNodesSnapshot() {
		_ = n.tcp.Close()
	}

	// time to reconnect
	time.Sleep(5 * time.Second)

	doReq(nil)
}

func Test_Offline(t *testing.T) {
	client := NewOfflineClient()

	doReq := func(expErr error) {
		var resp tl.Serializable
		err := client.QueryLiteserver(context.Background(), GetMasterchainInf{}, &resp)
		if err != ErrOfflineMode {
			t.Fatal("err", err)
		}
	}
	doReq(nil)
}

func Test_ConnSticky(t *testing.T) {
	client := NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		t.Fatal("add connections err", err)
	}

	ctx, err = client.StickyContextNextNodeBalanced(ctx)
	if err != nil {
		t.Fatal("next balanced err", err)
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
	for _, n := range client.activeNodesSnapshot() {
		_ = n.tcp.Close()
	}

	// time to reconnect
	time.Sleep(5 * time.Second)

	doReq(nil)
}

func Test_ServerProxy(t *testing.T) {
	client := NewConnectionPool()

	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		t.Fatal("add connections err", err)
	}

	pub, key, _ := ed25519.GenerateKey(nil)
	s := NewServer([]ed25519.PrivateKey{key})
	s.SetQueryHandler(func(ctx context.Context, sc *ServerClient, queryID []byte, query tl.Serializable) {
		println("PROXYING QUERY:", reflect.TypeOf(query).String())

		go func() {
			var resp tl.Serializable
			if err := client.QueryLiteserver(context.Background(), query, &resp); err != nil {
				println("PROXY QUERY ERR:", err.Error())
				return
			}
			sc.Answer(queryID, resp)
		}()
	})
	defer s.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("listen err:", err.Error())
	}
	addr := ln.Addr().String()
	go func() {
		if err := s.listen(ln); err != nil {
			t.Error("listen err:", err.Error())
		}
	}()

	clientProxy := NewConnectionPool()
	if err := clientProxy.AddConnection(context.Background(), addr, base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal("add err:", err.Error())
	}

	var resp tl.Serializable
	err = clientProxy.QueryLiteserver(context.Background(), GetMasterchainInf{}, &resp)
	if err != nil {
		t.Fatal("query err:", err.Error())
	}

	if resp.(MasterchainInfo).Last.SeqNo == 0 {
		t.Fatal("seqno empty")
	}

	println("SEQNO:", resp.(MasterchainInfo).Last.SeqNo)
}
