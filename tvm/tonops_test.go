package tvm

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	localec "github.com/xssnick/tonutils-go/tvm/internal/secp256k1"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func mustP256RawSignature(t *testing.T, priv *ecdsa.PrivateKey, data []byte) []byte {
	t.Helper()

	digest := sha256.Sum256(data)
	r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("failed to sign with p256: %v", err)
	}

	raw := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(raw[32-len(rBytes):32], rBytes)
	copy(raw[64-len(sBytes):64], sBytes)
	return raw
}

var (
	tonopsTestAddr        = address.MustParseAddr("EQAYqo4u7VF0fa4DPAebk4g9lBytj2VFny7pzXR0trjtXQaO")
	tonopsTestTime        = time.Unix(1_710_000_000, 0).UTC()
	tonopsTestSeed        = bytes.Repeat([]byte{0x11}, 32)
	tonopsTestBalance     = big.NewInt(10_000_000)
	tonopsTestBlockLT     = int64(987654321)
	tonopsTestLogicalTime = int64(123456789)
	tonopsTestStorageFees = int64(777)
	tonopsTestGlobalID    = int32(0x12345678)
)

type tonopsTestC7Config struct {
	ConfigRoot     *cell.Cell
	LegacyConfig   *cell.Cell
	MyCode         *cell.Cell
	IncomingValue  tuple.Tuple
	Balance        tuple.Tuple
	StorageFees    int64
	UnpackedConfig tuple.Tuple
	Globals        map[int]any
	ExtraParams    map[int]any
}

