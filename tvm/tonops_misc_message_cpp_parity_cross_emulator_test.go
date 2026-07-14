//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorStoreVarIntNaNRangeCheck(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := []struct {
		name string
		op   *cell.Builder
	}{
		{name: "STVARINT16", op: funcsop.STVARINT16().Serialize()},
		{name: "STVARUINT32", op: funcsop.STVARUINT32().Serialize()},
		{name: "STVARINT32", op: funcsop.STVARINT32().Serialize()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runTonOpsEdgeParityCase(
				t,
				prependRawMethodDrop(codeFromBuilders(t, tt.op)),
				[]any{cell.BeginCell(), vm.NaN{}},
				tuple.Tuple{},
				int32(vmerr.CodeRangeCheck),
				0,
			)
		})
	}
}

func TestTVMCrossEmulatorMessageStoreShortStackPrechecks(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := []struct {
		name string
		op   *cell.Builder
	}{
		{name: "STVARINT16", op: funcsop.STVARINT16().Serialize()},
		{name: "STVARUINT32", op: funcsop.STVARUINT32().Serialize()},
		{name: "STVARINT32", op: funcsop.STVARINT32().Serialize()},
		{name: "STSTDADDR", op: funcsop.STSTDADDR().Serialize()},
		{name: "STSTDADDRQ", op: funcsop.STSTDADDRQ().Serialize()},
		{name: "STOPTSTDADDR", op: funcsop.STOPTSTDADDR().Serialize()},
		{name: "STOPTSTDADDRQ", op: funcsop.STOPTSTDADDRQ().Serialize()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runTonOpsEdgeParityCase(
				t,
				prependRawMethodDrop(codeFromBuilders(t, tt.op)),
				[]any{cell.BeginCell().EndCell()},
				tuple.Tuple{},
				int32(vmerr.CodeStackUnderflow),
				0,
			)
		})
	}
}

func TestTVMCrossEmulatorStoreOptStdAddrQNonSliceVersionSemantics(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	code := codeFromBuilders(t,
		funcsop.STOPTSTDADDRQ().Serialize(),
		stackop.DROP().Serialize(),
		stackop.DROP().Serialize(),
		stackop.PUSHINT(big.NewInt(7)).Serialize(),
		mathop.EQUAL().Serialize(),
	)
	for _, version := range []int{12, 13, 14} {
		exit := int32(vmerr.CodeTypeCheck)
		if version >= 14 {
			exit = 0
		}

		t.Run("v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			runTonOpsEdgeVersionedParityCase(
				t,
				code,
				[]any{int64(7), cell.BeginCell()},
				tuple.Tuple{},
				version,
				exit,
				0,
			)
		})
	}
}

func TestTVMCrossEmulatorStoreOptStdAddrQLegacyNullSliceConsumers(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := []struct {
		name string
		code *cell.Cell
		exit int32
	}{
		{
			name: "slice use rejects null reference",
			code: codeFromBuilders(t,
				funcsop.STOPTSTDADDRQ().Serialize(),
				stackop.DROP().Serialize(),
				stackop.DROP().Serialize(),
				cellsliceop.SBITS().Serialize(),
			),
			exit: int32(vmerr.CodeTypeCheck),
		},
		{
			name: "duplicate preserves slice tag",
			code: codeFromBuilders(t,
				funcsop.STOPTSTDADDRQ().Serialize(),
				stackop.DROP().Serialize(),
				stackop.DROP().Serialize(),
				stackop.DUP().Serialize(),
				tupleop.ISNULL().Serialize(),
				stackop.NIP().Serialize(),
			),
		},
	}

	for _, version := range []int{12, 13} {
		for _, tt := range tests {
			t.Run("v"+big.NewInt(int64(version)).String()+"/"+tt.name, func(t *testing.T) {
				runTonOpsEdgeVersionedParityCase(
					t,
					tt.code,
					[]any{int64(7), cell.BeginCell()},
					tuple.Tuple{},
					version,
					tt.exit,
					0,
				)
			})
		}
	}
}
