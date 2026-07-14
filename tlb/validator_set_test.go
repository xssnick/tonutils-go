package tlb

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestValidatorSetLegacyUsesInlineHashmap(t *testing.T) {
	validators := cell.NewDict(16)
	validator := cell.BeginCell().
		MustStoreUInt(0x53, 8).
		MustStoreUInt(0x8e81278a, 32).
		MustStoreSlice(bytes.Repeat([]byte{0x11}, 32), 256).
		MustStoreUInt(100, 64).
		EndCell()
	if err := validators.SetIntKey(big.NewInt(0), validator); err != nil {
		t.Fatalf("set validator: %v", err)
	}

	root := cell.BeginCell().
		MustStoreUInt(0x11, 8).
		MustStoreUInt(1, 32).
		MustStoreUInt(2, 32).
		MustStoreUInt(1, 16).
		MustStoreUInt(1, 16).
		MustStoreBuilder(validators.AsCell().ToBuilder()).
		EndCell()

	var parsed ValidatorSetAny
	if err := LoadFromCell(&parsed, root.MustBeginParse()); err != nil {
		t.Fatalf("load legacy validator set: %v", err)
	}

	set, ok := parsed.Validators.(ValidatorSet)
	if !ok {
		t.Fatalf("parsed validators type = %T, want ValidatorSet", parsed.Validators)
	}

	items, err := set.List.LoadAll()
	if err != nil {
		t.Fatalf("load inline legacy validator dictionary: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("loaded %d validators, want 1", len(items))
	}
	if key := items[0].Key.MustLoadUInt(16); key != 0 {
		t.Fatalf("validator key = %d, want 0", key)
	}

	var value Validator
	if err = LoadFromCell(&value, items[0].Value.Copy()); err != nil {
		t.Fatalf("load validator value: %v", err)
	}
	if value.Weight != 100 {
		t.Fatalf("validator weight = %d, want 100", value.Weight)
	}
}