func makeTonopsTestC7(t *testing.T, cfg tonopsTestC7Config) tuple.Tuple {
	t.Helper()

	codeCell := cfg.MyCode
	if codeCell == nil {
		codeCell = cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	}

	balance := cfg.Balance
	if balance.Len() == 0 {
		balance = *tuple.NewTuple(new(big.Int).Set(tonopsTestBalance), nil)
	}

	incomingValue := cfg.IncomingValue
	if incomingValue.Len() == 0 {
		incomingValue = *tuple.NewTuple(big.NewInt(7777), nil)
	}

	unpacked := cfg.UnpackedConfig
	if unpacked.Len() == 0 {
		unpacked = tuple.NewTupleSized(2)
		if err := unpacked.Set(1, cell.BeginCell().MustStoreUInt(uint64(uint32(tonopsTestGlobalID)), 32).ToSlice()); err != nil {
			t.Fatalf("failed to seed unpacked config: %v", err)
		}
	}

	inner := tuple.NewTupleSized(20)
	storageFees := cfg.StorageFees
	if storageFees == 0 {
		storageFees = tonopsTestStorageFees
	}

	mustSetTupleValue(t, &inner, 0, uint32(0x076ef1ea))
	mustSetTupleValue(t, &inner, 1, uint8(0))
	mustSetTupleValue(t, &inner, 2, uint8(0))
	mustSetTupleValue(t, &inner, 3, tonopsTestTime.Unix())
	mustSetTupleValue(t, &inner, 4, tonopsTestBlockLT)
	mustSetTupleValue(t, &inner, 5, tonopsTestLogicalTime)
	mustSetTupleValue(t, &inner, 6, new(big.Int).SetBytes(tonopsTestSeed))
	mustSetTupleValue(t, &inner, 7, balance)
	mustSetTupleValue(t, &inner, 8, cell.BeginCell().MustStoreAddr(tonopsTestAddr).ToSlice())
	mustSetTupleValue(t, &inner, 9, cfg.ConfigRoot)
	mustSetTupleValue(t, &inner, 10, codeCell)
	mustSetTupleValue(t, &inner, 11, incomingValue)
	mustSetTupleValue(t, &inner, 12, storageFees)
	mustSetTupleValue(t, &inner, 14, unpacked)
	mustSetTupleValue(t, &inner, 19, cfg.LegacyConfig)
	for idx, val := range cfg.ExtraParams {
		mustSetTupleValue(t, &inner, idx, val)
	}

	maxIdx := 0
	for idx := range cfg.Globals {
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	top := tuple.NewTupleSized(maxIdx + 1)
	mustSetTupleValue(t, &top, 0, inner)
	for idx, val := range cfg.Globals {
		mustSetTupleValue(t, &top, idx, val)
	}
	return top
}

func mustSetTupleValue(t *testing.T, tup *tuple.Tuple, idx int, val any) {
	t.Helper()
	if idx >= tup.Len() {
		tup.Resize(idx + 1)
	}
	if err := tup.Set(idx, normalizeTonopsValue(val)); err != nil {
		t.Fatalf("failed to set tuple index %d: %v", idx, err)
	}
}

func normalizeTonopsValue(val any) any {
	switch v := val.(type) {
	case int:
		return big.NewInt(int64(v))
	case int8:
		return big.NewInt(int64(v))
	case int16:
		return big.NewInt(int64(v))
	case int32:
		return big.NewInt(int64(v))
	case int64:
		return big.NewInt(v)
	case uint8:
		return new(big.Int).SetUint64(uint64(v))
	case uint16:
		return new(big.Int).SetUint64(uint64(v))
	case uint32:
		return new(big.Int).SetUint64(uint64(v))
	case uint64:
		return new(big.Int).SetUint64(v)
	case *cell.Cell:
		if v == nil {
			return nil
		}
		return v
	case *cell.Slice:
		if v == nil {
			return nil
		}
		return v
	case *cell.Builder:
		if v == nil {
			return nil
		}
		return v
	default:
		return val
	}
}

func mustUintKeyCell(t *testing.T, key uint64, bits uint) *cell.Cell {
	t.Helper()
	return cell.BeginCell().MustStoreUInt(key, bits).EndCell()
}

func mustConfigDictCell(t *testing.T, entries map[uint32]*cell.Cell) *cell.Cell {
	t.Helper()
	dict := cell.NewDict(32)
	for key, value := range entries {
		if _, err := dict.SetRefWithMode(mustUintKeyCell(t, uint64(key), 32), value, cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to seed config entry %d: %v", key, err)
		}
	}
	return dict.AsCell()
}

func mustDeepCell(t *testing.T, depth int) *cell.Cell {
	t.Helper()
	cl := cell.BeginCell().EndCell()
	for i := 0; i < depth; i++ {
		cl = cell.BeginCell().MustStoreRef(cl).EndCell()
	}
	return cl
}

func runRawCodeWithEnv(t *testing.T, code, data *cell.Cell, c7 tuple.Tuple, globalVersion int, values ...any) (*vmcore.Stack, *ExecutionResult, error) {
	t.Helper()

	stack := vmcore.NewStack()
	for _, value := range values {
		switch v := value.(type) {
		case int:
			if err := stack.PushInt(big.NewInt(int64(v))); err != nil {
				return nil, nil, err
			}
		case int64:
			if err := stack.PushInt(big.NewInt(v)); err != nil {
				return nil, nil, err
			}
		case uint64:
			if err := stack.PushInt(new(big.Int).SetUint64(v)); err != nil {
				return nil, nil, err
			}
		case *big.Int:
			if err := stack.PushInt(v); err != nil {
				return nil, nil, err
			}
		default:
			if err := stack.PushAny(value); err != nil {
				return nil, nil, err
			}
		}
	}

	machine := NewTVM()
	machine.globalVersion = globalVersion
	res, err := machine.ExecuteDetailed(code, data, c7, vmcore.GasWithLimit(1_000_000), stack)
	return stack, res, err
}

func TestTonOpsGoSemantics(t *testing.T) {
	t.Run("SetGasLimitOutOfGasPushesConsumedGas", func(t *testing.T) {
		code := codeFromBuilders(t,
			stackop.PUSHINT(big.NewInt(1)).Serialize(),
			funcsop.SETGASLIMIT().Serialize(),
		)

		stack, res, err := runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.DefaultGlobalVersion)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeOutOfGas {
			t.Fatalf("expected out of gas exit, got %d", code)
		}

		stack = res.Stack
		if stack.Len() != 1 {
			t.Fatalf("expected one stack value after OOG, got %d", stack.Len())
		}
		used, popErr := stack.PopIntFinite()
		if popErr != nil {
			t.Fatalf("failed to pop used gas: %v", popErr)
		}
		if used.Int64() <= 1 {
			t.Fatalf("expected consumed gas greater than new limit, got %s", used.String())
		}
	})

	t.Run("CommitSnapshotSurvivesLaterFailure", func(t *testing.T) {
		msg1 := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		msg2 := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		code := codeFromBuilders(t,
			stackop.PUSHREF(msg1).Serialize(),
			stackop.PUSHINT(big.NewInt(0)).Serialize(),
			funcsop.SENDRAWMSG().Serialize(),
			funcsop.COMMIT().Serialize(),
			stackop.PUSHREF(msg2).Serialize(),
			stackop.PUSHINT(big.NewInt(0)).Serialize(),
			funcsop.SENDRAWMSG().Serialize(),
			cell.BeginCell().MustStoreUInt(0xF205, 16),
		)

		_, res, err := runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.DefaultGlobalVersion)
		if code := exitCodeFromResult(res, err); code != 5 {
			t.Fatalf("expected exception 5, got %d", code)
		}
		if res == nil {
			t.Fatalf("expected execution result on committed failure")
		}

		wantActions, convErr := tlb.ToCell(tlb.OutList{
			Prev: cell.BeginCell().EndCell(),
			Out:  tlb.ActionSendMsg{Mode: 0, Msg: msg1},
		})
		if convErr != nil {
			t.Fatalf("failed to build expected actions: %v", convErr)
		}

		if res.Actions == nil {
			t.Fatalf("expected committed actions to be returned")
		}
		if !bytes.Equal(res.Actions.Hash(), wantActions.Hash()) {
			t.Fatalf("unexpected committed actions:\nwant=%s\ngot=%s", wantActions.Dump(), res.Actions.Dump())
		}
	})

	t.Run("CommitRejectsTooDeepData", func(t *testing.T) {
		code := codeFromBuilders(t, funcsop.COMMIT().Serialize())
		_, res, err := runRawCodeWithEnv(t, code, mustDeepCell(t, vmcore.MaxDataDepth+1), tuple.Tuple{}, vmcore.DefaultGlobalVersion)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeCellOverflow {
			t.Fatalf("expected cell overflow, got %d", code)
		}
	})

	t.Run("ConfigParamAndOptParamHitMiss", func(t *testing.T) {
		configValue := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		c7 := makeTonopsTestC7(t, tonopsTestC7Config{
			ConfigRoot: mustConfigDictCell(t, map[uint32]*cell.Cell{7: configValue}),
		})

		stack, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.CONFIGPARAM().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			int64(7),
		)
		if err != nil {
			t.Fatalf("configparam unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("configparam unexpected exit code: %d", res.ExitCode)
		}
		stack = res.Stack
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop configparam flag: %v", err)
		}
		if !ok {
			t.Fatalf("expected configparam hit flag")
		}
		gotCell, err := stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop configparam cell: %v", err)
		}
		if !bytes.Equal(gotCell.Hash(), configValue.Hash()) {
			t.Fatalf("unexpected configparam value")
		}

		stack, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.CONFIGPARAM().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			int64(8),
		)
		if err != nil {
			t.Fatalf("configparam miss unexpected error: %v", err)
		}
		stack = res.Stack
		ok, err = stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop configparam miss flag: %v", err)
		}
		if ok {
			t.Fatalf("expected configparam miss flag to be false")
		}

		stack, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.CONFIGOPTPARAM().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			int64(8),
		)
		if err != nil {
			t.Fatalf("configoptparam miss unexpected error: %v", err)
		}
		stack = res.Stack
		val, err := stack.PopMaybeCell()
		if err != nil {
			t.Fatalf("failed to pop configoptparam miss value: %v", err)
		}
		if val != nil {
			t.Fatalf("expected nil from configoptparam miss, got %T", val)
		}
	})

	t.Run("GlobalsSetAndNoopNilPastEnd", func(t *testing.T) {
		c7 := makeTonopsTestC7(t, tonopsTestC7Config{
			Globals: map[int]any{1: int64(123)},
		})

		code := codeFromBuilders(t,
			funcsop.SETGLOB(2).Serialize(),
			funcsop.GETGLOB(2).Serialize(),
		)
		stack, res, err := runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, int64(456))
		if err != nil {
			t.Fatalf("setglob/getglob unexpected error: %v", err)
		}
		stack = res.Stack
		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop setglob result: %v", err)
		}
		if got.Int64() != 456 {
			t.Fatalf("expected 456, got %s", got.String())
		}

		code = codeFromBuilders(t,
			funcsop.SETGLOBVAR().Serialize(),
			stackop.PUSHINT(big.NewInt(10)).Serialize(),
			funcsop.GETGLOBVAR().Serialize(),
		)
		stack, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, nil, int64(10))
		if err != nil {
			t.Fatalf("setglobvar noop unexpected error: %v", err)
		}
		stack = res.Stack
		val, err := stack.PopAny()
		if err != nil {
			t.Fatalf("failed to pop getglobvar result: %v", err)
		}
		if val != nil {
			t.Fatalf("expected nil global after noop set, got %T", val)
		}
	})

	t.Run("GetParamLong", func(t *testing.T) {
		c7 := makeTonopsTestC7(t, tonopsTestC7Config{
			ExtraParams: map[int]any{18: int64(0x55AA)},
		})

		code := codeFromBuilders(t, funcsop.GETPARAMLONG(18).Serialize())
		stack, res, err := runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion)
		if err != nil {
			t.Fatalf("getparamlong unexpected error: %v", err)
		}
		stack = res.Stack
		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop getparamlong result: %v", err)
		}
		if got.Int64() != 0x55AA {
			t.Fatalf("expected 0x55AA, got %s", got.String())
		}
	})

}

