package wallet

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"testing"
)

type configCustomV5R1 struct {
	code *cell.Cell
	ConfigV5R1Final
}

func (c *configCustomV5R1) ParsePubKeyFromData(data *cell.Cell) (ed25519.PublicKey, error) {
	return nil, fmt.Errorf("not implemented")
}

func newConfigCustomV5R1(code *cell.Cell) ConfigCustom {
	return &configCustomV5R1{code: code, ConfigV5R1Final: ConfigV5R1Final{
		NetworkGlobalID: MainnetGlobalID,
	}}
}

type customSpecV5R1 struct {
	SpecV5R1Final
}

func (c *customSpecV5R1) BuildMessage(ctx context.Context, messages []*Message) (*cell.Cell, error) {
	return c.SpecV5R1Final.BuildMessage(ctx, false, nil, messages)
}

func (c *configCustomV5R1) GetSpec(w *Wallet) MessageBuilder {
	return &customSpecV5R1{SpecV5R1Final: SpecV5R1Final{
		SpecRegular: SpecRegular{
			wallet:      w,
			messagesTTL: 60 * 3,
		},
		SpecSeqno: SpecSeqno{seqnoFetcher: nil},
		config:    c.ConfigV5R1Final,
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
		t.Error("orig and custom v5r1 wallet address mismatch")
	}
}

type configCustomHighloadV3 struct {
	code *cell.Cell
	ConfigHighloadV3
}

func (c *configCustomHighloadV3) ParsePubKeyFromData(data *cell.Cell) (ed25519.PublicKey, error) {
	key, err := data.BeginParse().LoadSlice(256)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pubkey: %w", err)
	}
	return key, nil
}

func newConfigCustomHighloadV3(code *cell.Cell) ConfigCustom {
	return &configCustomHighloadV3{code: code, ConfigHighloadV3: ConfigHighloadV3{
		MessageTTL: 60,
		MessageBuilder: func(ctx context.Context, subWalletId uint32) (id uint32, createdAt int64, err error) {
			return 1, 1733333333, nil
		},
	}}
}

func (c *configCustomHighloadV3) GetSpec(w *Wallet) MessageBuilder {
	return &SpecHighloadV3{wallet: w, config: c.ConfigHighloadV3}
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
		t.Error("orig and custom HL3 wallet address mismatch")
	}

	if wCustomSubExtMgsBocHex != wOrigSubExtMgsBocHex {
		t.Error("orig and custom ext boc msg mismatch")
	}
}
