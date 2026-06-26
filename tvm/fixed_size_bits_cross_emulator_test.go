//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestTVMCrossEmulatorFixedSizeBitsOps(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	leafTuple := tuplepkg.NewTupleValue(big.NewInt(2), big.NewInt(3))
	midTuple := tuplepkg.NewTupleValue(big.NewInt(1), leafTuple)
	nestedTuple := tuplepkg.NewTupleValue(midTuple)

	type testCase struct {
		name         string
		code         *cell.Cell
		stack        []any
		exit         int32
		noStackCheck bool
	}

	tests := []testCase{
		{
			name: "tuple_make",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(11)).Serialize(),
				stackop.PUSHINT(big.NewInt(22)).Serialize(),
				tupleop.TUPLE(2).Serialize(),
			),
			exit: 0,
		},
		{
			name:  "index3_nested",
			code:  codeFromBuilders(t, tupleop.INDEX3(0, 1, 0).Serialize()),
			stack: []any{nestedTuple},
			exit:  0,
		},
		{
			name:  "indexq_out_of_range",
			code:  codeFromBuilders(t, tupleop.INDEXQ(2).Serialize()),
			stack: []any{tuplepkg.NewTupleValue(big.NewInt(11))},
			exit:  0,
		},
		{
			name:  "qtlen_non_tuple",
			code:  codeFromBuilders(t, tupleop.QTLEN().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
		},
		{
			name:  "istuple_non_tuple",
			code:  codeFromBuilders(t, tupleop.ISTUPLE().Serialize()),
			stack: []any{int64(7)},
			exit:  0,
		},
		{
			name: "jmpxargs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, stackop.PUSHINT(big.NewInt(7)).Serialize())).Serialize(),
				execop.JMPXARGS(0).Serialize(),
			),
			exit: 0,
		},
		{
			name: "throwany",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(44)).Serialize(),
				cell.BeginCell().MustStoreUInt(0xF2F0, 16),
			),
			exit:         44,
			noStackCheck: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := prependRawMethodDrop(tt.code)

			goStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack(tt.stack...)
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCode(code, cell.BeginCell().EndCell(), tuplepkg.Tuple{}, goStack)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}

			refRes, err := runReferenceCrossCode(code, cell.BeginCell().EndCell(), tuplepkg.Tuple{}, refStack)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
				t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tt.exit)
			}
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}

			if tt.noStackCheck {
				return
			}

			goStackCell, err := normalizeStackCell(goRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize go stack: %v", err)
			}
			refStackCell, err := normalizeStackCell(refRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize reference stack: %v", err)
			}
			if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
			}
		})
	}
}

func TestTVMCrossEmulatorFixedSizeBitsAllGlobalVersionsSmoke(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	tests := fixedSizeBitsVersionedCases(t, 7)
	versions := crossEmulatorVersionAuditVersions(t, "TVM_FIXED_SIZE_BITS_VERSION_AUDIT")
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			for _, version := range versions {
				version := version
				t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
					runFixedSizeBitsVersionedCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorFixedSizeBitsGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%fixedSizeBitsVersionedCaseCount), uint16(7+version))
	}
	for i := 0; i < fixedSizeBitsVersionedCaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i), uint16(0x20+i))
	}
	f.Add(uint8(255), uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawCase uint8, rawValue uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		tests := fixedSizeBitsVersionedCases(t, rawValue)
		runFixedSizeBitsVersionedCase(t, tests[int(rawCase)%len(tests)], version)
	})
}

type fixedSizeBitsVersionedCase struct {
	name         string
	code         *cell.Cell
	stack        []any
	exit         int32
	noStackCheck bool
}

const fixedSizeBitsVersionedCaseCount = 27

