package tvm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	dictop "github.com/xssnick/tonutils-go/tvm/op/dict"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestDictOpsLoadFamilies(t *testing.T) {
	root := mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
	serialized := cell.BeginCell().MustStoreMaybeRef(root).ToSlice()

	t.Run("LDDICT", func(t *testing.T) {
		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF404), serialized)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		remainder, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop remainder: %v", err)
		}
		gotRoot, err := stack.PopMaybeCell()
		if err != nil {
			t.Fatalf("failed to pop dict root: %v", err)
		}
		if gotRoot == nil || string(gotRoot.Hash()) != string(root.Hash()) {
			t.Fatalf("unexpected dict root: %v", gotRoot)
		}
		if remainder.BitsLeft() != 0 || remainder.RefsNum() != 0 {
			t.Fatalf("unexpected remainder: %s", remainder.String())
		}
	})

	t.Run("PLDDICTQInvalid", func(t *testing.T) {
		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF407), cell.BeginCell().EndCell().BeginParse())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop result flag: %v", err)
		}
		if ok {
			t.Fatalf("expected false for invalid quiet preload")
		}
	})

	t.Run("LDDICTS", func(t *testing.T) {
		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF402), serialized)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		remainder, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop remainder: %v", err)
		}
		dictSlice, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop dict slice: %v", err)
		}
		if dictSlice.BitsLeft() != 1 || dictSlice.RefsNum() != 1 {
			t.Fatalf("unexpected dict slice shape: bits=%d refs=%d", dictSlice.BitsLeft(), dictSlice.RefsNum())
		}
		if remainder.BitsLeft() != 0 || remainder.RefsNum() != 0 {
			t.Fatalf("unexpected remainder: %s", remainder.String())
		}
	})
}

func TestDictOpsGetSetDeleteFamilies(t *testing.T) {
	t.Run("DICTGETPlain", func(t *testing.T) {
		root := mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
		stack, _, err := runRawCode(
			codeFromOpcodes(t, 0xF40A),
			mustSliceKey(t, 0x12, 8),
			root,
			int64(8),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop bool: %v", err)
		}
		value, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop value: %v", err)
		}
		if !ok || value.MustLoadUInt(8) != 0x34 {
			t.Fatalf("unexpected get result: ok=%v value=%s", ok, value.String())
		}
	})

	t.Run("DICTUGETREF", func(t *testing.T) {
		dict := cell.NewDict(8)
		ref := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		if _, err := dict.SetRefWithMode(mustKeyCell(t, 0xFE, 8), ref, cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to seed dict: %v", err)
		}
		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF40F), big.NewInt(0xFE), dict.AsCell(), int64(8))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop bool: %v", err)
		}
		gotRef, err := stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop ref: %v", err)
		}
		if !ok || string(gotRef.Hash()) != string(ref.Hash()) {
			t.Fatalf("unexpected ref lookup result")
		}
	})

	t.Run("DICTIGETOutOfRangeMiss", func(t *testing.T) {
		root := mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF40C), big.NewInt(1<<20), root, int64(8))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop bool: %v", err)
		}
		if ok {
			t.Fatalf("expected miss for out-of-range signed key")
		}
	})

	t.Run("DICTSETGETMissingReturnsFalse", func(t *testing.T) {
		stack, _, err := runRawCode(
			codeFromOpcodes(t, 0xF41A),
			cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse(),
			mustSliceKey(t, 0x01, 8),
			nil,
			int64(8),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop bool: %v", err)
		}
		newRoot, err := stack.PopMaybeCell()
		if err != nil {
			t.Fatalf("failed to pop dict root: %v", err)
		}
		if ok {
			t.Fatalf("expected false when SETGET inserts into empty dict")
		}
		if newRoot == nil {
			t.Fatalf("expected non-empty dict after insert")
		}
	})

	t.Run("DICTDELGETREF", func(t *testing.T) {
		dict := cell.NewDict(8)
		ref := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
		if _, err := dict.SetRefWithMode(mustKeyCell(t, 0x10, 8), ref, cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to seed dict: %v", err)
		}
		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF463), mustSliceKey(t, 0x10, 8), dict.AsCell(), int64(8))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop bool: %v", err)
		}
		gotRef, err := stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop ref: %v", err)
		}
		newRoot, err := stack.PopMaybeCell()
		if err != nil {
			t.Fatalf("failed to pop dict root: %v", err)
		}
		if !ok || string(gotRef.Hash()) != string(ref.Hash()) || newRoot != nil {
			t.Fatalf("unexpected DELGETREF result")
		}
	})

	t.Run("DICTGETOPTREFMiss", func(t *testing.T) {
		root := mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF469), mustSliceKey(t, 0x99, 8), root, int64(8))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		value, err := stack.PopMaybeCell()
		if err != nil {
			t.Fatalf("failed to pop maybe-ref: %v", err)
		}
		if value != nil {
			t.Fatalf("expected nil maybe-ref on miss")
		}
	})
}

