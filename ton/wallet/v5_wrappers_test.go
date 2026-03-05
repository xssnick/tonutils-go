package wallet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func unitTestAddress() *address.Address {
	return address.MustParseAddr("EQC9bWZd29foipyPOGWlVNVCQzpGAjvi1rGWF7EbNcSVClpA")
}

func unitTestMessage(mode uint8) *Message {
	return &Message{
		Mode: mode,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      true,
			DstAddr:     unitTestAddress(),
			Amount:      tlb.FromNanoTONU(1),
			Body:        cell.BeginCell().MustStoreUInt(1, 1).EndCell(),
		},
	}
}

func newUnitV3Wallet(t *testing.T, m *MockAPI) *Wallet {
	t.Helper()

	if m.getBlockInfo == nil {
		m.getBlockInfo = func(ctx context.Context) (*ton.BlockIDExt, error) {
			return &ton.BlockIDExt{
				Workchain: -1,
				SeqNo:     1,
			}, nil
		}
	}

	if m.getAccount == nil {
		m.getAccount = func(ctx context.Context, block *ton.BlockIDExt, addr *address.Address) (*tlb.Account, error) {
			return &tlb.Account{
				IsActive: true,
				State: &tlb.AccountState{
					IsValid: true,
					Address: addr,
					AccountStorage: tlb.AccountStorage{
						Status: tlb.AccountStatusActive,
					},
				},
			}, nil
		}
	}

	if m.runGetMethod == nil {
		m.runGetMethod = func(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...interface{}) (*ton.ExecutionResult, error) {
			return ton.NewExecutionResult([]any{big.NewInt(7)}), nil
		}
	}

	if m.sendExternalMessage == nil {
		m.sendExternalMessage = func(ctx context.Context, msg *tlb.ExternalMessage) error {
			return nil
		}
	}

	if m.sendExternalMessageWait == nil {
		m.sendExternalMessageWait = func(ctx context.Context, ext *tlb.ExternalMessage) (*tlb.Transaction, *ton.BlockIDExt, []byte, error) {
			return &tlb.Transaction{Hash: []byte{1, 2, 3}}, &ton.BlockIDExt{SeqNo: 11}, []byte{9, 9, 9}, nil
		}
	}

	w, err := FromPrivateKey(m, ed25519.NewKeyFromSeed([]byte("12345678901234567890123456789012")), V3)
	if err != nil {
		t.Fatalf("failed to init test wallet: %v", err)
	}
	return w
}

func mustLoadSingleV3Message(t *testing.T, ext *tlb.ExternalMessage) *tlb.InternalMessage {
	t.Helper()

	if ext == nil || ext.Body == nil {
		t.Fatal("external message body should not be nil")
	}

	ldr := ext.Body.BeginParse()
	_ = ldr.MustLoadSlice(512) // signature
	_ = ldr.MustLoadUInt(32)   // subwallet
	_ = ldr.MustLoadUInt(32)   // valid until
	_ = ldr.MustLoadUInt(32)   // seqno
	_ = ldr.MustLoadUInt(8)    // send mode

	msgRef := ldr.MustLoadRef()

	var msg tlb.InternalMessage
	if err := tlb.LoadFromCell(&msg, msgRef); err != nil {
		t.Fatalf("failed to decode internal message: %v", err)
	}
	return &msg
}

