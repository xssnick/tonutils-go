package wallet

import (
	"crypto/ed25519"
	"fmt"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"testing"
)

type configCustom struct {
	code *cell.Cell
	ConfigV5R1Final
}

func newConfigCustom(code *cell.Cell) ConfigCustom {
	return &configCustom{code: code, ConfigV5R1Final: ConfigV5R1Final{
		NetworkGlobalID: MainnetGlobalID,
	}}
}

func (c *configCustom) GetStateInit(pubKey ed25519.PublicKey, subWallet uint32) (*tlb.StateInit, error) {
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

func (c *configCustom) getSpec(w *Wallet) RegularBuilder {
	return &SpecV5R1Final{
		SpecRegular: SpecRegular{
			wallet:      w,
			messagesTTL: 60 * 3,
		},
		SpecSeqno: SpecSeqno{},
		config:    c.ConfigV5R1Final,
	}
}

func ExampleConfigCustom() {
	pkey := ed25519.NewKeyFromSeed([]byte("12345678901234567890123456789012"))
	cfg := newConfigCustom(walletCode[V5R1Final])
	w, _ := FromPrivateKey(nil, pkey, cfg)
	w, _ = w.GetSubwallet(1)
	fmt.Println(w.WalletAddress())
	// Output:
	// UQAFbC0rPzK6i2pFz3aDuCav8WO73bqM1bwdcZfF6p8ykBuF
}

func TestConfigCustom(t *testing.T) {
	pkey := ed25519.NewKeyFromSeed([]byte("12345678901234567890123456789012"))
	cfg := newConfigCustom(walletCode[V5R1Final])
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