func fixedSizeBitsVersionedCases(t *testing.T, value uint16) []fixedSizeBitsVersionedCase {
	t.Helper()

	stackValue := int64(value)
	negValue := -int64(value&0x7f) - 1
	leafTuple := tuplepkg.NewTupleValue(big.NewInt(stackValue+2), big.NewInt(stackValue+3))
	midTuple := tuplepkg.NewTupleValue(big.NewInt(stackValue+1), leafTuple)
	nestedTuple := tuplepkg.NewTupleValue(midTuple)
	tripleTuple := tuplepkg.NewTupleValue(big.NewInt(stackValue+1), big.NewInt(stackValue+2), big.NewInt(stackValue+3))
	pairTuple := tuplepkg.NewTupleValue(big.NewInt(stackValue+4), big.NewInt(stackValue+5))
	outerTuple := tuplepkg.NewTupleValue(big.NewInt(stackValue), pairTuple)
	contBody := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(stackValue+77)).Serialize())
	blessCode := codeFromBuilders(t, stackop.PUSHINT(big.NewInt(stackValue+88)).Serialize()).MustBeginParse()
	throwExit := int32(1 + int(value)%0x400)

	return []fixedSizeBitsVersionedCase{
		{
			name: "tuple_make",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(stackValue+11)).Serialize(),
				stackop.PUSHINT(big.NewInt(stackValue+22)).Serialize(),
				tupleop.TUPLE(2).Serialize(),
			),
		},
		{
			name:  "index3_nested",
			code:  codeFromBuilders(t, tupleop.INDEX3(0, 1, 0).Serialize()),
			stack: []any{nestedTuple},
		},
		{
			name:  "indexq_out_of_range",
			code:  codeFromBuilders(t, tupleop.INDEXQ(2).Serialize()),
			stack: []any{tuplepkg.NewTupleValue(big.NewInt(stackValue + 11))},
		},
		{
			name:  "qtlen_non_tuple",
			code:  codeFromBuilders(t, tupleop.QTLEN().Serialize()),
			stack: []any{stackValue},
		},
		{
			name:  "istuple_non_tuple",
			code:  codeFromBuilders(t, tupleop.ISTUPLE().Serialize()),
			stack: []any{stackValue},
		},
		{
			name: "jmpxargs",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, stackop.PUSHINT(big.NewInt(stackValue+7)).Serialize())).Serialize(),
				execop.JMPXARGS(0).Serialize(),
			),
		},
		{
			name: "throwany",
			code: codeFromBuilders(t,
				stackop.PUSHINT(big.NewInt(int64(throwExit))).Serialize(),
				cell.BeginCell().MustStoreUInt(0xF2F0, 16),
			),
			exit:         throwExit,
			noStackCheck: true,
		},
		{
			name:  "untuple_pair",
			code:  codeFromBuilders(t, tupleop.UNTUPLE(2).Serialize()),
			stack: []any{pairTuple},
		},
		{
			name:  "unpackfirst_one_from_triple",
			code:  codeFromBuilders(t, tupleop.UNPACKFIRST(1).Serialize()),
			stack: []any{tripleTuple},
		},
		{
			name:  "explode_triple",
			code:  codeFromBuilders(t, tupleop.EXPLODE(3).Serialize()),
			stack: []any{tripleTuple},
		},
		{
			name:  "index_direct",
			code:  codeFromBuilders(t, tupleop.INDEX(1).Serialize()),
			stack: []any{tripleTuple},
		},
		{
			name:  "index2_nested",
			code:  codeFromBuilders(t, tupleop.INDEX2(1, 0).Serialize()),
			stack: []any{outerTuple},
		},
		{
			name:  "setindex_middle",
			code:  codeFromBuilders(t, tupleop.SETINDEX(1).Serialize()),
			stack: []any{tripleTuple, stackValue + 99},
		},
		{
			name:  "setindexq_extends_tuple",
			code:  codeFromBuilders(t, tupleop.SETINDEXQ(3).Serialize()),
			stack: []any{pairTuple, stackValue + 100},
		},
		{
			name:  "blkdrop_two",
			code:  codeFromBuilders(t, stackop.BLKDROP(2).Serialize()),
			stack: []any{stackValue, stackValue + 1, stackValue + 2, stackValue + 3},
		},
		{
			name:  "blkdrop2_split",
			code:  codeFromBuilders(t, stackop.BLKDROP2(1, 2).Serialize()),
			stack: []any{stackValue, stackValue + 1, stackValue + 2, stackValue + 3, stackValue + 4},
		},
		{
			name:  "blkpush_repeat",
			code:  codeFromBuilders(t, stackop.BLKPUSH(2, 1).Serialize()),
			stack: []any{stackValue, stackValue + 1, stackValue + 2},
		},
		{
			name:  "blkswap_blocks",
			code:  codeFromBuilders(t, stackop.BLKSWAP(2, 1).Serialize()),
			stack: []any{stackValue, stackValue + 1, stackValue + 2, stackValue + 3},
		},
		{
			name:  "reverse_window",
			code:  codeFromBuilders(t, stackop.REVERSE(3, 1).Serialize()),
			stack: []any{stackValue, stackValue + 1, stackValue + 2, stackValue + 3, stackValue + 4},
		},
		{
			name:  "xcpu_suffix",
			code:  codeFromBuilders(t, stackop.XCPU(2, 1).Serialize()),
			stack: []any{stackValue, stackValue + 1, stackValue + 2, stackValue + 3},
		},
		{
			name:  "push2_suffix",
			code:  codeFromBuilders(t, stackop.PUSH2(1, 2).Serialize()),
			stack: []any{stackValue, stackValue + 1, stackValue + 2, stackValue + 3, stackValue + 4},
		},
		{
			name:  "puxc_suffix",
			code:  codeFromBuilders(t, stackop.PUXC(2, 1).Serialize()),
			stack: []any{stackValue, stackValue + 1, stackValue + 2, stackValue + 3},
		},
		{
			name:  "addint_negative_suffix",
			code:  codeFromBuilders(t, mathop.ADDINT(int8(negValue)).Serialize()),
			stack: []any{stackValue + 200},
		},
		{
			name:  "mulint_negative_suffix",
			code:  codeFromBuilders(t, mathop.MULINT(int8(negValue)).Serialize()),
			stack: []any{stackValue + 3},
		},
		{
			name:  "eqint_suffix",
			code:  codeFromBuilders(t, mathop.EQINT(int8(value)).Serialize()),
			stack: []any{int64(int8(value))},
		},
		{
			name:  "lessint_suffix",
			code:  codeFromBuilders(t, mathop.LESSINT(int8(value)).Serialize()),
			stack: []any{int64(int8(value)) - 1},
		},
		{
			name: "callxargsp_suffix",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(contBody).Serialize(),
				execop.CALLXARGSP(0).Serialize(),
			),
		},
		{
			name: "tryargs_suffix_success",
			code: codeFromBuilders(t,
				stackop.PUSHCONT(codeFromBuilders(t, mathop.INC().Serialize(), execop.RETARGS(1).Serialize())).Serialize(),
				stackop.PUSHCONT(codeFromBuilders(t, stackop.PUSHINT(big.NewInt(stackValue+55)).Serialize())).Serialize(),
				execop.TRYARGS(1, 1).Serialize(),
			),
			stack: []any{stackValue},
		},
		{
			name: "blessargs_suffix_executes",
			code: codeFromBuilders(t,
				execop.BLESSARGS(0, -1).Serialize(),
				execop.EXECUTE().Serialize(),
			),
			stack: []any{blessCode},
		},
		{
			name: "returnargs_underflow_suffix",
			code: codeFromBuilders(t,
				execop.RETURNARGS(3).Serialize(),
			),
			stack: []any{stackValue},
			exit:  vmerr.CodeStackUnderflow,
		},
	}
}

func runFixedSizeBitsVersionedCase(t *testing.T, tt fixedSizeBitsVersionedCase, globalVersion int) {
	t.Helper()

	code := prependRawMethodDrop(tt.code)
	goStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack(tt.stack...)
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}

	goRes, err := runGoCrossCodeWithVersion(code, cell.BeginCell().EndCell(), tuplepkg.Tuple{}, goStack, globalVersion)
	if err != nil {
		t.Fatalf("go tvm execution failed: %v", err)
	}

	refCfg := tonopsCrossRefConfig(tonopsCrossConfigWithGlobalVersion(t, uint32(globalVersion)))
	refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, *refCfg)
	if err != nil {
		t.Fatalf("reference tvm execution failed: %v", err)
	}

	if goRes.exitCode != tt.exit || refRes.exitCode != tt.exit {
		t.Fatalf("unexpected exit code: go=%d reference=%d expected=%d", goRes.exitCode, refRes.exitCode, tt.exit)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
	}

	if tt.noStackCheck {
		return
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("failed to normalize reference stack: %v", err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
	}
}
