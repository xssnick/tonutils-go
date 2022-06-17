package wallet

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"golang.org/x/crypto/ed25519"
)

type MockAPI struct {
	getBlockInfo            func(ctx context.Context) (*tlb.BlockInfo, error)
	getAccount              func(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*ton.Account, error)
	sendExternalMessage     func(ctx context.Context, addr *address.Address, msg *cell.Cell) error
	sendExternalInitMessage func(ctx context.Context, addr *address.Address, msg *cell.Cell, state *tlb.StateInit) error
	runGetMethod            func(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...interface{}) ([]interface{}, error)
}

func (m MockAPI) GetBlockInfo(ctx context.Context) (*tlb.BlockInfo, error) {
	return m.getBlockInfo(ctx)
}

func (m MockAPI) GetAccount(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*ton.Account, error) {
	return m.getAccount(ctx, block, addr)
}

func (m MockAPI) SendExternalMessage(ctx context.Context, addr *address.Address, msg *cell.Cell) error {
	return m.sendExternalMessage(ctx, addr, msg)
}

func (m MockAPI) SendExternalInitMessage(ctx context.Context, addr *address.Address, msg *cell.Cell, state *tlb.StateInit) error {
	return m.sendExternalInitMessage(ctx, addr, msg, state)
}

func (m MockAPI) RunGetMethod(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...interface{}) ([]interface{}, error) {
	return m.runGetMethod(ctx, blockInfo, addr, method, params...)
}

func TestWallet_Send(t *testing.T) {
	m := &MockAPI{}
	pkey := ed25519.NewKeyFromSeed([]byte("12345678901234567890123456789012"))

	// cases
	const (
		OK = iota
		SeqnoNotInt
		BlockErr
		RunErr
		SendErr
		UnsupportedVer
		SendWithInit
	)

	var errTest = errors.New("test")

	intMsg := &tlb.InternalMessage{
		IHRDisabled: false,
		Bounce:      true,
		Bounced:     false,
		SrcAddr:     nil,
		DstAddr:     nil,
		Amount:      nil,
		IHRFee:      nil,
		FwdFee:      nil,
		CreatedLT:   0,
		CreatedAt:   0,
		StateInit:   nil,
		Body:        cell.BeginCell().MustStoreUInt(777, 27).EndCell(),
	}

	for _, flow := range []int{OK, BlockErr, SeqnoNotInt, RunErr, UnsupportedVer, SendErr, SendWithInit} {
		w, err := FromPrivateKey(m, pkey, V3)
		if err != nil {
			t.Fatal(err)
			return
		}

		if flow == UnsupportedVer {
			w.ver = 777
		}

		m.getBlockInfo = func(ctx context.Context) (*tlb.BlockInfo, error) {
			if flow == BlockErr {
				return nil, errTest
			}

			return &tlb.BlockInfo{
				Workchain: 333,
			}, nil
		}

		m.runGetMethod = func(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...interface{}) ([]interface{}, error) {
			if flow == RunErr {
				return nil, errTest
			}

			if flow == SendWithInit {
				return nil, ton.ContractExecError{
					Code: 4294967040,
				}
			}

			if blockInfo.Workchain != 333 {
				t.Fatal("bad block")
				return nil, nil
			}

			if addr.String() != w.addr.String() {
				t.Fatal("not wallet addr")
				return nil, nil
			}

			if method != "seqno" {
				t.Fatal("method not seqno")
				return nil, nil
			}

			if len(params) != 0 {
				t.Fatal("not zero params")
				return nil, nil
			}

			if flow == SeqnoNotInt {
				return []interface{}{"aa"}, nil
			}

			return []interface{}{uint64(3)}, nil
		}

		m.sendExternalInitMessage = func(ctx context.Context, addr *address.Address, msg *cell.Cell, state *tlb.StateInit) error {
			if flow == SendErr {
				return errTest
			}

			if addr.String() != w.addr.String() {
				t.Fatal("not wallet addr")
				return nil
			}

			if flow != SendWithInit && state != nil {
				t.Fatal("state not nil")
				return nil
			}

			if flow == SendWithInit {
				if state == nil {
					t.Fatal("state is nil")
					return nil
				}

				state.Code.Hash()
			}

			p := msg.BeginParse()

			sign := p.MustLoadSlice(512)

			if p.MustLoadUInt(32) != _SubWalletV3 {
				t.Fatal("subwallet id incorrect")
				return nil
			}

			if p.MustLoadUInt(32) != 0xFFFFFFFF {
				t.Fatal("expire incorrect")
				return nil
			}

			seq := uint64(3)
			if flow == SendWithInit {
				seq = 0
			}

			if p.MustLoadUInt(32) != seq {
				t.Fatal("seqno incorrect")
				return nil
			}

			if p.MustLoadUInt(8) != uint64(128) {
				t.Fatal("mode incorrect")
				return nil
			}

			intMsgRef, _ := intMsg.ToCell()
			payload := cell.BeginCell().MustStoreUInt(_SubWalletV3, 32).
				MustStoreUInt(uint64(0xFFFFFFFF), 32).
				MustStoreUInt(seq, 32).MustStoreUInt(uint64(128), 8).MustStoreRef(intMsgRef)

			if !bytes.Equal(p.MustLoadRef().MustToCell().Hash(), intMsgRef.Hash()) {
				t.Fatal("int msg incorrect")
				return nil
			}

			if !ed25519.Verify(w.key.Public().(ed25519.PublicKey), payload.EndCell().Hash(), sign) {
				t.Fatal("sign incorrect")
				return nil
			}

			return nil
		}

		err = w.Send(context.Background(), 128, intMsg)
		if err != nil {
			switch flow {
			case UnsupportedVer:
				if strings.EqualFold(err.Error(), "send is not yet supported for wallet with this version") {
					continue
				}
			case SeqnoNotInt:
				if strings.EqualFold(err.Error(), "seqno is not int") {
					continue
				}
			case BlockErr, RunErr, SendErr:
				if errors.Is(err, errTest) {
					continue
				}
			}
			t.Fatal(flow, err)
		}

		if flow == OK || flow == SendWithInit {
			continue
		}

		t.Fatal(flow, "no error")
	}
}
