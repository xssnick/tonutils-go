package wallet

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"testing"
)

type configCustomV5R1 struct {
	code *cell.Cell
	ConfigV5R1Final
}

func newConfigCustomV5R1(code *cell.Cell) ConfigCustom {
	return &configCustomV5R1{code: code, ConfigV5R1Final: ConfigV5R1Final{
		NetworkGlobalID: MainnetGlobalID,
	}}
}

func (c *configCustomV5R1) GetStateInit(pubKey ed25519.PublicKey, subWallet uint32) (*tlb.StateInit, error) {
	walletId := V5R1ID{
		NetworkGlobalID: c.NetworkGlobalID,
		WorkChain:       c.Workchain,
		SubwalletNumber: uint16(subWallet),
		WalletVersion:   0,
	}

	data := cell.BeginCell().
		MustStoreBoolBit(true).
		MustStoreUInt(0, 32).
		MustStoreUInt(uint64(walletId.Serialized()), 32).
		MustStoreSlice(pubKey, 256).
		MustStoreDict(nil).
		EndCell()

	return &tlb.StateInit{
		Data: data,
		Code: c.code,
	}, nil
}

func (c *configCustomV5R1) getSpec(w *Wallet) RegularBuilder {
	return &SpecV5R1Final{
		SpecRegular: SpecRegular{
			wallet:      w,
			messagesTTL: 60 * 3,
		},
		SpecSeqno: SpecSeqno{seqnoFetcher: nil},
		config:    c.ConfigV5R1Final,
	}
}

func TestConfigCustom_CmpV5SubWalletAddress(t *testing.T) {
	pkey := ed25519.NewKeyFromSeed([]byte("12345678901234567890123456789012"))
	cfg := newConfigCustomV5R1(walletCode[V5R1Final])
	wCustom, err := FromPrivateKey(nil, pkey, cfg)
	if err != nil {
		t.Fatalf("failed to get custom v5r1 wallet from pk, err: %s", err)
	}

	wCustomSub, err := wCustom.GetSubwallet(1)
	if err != nil {
		t.Fatalf("failed to get custom sub v5r1 wallet, err: %s", err)
	}

	wOrig, err := FromPrivateKey(nil, pkey, ConfigV5R1Final{
		NetworkGlobalID: MainnetGlobalID,
	})
	if err != nil {
		t.Fatalf("failed to get orig v5r1 wallet from pk, err: %s", err)
	}

	wOrigSub, err := wOrig.GetSubwallet(1)
	if err != nil {
		t.Fatalf("failed to get orig sub v5r1 wallet, err: %s", err)
	}

	if !wCustomSub.WalletAddress().Equals(wOrigSub.WalletAddress()) {
		t.Fatalf("orig and custom v5r1 wallet addresses mismatch")
	}
}

type configCustomHighloadV3 struct {
	code *cell.Cell
	ConfigHighloadV3
}

func newConfigCustomHighloadV3(code *cell.Cell) ConfigCustom {
	return &configCustomHighloadV3{code: code, ConfigHighloadV3: ConfigHighloadV3{
		MessageTTL: 60,
		MessageBuilder: func(ctx context.Context, subWalletId uint32) (id uint32, createdAt int64, err error) {
			return 1, 1733333333, nil
		},
	}}
}

func (c *configCustomHighloadV3) GetStateInit(pubKey ed25519.PublicKey, subWallet uint32) (*tlb.StateInit, error) {
	timeout := c.MessageTTL
	if timeout >= 1<<22 {
		return nil, fmt.Errorf("too big timeout")
	}

	data := cell.BeginCell().
		MustStoreSlice(pubKey, 256).
		MustStoreUInt(uint64(subWallet), 32).
		MustStoreUInt(0, 66).
		MustStoreUInt(uint64(timeout), 22).
		EndCell()

	return &tlb.StateInit{
		Data: data,
		Code: c.code,
	}, nil
}

type specCustomHighloadV3 struct {
	SpecHighloadV3
}

func (s *specCustomHighloadV3) BuildMessage(ctx context.Context, isInitialized bool, _ *ton.BlockIDExt, messages []*Message) (*cell.Cell, error) {
	return s.SpecHighloadV3.BuildMessage(ctx, messages)
}

func (c *configCustomHighloadV3) getSpec(w *Wallet) RegularBuilder {
	return &specCustomHighloadV3{SpecHighloadV3: SpecHighloadV3{
		wallet: w,
		config: ConfigHighloadV3{
			MessageTTL: 60,
			MessageBuilder: func(ctx context.Context, subWalletId uint32) (id uint32, createdAt int64, err error) {
				return 1, 1733333333, nil
			},
		},
	}}
}

func TestConfigCustom_V3BocTx(t *testing.T) {
	pkey := ed25519.NewKeyFromSeed([]byte("12345678901234567890123456789012"))
	cfg := newConfigCustomHighloadV3(walletCode[HighloadV3])
	wCustom, err := FromPrivateKey(nil, pkey, cfg)
	if err != nil {
		t.Fatalf("failed to get custom HL3 wallet from pk, err: %s", err)
	}

	wCustomSub, err := wCustom.GetSubwallet(1)
	if err != nil {
		t.Fatalf("failed to get custom sub HL3 wallet, err: %s", err)
	}

	wCustomSubExtMgs, _ := wCustomSub.PrepareExternalMessageForMany(context.Background(), false, []*Message{SimpleMessage(wCustomSub.WalletAddress(), tlb.MustFromTON("0.5"), nil)})
	wCustomSubExtMgsCell, _ := tlb.ToCell(wCustomSubExtMgs)
	wCustomSubExtMgsBocHex := hex.EncodeToString(wCustomSubExtMgsCell.ToBOCWithFlags(false))

	wOrig, err := FromPrivateKey(nil, pkey, ConfigHighloadV3{
		MessageTTL: 60,
		MessageBuilder: func(ctx context.Context, subWalletId uint32) (id uint32, createdAt int64, err error) {
			return 1, 1733333333, nil
		},
	})
	if err != nil {
		t.Fatalf("failed to get orig HL3 wallet from pk, err: %s", err)
	}

	wOrigSub, err := wOrig.GetSubwallet(1)
	if err != nil {
		t.Fatalf("failed to get orig sub HL3 wallet, err: %s", err)
	}

	wOrigSubExtMgs, _ := wCustomSub.PrepareExternalMessageForMany(context.Background(), false, []*Message{SimpleMessage(wOrigSub.WalletAddress(), tlb.MustFromTON("0.5"), nil)})
	wOrigSubExtMgsCell, _ := tlb.ToCell(wOrigSubExtMgs)
	wOrigSubExtMgsBocHex := hex.EncodeToString(wOrigSubExtMgsCell.ToBOCWithFlags(false))

	if !wCustomSub.WalletAddress().Equals(wOrigSub.WalletAddress()) {
		t.Fatalf("orig and custom v5r1 wallet addresses mismatch")
	}

	if wCustomSubExtMgsBocHex != wOrigSubExtMgsBocHex {
		t.Fatalf("orig and custom ext boc msg mismatch")
	}
}
