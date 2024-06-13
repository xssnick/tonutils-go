package liteclient

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"reflect"
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

	err := client.AddConnectionsFromConfigUrl(ctx, "https://tonutils.com/global.config.json")
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

	err := client.AddConnectionsFromConfigUrl(ctx, "https://tonutils.com/global.config.json")
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
	for _, n := range client.activeNodes {
		_ = n.tcp.Close()
	}

	// time to reconnect
	time.Sleep(5 * time.Second)

	doReq(nil)
}

func Test_ServerProxy(t *testing.T) {
	client := NewConnectionPool()

	err := client.AddConnectionsFromConfigUrl(context.Background(), "https://tonutils.com/global.config.json")
	if err != nil {
		t.Fatal("add connections err", err)
	}

	pub, key, _ := ed25519.GenerateKey(nil)
	s := NewServer([]ed25519.PrivateKey{key})
	s.SetMessageHandler(func(ctx context.Context, sc *ServerClient, msg tl.Serializable) error {
		switch m := msg.(type) {
		case adnl.MessageQuery:
			switch q := m.Data.(type) {
			case LiteServerQuery:
				println("PROXYING QUERY:", reflect.TypeOf(q.Data).String())

				var resp tl.Serializable
				if err = client.QueryLiteserver(context.Background(), q.Data, &resp); err != nil {
					return err
				}

				return sc.Send(adnl.MessageAnswer{ID: m.ID, Data: resp})
			}
		case TCPPing:
			return sc.Send(TCPPong{RandomID: m.RandomID})
		}

		return fmt.Errorf("something unknown: %s", reflect.TypeOf(msg).String())
	})
	defer s.Close()

	addr := "127.0.0.1:7657"
	go func() {
		if err := s.Listen(addr); err != nil {
			t.Fatal("listen err:", err.Error())
		}
	}()
	time.Sleep(300 * time.Millisecond)

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
