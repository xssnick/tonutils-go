package wallet

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"golang.org/x/crypto/ed25519"
)

type MockAPI struct {
	getBlockInfo        func(ctx context.Context) (*tlb.BlockInfo, error)
	getAccount          func(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*tlb.Account, error)
	sendExternalMessage func(ctx context.Context, msg *tlb.ExternalMessage) error
	runGetMethod        func(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...interface{}) ([]interface{}, error)
}

func (m MockAPI) GetMasterchainInfo(ctx context.Context) (*tlb.BlockInfo, error) {
	return m.getBlockInfo(ctx)
}

func (m MockAPI) GetAccount(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*tlb.Account, error) {
	return m.getAccount(ctx, block, addr)
}

func (m MockAPI) SendExternalMessage(ctx context.Context, msg *tlb.ExternalMessage) error {
	return m.sendExternalMessage(ctx, msg)
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
		AccountErr
		RunErr
		SendErr
		UnsupportedVer
		SendWithInit1
		SendWithInit2
		TooMuchMessages
	)

	var errTest = errors.New("test")

	intMsg := &tlb.InternalMessage{
		IHRDisabled: false,
		Bounce:      true,
		Bounced:     false,
		SrcAddr:     nil,
		DstAddr:     nil,
		CreatedLT:   0,
		CreatedAt:   0,
		StateInit:   nil,
		Body:        cell.BeginCell().MustStoreUInt(777, 27).EndCell(),
	}

	for _, ver := range []Version{V3, V4R2} {
		for _, flow := range []int{OK, BlockErr, AccountErr, SeqnoNotInt, RunErr, UnsupportedVer, SendErr, SendWithInit1, SendWithInit2, TooMuchMessages} {
			w, err := FromPrivateKey(m, pkey, ver)
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

			m.getAccount = func(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*tlb.Account, error) {
				if flow == AccountErr {
					return nil, errTest
				}

				a := &tlb.Account{
					IsActive: true,
					State: &tlb.AccountState{
						IsValid: true,
						Address: addr,
						AccountStorage: tlb.AccountStorage{
							Status: tlb.AccountStatusActive,
						},
					},
				}

				if flow == SendWithInit1 {
					a.IsActive = false
				}
				if flow == SendWithInit2 {
					a.State.AccountStorage.Status = tlb.AccountStatusUninit
				}

				return a, nil
			}

			m.runGetMethod = func(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...interface{}) ([]interface{}, error) {
				if flow == RunErr {
					return nil, errTest
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

			m.sendExternalMessage = func(ctx context.Context, msg *tlb.ExternalMessage) error {
				if flow == SendErr {
					return errTest
				}

				if msg.DstAddr.String() != w.addr.String() {
					t.Fatal("not wallet addr")
					return nil
				}

				if flow != SendWithInit1 && flow != SendWithInit2 && msg.StateInit != nil {
					t.Fatal("state not nil")
					return nil
				}

				if flow == SendWithInit1 || flow == SendWithInit2 {
					if msg.StateInit == nil {
						t.Fatal("state is nil")
						return nil
					}

					msg.StateInit.Code.Hash()
				}

				p := msg.Body.BeginParse()

				sign := p.MustLoadSlice(512)

				if p.MustLoadUInt(32) != DefaultSubwallet {
					t.Fatal("subwallet id incorrect")
					return nil
				}

				if p.MustLoadUInt(32) != 0xFFFFFFFF {
					t.Fatal("expire incorrect")
					return nil
				}

				seq := uint64(3)
				if flow == SendWithInit1 || flow == SendWithInit2 {
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
				payload := cell.BeginCell().MustStoreUInt(DefaultSubwallet, 32).
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

			msg := &Message{
				Mode:            128,
				InternalMessage: intMsg,
			}

			if flow == TooMuchMessages {
				err = w.SendMany(context.Background(), []*Message{msg, msg, msg, msg, msg})
			} else {
				err = w.Send(context.Background(), msg)
			}
			if err != nil {
				switch flow {
				case UnsupportedVer:
					if strings.EqualFold(err.Error(), "send is not yet supported for wallet with this version") {
						continue
					}
				case SeqnoNotInt:
					if strings.EqualFold(err.Error(), "build message err: seqno is not an integer") {
						continue
					}
				case TooMuchMessages:
					if strings.EqualFold(err.Error(), "build message err: for this type of wallet max 4 messages can be sent in the same time") {
						continue
					}
				case BlockErr, AccountErr, RunErr, SendErr:
					if errors.Is(err, errTest) {
						continue
					}
				}
				t.Fatal(flow, err)
			}

			if flow == OK || flow == SendWithInit1 || flow == SendWithInit2 {
				continue
			}

			t.Fatal(flow, "no error")
		}
	}
}