func TestDictOpsMinPrefixAndExecFamilies(t *testing.T) {
	t.Run("DICTREMMAXREF", func(t *testing.T) {
		dict := cell.NewDict(8)
		minRef := cell.BeginCell().MustStoreUInt(0x1111, 16).EndCell()
		maxRef := cell.BeginCell().MustStoreUInt(0x2222, 16).EndCell()
		if _, err := dict.SetRefWithMode(mustKeyCell(t, 0x01, 8), minRef, cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to seed min ref: %v", err)
		}
		if _, err := dict.SetRefWithMode(mustKeyCell(t, 0xFE, 8), maxRef, cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to seed max ref: %v", err)
		}

		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF49B), dict.AsCell(), int64(8))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop bool: %v", err)
		}
		key, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop key slice: %v", err)
		}
		value, err := stack.PopCell()
		if err != nil {
			t.Fatalf("failed to pop ref: %v", err)
		}
		rest, err := stack.PopMaybeCell()
		if err != nil {
			t.Fatalf("failed to pop remaining dict: %v", err)
		}
		if !ok || key.MustLoadUInt(8) != 0xFE || string(value.Hash()) != string(maxRef.Hash()) || rest == nil {
			t.Fatalf("unexpected REMMAXREF result")
		}
	})

	t.Run("PFXDICTGETQMiss", func(t *testing.T) {
		dict := mustPrefixDictCell(t, 4, map[uint64]prefixEntry{
			0b10: {bits: 2, value: cell.BeginCell().MustStoreUInt(0xA, 4).EndCell()},
		})
		input := mustSliceKey(t, 0b0111, 4)
		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF4A8), input, dict, int64(4))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop bool: %v", err)
		}
		rest, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop original slice: %v", err)
		}
		if ok || rest.MustLoadUInt(4) != 0b0111 {
			t.Fatalf("unexpected PFXDICTGETQ miss result")
		}
	})

	t.Run("PFXDICTGETHit", func(t *testing.T) {
		value := cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()
		dict := mustPrefixDictCell(t, 4, map[uint64]prefixEntry{
			0b10: {bits: 2, value: value},
		})
		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF4A9), mustSliceKey(t, 0b1011, 4), dict, int64(4))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		remainder, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop remainder: %v", err)
		}
		gotValue, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop value: %v", err)
		}
		prefix, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop prefix: %v", err)
		}
		if remainder.MustLoadUInt(2) != 0b11 || gotValue.MustLoadUInt(4) != 0xD || prefix.MustLoadUInt(2) != 0b10 {
			t.Fatalf("unexpected PFXDICTGET result")
		}
	})

	t.Run("DICTIGETJMPZHit", func(t *testing.T) {
		dict := cell.NewDict(8)
		cont := codeFromOpcodes(t, 0x70|7)
		if _, err := dict.SetWithMode(mustKeyCell(t, 0x2A, 8), cont, cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to seed continuation dict: %v", err)
		}
		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF4BC), big.NewInt(0x2A), dict.AsCell(), int64(8))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop jump result: %v", err)
		}
		if got.Int64() != 7 {
			t.Fatalf("unexpected jump result: %s", got.String())
		}
	})

	t.Run("PFXDICTSWITCHHit", func(t *testing.T) {
		cont := codeFromOpcodes(t, 0x70|9)
		dict := mustPrefixDictCell(t, 4, map[uint64]prefixEntry{
			0b10: {bits: 2, value: cont},
		})
		code := codeFromBuilders(t, dictop.PFXDICTSWITCH(dict, 4).Serialize())
		stack, _, err := runRawCode(code, mustSliceKey(t, 0b1011, 4))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop pushed int: %v", err)
		}
		remainder, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop remainder: %v", err)
		}
		prefix, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop prefix: %v", err)
		}
		if got.Int64() != 9 || remainder.MustLoadUInt(2) != 0b11 || prefix.MustLoadUInt(2) != 0b10 {
			t.Fatalf("unexpected PFXDICTSWITCH result")
		}
	})

	t.Run("DICTGETNEXTEQAndIntegerBounds", func(t *testing.T) {
		root := mustPlainDictCell(t, 8, map[uint64]uint64{
			0x10: 0xA1,
			0x7F: 0xB2,
			0x80: 0xC3,
			0xF0: 0xD4,
		}, 8)

		stack, _, err := runRawCode(codeFromOpcodes(t, 0xF475), mustSliceKey(t, 0x7F, 8), root, int64(8))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop bool: %v", err)
		}
		key, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop nearest key: %v", err)
		}
		value, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop nearest value: %v", err)
		}
		if !ok || key.MustLoadUInt(8) != 0x7F || value.MustLoadUInt(8) != 0xB2 {
			t.Fatalf("unexpected GETNEXTEQ result: ok=%v key=%s value=%s", ok, key.String(), value.String())
		}

		stack, _, err = runRawCode(codeFromOpcodes(t, 0xF47E), big.NewInt(1<<20), root, int64(8))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ok, err = stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop bool: %v", err)
		}
		keyInt, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop nearest integer key: %v", err)
		}
		value, err = stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop nearest value: %v", err)
		}
		if !ok || keyInt.Uint64() != 0xF0 || value.MustLoadUInt(8) != 0xD4 {
			t.Fatalf("unexpected UGETPREV result: ok=%v key=%s value=%s", ok, keyInt.String(), value.String())
		}

		stack, _, err = runRawCode(codeFromOpcodes(t, 0xF47C), big.NewInt(-1), root, int64(8))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ok, err = stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop bool: %v", err)
		}
		keyInt, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop nearest integer key: %v", err)
		}
		value, err = stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop nearest value: %v", err)
		}
		if !ok || keyInt.Uint64() != 0x10 || value.MustLoadUInt(8) != 0xA1 {
			t.Fatalf("unexpected UGETNEXT result: ok=%v key=%s value=%s", ok, keyInt.String(), value.String())
		}
	})
}

