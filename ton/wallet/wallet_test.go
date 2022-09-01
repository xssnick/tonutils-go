package wallet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ed25519"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type MockAPI struct {
	getBlockInfo        func(ctx context.Context) (*tlb.BlockInfo, error)
	getAccount          func(ctx context.Context, block *tlb.BlockInfo, addr *address.Address) (*tlb.Account, error)
	sendExternalMessage func(ctx context.Context, msg *tlb.ExternalMessage) error
	runGetMethod        func(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...interface{}) ([]interface{}, error)
	listTransactions    func(ctx context.Context, addr *address.Address, limit uint32, lt uint64, txHash []byte) ([]*tlb.Transaction, error)

	extMsgSent *tlb.ExternalMessage
}

func (m MockAPI) CurrentMasterchainInfo(ctx context.Context) (*tlb.BlockInfo, error) {
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

func (m MockAPI) ListTransactions(ctx context.Context, addr *address.Address, limit uint32, lt uint64, txHash []byte) ([]*tlb.Transaction, error) {
	return m.listTransactions(ctx, addr, limit, lt, txHash)
}

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
	SendWait
	SendWaitErr
)

var pseudoRnd = uint32(0xAABBCCDD)

func TestWallet_Send(t *testing.T) {
	timeNow = func() time.Time {
		return time.Unix(1000000, 0)
	}
	randUint32 = func() uint32 {
		return pseudoRnd
	}

	m := &MockAPI{}
	pkey := ed25519.NewKeyFromSeed([]byte("12345678901234567890123456789012"))

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

	// TODO: SendWait, SendWaitErr
	cases := map[Version][]int{
		V3:           {OK, BlockErr, AccountErr, SeqnoNotInt, RunErr, UnsupportedVer, SendErr, SendWithInit1, SendWithInit2, TooMuchMessages},
		V4R2:         {OK, BlockErr, AccountErr, SeqnoNotInt, RunErr, UnsupportedVer, SendErr, SendWithInit1, SendWithInit2, TooMuchMessages},
		HighloadV2R2: {OK, BlockErr, AccountErr, UnsupportedVer, SendErr, SendWithInit1, SendWithInit2, TooMuchMessages},
	}

	for _, ver := range []Version{V3, V4R2, HighloadV2R2} {
		for _, flow := range cases[ver] {

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
					SeqNo:     2,
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

				return []interface{}{int64(3)}, nil
			}

			m.sendExternalMessage = func(ctx context.Context, msg *tlb.ExternalMessage) error {
				m.extMsgSent = msg
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

				switch ver {
				case V3:
					t.Run("v3 body check", func(t *testing.T) {
						checkV3(t, msg.Body.BeginParse(), w, flow, intMsg)
					})
				case V4R2:
					t.Run("v4r2 body check", func(t *testing.T) {
						checkV4R2(t, msg.Body.BeginParse(), w, flow, intMsg)
					})
				case HighloadV2R2:
					t.Run("highloadV2R2 body check", func(t *testing.T) {
						checkHighloadV2R2(t, msg.Body.BeginParse(), w, intMsg)
					})
				}
				return nil
			}

			msg := &Message{
				Mode:            128,
				InternalMessage: intMsg,
			}

			max := 4
			if ver == HighloadV2R2 {
				max = 254
			}

			if flow == TooMuchMessages {
				var msgs []*Message
				for mi := 0; mi < max+1; mi++ {
					msgs = append(msgs, msg)
				}

				err = w.SendMany(context.Background(), msgs)
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				wait := flow == SendWait || flow == SendWaitErr

				m.listTransactions = func(ctx context.Context, addr *address.Address, limit uint32, lt uint64, txHash []byte) ([]*tlb.Transaction, error) {
					list := []*tlb.Transaction{
						{
							LT:         lt,
							PrevTxHash: nil,
							PrevTxLT:   0,
							IO: struct {
								In  *tlb.Message   `tlb:"maybe ^"`
								Out []*tlb.Message `tlb:"dict 15 -> array ^"`
							}{
								In: &tlb.Message{
									MsgType: tlb.MsgTypeExternalIn,
									Msg:     m.extMsgSent,
								},
							},
						},
					}

					return list, nil
				}

				err = w.Send(ctx, msg, wait)
				cancel()
			}
			if err != nil {
				switch flow {
				case UnsupportedVer:
					if errors.Is(err, ErrUnsupportedWalletVersion) {
						continue
					}
				case SeqnoNotInt:
					if strings.EqualFold(err.Error(), "build message err: seqno is not an integer") {
						continue
					}
				case TooMuchMessages:
					if strings.EqualFold(err.Error(), "build message err: for this type of wallet max "+fmt.Sprint(max)+" messages can be sent in the same time") {
						continue
					}
				case BlockErr, AccountErr, RunErr, SendErr:
					if errors.Is(err, errTest) {
						continue
					}
				}
				t.Fatal(flow, err)
			}

			if flow == OK || flow == SendWithInit1 || flow == SendWithInit2 || flow == SendWait {
				continue
			}

			t.Fatal(flow, "no error")
		}
	}
}