func TestTonOpsCHKSIGN(t *testing.T) {
	t.Run("CHKSIGNUVerifiesAndUsesFreeGasBudget", func(t *testing.T) {
		seed := bytes.Repeat([]byte{0x42}, 32)
		priv := ed25519.NewKeyFromSeed(seed)
		pub := priv.Public().(ed25519.PublicKey)
		hash := bytes.Repeat([]byte{0xAB}, 32)
		sig := ed25519.Sign(priv, hash)

		st := vmcore.NewStack()
		if err := st.PushInt(new(big.Int).SetBytes(hash)); err != nil {
			t.Fatalf("failed to push hash: %v", err)
		}
		if err := st.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice()); err != nil {
			t.Fatalf("failed to push signature: %v", err)
		}
		if err := st.PushInt(new(big.Int).SetBytes(pub)); err != nil {
			t.Fatalf("failed to push pubkey: %v", err)
		}

		state := &vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		}
		if err := funcsop.CHKSIGNU().Interpret(state); err != nil {
			t.Fatalf("CHKSIGNU failed: %v", err)
		}

		ok, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop verification result: %v", err)
		}
		if !ok {
			t.Fatal("expected signature verification to succeed")
		}
		if state.Gas.FreeConsumed != vmcore.ChksgnGasPrice {
			t.Fatalf("unexpected deferred gas: %d", state.Gas.FreeConsumed)
		}
	})

	t.Run("CHKSIGNURejectsInvalidSignature", func(t *testing.T) {
		seed := bytes.Repeat([]byte{0x24}, 32)
		priv := ed25519.NewKeyFromSeed(seed)
		pub := priv.Public().(ed25519.PublicKey)
		hash := bytes.Repeat([]byte{0xCD}, 32)
		sig := ed25519.Sign(priv, hash)
		sig[0] ^= 0xFF

		st := vmcore.NewStack()
		_ = st.PushInt(new(big.Int).SetBytes(hash))
		_ = st.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice())
		_ = st.PushInt(new(big.Int).SetBytes(pub))

		if err := funcsop.CHKSIGNU().Interpret(&vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		}); err != nil {
			t.Fatalf("CHKSIGNU failed: %v", err)
		}

		ok, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop verification result: %v", err)
		}
		if ok {
			t.Fatal("expected signature verification to fail")
		}
	})

	t.Run("CHKSIGNSRejectsUnalignedSlice", func(t *testing.T) {
		seed := bytes.Repeat([]byte{0x55}, 32)
		priv := ed25519.NewKeyFromSeed(seed)
		pub := priv.Public().(ed25519.PublicKey)
		hash := bytes.Repeat([]byte{0x11}, 32)
		sig := ed25519.Sign(priv, hash)

		st := vmcore.NewStack()
		_ = st.PushSlice(cell.BeginCell().MustStoreUInt(0x7F, 7).ToSlice())
		_ = st.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice())
		_ = st.PushInt(new(big.Int).SetBytes(pub))

		err := funcsop.CHKSIGNS().Interpret(&vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		})
		if err == nil {
			t.Fatal("expected CHKSIGNS to fail on unaligned slice")
		}
		if code := exitCodeFromResult(nil, err); code != vmerr.CodeCellUnderflow {
			t.Fatalf("unexpected exit code: %d", code)
		}
	})
}