func TestDictOpsCornerCases(t *testing.T) {
	t.Run("DICTGETShortKeyFailsWithCellUnderflow", func(t *testing.T) {
		root := mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8)
		_, res, err := runRawCode(codeFromOpcodes(t, 0xF40A), mustSliceKey(t, 0x1, 4), root, int64(8))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeCellUnderflow {
			t.Fatalf("unexpected exit code: %d err=%v", code, err)
		}
	})

	t.Run("PFXDICTGETMissFailsWithCellUnderflow", func(t *testing.T) {
		dict := mustPrefixDictCell(t, 4, map[uint64]prefixEntry{
			0b10: {bits: 2, value: cell.BeginCell().MustStoreUInt(1, 1).EndCell()},
		})
		_, res, err := runRawCode(codeFromOpcodes(t, 0xF4A9), mustSliceKey(t, 0b0111, 4), dict, int64(4))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeCellUnderflow {
			t.Fatalf("unexpected exit code: %d err=%v", code, err)
		}
	})

	t.Run("DICTUSETNegativeKeyFailsWithRangeCheck", func(t *testing.T) {
		_, res, err := runRawCode(
			codeFromOpcodes(t, 0xF416),
			cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse(),
			big.NewInt(-1),
			nil,
			int64(8),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected exit code: %d err=%v", code, err)
		}
	})
}

type prefixEntry struct {
	bits  uint
	value *cell.Cell
}

func mustPlainDictCell(t *testing.T, keyBits uint, items map[uint64]uint64, valueBits uint) *cell.Cell {
	t.Helper()
	dict := cell.NewDict(keyBits)
	for key, value := range items {
		if _, err := dict.SetWithMode(
			mustKeyCell(t, key, keyBits),
			cell.BeginCell().MustStoreUInt(value, valueBits).EndCell(),
			cell.DictSetModeSet,
		); err != nil {
			t.Fatalf("failed to seed plain dict: %v", err)
		}
	}
	return dict.AsCell()
}

func mustPrefixDictCell(t *testing.T, keyBits uint, items map[uint64]prefixEntry) *cell.Cell {
	t.Helper()
	dict := cell.NewPrefixDict(keyBits)
	for key, item := range items {
		if _, err := dict.SetWithMode(
			mustKeyCell(t, key, item.bits),
			item.value,
			cell.DictSetModeSet,
		); err != nil {
			t.Fatalf("failed to seed prefix dict: %v", err)
		}
	}
	return dict.AsCell()
}

func mustKeyCell(t *testing.T, value uint64, bits uint) *cell.Cell {
	t.Helper()
	return cell.BeginCell().MustStoreUInt(value, bits).EndCell()
}

func mustSliceKey(t *testing.T, value uint64, bits uint) *cell.Slice {
	t.Helper()
	return mustKeyCell(t, value, bits).BeginParse()
}

func codeFromOpcodes(t *testing.T, opcodes ...uint16) *cell.Cell {
	t.Helper()
	builders := make([]*cell.Builder, 0, len(opcodes))
	for _, opcode := range opcodes {
		builders = append(builders, cell.BeginCell().MustStoreUInt(uint64(opcode), 16))
	}
	return codeFromBuilders(t, builders...)
}

func codeFromBuilders(t *testing.T, builders ...*cell.Builder) *cell.Cell {
	t.Helper()
	code := cell.BeginCell()
	for _, builder := range builders {
		if err := code.StoreBuilder(builder); err != nil {
			t.Fatalf("failed to build code cell: %v", err)
		}
	}
	return code.EndCell()
}

func runRawCodeWithGas(code *cell.Cell, gasLimit int64, values ...any) (*vmcore.Stack, *ExecutionResult, error) {
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

	res, err := NewTVM().ExecuteDetailed(code, cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.GasWithLimit(gasLimit), stack)
	return stack, res, err
}

func runRawCode(code *cell.Cell, values ...any) (*vmcore.Stack, *ExecutionResult, error) {
	return runRawCodeWithGas(code, 1_000_000, values...)
}

func exitCodeFromResult(res *ExecutionResult, err error) int64 {
	if err == nil {
		if res == nil {
			return 0
		}
		return res.ExitCode
	}
	var vmErr vmerr.VMError
	if errors.As(err, &vmErr) {
		return vmErr.Code
	}
	return -1
}
