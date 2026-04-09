package funcs

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

func TestTonopsMoreEdges(t *testing.T) {
	t.Run("config and global ops propagate type and range errors", func(t *testing.T) {
		st := newFuncTestState(t, map[int]any{9: "bad-config-root"})
		if err := CONFIGDICT().Interpret(st); err == nil {
			t.Fatal("CONFIGDICT should reject unsupported config root host values")
		}
		if _, err := configRootFromC7(st); err == nil {
			t.Fatal("configRootFromC7 should reject non-cell config roots")
		}

		st = newFuncTestState(t, map[int]any{9: "bad-config-root"})
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := CONFIGPARAM().Interpret(st); err == nil {
			t.Fatal("CONFIGPARAM should reject invalid config roots")
		}

		st = newFuncTestState(t, map[int]any{9: mustConfigRoot(t, 1, cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell())})
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := CONFIGPARAM().Interpret(st); err != nil {
			t.Fatalf("CONFIGPARAM(negative) failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("CONFIGPARAM(negative) = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, map[int]any{9: mustConfigRoot(t, 1, cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell())})
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := CONFIGOPTPARAM().Interpret(st); err != nil {
			t.Fatalf("CONFIGOPTPARAM(negative) failed: %v", err)
		}
		raw, err := st.Stack.PopAny()
		if err != nil || raw != nil {
			t.Fatalf("CONFIGOPTPARAM(negative) = (%v, %v), want nil", raw, err)
		}

		st = newFuncTestState(t, map[int]any{14: *tuple.NewTuple(nil, big.NewInt(1))})
		if err := GLOBALID().Interpret(st); err == nil {
			t.Fatal("GLOBALID should reject non-slice config entries")
		}

		st = newFuncTestState(t, nil)
		st.InitForExecution()
		if err := st.SetGlobal(3, "bad-global"); err != nil {
			t.Fatalf("SetGlobal failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := GETGLOBVAR().Interpret(st); err == nil {
			t.Fatal("GETGLOBVAR should reject unsupported global host values")
		}

		st = newFuncTestState(t, nil)
		st.InitForExecution()
		if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := SETGLOBVAR().Interpret(st); err == nil {
			t.Fatal("SETGLOBVAR should fail when the value is missing")
		}
	})

	t.Run("signature and secp ops cover remaining failure branches", func(t *testing.T) {
		seed := bytes.Repeat([]byte{0x22}, ed25519.SeedSize)
		priv := ed25519.NewKeyFromSeed(seed)
		pub := priv.Public().(ed25519.PublicKey)
		hash := sha256.Sum256([]byte("tonops-more"))

		st := newFuncTestState(t, nil)
		if err := st.Stack.PushInt(new(big.Int).SetBytes(hash[:])); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushInt(new(big.Int).SetBytes(pub)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := CHKSIGNU().Interpret(st); err == nil {
			t.Fatal("CHKSIGNU should reject short signatures")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(new(big.Int).SetBytes(hash[:])); err != nil {
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
		if err := ECRECOVER().Interpret(st); err != nil {
			t.Fatalf("ECRECOVER(zero sig) failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("ECRECOVER(zero sig) = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := ECRECOVER().Interpret(st); err == nil {
			t.Fatal("ECRECOVER should reject negative r values")
		}

		st = newFuncTestState(t, nil)
		if err := ECRECOVER().Interpret(st); err == nil {
			t.Fatal("ECRECOVER should fail when the stack is empty")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(new(big.Int).SetBytes(hash[:])); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := ECRECOVER().Interpret(st); err == nil {
			t.Fatal("ECRECOVER should fail when v/r/s are missing")
		}

		st = newFuncTestState(t, nil)
		if err := SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Interpret(st); err == nil {
			t.Fatal("SECP256K1_XONLY_PUBKEY_TWEAK_ADD should fail when the stack is empty")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Interpret(st); err == nil {
			t.Fatal("SECP256K1_XONLY_PUBKEY_TWEAK_ADD should reject negative tweaks")
		}
	})
}
