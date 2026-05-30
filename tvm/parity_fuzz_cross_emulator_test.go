//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"strconv"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

const (
	defaultDifferentialFuzzSeeds = 128
	differentialFuzzGasLimit     = referenceDefaultMaxGas
)

type differentialFuzzCase struct {
	seed     uint64
	family   string
	op       string
	code     *cell.Cell
	stack    []any
	gasLimit int64
}

func TestTVMDifferentialFuzzSeeds(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	startInt := parityFuzzEnvInt(t, "TVM_PARITY_START", 0)
	if startInt < 0 {
		t.Fatal("TVM_PARITY_START must be non-negative")
	}
	start := uint64(startInt)
	seeds := parityFuzzEnvInt(t, "TVM_PARITY_SEEDS", defaultDifferentialFuzzSeeds)
	if seeds <= 0 {
		t.Skip("TVM_PARITY_SEEDS <= 0")
	}

	for i := 0; i < seeds; i++ {
		seed := start + uint64(i)
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			r := rand.New(rand.NewSource(int64(seed)))
			tc := generateDifferentialFuzzCase(t, r, seed)
			runDifferentialFuzzCase(t, tc)
		})
	}
}

func parityFuzzEnvInt(t *testing.T, key string, fallback int) int {
	t.Helper()

	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s must be an integer: %v", key, err)
	}
	return v
}

func generateDifferentialFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	switch r.Intn(4) {
	case 0:
		return generateDataSizeFuzzCase(t, r, seed)
	case 1:
		return generateSliceLoadFuzzCase(t, r, seed)
	case 2:
		return generateSlicePredicateFuzzCase(t, r, seed)
	default:
		return generateSliceStoreFuzzCase(t, r, seed)
	}
}

func runDifferentialFuzzCase(t *testing.T, tc differentialFuzzCase) {
	t.Helper()

	code := prependRawMethodDrop(tc.code)
	gasLimit := tc.gasLimit
	if gasLimit == 0 {
		gasLimit = differentialFuzzGasLimit
	}

	goStack, err := buildCrossStack(tc.stack...)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: failed to build go stack: %v", tc.seed, tc.family, tc.op, err)
	}
	refStack, err := buildCrossStack(tc.stack...)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: failed to build reference stack: %v", tc.seed, tc.family, tc.op, err)
	}

	goRes, err := runGoCrossCodeWithGas(code, testEmptyCell(), tuple.Tuple{}, goStack, gasLimit)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: go tvm execution failed: %v", tc.seed, tc.family, tc.op, err)
	}
	refRes, err := runReferenceCrossCodeWithGas(code, testEmptyCell(), tuple.Tuple{}, refStack, gasLimit)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: reference tvm execution failed: %v", tc.seed, tc.family, tc.op, err)
	}

	if goRes.exitCode != refRes.exitCode {
		t.Fatalf("seed=%d family=%s op=%s: exit code mismatch: go=%d reference=%d",
			tc.seed, tc.family, tc.op, goRes.exitCode, refRes.exitCode)
	}
	if goRes.gasUsed != refRes.gasUsed {
		t.Fatalf("seed=%d family=%s op=%s: gas mismatch: go=%d reference=%d",
			tc.seed, tc.family, tc.op, goRes.gasUsed, refRes.gasUsed)
	}

	goStackCell, err := normalizeStackCell(goRes.stack)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: failed to normalize go stack: %v", tc.seed, tc.family, tc.op, err)
	}
	refStackCell, err := normalizeStackCell(refRes.stack)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: failed to normalize reference stack: %v", tc.seed, tc.family, tc.op, err)
	}
	if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
		t.Fatalf("seed=%d family=%s op=%s: stack mismatch\ngo=%s\nreference=%s",
			tc.seed, tc.family, tc.op, goStackCell.Dump(), refStackCell.Dump())
	}
}

func generateDataSizeFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	ops := []struct {
		name     string
		builder  *cell.Builder
		sliceArg bool
		lowGasOK bool
	}{
		{"CDATASIZE", funcsop.CDATASIZE().Serialize(), false, true},
		{"CDATASIZEQ", funcsop.CDATASIZEQ().Serialize(), false, true},
		{"SDATASIZE", funcsop.SDATASIZE().Serialize(), true, false},
		{"SDATASIZEQ", funcsop.SDATASIZEQ().Serialize(), true, false},
	}
	op := ops[r.Intn(len(ops))]

	stack := []any{parityFuzzDataSizeArg(t, r, op.sliceArg), parityFuzzBound(t, r)}
	if r.Intn(8) == 0 {
		stack = stack[1:]
	}

	gasLimit := int64(0)
	if op.lowGasOK && r.Intn(24) == 0 {
		gasLimit = 120
	}

	return differentialFuzzCase{
		seed:     seed,
		family:   "datasize",
		op:       op.name,
		code:     codeFromBuilders(t, op.builder),
		stack:    stack,
		gasLimit: gasLimit,
	}
}

func generateSliceLoadFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	ops := []struct {
		name    string
		builder *cell.Builder
		width   bool
	}{
		{"LDIX", cellsliceop.LDIX().Serialize(), true},
		{"LDUX", cellsliceop.LDUX().Serialize(), true},
		{"PLDIX", cellsliceop.PLDIX().Serialize(), true},
		{"PLDUX", cellsliceop.PLDUX().Serialize(), true},
		{"LDIXQ", cellsliceop.LDIXQ().Serialize(), true},
		{"LDUXQ", cellsliceop.LDUXQ().Serialize(), true},
		{"LDSLICEX", cellsliceop.LDSLICEX().Serialize(), true},
		{"LDSLICEXQ", cellsliceop.LDSLICEXQ().Serialize(), true},
		{"PLDSLICEX", cellsliceop.PLDSLICEX().Serialize(), true},
		{"PLDSLICEXQ", cellsliceop.PLDSLICEXQ().Serialize(), true},
		{"LDI8", cellsliceop.LDI(8).Serialize(), false},
		{"LDU8", cellsliceop.LDU(8).Serialize(), false},
		{"PLDU8", cellsliceop.PLDU(8).Serialize(), false},
	}
	op := ops[r.Intn(len(ops))]

	var stack []any
	if op.width {
		stack = []any{parityFuzzMaybeSlice(t, r), parityFuzzWidth(r)}
		if r.Intn(10) == 0 {
			stack = stack[1:]
		}
	} else {
		stack = []any{parityFuzzMaybeSlice(t, r)}
		if r.Intn(10) == 0 {
			stack = nil
		}
	}

	return differentialFuzzCase{
		seed:   seed,
		family: "slice_load",
		op:     op.name,
		code:   codeFromBuilders(t, op.builder),
		stack:  stack,
	}
}

func generateSlicePredicateFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	unary := []struct {
		name    string
		builder *cell.Builder
	}{
		{"SEMPTY", cellsliceop.SEMPTY().Serialize()},
		{"SDEMPTY", cellsliceop.SDEMPTY().Serialize()},
		{"SREMPTY", cellsliceop.SREMPTY().Serialize()},
		{"SDFIRST", cellsliceop.SDFIRST().Serialize()},
		{"SDCNTLEAD0", cellsliceop.SDCNTLEAD0().Serialize()},
		{"SDCNTLEAD1", cellsliceop.SDCNTLEAD1().Serialize()},
		{"SDCNTTRAIL0", cellsliceop.SDCNTTRAIL0().Serialize()},
		{"SDCNTTRAIL1", cellsliceop.SDCNTTRAIL1().Serialize()},
		{"SBITS", cellsliceop.SBITS().Serialize()},
		{"SREFS", cellsliceop.SREFS().Serialize()},
	}
	binary := []struct {
		name    string
		builder *cell.Builder
	}{
		{"SDEQ", cellsliceop.SDEQ().Serialize()},
		{"SDLEXCMP", cellsliceop.SDLEXCMP().Serialize()},
		{"SDPFX", cellsliceop.SDPFX().Serialize()},
		{"SDPFXREV", cellsliceop.SDPFXREV().Serialize()},
		{"SDPPFX", cellsliceop.SDPPFX().Serialize()},
		{"SDPPFXREV", cellsliceop.SDPPFXREV().Serialize()},
		{"SDSFX", cellsliceop.SDSFX().Serialize()},
		{"SDSFXREV", cellsliceop.SDSFXREV().Serialize()},
		{"SDPSFX", cellsliceop.SDPSFX().Serialize()},
		{"SDPSFXREV", cellsliceop.SDPSFXREV().Serialize()},
		{"SDBEGINSX", cellsliceop.SDBEGINSX().Serialize()},
		{"SDBEGINSXQ", cellsliceop.SDBEGINSXQ().Serialize()},
	}

	if r.Intn(2) == 0 {
		op := unary[r.Intn(len(unary))]
		stack := []any{parityFuzzMaybeSlice(t, r)}
		if r.Intn(10) == 0 {
			stack = nil
		}
		return differentialFuzzCase{seed: seed, family: "slice_predicate", op: op.name, code: codeFromBuilders(t, op.builder), stack: stack}
	}

	op := binary[r.Intn(len(binary))]
	stack := []any{parityFuzzMaybeSlice(t, r), parityFuzzMaybeSlice(t, r)}
	if r.Intn(10) == 0 {
		stack = stack[:1]
	}
	return differentialFuzzCase{seed: seed, family: "slice_predicate", op: op.name, code: codeFromBuilders(t, op.builder), stack: stack}
}

func generateSliceStoreFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	ops := []struct {
		name    string
		builder *cell.Builder
		stack   func(*testing.T, *rand.Rand) []any
	}{
		{"STU8", cellsliceop.STU(8).Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzStoreInt(r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"STI8", cellsliceop.STI(8).Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzStoreInt(r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"STREF", cellsliceop.STREF().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeCell(t, r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"STREFQ", cellsliceop.STREFQ().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeCell(t, r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"STSLICE", cellsliceop.STSLICE().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeSlice(t, r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"STSLICEQ", cellsliceop.STSLICEQ().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeSlice(t, r), parityFuzzMaybeBuilder(t, r)}
		}},
		{"BCHKBITS", cellsliceop.BCHKBITS().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeBuilder(t, r), parityFuzzWidth(r)}
		}},
		{"BCHKBITSQ", cellsliceop.BCHKBITSQ().Serialize(), func(t *testing.T, r *rand.Rand) []any {
			return []any{parityFuzzMaybeBuilder(t, r), parityFuzzWidth(r)}
		}},
	}
	op := ops[r.Intn(len(ops))]
	stack := op.stack(t, r)
	if r.Intn(10) == 0 {
		stack = stack[:1]
	}

	return differentialFuzzCase{
		seed:   seed,
		family: "slice_store",
		op:     op.name,
		code:   codeFromBuilders(t, op.builder),
		stack:  stack,
	}
}

func parityFuzzDataSizeArg(t *testing.T, r *rand.Rand, sliceArg bool) any {
	t.Helper()

	if sliceArg {
		if r.Intn(4) == 0 {
			return parityFuzzWrongValue(t, r)
		}
		return parityFuzzSlice(t, r)
	}
	if r.Intn(5) == 0 {
		return parityFuzzWrongValue(t, r)
	}
	if r.Intn(5) == 0 {
		return nil
	}
	return parityFuzzGraphCell(t, r)
}

func parityFuzzBound(t *testing.T, r *rand.Rand) any {
	t.Helper()

	switch r.Intn(10) {
	case 0:
		return vm.NaN{}
	case 1:
		return int64(-1)
	case 2:
		return new(big.Int).Lsh(big.NewInt(1), 200)
	case 3:
		return parityFuzzWrongValue(t, r)
	default:
		return int64([]int{0, 1, 2, 3, 10}[r.Intn(5)])
	}
}

func parityFuzzWidth(r *rand.Rand) any {
	switch r.Intn(12) {
	case 0:
		return vm.NaN{}
	case 1:
		return int64(-1)
	case 2:
		return int64(257)
	case 3:
		return int64(258)
	case 4:
		return int64(1024)
	default:
		return int64([]int{0, 1, 7, 8, 16, 255, 256}[r.Intn(7)])
	}
}

func parityFuzzStoreInt(r *rand.Rand) any {
	switch r.Intn(9) {
	case 0:
		return vm.NaN{}
	case 1:
		return int64(-129)
	case 2:
		return int64(-1)
	case 3:
		return int64(0)
	case 4:
		return int64(1)
	case 5:
		return int64(255)
	case 6:
		return int64(256)
	default:
		return big.NewInt(int64(r.Intn(256)))
	}
}

func parityFuzzMaybeCell(t *testing.T, r *rand.Rand) any {
	t.Helper()

	if r.Intn(5) == 0 {
		return parityFuzzWrongValue(t, r)
	}
	return parityFuzzGraphCell(t, r)
}

func parityFuzzMaybeSlice(t *testing.T, r *rand.Rand) any {
	t.Helper()

	if r.Intn(5) == 0 {
		return parityFuzzWrongValue(t, r)
	}
	return parityFuzzSlice(t, r)
}

func parityFuzzMaybeBuilder(t *testing.T, r *rand.Rand) any {
	t.Helper()

	if r.Intn(5) == 0 {
		return parityFuzzWrongValue(t, r)
	}
	return matrixBuilder(t, uint([]int{0, 1, 8, 1016, 1023}[r.Intn(5)]), r.Intn(5))
}

func parityFuzzWrongValue(t *testing.T, r *rand.Rand) any {
	t.Helper()

	switch r.Intn(5) {
	case 0:
		return int64(777)
	case 1:
		return parityFuzzSlice(t, r)
	case 2:
		return parityFuzzGraphCell(t, r)
	case 3:
		return cell.BeginCell()
	default:
		return nil
	}
}

func parityFuzzSlice(t *testing.T, r *rand.Rand) *cell.Slice {
	t.Helper()

	return parityFuzzGraphCell(t, r).MustBeginParse()
}

func parityFuzzGraphCell(t *testing.T, r *rand.Rand) *cell.Cell {
	t.Helper()

	bits := uint([]int{0, 1, 7, 8, 16, 255, 512, 1023}[r.Intn(8)])
	refs := r.Intn(5)
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreSlice(matrixPattern(bits), bits)
	}

	if refs == 0 {
		return b.EndCell()
	}

	shared := cell.BeginCell().MustStoreUInt(uint64(r.Intn(256)), 8).EndCell()
	for i := 0; i < refs; i++ {
		if i%2 == 0 {
			b.MustStoreRef(shared)
		} else {
			b.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(i), 8).EndCell())
		}
	}
	return b.EndCell()
}