func TestTonOpsCryptoPrimitives(t *testing.T) {
	t.Run("ECRECOVERRecoversUncompressedPublicKey", func(t *testing.T) {
		hash := bytes.Repeat([]byte{0x42}, 32)
		v, r, s, pub, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x57}, 32), hash)
		if !ok {
			t.Fatal("failed to build secp256k1 test vector")
		}

		st := vmcore.NewStack()
		if err := st.PushInt(new(big.Int).SetBytes(hash)); err != nil {
			t.Fatalf("failed to push hash: %v", err)
		}
		if err := st.PushInt(big.NewInt(int64(v))); err != nil {
			t.Fatalf("failed to push v: %v", err)
		}
		if err := st.PushInt(r); err != nil {
			t.Fatalf("failed to push r: %v", err)
		}
		if err := st.PushInt(s); err != nil {
			t.Fatalf("failed to push s: %v", err)
		}

		state := &vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		}
		if err := funcsop.ECRECOVER().Interpret(state); err != nil {
			t.Fatalf("ECRECOVER failed: %v", err)
		}

		ok, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop success flag: %v", err)
		}
		if !ok {
			t.Fatal("expected recovery to succeed")
		}
		y, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop y: %v", err)
		}
		x, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop x: %v", err)
		}
		h, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop format byte: %v", err)
		}

		if h.Int64() != int64(pub[0]) {
			t.Fatalf("unexpected format byte: got %d want %d", h.Int64(), pub[0])
		}
		if !bytes.Equal(x.FillBytes(make([]byte, 32)), pub[1:33]) {
			t.Fatalf("unexpected x coordinate")
		}
		if !bytes.Equal(y.FillBytes(make([]byte, 32)), pub[33:65]) {
			t.Fatalf("unexpected y coordinate")
		}
		if state.Gas.Used() != vmcore.EcrecoverGasPrice {
			t.Fatalf("unexpected gas used: %d", state.Gas.Used())
		}
	})

	t.Run("ECRECOVERReturnsFalseOnInvalidRecoveryID", func(t *testing.T) {
		hash := bytes.Repeat([]byte{0x44}, 32)

		st := vmcore.NewStack()
		_ = st.PushInt(new(big.Int).SetBytes(hash))
		_ = st.PushInt(big.NewInt(4))
		_ = st.PushInt(big.NewInt(1))
		_ = st.PushInt(big.NewInt(1))

		state := &vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		}
		if err := funcsop.ECRECOVER().Interpret(state); err != nil {
			t.Fatalf("ECRECOVER failed: %v", err)
		}
		ok, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop recovery result: %v", err)
		}
		if ok {
			t.Fatal("expected recovery to fail")
		}
		if state.Gas.Used() != vmcore.EcrecoverGasPrice {
			t.Fatalf("unexpected gas used: %d", state.Gas.Used())
		}
	})

	t.Run("SECP256K1XOnlyPubkeyTweakAddReturnsTweakedPubKey", func(t *testing.T) {
		hash := bytes.Repeat([]byte{0x21}, 32)
		_, _, _, pub, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x57}, 32), hash)
		if !ok {
			t.Fatal("failed to build secp256k1 xonly fixture")
		}

		tweak := big.NewInt(7)
		expected, ok := localec.XOnlyPubkeyTweakAddUncompressed(pub[1:33], tweak.FillBytes(make([]byte, 32)))
		if !ok {
			t.Fatal("failed to build expected tweaked pubkey")
		}

		st := vmcore.NewStack()
		_ = st.PushInt(new(big.Int).SetBytes(pub[1:33]))
		_ = st.PushInt(big.NewInt(7))

		state := &vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		}
		if err := funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Interpret(state); err != nil {
			t.Fatalf("SECP256K1_XONLY_PUBKEY_TWEAK_ADD failed: %v", err)
		}

		ok, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop success flag: %v", err)
		}
		if !ok {
			t.Fatal("expected xonly tweak add to succeed")
		}
		y, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop y: %v", err)
		}
		x, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop x: %v", err)
		}
		h, err := st.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop format byte: %v", err)
		}

		if h.Int64() != int64(expected[0]) {
			t.Fatalf("unexpected format byte: got %d want %d", h.Int64(), expected[0])
		}
		if !bytes.Equal(x.FillBytes(make([]byte, 32)), expected[1:33]) {
			t.Fatalf("unexpected x coordinate")
		}
		if !bytes.Equal(y.FillBytes(make([]byte, 32)), expected[33:65]) {
			t.Fatalf("unexpected y coordinate")
		}
		if state.Gas.Used() != vmcore.Secp256k1XonlyPubkeyTweakAddGasPrice {
			t.Fatalf("unexpected gas used: %d", state.Gas.Used())
		}
	})

	t.Run("SECP256K1XOnlyPubkeyTweakAddReturnsFalseOnInvalidInput", func(t *testing.T) {
		st := vmcore.NewStack()
		_ = st.PushInt(new(big.Int).SetBytes(bytes.Repeat([]byte{0xFF}, 32)))
		_ = st.PushInt(big.NewInt(1))

		state := &vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		}
		if err := funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Interpret(state); err != nil {
			t.Fatalf("SECP256K1_XONLY_PUBKEY_TWEAK_ADD failed: %v", err)
		}
		ok, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop result flag: %v", err)
		}
		if ok {
			t.Fatal("expected invalid xonly key to fail")
		}
		if state.Gas.Used() != vmcore.Secp256k1XonlyPubkeyTweakAddGasPrice {
			t.Fatalf("unexpected gas used: %d", state.Gas.Used())
		}
	})

	t.Run("SECP256K1XOnlyPubkeyTweakAddReturnsFalseOnTooLargeTweak", func(t *testing.T) {
		hash := bytes.Repeat([]byte{0x21}, 32)
		_, _, _, pub, ok := localec.SignRecoverable(bytes.Repeat([]byte{0x31}, 32), bytes.Repeat([]byte{0x57}, 32), hash)
		if !ok {
			t.Fatal("failed to build secp256k1 xonly fixture")
		}

		st := vmcore.NewStack()
		_ = st.PushInt(new(big.Int).SetBytes(pub[1:33]))
		_ = st.PushInt(new(big.Int).SetBytes(localec.CurveOrderBytes()))

		state := &vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		}
		if err := funcsop.SECP256K1_XONLY_PUBKEY_TWEAK_ADD().Interpret(state); err != nil {
			t.Fatalf("SECP256K1_XONLY_PUBKEY_TWEAK_ADD failed: %v", err)
		}
		ok, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop result flag: %v", err)
		}
		if ok {
			t.Fatal("expected tweak >= n to fail")
		}
		if state.Gas.Used() != vmcore.Secp256k1XonlyPubkeyTweakAddGasPrice {
			t.Fatalf("unexpected gas used: %d", state.Gas.Used())
		}
	})

	t.Run("ECRECOVERRejectsOversizedHash", func(t *testing.T) {
		hash := new(big.Int).Lsh(big.NewInt(1), 256)

		st := vmcore.NewStack()
		_ = st.PushAny(hash)
		_ = st.PushInt(big.NewInt(0))
		_ = st.PushInt(big.NewInt(1))
		_ = st.PushInt(big.NewInt(1))

		state := &vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		}
		err := funcsop.ECRECOVER().Interpret(state)
		if err == nil {
			t.Fatal("expected ECRECOVER to reject oversized hash")
		}
		if code := exitCodeFromResult(nil, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected exit code: %d", code)
		}
		if state.Gas.Used() != 0 {
			t.Fatalf("expected no gas consumed before range check, got %d", state.Gas.Used())
		}
	})

	t.Run("P256CHKSIGNUVerifiesCompressedKey", func(t *testing.T) {
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("failed to generate p256 key: %v", err)
		}
		hashBytes := bytes.Repeat([]byte{0x55}, 32)
		sig := mustP256RawSignature(t, priv, hashBytes)
		pub := elliptic.MarshalCompressed(elliptic.P256(), priv.PublicKey.X, priv.PublicKey.Y)

		st := vmcore.NewStack()
		_ = st.PushInt(new(big.Int).SetBytes(hashBytes))
		_ = st.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice())
		_ = st.PushSlice(cell.BeginCell().MustStoreSlice(pub, 264).ToSlice())

		state := &vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		}
		if err = funcsop.P256_CHKSIGNU().Interpret(state); err != nil {
			t.Fatalf("P256_CHKSIGNU failed: %v", err)
		}
		ok, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop verification result: %v", err)
		}
		if !ok {
			t.Fatal("expected p256 verification to succeed")
		}
		if state.Gas.Used() != vmcore.P256ChksgnGasPrice {
			t.Fatalf("unexpected gas used: %d", state.Gas.Used())
		}
	})

	t.Run("P256CHKSIGNUReturnsFalseForMalformedKey", func(t *testing.T) {
		hashBytes := bytes.Repeat([]byte{0x66}, 32)
		sig := make([]byte, 64)
		badKey := append([]byte{0x05}, bytes.Repeat([]byte{0x01}, 32)...)

		st := vmcore.NewStack()
		_ = st.PushInt(new(big.Int).SetBytes(hashBytes))
		_ = st.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice())
		_ = st.PushSlice(cell.BeginCell().MustStoreSlice(badKey, 264).ToSlice())

		state := &vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		}
		if err := funcsop.P256_CHKSIGNU().Interpret(state); err != nil {
			t.Fatalf("P256_CHKSIGNU failed: %v", err)
		}
		ok, err := st.PopBool()
		if err != nil {
			t.Fatalf("failed to pop verification result: %v", err)
		}
		if ok {
			t.Fatal("expected malformed key verification to fail")
		}
		if state.Gas.Used() != vmcore.P256ChksgnGasPrice {
			t.Fatalf("unexpected gas used: %d", state.Gas.Used())
		}
	})

	t.Run("P256CHKSIGNSRejectsUnalignedSlice", func(t *testing.T) {
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("failed to generate p256 key: %v", err)
		}
		sig := mustP256RawSignature(t, priv, []byte("slice"))
		pub := elliptic.MarshalCompressed(elliptic.P256(), priv.PublicKey.X, priv.PublicKey.Y)

		st := vmcore.NewStack()
		_ = st.PushSlice(cell.BeginCell().MustStoreUInt(0x7F, 7).ToSlice())
		_ = st.PushSlice(cell.BeginCell().MustStoreSlice(sig, 512).ToSlice())
		_ = st.PushSlice(cell.BeginCell().MustStoreSlice(pub, 264).ToSlice())

		state := &vmcore.State{
			GlobalVersion: vmcore.DefaultGlobalVersion,
			Stack:         st,
			Gas:           vmcore.NewGas(),
		}
		err = funcsop.P256_CHKSIGNS().Interpret(state)
		if err == nil {
			t.Fatal("expected P256_CHKSIGNS to fail on unaligned slice")
		}
		if code := exitCodeFromResult(nil, err); code != vmerr.CodeCellUnderflow {
			t.Fatalf("unexpected exit code: %d", code)
		}
		if state.Gas.Used() != 0 {
			t.Fatalf("expected no gas consumed before underflow, got %d", state.Gas.Used())
		}
	})
}

func TestTonOpsErrorsRemainVMErrors(t *testing.T) {
	c7 := makeTonopsTestC7(t, tonopsTestC7Config{})
	_, _, err := runRawCodeWithEnv(t, codeFromBuilders(t, funcsop.CONFIGPARAM().Serialize()), cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, "not-an-int")
	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeTypeCheck {
		t.Fatalf("expected type check, got %v", err)
	}
}