func checkV4R2(t *testing.T, p *cell.Slice, w *Wallet, flow int, intMsg *tlb.InternalMessage) {
	sign := p.MustLoadSlice(512)

	if p.MustLoadUInt(32) != DefaultSubwallet {
		t.Fatal("subwallet id incorrect")
	}

	exp := uint64(timeNow().Add(60 * 3 * time.Second).UTC().Unix())

	if p.MustLoadUInt(32) != exp {
		t.Fatal("expire incorrect")
	}

	seq := uint64(3)
	if flow == SendWithInit1 || flow == SendWithInit2 {
		seq = 0
	}

	if p.MustLoadUInt(32) != seq {
		t.Fatal("seqno incorrect")
	}

	if p.MustLoadUInt(8) != 0 {
		t.Fatal("op incorrect")
	}

	if p.MustLoadUInt(8) != uint64(128) {
		t.Fatal("mode incorrect")
	}

	intMsgRef, _ := intMsg.ToCell()
	payload := cell.BeginCell().MustStoreUInt(DefaultSubwallet, 32).
		MustStoreUInt(exp, 32).
		MustStoreUInt(seq, 32)

	payload.MustStoreUInt(0, 8)

	payload.MustStoreUInt(uint64(128), 8).MustStoreRef(intMsgRef)

	if !bytes.Equal(p.MustLoadRef().MustToCell().Hash(), intMsgRef.Hash()) {
		t.Fatal("int msg incorrect")
	}

	if !ed25519.Verify(w.key.Public().(ed25519.PublicKey), payload.EndCell().Hash(), sign) {
		t.Fatal("sign incorrect")
	}
}

func checkV3(t *testing.T, p *cell.Slice, w *Wallet, flow int, intMsg *tlb.InternalMessage) {
	sign := p.MustLoadSlice(512)

	if p.MustLoadUInt(32) != DefaultSubwallet {
		t.Fatal("subwallet id incorrect")
	}

	exp := uint64(timeNow().Add(60 * 3 * time.Second).UTC().Unix())

	if p.MustLoadUInt(32) != exp {
		t.Fatal("expire incorrect")
	}

	seq := uint64(3)
	if flow == SendWithInit1 || flow == SendWithInit2 {
		seq = 0
	}

	if p.MustLoadUInt(32) != seq {
		t.Fatal("seqno incorrect")
	}

	if p.MustLoadUInt(8) != uint64(128) {
		t.Fatal("mode incorrect")
	}

	intMsgRef, _ := intMsg.ToCell()
	payload := cell.BeginCell().MustStoreUInt(DefaultSubwallet, 32).
		MustStoreUInt(exp, 32).
		MustStoreUInt(seq, 32)

	payload.MustStoreUInt(uint64(128), 8).MustStoreRef(intMsgRef)

	if !bytes.Equal(p.MustLoadRef().MustToCell().Hash(), intMsgRef.Hash()) {
		t.Fatal("int msg incorrect")
	}

	if !ed25519.Verify(w.key.Public().(ed25519.PublicKey), payload.EndCell().Hash(), sign) {
		t.Fatal("sign incorrect")
	}
}

func checkHighloadV2R2(t *testing.T, p *cell.Slice, w *Wallet, intMsg *tlb.InternalMessage) {
	sign := p.MustLoadSlice(512)

	if p.MustLoadUInt(32) != DefaultSubwallet {
		t.Fatal("subwallet id incorrect")
	}

	exp := uint64(timeNow().Add(60 * 3 * time.Second).UTC().Unix())
	qid := (exp << 32) + uint64(randUint32())

	if p.MustLoadUInt(64) != qid {
		t.Fatal("query id is incorrect")
	}

	if len(p.MustLoadDict(16).All()) != 1 {
		t.Fatal("dict incorrect")
	}

	intMsgRef, _ := intMsg.ToCell()

	dict := cell.NewDict(16)
	err := dict.SetIntKey(big.NewInt(0), cell.BeginCell().
		MustStoreUInt(uint64(128), 8).
		MustStoreRef(intMsgRef).
		EndCell())
	if err != nil {
		t.Fatal("set map key err", err.Error())
	}

	payload := cell.BeginCell().MustStoreUInt(DefaultSubwallet, 32).
		MustStoreUInt(qid, 64).
		MustStoreDict(dict)

	if !ed25519.Verify(w.key.Public().(ed25519.PublicKey), payload.EndCell().Hash(), sign) {
		t.Fatal("sign incorrect")
	}
}
