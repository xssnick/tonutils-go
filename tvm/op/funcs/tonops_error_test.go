package funcs

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTonopsAdditionalErrorPaths(t *testing.T) {
	t.Run("ConfigAndSignatureHelpers", func(t *testing.T) {
		st := newFuncTestState(t, map[int]any{9: (*cell.Cell)(nil)})
		root, err := configRootFromC7(st)
		if err != nil || root != nil {
			t.Fatalf("configRootFromC7(nil) = (%v, %v)", root, err)
		}
		val, err := loadConfigValue(st, big.NewInt(1))
		if err != nil || val != nil {
			t.Fatalf("loadConfigValue(nil root) = (%v, %v)", val, err)
		}
		signed32Min := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 31))
		signed32Max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 31), big.NewInt(1))
		belowSigned32Min := new(big.Int).Sub(new(big.Int).Set(signed32Min), big.NewInt(1))
		aboveSigned32Max := new(big.Int).Lsh(big.NewInt(1), 31)

		if !fitsSignedBits(signed32Min, 32) {
			t.Fatal("fitsSignedBits should accept signed 32-bit min")
		}
		if !fitsSignedBits(signed32Max, 32) {
			t.Fatal("fitsSignedBits should accept signed 32-bit max")
		}
		if fitsSignedBits(belowSigned32Min, 32) {
			t.Fatal("fitsSignedBits should reject below signed 32-bit min")
		}
		if fitsSignedBits(aboveSigned32Max, 32) {
			t.Fatal("fitsSignedBits should reject above signed 32-bit max")
		}

		if _, err := preloadFixedBytes(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice(), 16, "need 16 bits"); err == nil {
			t.Fatal("preloadFixedBytes should reject short slices")
		}

		misaligned := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()
		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(misaligned); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0x01}, 64), 512).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := CHKSIGNS().Interpret(st); err == nil {
			t.Fatal("CHKSIGNS should reject non-byte-aligned messages")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0x01}, 64), 512).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := CHKSIGNU().Interpret(st); err == nil {
			t.Fatal("CHKSIGNU should reject oversized public keys")
		}
	})

	t.Run("CurveOps", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := ECRECOVER().Interpret(st); err == nil {
			t.Fatal("ECRECOVER should reject oversized data hashes")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Interpret(st); err != nil {
			t.Fatalf("SECP256K1_XONLY_PUBKEY_TWEAK_ADD(invalid) failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("SECP256K1_XONLY_PUBKEY_TWEAK_ADD(invalid) = (%v, %v), want false", ok, err)
		}

		badPub := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xFF}, 33), 264).ToSlice()
		sig := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0x11}, 64), 512).ToSlice()

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(sig); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(badPub); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := P256_CHKSIGNU().Interpret(st); err != nil {
			t.Fatalf("P256_CHKSIGNU(invalid pub) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("P256_CHKSIGNU(invalid pub) = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(1, 1).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(sig); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushSlice(badPub); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := P256_CHKSIGNS().Interpret(st); err == nil {
			t.Fatal("P256_CHKSIGNS should reject non-byte-aligned messages")
		}
	})
}