func TestValidateMessageFields(t *testing.T) {
	t.Run("too many messages", func(t *testing.T) {
		err := validateMessageFields(make([]*Message, 256))
		if err == nil || !strings.Contains(err.Error(), "max 255 messages") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil internal message", func(t *testing.T) {
		err := validateMessageFields([]*Message{{Mode: PayGasSeparately}})
		if err == nil || !strings.Contains(err.Error(), "internal message cannot be nil") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid", func(t *testing.T) {
		if err := validateMessageFields([]*Message{unitTestMessage(PayGasSeparately)}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestPackV5OutActions(t *testing.T) {
	t.Run("validation error", func(t *testing.T) {
		_, err := PackV5OutActions([]*Message{{Mode: PayGasSeparately}})
		if err == nil || !strings.Contains(err.Error(), "internal message cannot be nil") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("packed list structure", func(t *testing.T) {
		messages := []*Message{
			unitTestMessage(PayGasSeparately),
			unitTestMessage(PayGasSeparately + IgnoreErrors),
		}

		actions, err := PackV5OutActions(messages)
		if err != nil {
			t.Fatalf("pack error: %v", err)
		}

		root := actions.EndCell().BeginParse()
		if root.MustLoadUInt(1) != 1 {
			t.Fatal("actions should start with 1 bit")
		}

		list := root.MustLoadRef()
		if root.MustLoadUInt(1) != 0 {
			t.Fatal("actions should end with 0 bit")
		}

		for i := len(messages) - 1; i >= 0; i-- {
			prev := list.MustLoadRef()

			if op := list.MustLoadUInt(32); op != 0x0ec3c86d {
				t.Fatalf("unexpected action opcode: %x", op)
			}
			if mode := list.MustLoadUInt(8); mode != uint64(messages[i].Mode) {
				t.Fatalf("unexpected action mode: %d", mode)
			}
			if _, err = list.LoadRef(); err != nil {
				t.Fatalf("failed to load out message ref: %v", err)
			}

			list = prev
		}

		if list.BitsLeft() != 0 || list.RefsNum() != 0 {
			t.Fatal("list tail should be empty")
		}
	})
}

func TestSpecV5R1Final_BuildMessageErrors(t *testing.T) {
	t.Run("messages limit", func(t *testing.T) {
		spec := &SpecV5R1Final{}

		_, err := spec.BuildMessage(context.Background(), false, nil, make([]*Message, 256))
		if err == nil || !strings.Contains(err.Error(), "max 255 messages") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("signer error", func(t *testing.T) {
		errSign := errors.New("sign failed")
		w := &Wallet{
			subwallet: 77,
			signer: func(ctx context.Context, toSign *cell.Cell, subwallet uint32) ([]byte, error) {
				if subwallet != 77 {
					t.Fatalf("unexpected subwallet passed to signer: %d", subwallet)
				}
				if toSign == nil {
					t.Fatal("sign payload should not be nil")
				}
				return nil, errSign
			},
		}

		spec := &SpecV5R1Final{
			SpecRegular: SpecRegular{
				wallet:      w,
				messagesTTL: 180,
			},
			SpecSeqno: SpecSeqno{
				seqnoFetcher: func(ctx context.Context, subWallet uint32) (uint32, error) {
					if subWallet != 77 {
						t.Fatalf("unexpected subwallet passed to seqno fetcher: %d", subWallet)
					}
					return 5, nil
				},
			},
			config: ConfigV5R1Final{
				NetworkGlobalID: MainnetGlobalID,
			},
		}

		_, err := spec.BuildMessage(context.Background(), false, nil, []*Message{unitTestMessage(PayGasSeparately)})
		if !errors.Is(err, errSign) || !strings.Contains(err.Error(), "failed to sign") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestSpecV5R1Beta_BuildMessageErrors(t *testing.T) {
	t.Run("messages limit", func(t *testing.T) {
		spec := &SpecV5R1Beta{}

		_, err := spec.BuildMessage(context.Background(), false, nil, make([]*Message, 256))
		if err == nil || !strings.Contains(err.Error(), "max 4 messages") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("signer error", func(t *testing.T) {
		errSign := errors.New("sign failed")
		w := &Wallet{
			subwallet: 78,
			signer: func(ctx context.Context, toSign *cell.Cell, subwallet uint32) ([]byte, error) {
				if subwallet != 78 {
					t.Fatalf("unexpected subwallet passed to signer: %d", subwallet)
				}
				if toSign == nil {
					t.Fatal("sign payload should not be nil")
				}
				return nil, errSign
			},
		}

		spec := &SpecV5R1Beta{
			SpecRegular: SpecRegular{
				wallet:      w,
				messagesTTL: 180,
			},
			SpecSeqno: SpecSeqno{
				seqnoFetcher: func(ctx context.Context, subWallet uint32) (uint32, error) {
					if subWallet != 78 {
						t.Fatalf("unexpected subwallet passed to seqno fetcher: %d", subWallet)
					}
					return 5, nil
				},
			},
			config: ConfigV5R1Beta{
				NetworkGlobalID: MainnetGlobalID,
			},
		}

		_, err := spec.BuildMessage(context.Background(), false, nil, []*Message{unitTestMessage(PayGasSeparately)})
		if !errors.Is(err, errSign) || !strings.Contains(err.Error(), "failed to sign") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestWallet_SendManyWaitWrappers(t *testing.T) {
	t.Run("SendManyWaitTxHash", func(t *testing.T) {
		var sendCalled, waitCalled bool
		wantTx := &tlb.Transaction{Hash: []byte{7, 8, 9}}

		m := &MockAPI{
			sendExternalMessage: func(ctx context.Context, msg *tlb.ExternalMessage) error {
				sendCalled = true
				return nil
			},
			sendExternalMessageWait: func(ctx context.Context, ext *tlb.ExternalMessage) (*tlb.Transaction, *ton.BlockIDExt, []byte, error) {
				waitCalled = true
				return wantTx, &ton.BlockIDExt{SeqNo: 100}, []byte{1, 1, 1}, nil
			},
		}
		w := newUnitV3Wallet(t, m)

		hash, err := w.SendManyWaitTxHash(context.Background(), []*Message{unitTestMessage(PayGasSeparately)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !waitCalled || sendCalled {
			t.Fatalf("unexpected send path used, sendCalled=%v waitCalled=%v", sendCalled, waitCalled)
		}
		if !bytes.Equal(hash, wantTx.Hash) {
			t.Fatalf("unexpected tx hash: %x", hash)
		}
	})

	t.Run("SendManyWaitTxHash error", func(t *testing.T) {
		waitErr := errors.New("wait failed")
		m := &MockAPI{
			sendExternalMessageWait: func(ctx context.Context, ext *tlb.ExternalMessage) (*tlb.Transaction, *ton.BlockIDExt, []byte, error) {
				return nil, nil, nil, waitErr
			},
		}
		w := newUnitV3Wallet(t, m)

		hash, err := w.SendManyWaitTxHash(context.Background(), []*Message{unitTestMessage(PayGasSeparately)})
		if !errors.Is(err, waitErr) {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash != nil {
			t.Fatalf("hash should be nil on error, got: %x", hash)
		}
	})

	t.Run("SendManyWaitTransaction", func(t *testing.T) {
		wantTx := &tlb.Transaction{Hash: []byte{4, 5, 6}}
		wantBlock := &ton.BlockIDExt{SeqNo: 321}

		m := &MockAPI{
			sendExternalMessageWait: func(ctx context.Context, ext *tlb.ExternalMessage) (*tlb.Transaction, *ton.BlockIDExt, []byte, error) {
				return wantTx, wantBlock, []byte{2, 2, 2}, nil
			},
		}
		w := newUnitV3Wallet(t, m)

		tx, block, err := w.SendManyWaitTransaction(context.Background(), []*Message{unitTestMessage(PayGasSeparately)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx != wantTx || block != wantBlock {
			t.Fatalf("unexpected tx or block returned: %v %v", tx, block)
		}
	})
}

func TestWallet_TransferWrappers(t *testing.T) {
	t.Run("TransferNoBounce", func(t *testing.T) {
		var sentExt *tlb.ExternalMessage
		var sendCalled, waitCalled bool

		m := &MockAPI{
			sendExternalMessage: func(ctx context.Context, msg *tlb.ExternalMessage) error {
				sendCalled = true
				sentExt = msg
				return nil
			},
			sendExternalMessageWait: func(ctx context.Context, ext *tlb.ExternalMessage) (*tlb.Transaction, *ton.BlockIDExt, []byte, error) {
				waitCalled = true
				return nil, nil, nil, nil
			},
		}
		w := newUnitV3Wallet(t, m)

		to := unitTestAddress()
		if err := w.TransferNoBounce(context.Background(), to, tlb.FromNanoTONU(10), "test-no-bounce"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !sendCalled || waitCalled {
			t.Fatalf("unexpected send path used, sendCalled=%v waitCalled=%v", sendCalled, waitCalled)
		}

		msg := mustLoadSingleV3Message(t, sentExt)
		if msg.Bounce {
			t.Fatal("bounce flag should be false for TransferNoBounce")
		}
		if !msg.DstAddr.Equals(to) {
			t.Fatal("destination address mismatch")
		}
		if msg.Comment() != "test-no-bounce" {
			t.Fatalf("unexpected comment payload: %q", msg.Comment())
		}
	})

	t.Run("Transfer with wait confirmation", func(t *testing.T) {
		var sentExt *tlb.ExternalMessage
		var sendCalled, waitCalled bool

		m := &MockAPI{
			sendExternalMessage: func(ctx context.Context, msg *tlb.ExternalMessage) error {
				sendCalled = true
				return nil
			},
			sendExternalMessageWait: func(ctx context.Context, ext *tlb.ExternalMessage) (*tlb.Transaction, *ton.BlockIDExt, []byte, error) {
				waitCalled = true
				sentExt = ext
				return &tlb.Transaction{Hash: []byte{1}}, &ton.BlockIDExt{SeqNo: 2}, []byte{3}, nil
			},
		}
		w := newUnitV3Wallet(t, m)

		to := unitTestAddress()
		if err := w.Transfer(context.Background(), to, tlb.FromNanoTONU(11), "test-bounce", true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sendCalled || !waitCalled {
			t.Fatalf("unexpected send path used, sendCalled=%v waitCalled=%v", sendCalled, waitCalled)
		}

		msg := mustLoadSingleV3Message(t, sentExt)
		if !msg.Bounce {
			t.Fatal("bounce flag should be true for Transfer")
		}
		if !msg.DstAddr.Equals(to) {
			t.Fatal("destination address mismatch")
		}
		if msg.Comment() != "test-bounce" {
			t.Fatalf("unexpected comment payload: %q", msg.Comment())
		}
	})

	t.Run("TransferWaitTransaction", func(t *testing.T) {
		var sentExt *tlb.ExternalMessage
		wantTx := &tlb.Transaction{Hash: []byte{8}}
		wantBlock := &ton.BlockIDExt{SeqNo: 12}

		m := &MockAPI{
			sendExternalMessageWait: func(ctx context.Context, ext *tlb.ExternalMessage) (*tlb.Transaction, *ton.BlockIDExt, []byte, error) {
				sentExt = ext
				return wantTx, wantBlock, []byte{6}, nil
			},
		}
		w := newUnitV3Wallet(t, m)

		to := unitTestAddress().Bounce(false)
		tx, block, err := w.TransferWaitTransaction(context.Background(), to, tlb.FromNanoTONU(12), "test-wait")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx != wantTx || block != wantBlock {
			t.Fatalf("unexpected tx or block returned: %v %v", tx, block)
		}

		msg := mustLoadSingleV3Message(t, sentExt)
		if msg.Bounce {
			t.Fatal("bounce flag should follow destination address bounceability")
		}
		if !msg.DstAddr.Equals(to) {
			t.Fatal("destination address mismatch")
		}
		if msg.Comment() != "test-wait" {
			t.Fatalf("unexpected comment payload: %q", msg.Comment())
		}
	})
}

func TestWallet_DeployWrappers(t *testing.T) {
	msgBody := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	code := cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()
	data := cell.BeginCell().MustStoreUInt(0xEF, 8).EndCell()

	t.Run("DeployContractWaitTransaction", func(t *testing.T) {
		var sentExt *tlb.ExternalMessage
		wantTx := &tlb.Transaction{Hash: []byte{7}}
		wantBlock := &ton.BlockIDExt{SeqNo: 70}

		m := &MockAPI{
			sendExternalMessageWait: func(ctx context.Context, ext *tlb.ExternalMessage) (*tlb.Transaction, *ton.BlockIDExt, []byte, error) {
				sentExt = ext
				return wantTx, wantBlock, []byte{1}, nil
			},
		}
		w := newUnitV3Wallet(t, m)

		addr, tx, block, err := w.DeployContractWaitTransaction(context.Background(), tlb.FromNanoTONU(100), msgBody, code, data, -1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx != wantTx || block != wantBlock {
			t.Fatalf("unexpected tx or block returned: %v %v", tx, block)
		}
		if addr.Workchain() != -1 {
			t.Fatalf("unexpected workchain in result address: %d", addr.Workchain())
		}

		msg := mustLoadSingleV3Message(t, sentExt)
		if msg.Bounce {
			t.Fatal("deploy message should never bounce")
		}
		if msg.StateInit == nil {
			t.Fatal("deploy message should include state init")
		}
		if !msg.DstAddr.Equals(addr) {
			t.Fatal("destination address should match returned contract address")
		}
	})

	t.Run("DeployContract", func(t *testing.T) {
		var sentExt *tlb.ExternalMessage
		var sendCalled bool

		m := &MockAPI{
			sendExternalMessage: func(ctx context.Context, msg *tlb.ExternalMessage) error {
				sendCalled = true
				sentExt = msg
				return nil
			},
		}
		w := newUnitV3Wallet(t, m)

		addr, err := w.DeployContract(context.Background(), tlb.FromNanoTONU(101), msgBody, code, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !sendCalled {
			t.Fatal("send path should be used")
		}
		if addr.Workchain() != 0 {
			t.Fatalf("unexpected workchain in result address: %d", addr.Workchain())
		}

		msg := mustLoadSingleV3Message(t, sentExt)
		if msg.Bounce {
			t.Fatal("deploy message should never bounce")
		}
		if msg.StateInit == nil {
			t.Fatal("deploy message should include state init")
		}
		if !msg.DstAddr.Equals(addr) {
			t.Fatal("destination address should match returned contract address")
		}
	})
}

func TestSimpleMessageAutoBounce(t *testing.T) {
	toBounceable := unitTestAddress()
	msg := SimpleMessageAutoBounce(toBounceable, tlb.FromNanoTONU(1), nil)
	if !msg.InternalMessage.Bounce {
		t.Fatal("message should be bounceable for bounceable destination")
	}

	toNonBounceable := unitTestAddress().Bounce(false)
	msg = SimpleMessageAutoBounce(toNonBounceable, tlb.FromNanoTONU(1), nil)
	if msg.InternalMessage.Bounce {
		t.Fatal("message should be non-bounceable for non-bounceable destination")
	}
}

func TestWallet_WithSignerAndWithWorkchain(t *testing.T) {
	var signerCalled bool
	var signerSubwallet uint32
	var signerPayloadHash []byte

	customSigner := func(ctx context.Context, toSign *cell.Cell, subwallet uint32) ([]byte, error) {
		signerCalled = true
		signerSubwallet = subwallet
		if toSign == nil {
			t.Fatal("sign payload should not be nil")
		}
		signerPayloadHash = toSign.Hash()
		return bytes.Repeat([]byte{0xAA}, 64), nil
	}

	w, err := FromPrivateKeyWithOptions(
		ed25519.NewKeyFromSeed([]byte("12345678901234567890123456789012")),
		V3,
		WithSigner(customSigner),
		WithWorkchain(-1),
	)
	if err != nil {
		t.Fatalf("failed to init wallet: %v", err)
	}

	if w.WalletAddress().Workchain() != -1 {
		t.Fatalf("unexpected workchain: %d", w.WalletAddress().Workchain())
	}

	spec, ok := w.GetSpec().(*SpecV3)
	if !ok {
		t.Fatalf("unexpected spec type: %T", w.GetSpec())
	}
	spec.SetSeqnoFetcher(func(ctx context.Context, subWallet uint32) (uint32, error) {
		return 1, nil
	})

	_, err = w.PrepareExternalMessageForMany(context.Background(), false, []*Message{SimpleMessage(unitTestAddress(), tlb.FromNanoTONU(1), nil)})
	if err != nil {
		t.Fatalf("prepare external message failed: %v", err)
	}
	if !signerCalled {
		t.Fatal("custom signer should be used")
	}
	if signerSubwallet != DefaultSubwallet {
		t.Fatalf("unexpected subwallet passed to signer: %d", signerSubwallet)
	}
	if len(signerPayloadHash) == 0 {
		t.Fatal("sign payload hash should not be empty")
	}
}

func TestRegularSpecSetters(t *testing.T) {
	t.Run("SetMessagesTTL", func(t *testing.T) {
		s := &SpecRegular{messagesTTL: 1}
		s.SetMessagesTTL(777)
		if s.messagesTTL != 777 {
			t.Fatalf("unexpected messagesTTL: %d", s.messagesTTL)
		}
	})

	t.Run("SetCustomSeqnoFetcher", func(t *testing.T) {
		s := &SpecSeqno{}
		s.SetCustomSeqnoFetcher(func() uint32 {
			return 123
		})

		seq, err := s.seqnoFetcher(context.Background(), 999)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if seq != 123 {
			t.Fatalf("unexpected seqno: %d", seq)
		}
	})

	t.Run("SetSeqnoFetcher", func(t *testing.T) {
		s := &SpecSeqno{}
		type ctxKey string
		key := ctxKey("test")

		s.SetSeqnoFetcher(func(ctx context.Context, subWallet uint32) (uint32, error) {
			if ctx.Value(key) != "ok" {
				t.Fatal("context value was not propagated")
			}
			if subWallet != 42 {
				t.Fatalf("unexpected subwallet: %d", subWallet)
			}
			return 321, nil
		})

		seq, err := s.seqnoFetcher(context.WithValue(context.Background(), key, "ok"), 42)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if seq != 321 {
			t.Fatalf("unexpected seqno: %d", seq)
		}
	})

	t.Run("SetCustomQueryIDFetcher", func(t *testing.T) {
		s := &SpecQuery{}
		s.SetCustomQueryIDFetcher(func() (ttl uint32, randPart uint32) {
			return 11, 22
		})

		ttl, randPart, err := s.customQueryIDFetcher(context.Background(), 100)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ttl != 11 || randPart != 22 {
			t.Fatalf("unexpected values: ttl=%d rand=%d", ttl, randPart)
		}
	})

	t.Run("SetCustomQueryIDFetcherWithContext", func(t *testing.T) {
		s := &SpecQuery{}
		type ctxKey string
		key := ctxKey("query")
		expectedErr := errors.New("query fetch failed")

		s.SetCustomQueryIDFetcherWithContext(func(ctx context.Context, subWalletId uint32) (ttl uint32, randPart uint32, err error) {
			if ctx.Value(key) != "ok" {
				t.Fatal("context value was not propagated")
			}
			if subWalletId != 77 {
				t.Fatalf("unexpected subwallet: %d", subWalletId)
			}
			return 33, 44, expectedErr
		})

		ttl, randPart, err := s.customQueryIDFetcher(context.WithValue(context.Background(), key, "ok"), 77)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("unexpected error: %v", err)
		}
		if ttl != 33 || randPart != 44 {
			t.Fatalf("unexpected values: ttl=%d rand=%d", ttl, randPart)
		}
	})
}
