//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	ophelpers "github.com/xssnick/tonutils-go/tvm/op/helpers"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

const (
	defaultDifferentialFuzzSeeds  = 128
	defaultParityProgramOps       = 24
	differentialFuzzGasLimit      = referenceDefaultMaxGas
	parityProgramChunkReserveBits = 32
	maxSmallIndexForParityProgram = (1 << 30) - 1
)

type differentialFuzzCase struct {
	seed     uint64
	family   string
	op       string
	code     *cell.Cell
	stack    []any
	gasLimit int64
	c7       tuple.Tuple
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

	switch r.Intn(8) {
	case 0:
		return generateDataSizeFuzzCase(t, r, seed)
	case 1:
		return generateSliceLoadFuzzCase(t, r, seed)
	case 2:
		return generateSlicePredicateFuzzCase(t, r, seed)
	case 3:
		return generateSliceStoreFuzzCase(t, r, seed)
	default:
		return generateProgramFuzzCase(t, r, seed)
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

	c7 := tc.c7
	if c7.IsNull() {
		c7 = tuple.Tuple{}
	}

	goRes, err := runGoCrossCodeWithGas(code, testEmptyCell(), c7, goStack, gasLimit)
	if err != nil {
		t.Fatalf("seed=%d family=%s op=%s: go tvm execution failed: %v", tc.seed, tc.family, tc.op, err)
	}
	refRes, err := runReferenceCrossCodeWithGas(code, testEmptyCell(), c7, refStack, gasLimit)
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

type parityProgramStackKind uint8

const (
	parityProgramInt parityProgramStackKind = iota
	parityProgramNaN
	parityProgramNull
	parityProgramTuple
	parityProgramCell
	parityProgramSlice
	parityProgramBuilder
)

type parityProgramStackValue struct {
	kind    parityProgramStackKind
	int     *big.Int
	tuple   []parityProgramStackValue
	cell    *cell.Cell
	slice   *cell.Slice
	builder *cell.Builder
}

type parityProgramGenerator struct {
	r       *rand.Rand
	stack   []parityProgramStackValue
	initial []parityProgramStackValue
	ops     []*cell.Builder
	trace   []string
	seed    []byte
	regD    [2]*cell.Cell
}

func generateProgramFuzzCase(t *testing.T, r *rand.Rand, seed uint64) differentialFuzzCase {
	t.Helper()

	steps := parityFuzzEnvInt(t, "TVM_PARITY_PROGRAM_OPS", defaultParityProgramOps)
	if steps <= 0 {
		steps = defaultParityProgramOps
	}

	empty := testEmptyCell()
	g := &parityProgramGenerator{
		r:    r,
		seed: append([]byte(nil), crossTestSeed...),
		regD: [2]*cell.Cell{
			empty,
			empty,
		},
	}
	g.seedInitialStack()
	for i := 0; i < steps; i++ {
		if !g.emitRandomOp() {
			g.emitPushValueOp()
		}
	}
	if len(g.stack) == 0 {
		g.emitPushValueOp()
	}

	return differentialFuzzCase{
		seed:   seed,
		family: "program",
		op:     strings.Join(g.trace, " -> "),
		code:   parityProgramCodeFromBuilders(t, g.ops...),
		stack:  parityProgramHostStack(g.initial),
		c7:     prepareCrossTestC7(nil, testEmptyCell()),
	}
}

func parityProgramCodeFromBuilders(t *testing.T, builders ...*cell.Builder) *cell.Cell {
	t.Helper()

	var chunks [][]*cell.Builder
	var chunk []*cell.Builder
	bits := uint(0)
	refs := 0
	flush := func() {
		if len(chunk) == 0 {
			return
		}
		chunks = append(chunks, chunk)
		chunk = nil
		bits = 0
		refs = 0
	}

	for _, builder := range builders {
		needBits := builder.BitsUsed()
		needRefs := builder.RefsUsed()
		if len(chunk) > 0 && (bits+needBits+parityProgramChunkReserveBits >= 1024 || refs+needRefs+1 > 4) {
			flush()
		}
		chunk = append(chunk, builder)
		bits += needBits
		refs += needRefs
	}
	flush()

	var next *cell.Cell
	for i := len(chunks) - 1; i >= 0; i-- {
		builder := cell.BeginCell()
		for _, instr := range chunks[i] {
			builder.MustStoreBuilder(instr)
		}
		if next != nil {
			builder.MustStoreBuilder(execop.JMPREF(next).Serialize())
		}
		next = builder.EndCell()
	}
	if next == nil {
		return cell.BeginCell().EndCell()
	}
	return next
}

func (g *parityProgramGenerator) seedInitialStack() {
	depth := 2 + g.r.Intn(5)
	g.stack = make([]parityProgramStackValue, 0, depth)
	for i := 0; i < depth; i++ {
		g.stack = append(g.stack, g.randomInitialValue(0))
	}
	g.initial = parityProgramCloneStack(g.stack)
}

func (g *parityProgramGenerator) randomInitialValue(depth int) parityProgramStackValue {
	if depth < 2 && g.r.Intn(5) == 0 {
		ln := g.r.Intn(4)
		items := make([]parityProgramStackValue, ln)
		for i := range items {
			items[i] = g.randomInitialValue(depth + 1)
		}
		return parityProgramStackValue{kind: parityProgramTuple, tuple: items}
	}
	switch g.r.Intn(12) {
	case 0:
		return parityProgramStackValue{kind: parityProgramNull}
	case 1:
		return parityProgramStackValue{kind: parityProgramCell, cell: g.randomCell()}
	case 2:
		return parityProgramStackValue{kind: parityProgramSlice, slice: g.randomCell().MustBeginParse()}
	case 3:
		return parityProgramStackValue{kind: parityProgramBuilder, builder: g.randomBuilder()}
	default:
		return parityProgramIntValue(g.smallInt())
	}
}

func (g *parityProgramGenerator) emitRandomOp() bool {
	for i := 0; i < 32; i++ {
		switch g.r.Intn(32) {
		case 0, 1:
			return g.emitPushValueOp()
		case 2, 3, 4, 5:
			if g.emitStackOp() {
				return true
			}
		case 6, 7, 8, 9:
			if g.emitMathOp() {
				return true
			}
		case 10, 11, 12:
			if g.emitCellSliceOp() {
				return true
			}
		case 13, 14, 15:
			if g.emitDictOp() {
				return true
			}
		case 16, 17:
			if g.emitControlOp() {
				return true
			}
		case 18, 19:
			if g.emitFuncParamOp() {
				return true
			}
		case 20, 21:
			if g.emitMessageAddressOp() {
				return true
			}
		case 22, 23, 24:
			if g.emitRuntimeControlOp() {
				return true
			}
		default:
			if g.emitTupleOp() {
				return true
			}
		}
	}
	return false
}

func (g *parityProgramGenerator) emitCellSliceOp() bool {
	switch g.r.Intn(24) {
	case 0:
		g.emit("NEWC", cellsliceop.NEWC().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: cell.BeginCell()})
	case 1:
		if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramBuilder {
			return false
		}
		g.emit("ENDC", cellsliceop.ENDC().Serialize())
		g.stack[len(g.stack)-1] = parityProgramStackValue{kind: parityProgramCell, cell: g.stack[len(g.stack)-1].builder.Copy().EndCell()}
	case 2:
		if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramCell {
			return false
		}
		g.emit("CTOS", cellsliceop.CTOS().Serialize())
		g.stack[len(g.stack)-1] = parityProgramStackValue{kind: parityProgramSlice, slice: g.stack[len(g.stack)-1].cell.MustBeginParse()}
	case 3:
		if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramSlice {
			return false
		}
		sl := g.stack[len(g.stack)-1].slice
		if sl.BitsLeft() != 0 || sl.RefsNum() != 0 {
			return false
		}
		g.emit("ENDS", cellsliceop.ENDS().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
	case 4:
		if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramCell {
			return false
		}
		g.emit("HASHCU", cellsliceop.HASHCU().Serialize())
		hash := new(big.Int).SetBytes(g.stack[len(g.stack)-1].cell.Hash())
		g.stack[len(g.stack)-1] = parityProgramIntValue(hash)
	case 5:
		if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramSlice {
			return false
		}
		g.emit("HASHSU", cellsliceop.HASHSU().Serialize())
		hash := new(big.Int).SetBytes(g.stack[len(g.stack)-1].slice.ToBuilder().EndCell().Hash())
		g.stack[len(g.stack)-1] = parityProgramIntValue(hash)
	case 6:
		return g.emitLoadRefOp()
	case 7:
		return g.emitLoadIntOp(false)
	case 8:
		return g.emitLoadIntOp(true)
	case 9:
		return g.emitStoreIntOp(false)
	case 10:
		return g.emitStoreIntOp(true)
	case 11:
		return g.emitStoreRefOp()
	case 12:
		return g.emitStoreSliceOp()
	case 13:
		return g.emitBuilderMetaOp("BBITS", cellsliceop.BBITS().Serialize(), func(b *cell.Builder) int64 {
			return int64(b.BitsUsed())
		})
	case 14:
		return g.emitBuilderMetaOp("BREFS", cellsliceop.BREFS().Serialize(), func(b *cell.Builder) int64 {
			return int64(b.RefsUsed())
		})
	case 15:
		return g.emitBuilderMetaOp("BDEPTH", cellsliceop.BDEPTH().Serialize(), func(b *cell.Builder) int64 {
			return int64(b.Depth())
		})
	default:
		return g.emitCellSliceMetaOp()
	}
	return true
}

func (g *parityProgramGenerator) emitCellSliceMetaOp() bool {
	base := len(g.stack)
	root := parityProgramStorageStatCell()

	switch g.r.Intn(36) {
	case 0:
		sl := root.MustBeginParse()
		g.emitPushSlice("sbits_src", sl)
		g.emit("SBITS", cellsliceop.SBITS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(sl.BitsLeft()))))
	case 1:
		sl := root.MustBeginParse()
		g.emitPushSlice("srefs_src", sl)
		g.emit("SREFS", cellsliceop.SREFS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(sl.RefsNum()))))
	case 2:
		sl := root.MustBeginParse()
		g.emitPushSlice("sbitrefs_src", sl)
		g.emit("SBITREFS", cellsliceop.SBITREFS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(sl.BitsLeft()))), parityProgramIntValue(big.NewInt(int64(sl.RefsNum()))))
	case 3:
		sl := root.MustBeginParse()
		g.emitPushSlice("sdepth_src", sl)
		g.emit("SDEPTH", cellsliceop.SDEPTH().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(sl.Depth()))))
	case 4:
		g.emitPushCell("cdepth_src", root)
		g.emit("CDEPTH", cellsliceop.CDEPTH().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(root.Depth()))))
	case 5:
		g.emitPushNull("cdepth_nil")
		g.emit("CDEPTH(nil)", cellsliceop.CDEPTH().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0)))
	case 6:
		g.emitPushCell("clevel_src", root)
		g.emit("CLEVEL", cellsliceop.CLEVEL().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(root.Level()))))
	case 7:
		g.emitPushCell("clevelmask_src", root)
		g.emit("CLEVELMASK", cellsliceop.CLEVELMASK().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(root.LevelMask().Mask))))
	case 8:
		g.emitPushCell("chashi_src", root)
		g.emit("CHASHI(0)", cellsliceop.CHASHI(0).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(root.Hash(0))))
	case 9:
		g.emitPushCell("cdepthi_src", root)
		g.emit("CDEPTHI(0)", cellsliceop.CDEPTHI(0).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(root.Depth(0)))))
	case 10:
		g.emitPushCell("chashix_src", root)
		g.emitPushInt("chashix_idx", big.NewInt(0))
		g.emit("CHASHIX", cellsliceop.CHASHIX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(root.Hash(0))))
	case 11:
		g.emitPushCell("cdepthix_src", root)
		g.emitPushInt("cdepthix_idx", big.NewInt(0))
		g.emit("CDEPTHIX", cellsliceop.CDEPTHIX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(root.Depth(0)))))
	case 12:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("bbitrefs_src", builder)
		g.emit("BBITREFS", cellsliceop.BBITREFS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(builder.BitsUsed()))), parityProgramIntValue(big.NewInt(int64(builder.RefsUsed()))))
	case 13:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("brembits_src", builder)
		g.emit("BREMBITS", cellsliceop.BREMBITS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(builder.BitsLeft()))))
	case 14:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("bremrefs_src", builder)
		g.emit("BREMREFS", cellsliceop.BREMREFS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(builder.RefsLeft()))))
	case 15:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("brembitrefs_src", builder)
		g.emit("BREMBITREFS", cellsliceop.BREMBITREFS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(builder.BitsLeft()))), parityProgramIntValue(big.NewInt(int64(builder.RefsLeft()))))
	case 16:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("bchkbitsimm_src", builder)
		g.emit("BCHKBITSIMM(8)", cellsliceop.BCHKBITSIMM(8, false).Serialize())
		g.stack = g.stack[:base]
	case 17:
		builder := parityProgramMetaBuilder()
		g.emitPushBuilder("bchkbitrefsq_src", builder)
		g.emitPushInt("bchkbitrefsq_bits", big.NewInt(8))
		g.emitPushInt("bchkbitrefsq_refs", big.NewInt(1))
		g.emit("BCHKBITREFSQ", cellsliceop.BCHKBITREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	default:
		switch g.r.Intn(6) {
		case 0:
			return g.emitCellSlicePredicateOp(g.r.Intn(18))
		case 1:
			return g.emitCellSliceTransformOp(g.r.Intn(31))
		case 2:
			return g.emitCellSliceLoadFamilyOp(g.r.Intn(28))
		case 3:
			return g.emitCellSliceLittleEndianOp(g.r.Intn(16))
		default:
			if g.r.Intn(3) == 0 {
				return g.emitCellSlicePrefixGramsOp(g.r.Intn(8))
			}
			return g.emitCellSliceBuilderAdvancedOp(g.r.Intn(34))
		}
	}
	return true
}

func (g *parityProgramGenerator) emitCellSlicePredicateOp(mode int) bool {
	base := len(g.stack)
	empty := cell.BeginCell().ToSlice()
	refOnly := cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).ToSlice()
	full := parityProgramBitsSlice(0b10110, 5)
	fullAlt := parityProgramBitsSlice(0b10111, 5)
	prefix := parityProgramBitsSlice(0b101, 3)
	suffix := parityProgramBitsSlice(0b110, 3)

	switch mode {
	case 0:
		g.emitPushSlice("sempty_src", empty)
		g.emit("SEMPTY", cellsliceop.SEMPTY().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 1:
		g.emitPushSlice("sdempty_src", refOnly)
		g.emit("SDEMPTY", cellsliceop.SDEMPTY().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 2:
		g.emitPushSlice("srempty_src", full)
		g.emit("SREMPTY", cellsliceop.SREMPTY().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 3:
		g.emitPushSlice("sdfirst_src", full)
		g.emit("SDFIRST", cellsliceop.SDFIRST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 4:
		src := parityProgramBitsSlice(0b00101, 5)
		g.emitPushSlice("sdcntlead0_src", src)
		g.emit("SDCNTLEAD0", cellsliceop.SDCNTLEAD0().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(src.CountLeading(false)))))
	case 5:
		src := parityProgramBitsSlice(0b11101, 5)
		g.emitPushSlice("sdcntlead1_src", src)
		g.emit("SDCNTLEAD1", cellsliceop.SDCNTLEAD1().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(src.CountLeading(true)))))
	case 6:
		src := parityProgramBitsSlice(0b10100, 5)
		g.emitPushSlice("sdcnttrail0_src", src)
		g.emit("SDCNTTRAIL0", cellsliceop.SDCNTTRAIL0().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(src.CountTrailing(false)))))
	case 7:
		src := parityProgramBitsSlice(0b10111, 5)
		g.emitPushSlice("sdcnttrail1_src", src)
		g.emit("SDCNTTRAIL1", cellsliceop.SDCNTTRAIL1().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(src.CountTrailing(true)))))
	case 8:
		g.emitPushSlice("sdeq_a", full)
		g.emitPushSlice("sdeq_b", parityProgramBitsSlice(0b10110, 5))
		g.emit("SDEQ", cellsliceop.SDEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 9:
		g.emitPushSlice("sdlexcmp_a", full)
		g.emitPushSlice("sdlexcmp_b", fullAlt)
		g.emit("SDLEXCMP", cellsliceop.SDLEXCMP().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(full.LexCompare(fullAlt)))))
	case 10:
		g.emitPushSlice("sdpfx_prefix", prefix)
		g.emitPushSlice("sdpfx_full", full)
		g.emit("SDPFX", cellsliceop.SDPFX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(prefix.IsPrefixOf(full)))
	case 11:
		g.emitPushSlice("sdpfxrev_full", full)
		g.emitPushSlice("sdpfxrev_prefix", prefix)
		g.emit("SDPFXREV", cellsliceop.SDPFXREV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(prefix.IsPrefixOf(full)))
	case 12:
		g.emitPushSlice("sdppfx_prefix", prefix)
		g.emitPushSlice("sdppfx_full", full)
		g.emit("SDPPFX", cellsliceop.SDPPFX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(prefix.IsProperPrefixOf(full)))
	case 13:
		g.emitPushSlice("sdppfxrev_full", full)
		g.emitPushSlice("sdppfxrev_prefix", prefix)
		g.emit("SDPPFXREV", cellsliceop.SDPPFXREV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(prefix.IsProperPrefixOf(full)))
	case 14:
		g.emitPushSlice("sdsfx_suffix", suffix)
		g.emitPushSlice("sdsfx_full", full)
		g.emit("SDSFX", cellsliceop.SDSFX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(suffix.IsSuffixOf(full)))
	case 15:
		g.emitPushSlice("sdsfxrev_full", full)
		g.emitPushSlice("sdsfxrev_suffix", suffix)
		g.emit("SDSFXREV", cellsliceop.SDSFXREV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(suffix.IsSuffixOf(full)))
	case 16:
		g.emitPushSlice("sdpsfx_suffix", suffix)
		g.emitPushSlice("sdpsfx_full", full)
		g.emit("SDPSFX", cellsliceop.SDPSFX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(suffix.IsProperSuffixOf(full)))
	default:
		g.emitPushSlice("sdpsfxrev_full", full)
		g.emitPushSlice("sdpsfxrev_suffix", suffix)
		g.emit("SDPSFXREV", cellsliceop.SDPSFXREV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(suffix.IsProperSuffixOf(full)))
	}
	return true
}

func (g *parityProgramGenerator) emitCellSliceTransformOp(mode int) bool {
	base := len(g.stack)
	bitsOnly := parityProgramBitsSlice(0b101101, 6)
	refSrc, ref0, ref1 := parityProgramRefSlice()
	mutate := func(src *cell.Slice, fn func(*cell.Slice) bool) *cell.Slice {
		cp := src.Copy()
		if !fn(cp) {
			panic("invalid parity program slice transform fixture")
		}
		return cp
	}

	switch mode {
	case 0:
		rest := refSrc.Copy()
		ref, err := rest.LoadRefCell()
		if err != nil {
			panic(err)
		}
		g.emitPushSlice("ldrefrotos_src", refSrc)
		g.emit("LDREFRTOS", cellsliceop.LDREFRTOS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: rest},
			parityProgramStackValue{kind: parityProgramSlice, slice: ref.MustBeginParse()},
		)
	case 1:
		want := mutate(bitsOnly, func(sl *cell.Slice) bool { return sl.OnlyFirst(3, 0) })
		g.emitPushSlice("sdcutfirst_src", bitsOnly)
		g.emitPushInt("sdcutfirst_bits", big.NewInt(3))
		g.emit("SDCUTFIRST", cellsliceop.SDCUTFIRST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 2:
		want := mutate(bitsOnly, func(sl *cell.Slice) bool { return sl.SkipFirst(2, 0) })
		g.emitPushSlice("sdskipfirst_src", bitsOnly)
		g.emitPushInt("sdskipfirst_bits", big.NewInt(2))
		g.emit("SDSKIPFIRST", cellsliceop.SDSKIPFIRST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 3:
		want := mutate(bitsOnly, func(sl *cell.Slice) bool { return sl.OnlyLast(4, 0) })
		g.emitPushSlice("sdcutlast_src", bitsOnly)
		g.emitPushInt("sdcutlast_bits", big.NewInt(4))
		g.emit("SDCUTLAST", cellsliceop.SDCUTLAST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 4:
		want := mutate(bitsOnly, func(sl *cell.Slice) bool { return sl.SkipLast(2, 0) })
		g.emitPushSlice("sdskiplast_src", bitsOnly)
		g.emitPushInt("sdskiplast_bits", big.NewInt(2))
		g.emit("SDSKIPLAST", cellsliceop.SDSKIPLAST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 5:
		want := mutate(bitsOnly, func(sl *cell.Slice) bool {
			return sl.SkipFirst(1, 0) && sl.OnlyFirst(4, 0)
		})
		g.emitPushSlice("sdsubstr_src", bitsOnly)
		g.emitPushInt("sdsubstr_offset", big.NewInt(1))
		g.emitPushInt("sdsubstr_bits", big.NewInt(4))
		g.emit("SDSUBSTR", cellsliceop.SDSUBSTR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 6:
		want := mutate(refSrc, func(sl *cell.Slice) bool { return sl.OnlyFirst(4, 1) })
		g.emitPushSlice("scutfirst_src", refSrc)
		g.emitPushInt("scutfirst_bits", big.NewInt(4))
		g.emitPushInt("scutfirst_refs", big.NewInt(1))
		g.emit("SCUTFIRST", cellsliceop.SCUTFIRST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 7:
		want := mutate(refSrc, func(sl *cell.Slice) bool { return sl.SkipFirst(2, 1) })
		g.emitPushSlice("sskipfirst_src", refSrc)
		g.emitPushInt("sskipfirst_bits", big.NewInt(2))
		g.emitPushInt("sskipfirst_refs", big.NewInt(1))
		g.emit("SSKIPFIRST", cellsliceop.SSKIPFIRST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 8:
		want := mutate(refSrc, func(sl *cell.Slice) bool { return sl.OnlyLast(3, 1) })
		g.emitPushSlice("scutlast_src", refSrc)
		g.emitPushInt("scutlast_bits", big.NewInt(3))
		g.emitPushInt("scutlast_refs", big.NewInt(1))
		g.emit("SCUTLAST", cellsliceop.SCUTLAST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 9:
		want := mutate(refSrc, func(sl *cell.Slice) bool { return sl.SkipLast(2, 1) })
		g.emitPushSlice("sskiplast_src", refSrc)
		g.emitPushInt("sskiplast_bits", big.NewInt(2))
		g.emitPushInt("sskiplast_refs", big.NewInt(1))
		g.emit("SSKIPLAST", cellsliceop.SSKIPLAST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 10:
		want := mutate(refSrc, func(sl *cell.Slice) bool {
			return sl.SkipFirst(1, 1) && sl.OnlyFirst(4, 1)
		})
		g.emitPushSlice("subslice_src", refSrc)
		g.emitPushInt("subslice_l1", big.NewInt(1))
		g.emitPushInt("subslice_r1", big.NewInt(1))
		g.emitPushInt("subslice_l2", big.NewInt(4))
		g.emitPushInt("subslice_r2", big.NewInt(1))
		g.emit("SUBSLICE", cellsliceop.SUBSLICE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: want})
	case 11, 12:
		first := mutate(refSrc, func(sl *cell.Slice) bool { return sl.OnlyFirst(3, 1) })
		rest := mutate(refSrc, func(sl *cell.Slice) bool { return sl.SkipFirst(3, 1) })
		quiet := mode == 12
		g.emitPushSlice("split_src", refSrc)
		g.emitPushInt("split_bits", big.NewInt(3))
		g.emitPushInt("split_refs", big.NewInt(1))
		if quiet {
			g.emit("SPLITQ", cellsliceop.SPLITQ().Serialize())
		} else {
			g.emit("SPLIT", cellsliceop.SPLIT().Serialize())
		}
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: first},
			parityProgramStackValue{kind: parityProgramSlice, slice: rest},
		)
		if quiet {
			g.stack = append(g.stack, parityProgramBoolValue(true))
		}
	case 13:
		g.emitPushSlice("splitq_fail_src", refSrc)
		g.emitPushInt("splitq_fail_bits", big.NewInt(7))
		g.emitPushInt("splitq_fail_refs", big.NewInt(0))
		g.emit("SPLITQ(fail)", cellsliceop.SPLITQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: refSrc.Copy()},
			parityProgramBoolValue(false),
		)
	case 14:
		g.emitPushSlice("schkbits_src", bitsOnly)
		g.emitPushInt("schkbits_bits", big.NewInt(6))
		g.emit("SCHKBITS", cellsliceop.SCHKBITS().Serialize())
		g.stack = g.stack[:base]
	case 15:
		g.emitPushSlice("schkrefs_src", refSrc)
		g.emitPushInt("schkrefs_refs", big.NewInt(2))
		g.emit("SCHKREFS", cellsliceop.SCHKREFS().Serialize())
		g.stack = g.stack[:base]
	case 16:
		g.emitPushSlice("schkbitrefs_src", refSrc)
		g.emitPushInt("schkbitrefs_bits", big.NewInt(6))
		g.emitPushInt("schkbitrefs_refs", big.NewInt(2))
		g.emit("SCHKBITREFS", cellsliceop.SCHKBITREFS().Serialize())
		g.stack = g.stack[:base]
	case 17:
		g.emitPushSlice("schkbitsq_src", bitsOnly)
		g.emitPushInt("schkbitsq_bits", big.NewInt(6))
		g.emit("SCHKBITSQ", cellsliceop.SCHKBITSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 18:
		g.emitPushSlice("schkrefsq_src", refSrc)
		g.emitPushInt("schkrefsq_refs", big.NewInt(3))
		g.emit("SCHKREFSQ(fail)", cellsliceop.SCHKREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 19:
		g.emitPushSlice("schkbitrefsq_src", refSrc)
		g.emitPushInt("schkbitrefsq_bits", big.NewInt(6))
		g.emitPushInt("schkbitrefsq_refs", big.NewInt(2))
		g.emit("SCHKBITREFSQ", cellsliceop.SCHKBITREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 20:
		g.emitPushSlice("schkbitrefsq_fail_src", refSrc)
		g.emitPushInt("schkbitrefsq_fail_bits", big.NewInt(7))
		g.emitPushInt("schkbitrefsq_fail_refs", big.NewInt(2))
		g.emit("SCHKBITREFSQ(fail)", cellsliceop.SCHKBITREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 21:
		g.emitPushSlice("pldrefvar_src", refSrc)
		g.emitPushInt("pldrefvar_idx", big.NewInt(1))
		g.emit("PLDREFVAR", cellsliceop.PLDREFVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref1})
	case 22:
		g.emitPushSlice("pldrefidx_src", refSrc)
		g.emit("PLDREFIDX(0)", cellsliceop.PLDREFIDX(0).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref0})
	case 23:
		src := parityProgramBitsSlice(0b001011, 6)
		want := mutate(src, func(sl *cell.Slice) bool { return sl.SkipFirst(uint(sl.CountLeading(false)), 0) })
		g.emitPushSlice("ldzeroes_src", src)
		g.emit("LDZEROES", cellsliceop.LDZEROES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramIntValue(big.NewInt(2)),
			parityProgramStackValue{kind: parityProgramSlice, slice: want},
		)
	case 24:
		src := parityProgramBitsSlice(0b111010, 6)
		want := mutate(src, func(sl *cell.Slice) bool { return sl.SkipFirst(uint(sl.CountLeading(true)), 0) })
		g.emitPushSlice("ldones_src", src)
		g.emit("LDONES", cellsliceop.LDONES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramIntValue(big.NewInt(3)),
			parityProgramStackValue{kind: parityProgramSlice, slice: want},
		)
	case 25:
		src := parityProgramBitsSlice(0b111010, 6)
		count := src.CountLeading(true)
		want := mutate(src, func(sl *cell.Slice) bool { return sl.SkipFirst(uint(count), 0) })
		g.emitPushSlice("ldsame_src", src)
		g.emitPushInt("ldsame_bit", big.NewInt(1))
		g.emit("LDSAME", cellsliceop.LDSAME().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramIntValue(big.NewInt(int64(count))),
			parityProgramStackValue{kind: parityProgramSlice, slice: want},
		)
	case 26:
		g.emitPushSlice("schkbitsq_fail_src", bitsOnly)
		g.emitPushInt("schkbitsq_fail_bits", big.NewInt(7))
		g.emit("SCHKBITSQ(fail)", cellsliceop.SCHKBITSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 27:
		g.emitPushSlice("schkrefsq_ok_src", refSrc)
		g.emitPushInt("schkrefsq_ok_refs", big.NewInt(2))
		g.emit("SCHKREFSQ", cellsliceop.SCHKREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 28:
		g.emitPushSlice("pldrefidx1_src", refSrc)
		g.emit("PLDREFIDX(1)", cellsliceop.PLDREFIDX(1).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref1})
	case 29:
		src := parityProgramBitsSlice(0b101011, 6)
		count := src.CountLeading(false)
		want := src.Copy()
		g.emitPushSlice("ldzeroes_zero_src", src)
		g.emit("LDZEROES(zero)", cellsliceop.LDZEROES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramIntValue(big.NewInt(int64(count))),
			parityProgramStackValue{kind: parityProgramSlice, slice: want},
		)
	default:
		src := parityProgramBitsSlice(0b010111, 6)
		count := src.CountLeading(true)
		want := src.Copy()
		g.emitPushSlice("ldones_zero_src", src)
		g.emit("LDONES(zero)", cellsliceop.LDONES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramIntValue(big.NewInt(int64(count))),
			parityProgramStackValue{kind: parityProgramSlice, slice: want},
		)
	}
	return true
}

func (g *parityProgramGenerator) emitCellSliceLoadFamilyOp(mode int) bool {
	base := len(g.stack)
	intSrc := parityProgramBitsSlice(0xABCD, 16)
	shortSrc := parityProgramBitsSlice(0b1011, 4)
	sliceSrc := parityProgramBitsSlice(0b101101, 6)
	loadInt := func(src *cell.Slice, bits uint, unsigned, preload bool) (*big.Int, *cell.Slice) {
		cp := src.Copy()
		var (
			val *big.Int
			err error
		)
		if unsigned {
			if preload {
				val, err = cp.PreloadBigUInt(bits)
			} else {
				val, err = cp.LoadBigUInt(bits)
			}
		} else if preload {
			val, err = cp.PreloadBigInt(bits)
		} else {
			val, err = cp.LoadBigInt(bits)
		}
		if err != nil {
			panic(err)
		}
		return val, cp
	}
	loadSlice := func(src *cell.Slice, bits uint, preload bool) (*cell.Slice, *cell.Slice) {
		cp := src.Copy()
		var (
			part *cell.Slice
			err  error
		)
		if preload {
			part, err = cp.PreloadSubslice(bits, 0)
		} else {
			part, err = cp.FetchSubslice(bits, 0)
		}
		if err != nil {
			panic(err)
		}
		return part, cp
	}

	switch mode {
	case 0:
		val, rest := loadInt(intSrc, 8, false, false)
		g.emitPushSlice("ldix_src", intSrc)
		g.emitPushInt("ldix_bits", big.NewInt(8))
		g.emit("LDIX", cellsliceop.LDIX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 1:
		val, rest := loadInt(intSrc, 12, true, false)
		g.emitPushSlice("ldux_src", intSrc)
		g.emitPushInt("ldux_bits", big.NewInt(12))
		g.emit("LDUX", cellsliceop.LDUX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 2:
		val, _ := loadInt(intSrc, 8, false, true)
		g.emitPushSlice("pldix_src", intSrc)
		g.emitPushInt("pldix_bits", big.NewInt(8))
		g.emit("PLDIX", cellsliceop.PLDIX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 3:
		val, _ := loadInt(intSrc, 12, true, true)
		g.emitPushSlice("pldux_src", intSrc)
		g.emitPushInt("pldux_bits", big.NewInt(12))
		g.emit("PLDUX", cellsliceop.PLDUX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 4:
		val, rest := loadInt(intSrc, 8, false, false)
		g.emitPushSlice("ldixq_src", intSrc)
		g.emitPushInt("ldixq_bits", big.NewInt(8))
		g.emit("LDIXQ", cellsliceop.LDIXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest}, parityProgramBoolValue(true))
	case 5:
		g.emitPushSlice("lduxq_fail_src", shortSrc)
		g.emitPushInt("lduxq_fail_bits", big.NewInt(8))
		g.emit("LDUXQ(fail)", cellsliceop.LDUXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: shortSrc.Copy()}, parityProgramBoolValue(false))
	case 6:
		val, _ := loadInt(intSrc, 8, false, true)
		g.emitPushSlice("pldixq_src", intSrc)
		g.emitPushInt("pldixq_bits", big.NewInt(8))
		g.emit("PLDIXQ", cellsliceop.PLDIXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramBoolValue(true))
	case 7:
		g.emitPushSlice("plduxq_fail_src", shortSrc)
		g.emitPushInt("plduxq_fail_bits", big.NewInt(8))
		g.emit("PLDUXQ(fail)", cellsliceop.PLDUXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 8:
		val, rest := loadInt(intSrc, 9, false, false)
		g.emitPushSlice("ldifix_src", intSrc)
		g.emit("LDIFIX(9)", cellsliceop.LDIFIX(9, false, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 9:
		val, rest := loadInt(intSrc, 9, true, false)
		g.emitPushSlice("ldufix_src", intSrc)
		g.emit("LDUFIX(9)", cellsliceop.LDUFIX(9, false, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 10:
		val, _ := loadInt(intSrc, 9, false, true)
		g.emitPushSlice("pldifix_src", intSrc)
		g.emit("PLDIFIX(9)", cellsliceop.PLDIFIX(9, false, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 11:
		val, _ := loadInt(intSrc, 9, true, true)
		g.emitPushSlice("pldufix_src", intSrc)
		g.emit("PLDUFIX(9)", cellsliceop.PLDUFIX(9, false, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 12:
		val, rest := loadInt(intSrc, 9, true, false)
		g.emitPushSlice("ldufixq_src", intSrc)
		g.emit("LDUFIXQ(9)", cellsliceop.LDUFIX(9, true, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest}, parityProgramBoolValue(true))
	case 13:
		g.emitPushSlice("pldufixq_fail_src", shortSrc)
		g.emit("PLDUFIXQ(fail)", cellsliceop.PLDUFIX(9, true, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 14:
		loadBits := shortSrc.BitsLeft()
		val, _ := loadInt(shortSrc, loadBits, true, true)
		val = new(big.Int).Lsh(val, 32-loadBits)
		g.emitPushSlice("plduz_src", shortSrc)
		g.emit("PLDUZ(32)", cellsliceop.PLDUZ(32).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: shortSrc.Copy()}, parityProgramIntValue(val))
	case 15:
		part, rest := loadSlice(sliceSrc, 3, false)
		g.emitPushSlice("ldslicex_src", sliceSrc)
		g.emitPushInt("ldslicex_bits", big.NewInt(3))
		g.emit("LDSLICEX", cellsliceop.LDSLICEX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part}, parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 16:
		part, _ := loadSlice(sliceSrc, 3, true)
		g.emitPushSlice("pldslicex_src", sliceSrc)
		g.emitPushInt("pldslicex_bits", big.NewInt(3))
		g.emit("PLDSLICEX", cellsliceop.PLDSLICEX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part})
	case 17:
		part, rest := loadSlice(sliceSrc, 3, false)
		g.emitPushSlice("ldslicexq_src", sliceSrc)
		g.emitPushInt("ldslicexq_bits", big.NewInt(3))
		g.emit("LDSLICEXQ", cellsliceop.LDSLICEXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: part},
			parityProgramStackValue{kind: parityProgramSlice, slice: rest},
			parityProgramBoolValue(true),
		)
	case 18:
		g.emitPushSlice("ldslicexq_fail_src", shortSrc)
		g.emitPushInt("ldslicexq_fail_bits", big.NewInt(7))
		g.emit("LDSLICEXQ(fail)", cellsliceop.LDSLICEXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: shortSrc.Copy()}, parityProgramBoolValue(false))
	case 19:
		part, _ := loadSlice(sliceSrc, 3, true)
		g.emitPushSlice("pldslicexq_src", sliceSrc)
		g.emitPushInt("pldslicexq_bits", big.NewInt(3))
		g.emit("PLDSLICEXQ", cellsliceop.PLDSLICEXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part}, parityProgramBoolValue(true))
	case 20:
		g.emitPushSlice("pldslicexq_fail_src", shortSrc)
		g.emitPushInt("pldslicexq_fail_bits", big.NewInt(7))
		g.emit("PLDSLICEXQ(fail)", cellsliceop.PLDSLICEXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 21:
		part, rest := loadSlice(sliceSrc, 3, false)
		g.emitPushSlice("ldslice_src", sliceSrc)
		g.emit("LDSLICE(3)", cellsliceop.LDSLICE(3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part}, parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 22:
		part, rest := loadSlice(sliceSrc, 3, false)
		g.emitPushSlice("ldslicefix_src", sliceSrc)
		g.emit("LDSLICEFIX(3)", cellsliceop.LDSLICEFIX(3, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part}, parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 23:
		part, _ := loadSlice(sliceSrc, 3, true)
		g.emitPushSlice("pldslicefix_src", sliceSrc)
		g.emit("PLDSLICEFIX(3)", cellsliceop.PLDSLICEFIX(3, false, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part})
	case 24:
		part, rest := loadSlice(sliceSrc, 3, false)
		g.emitPushSlice("ldslicefixq_src", sliceSrc)
		g.emit("LDSLICEFIXQ(3)", cellsliceop.LDSLICEFIX(3, true, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: part},
			parityProgramStackValue{kind: parityProgramSlice, slice: rest},
			parityProgramBoolValue(true),
		)
	case 25:
		part, _ := loadSlice(sliceSrc, 3, true)
		g.emitPushSlice("pldslicefixq_src", sliceSrc)
		g.emit("PLDSLICEFIXQ(3)", cellsliceop.PLDSLICEFIX(3, true, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: part}, parityProgramBoolValue(true))
	case 26:
		g.emitPushSlice("pldslicefixq_fail_src", shortSrc)
		g.emit("PLDSLICEFIXQ(fail)", cellsliceop.PLDSLICEFIX(7, true, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	default:
		g.emitPushSlice("ldslicefixq_fail_src", shortSrc)
		g.emit("LDSLICEFIXQ(fail)", cellsliceop.LDSLICEFIX(7, true, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: shortSrc.Copy()}, parityProgramBoolValue(false))
	}
	return true
}

func (g *parityProgramGenerator) emitCellSliceLittleEndianOp(mode int) bool {
	base := len(g.stack)
	src4 := parityProgramByteSlice([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xA5})
	src8 := parityProgramByteSlice([]byte{0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, 0xA5})
	short := parityProgramByteSlice([]byte{0x01, 0x02, 0x03, 0x04})
	load := func(src *cell.Slice, bytesLen int, unsigned, preload bool) (*big.Int, *cell.Slice) {
		cp := src.Copy()
		var (
			data []byte
			err  error
		)
		if preload {
			data, err = cp.PreloadSlice(uint(bytesLen * 8))
		} else {
			data, err = cp.LoadSlice(uint(bytesLen * 8))
		}
		if err != nil {
			panic(err)
		}
		return parityProgramDecodeLEInt(data, unsigned), cp
	}
	store := func(v *big.Int, bytesLen int, unsigned bool) *cell.Builder {
		data := parityProgramEncodeLEInt(v, bytesLen, unsigned)
		builder := cell.BeginCell()
		if err := builder.StoreSlice(data, uint(bytesLen*8)); err != nil {
			panic(err)
		}
		return builder
	}

	switch mode {
	case 0:
		val, rest := load(src4, 4, false, false)
		g.emitPushSlice("ldile4_src", src4)
		g.emit("LDILE4", cellsliceop.LDILE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 1:
		val, rest := load(src4, 4, true, false)
		g.emitPushSlice("ldule4_src", src4)
		g.emit("LDULE4", cellsliceop.LDULE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 2:
		val, rest := load(src8, 8, false, false)
		g.emitPushSlice("ldile8_src", src8)
		g.emit("LDILE8", cellsliceop.LDILE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 3:
		val, rest := load(src8, 8, true, false)
		g.emitPushSlice("ldule8_src", src8)
		g.emit("LDULE8", cellsliceop.LDULE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	case 4:
		val, _ := load(src4, 4, false, true)
		g.emitPushSlice("pldile4_src", src4)
		g.emit("PLDILE4", cellsliceop.PLDILE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 5:
		val, _ := load(src4, 4, true, true)
		g.emitPushSlice("pldule4_src", src4)
		g.emit("PLDULE4", cellsliceop.PLDULE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 6:
		val, _ := load(src8, 8, false, true)
		g.emitPushSlice("pldile8_src", src8)
		g.emit("PLDILE8", cellsliceop.PLDILE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 7:
		val, _ := load(src8, 8, true, true)
		g.emitPushSlice("pldule8_src", src8)
		g.emit("PLDULE8", cellsliceop.PLDULE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val))
	case 8:
		val, rest := load(src4, 4, false, false)
		g.emitPushSlice("ldile4q_src", src4)
		g.emit("LDILE4Q", cellsliceop.LDILE4Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramStackValue{kind: parityProgramSlice, slice: rest}, parityProgramBoolValue(true))
	case 9:
		g.emitPushSlice("ldule8q_fail_src", short)
		g.emit("LDULE8Q(fail)", cellsliceop.LDULE8Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: short.Copy()}, parityProgramBoolValue(false))
	case 10:
		val, _ := load(src4, 4, false, true)
		g.emitPushSlice("pldile4q_src", src4)
		g.emit("PLDILE4Q", cellsliceop.PLDILE4Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(val), parityProgramBoolValue(true))
	case 11:
		g.emitPushSlice("pldule8q_fail_src", short)
		g.emit("PLDULE8Q(fail)", cellsliceop.PLDULE8Q().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 12:
		v := big.NewInt(-2)
		want := store(v, 4, false)
		g.emitPushInt("stile4_val", v)
		g.emitPushBuilder("stile4_builder", cell.BeginCell())
		g.emit("STILE4", cellsliceop.STILE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 13:
		v := big.NewInt(0x01020304)
		want := store(v, 4, true)
		g.emitPushInt("stule4_val", v)
		g.emitPushBuilder("stule4_builder", cell.BeginCell())
		g.emit("STULE4", cellsliceop.STULE4().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 14:
		v := big.NewInt(-2)
		want := store(v, 8, false)
		g.emitPushInt("stile8_val", v)
		g.emitPushBuilder("stile8_builder", cell.BeginCell())
		g.emit("STILE8", cellsliceop.STILE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	default:
		v := new(big.Int).SetUint64(0x0102030405060708)
		want := store(v, 8, true)
		g.emitPushInt("stule8_val", v)
		g.emitPushBuilder("stule8_builder", cell.BeginCell())
		g.emit("STULE8", cellsliceop.STULE8().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	}
	return true
}

func (g *parityProgramGenerator) emitCellSliceBuilderAdvancedOp(mode int) bool {
	base := len(g.stack)
	ref := cell.BeginCell().MustStoreUInt(0xC, 4).EndCell()
	srcBuilder := cell.BeginCell().MustStoreUInt(0xA, 4)
	dstBuilder := cell.BeginCell().MustStoreUInt(0x5, 3)
	srcSlice := parityProgramBitsSlice(0b1011, 4)
	fullRefs := parityProgramFullRefsBuilder()
	fullBits := parityProgramFullBitsBuilder()
	storeRef := func(dst *cell.Builder, cl *cell.Cell) *cell.Builder {
		cp := dst.Copy()
		if err := cp.StoreRefUncheckedDepth(cl); err != nil {
			panic(err)
		}
		return cp
	}
	storeBuilder := func(dst, src *cell.Builder) *cell.Builder {
		cp := dst.Copy()
		if err := cp.StoreBuilderUncheckedDepth(src.Copy()); err != nil {
			panic(err)
		}
		return cp
	}
	storeSlice := func(dst *cell.Builder, sl *cell.Slice) *cell.Builder {
		cp := dst.Copy()
		if err := cp.StoreBuilderUncheckedDepth(sl.ToBuilder()); err != nil {
			panic(err)
		}
		return cp
	}
	endBuilder := func(src *cell.Builder) *cell.Cell {
		cl, err := src.Copy().EndCellSpecial(false)
		if err != nil {
			panic(err)
		}
		return cl
	}

	switch mode {
	case 0:
		want := storeRef(dstBuilder, endBuilder(srcBuilder))
		g.emitPushBuilder("stbref_src", srcBuilder)
		g.emitPushBuilder("stbref_dst", dstBuilder)
		g.emit("STBREF", cellsliceop.STBREF().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 1:
		want := storeRef(dstBuilder, endBuilder(srcBuilder))
		g.emitPushBuilder("stbrefr_dst", dstBuilder)
		g.emitPushBuilder("stbrefr_src", srcBuilder)
		g.emit("STBREFR", cellsliceop.STBREFR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 2:
		want := storeRef(dstBuilder, ref)
		g.emitPushBuilder("strefr_dst", dstBuilder)
		g.emitPushCell("strefr_ref", ref)
		g.emit("STREFR", cellsliceop.STREFR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 3:
		want := storeSlice(dstBuilder, srcSlice)
		g.emitPushBuilder("stslicer_dst", dstBuilder)
		g.emitPushSlice("stslicer_src", srcSlice)
		g.emit("STSLICER", cellsliceop.STSLICER().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 4:
		want := storeBuilder(dstBuilder, srcBuilder)
		g.emitPushBuilder("stbr_dst", dstBuilder)
		g.emitPushBuilder("stbr_src", srcBuilder)
		g.emit("STBR", cellsliceop.STBR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 5:
		want := storeRef(dstBuilder, ref)
		g.emitPushCell("strefq_ref", ref)
		g.emitPushBuilder("strefq_dst", dstBuilder)
		g.emit("STREFQ", cellsliceop.STREFQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 6:
		g.emitPushCell("strefq_fail_ref", ref)
		g.emitPushBuilder("strefq_fail_dst", fullRefs)
		g.emit("STREFQ(fail)", cellsliceop.STREFQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramCell, cell: ref},
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullRefs.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 7:
		want := storeRef(dstBuilder, ref)
		g.emitPushBuilder("strefrq_dst", dstBuilder)
		g.emitPushCell("strefrq_ref", ref)
		g.emit("STREFRQ", cellsliceop.STREFRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 8:
		g.emitPushBuilder("strefrq_fail_dst", fullRefs)
		g.emitPushCell("strefrq_fail_ref", ref)
		g.emit("STREFRQ(fail)", cellsliceop.STREFRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullRefs.Copy()},
			parityProgramStackValue{kind: parityProgramCell, cell: ref},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 9:
		want := storeRef(dstBuilder, endBuilder(srcBuilder))
		g.emitPushBuilder("stbrefq_src", srcBuilder)
		g.emitPushBuilder("stbrefq_dst", dstBuilder)
		g.emit("STBREFQ", cellsliceop.STBREFQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 10:
		g.emitPushBuilder("stbrefq_fail_src", srcBuilder)
		g.emitPushBuilder("stbrefq_fail_dst", fullRefs)
		g.emit("STBREFQ(fail)", cellsliceop.STBREFQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: srcBuilder.Copy()},
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullRefs.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 11:
		want := storeRef(dstBuilder, endBuilder(srcBuilder))
		g.emitPushBuilder("stbrefrq_dst", dstBuilder)
		g.emitPushBuilder("stbrefrq_src", srcBuilder)
		g.emit("STBREFRQ", cellsliceop.STBREFRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 12:
		g.emitPushBuilder("stbrefrq_fail_dst", fullRefs)
		g.emitPushBuilder("stbrefrq_fail_src", srcBuilder)
		g.emit("STBREFRQ(fail)", cellsliceop.STBREFRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullRefs.Copy()},
			parityProgramStackValue{kind: parityProgramBuilder, builder: srcBuilder.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 13:
		want := storeSlice(dstBuilder, srcSlice)
		g.emitPushSlice("stsliceq_src", srcSlice)
		g.emitPushBuilder("stsliceq_dst", dstBuilder)
		g.emit("STSLICEQ", cellsliceop.STSLICEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 14:
		g.emitPushSlice("stsliceq_fail_src", srcSlice)
		g.emitPushBuilder("stsliceq_fail_dst", fullBits)
		g.emit("STSLICEQ(fail)", cellsliceop.STSLICEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: srcSlice.Copy()},
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullBits.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 15:
		want := storeSlice(dstBuilder, srcSlice)
		g.emitPushBuilder("stslicerq_dst", dstBuilder)
		g.emitPushSlice("stslicerq_src", srcSlice)
		g.emit("STSLICERQ", cellsliceop.STSLICERQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 16:
		g.emitPushBuilder("stslicerq_fail_dst", fullBits)
		g.emitPushSlice("stslicerq_fail_src", srcSlice)
		g.emit("STSLICERQ(fail)", cellsliceop.STSLICERQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullBits.Copy()},
			parityProgramStackValue{kind: parityProgramSlice, slice: srcSlice.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 17:
		want := storeBuilder(dstBuilder, srcBuilder)
		g.emitPushBuilder("stbq_src", srcBuilder)
		g.emitPushBuilder("stbq_dst", dstBuilder)
		g.emit("STBQ", cellsliceop.STBQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 18:
		g.emitPushBuilder("stbq_fail_src", srcBuilder)
		g.emitPushBuilder("stbq_fail_dst", fullBits)
		g.emit("STBQ(fail)", cellsliceop.STBQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: srcBuilder.Copy()},
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullBits.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 19:
		want := storeBuilder(dstBuilder, srcBuilder)
		g.emitPushBuilder("stbrq_dst", dstBuilder)
		g.emitPushBuilder("stbrq_src", srcBuilder)
		g.emit("STBRQ", cellsliceop.STBRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want}, parityProgramIntValue(big.NewInt(0)))
	case 20:
		g.emitPushBuilder("stbrq_fail_dst", fullBits)
		g.emitPushBuilder("stbrq_fail_src", srcBuilder)
		g.emit("STBRQ(fail)", cellsliceop.STBRQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramBuilder, builder: fullBits.Copy()},
			parityProgramStackValue{kind: parityProgramBuilder, builder: srcBuilder.Copy()},
			parityProgramIntValue(big.NewInt(-1)),
		)
	case 21:
		want := storeRef(dstBuilder, ref)
		g.emitPushBuilder("strefconst_dst", dstBuilder)
		g.emit("STREFCONST", cellsliceop.STREFCONST(ref).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 22:
		ref2 := cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()
		want := storeRef(storeRef(dstBuilder, ref), ref2)
		g.emitPushBuilder("stref2const_dst", dstBuilder)
		g.emit("STREF2CONST", cellsliceop.STREF2CONST(ref, ref2).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 23:
		want := storeSlice(dstBuilder, srcSlice)
		g.emitPushBuilder("stsliceconst_dst", dstBuilder)
		g.emit("STSLICECONST", cellsliceop.STSLICECONST(srcSlice).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 24:
		want := storeRef(dstBuilder, endBuilder(srcBuilder))
		g.emitPushBuilder("endcst_dst", dstBuilder)
		g.emitPushBuilder("endcst_src", srcBuilder)
		g.emit("ENDCST", cellsliceop.ENDCST().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 25:
		cl, err := srcBuilder.Copy().EndCellSpecial(false)
		if err != nil {
			panic(err)
		}
		g.emitPushBuilder("endxc_builder", srcBuilder)
		g.emitPushInt("endxc_special", big.NewInt(0))
		g.emit("ENDXC", cellsliceop.ENDXC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: cl})
	case 26:
		cl := endBuilder(srcBuilder)
		g.emitPushBuilder("btos_builder", srcBuilder)
		g.emit("BTOS", cellsliceop.BTOS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: cl.MustBeginParse()})
	case 27:
		g.emitPushBuilder("bchkbits_src", dstBuilder)
		g.emitPushInt("bchkbits_bits", big.NewInt(8))
		g.emit("BCHKBITS", cellsliceop.BCHKBITS().Serialize())
		g.stack = g.stack[:base]
	case 28:
		g.emitPushBuilder("bchkrefs_src", dstBuilder)
		g.emitPushInt("bchkrefs_refs", big.NewInt(1))
		g.emit("BCHKREFS", cellsliceop.BCHKREFS().Serialize())
		g.stack = g.stack[:base]
	case 29:
		g.emitPushBuilder("bchkrefsq_fail_src", fullRefs)
		g.emitPushInt("bchkrefsq_fail_refs", big.NewInt(1))
		g.emit("BCHKREFSQ(fail)", cellsliceop.BCHKREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(false))
	case 30:
		want := dstBuilder.Copy()
		if err := want.StoreSameBit(false, 3); err != nil {
			panic(err)
		}
		g.emitPushBuilder("stzeroes_builder", dstBuilder)
		g.emitPushInt("stzeroes_bits", big.NewInt(3))
		g.emit("STZEROES", cellsliceop.STZEROES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 31:
		want := dstBuilder.Copy()
		if err := want.StoreSameBit(true, 4); err != nil {
			panic(err)
		}
		g.emitPushBuilder("stones_builder", dstBuilder)
		g.emitPushInt("stones_bits", big.NewInt(4))
		g.emit("STONES", cellsliceop.STONES().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	case 32:
		want := dstBuilder.Copy()
		if err := want.StoreSameBit(true, 5); err != nil {
			panic(err)
		}
		g.emitPushBuilder("stsame_builder", dstBuilder)
		g.emitPushInt("stsame_bits", big.NewInt(5))
		g.emitPushInt("stsame_bit", big.NewInt(1))
		g.emit("STSAME", cellsliceop.STSAME().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	default:
		g.emitPushBuilder("bchkbitrefsq_src", dstBuilder)
		g.emitPushInt("bchkbitrefsq_bits", big.NewInt(8))
		g.emitPushInt("bchkbitrefsq_refs", big.NewInt(1))
		g.emit("BCHKBITREFSQ", cellsliceop.BCHKBITREFSQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	}
	return true
}

func (g *parityProgramGenerator) emitCellSlicePrefixGramsOp(mode int) bool {
	base := len(g.stack)
	full := parityProgramBitsSlice(0b101101, 6)
	prefix := parityProgramBitsSlice(0b101, 3)
	wrongPrefix := parityProgramBitsSlice(0b111, 3)
	restAfterPrefix := func(src *cell.Slice, bits uint) *cell.Slice {
		cp := src.Copy()
		if err := cp.SkipBits(bits); err != nil {
			panic(err)
		}
		return cp
	}

	switch mode {
	case 0:
		g.emitPushSlice("sdbeginsx_full", full)
		g.emitPushSlice("sdbeginsx_prefix", prefix)
		g.emit("SDBEGINSX", cellsliceop.SDBEGINSX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: restAfterPrefix(full, prefix.BitsLeft())})
	case 1:
		g.emitPushSlice("sdbeginsxq_full", full)
		g.emitPushSlice("sdbeginsxq_prefix", prefix)
		g.emit("SDBEGINSXQ", cellsliceop.SDBEGINSXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: restAfterPrefix(full, prefix.BitsLeft())}, parityProgramBoolValue(true))
	case 2:
		g.emitPushSlice("sdbeginsxq_fail_full", full)
		g.emitPushSlice("sdbeginsxq_fail_prefix", wrongPrefix)
		g.emit("SDBEGINSXQ(fail)", cellsliceop.SDBEGINSXQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: full.Copy()}, parityProgramBoolValue(false))
	case 3:
		g.emitPushSlice("sdbeginsconst_full", full)
		g.emit("SDBEGINSCONST", cellsliceop.SDBEGINSCONST(prefix, false).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: restAfterPrefix(full, prefix.BitsLeft())})
	case 4:
		g.emitPushSlice("sdbeginsconstq_full", full)
		g.emit("SDBEGINSCONSTQ", cellsliceop.SDBEGINSCONST(prefix, true).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: restAfterPrefix(full, prefix.BitsLeft())}, parityProgramBoolValue(true))
	case 5:
		g.emitPushSlice("sdbeginsconstq_fail_full", full)
		g.emit("SDBEGINSCONSTQ(fail)", cellsliceop.SDBEGINSCONST(wrongPrefix, true).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: full.Copy()}, parityProgramBoolValue(false))
	case 6:
		amount := big.NewInt(123456789)
		src := cell.BeginCell().MustStoreBigCoins(amount).MustStoreUInt(0xA, 4).ToSlice()
		rest := src.Copy()
		coins, err := rest.LoadBigCoins()
		if err != nil {
			panic(err)
		}
		g.emitPushSlice("ldgrams_src", src)
		g.emit("LDGRAMS", cellsliceop.LDGRAMS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(coins), parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	default:
		amount := big.NewInt(987654321)
		want := cell.BeginCell().MustStoreUInt(0xA, 4)
		if err := want.StoreBigCoins(amount); err != nil {
			panic(err)
		}
		g.emitPushBuilder("stgrams_builder", cell.BeginCell().MustStoreUInt(0xA, 4))
		g.emitPushInt("stgrams_amount", amount)
		g.emit("STGRAMS", cellsliceop.STGRAMS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: want})
	}
	return true
}

func (g *parityProgramGenerator) emitDictOp() bool {
	switch g.r.Intn(48) {
	case 0:
		return g.emitLoadDictOp(false)
	case 1:
		return g.emitLoadDictOp(true)
	case 2:
		return g.emitDictGetOp(false, false, false)
	case 3:
		return g.emitDictGetOp(true, false, false)
	case 4:
		return g.emitDictGetOp(false, true, false)
	case 5:
		return g.emitDictGetOp(false, false, true)
	case 6:
		return g.emitDictSetOp(false, false)
	case 7:
		return g.emitDictSetOp(false, true)
	case 8:
		return g.emitDictDeleteOp(false)
	case 9:
		return g.emitDictMinMaxOp(false)
	case 10:
		return g.emitDictMinMaxOp(true)
	case 11:
		return g.emitPrefixDictGetQOp()
	case 12:
		return g.emitDictGetOptRefOp(false)
	case 13:
		return g.emitDictGetOptRefOp(true)
	case 14:
		return g.emitDictSetGetOp(false)
	case 15:
		return g.emitDictSetGetOp(true)
	case 16:
		return g.emitDictReplaceAddOp(false)
	case 17:
		return g.emitDictReplaceAddOp(true)
	case 18:
		return g.emitDictRemMinMaxOp(false)
	case 19:
		return g.emitDictRemMinMaxOp(true)
	case 20:
		return g.emitDictNearOp()
	case 21:
		return g.emitStoreDictOp()
	case 22:
		return g.emitSkipDictOp()
	case 23:
		return g.emitLoadDictSliceOp(false, false)
	case 24:
		return g.emitLoadDictSliceOp(true, false)
	case 25:
		return g.emitLoadDictQuietOp(false)
	case 26:
		return g.emitLoadDictQuietOp(true)
	case 27:
		return g.emitPrefixDictGetOp(false)
	case 28:
		return g.emitPrefixDictReplaceAddOp(false)
	case 29:
		return g.emitPrefixDictReplaceAddOp(true)
	case 30:
		return g.emitDictBuilderSetOp(cell.DictSetModeSet, 0)
	case 31:
		return g.emitDictBuilderSetOp(cell.DictSetModeReplace, 2)
	case 32:
		return g.emitDictBuilderSetOp(cell.DictSetModeAdd, 1)
	case 33:
		return g.emitDictBuilderSetGetOp(cell.DictSetModeSet, 0)
	case 34:
		return g.emitDictBuilderSetGetOp(cell.DictSetModeReplace, 2)
	case 35:
		return g.emitDictBuilderSetGetOp(cell.DictSetModeAdd, 1)
	case 36:
		return g.emitDictDeleteGetOp(false, 0)
	case 37:
		return g.emitDictDeleteGetOp(true, 2)
	case 38:
		return g.emitDictSetGetOptRefOp(false, 2)
	case 39:
		return g.emitDictSetGetOptRefOp(true, 2)
	case 40:
		return g.emitDictNearExtraOp(0)
	case 41:
		return g.emitDictNearExtraOp(1)
	case 42:
		return g.emitDictNearExtraOp(2)
	case 43:
		return g.emitDictNearExtraOp(3)
	case 44:
		return g.emitSubdictOp(false, 0)
	case 45:
		return g.emitSubdictOp(true, 0)
	case 46:
		return g.emitSubdictOp(false, 2)
	default:
		return g.emitPrefixDictSetDelOp(g.r.Intn(2) == 0)
	}
}

func (g *parityProgramGenerator) emitControlOp() bool {
	switch g.r.Intn(10) {
	case 0:
		return g.emitCondSelectOp(false)
	case 1:
		return g.emitCondSelectOp(true)
	case 2:
		g.emit("SETCP(0)", funcsop.SETCP(0).Serialize())
	case 3:
		base := len(g.stack)
		g.emitPushInt("setcpx_cp", big.NewInt(0))
		g.emit("SETCPX", funcsop.SETCPX().Serialize())
		g.stack = g.stack[:base]
	case 4:
		base := len(g.stack)
		g.emitPushInt("throwif_false", big.NewInt(0))
		g.emit("THROWIF(skip)", parityProgramRawOp(0xF240|42, 16))
		g.stack = g.stack[:base]
	case 5:
		base := len(g.stack)
		g.emitPushInt("throwifnot_true", big.NewInt(-1))
		g.emit("THROWIFNOT(skip)", parityProgramRawOp(0xF280|42, 16))
		g.stack = g.stack[:base]
	default:
		return g.emitContinuationControlOp(g.r.Intn(12))
	}
	return true
}

func (g *parityProgramGenerator) emitContinuationControlOp(mode int) bool {
	base := len(g.stack)
	intBody := func(v int64) *cell.Cell {
		return parityProgramCodeCell(stackop.PUSHINT(big.NewInt(v)).Serialize())
	}
	pushResult := func(v int64) {
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(v)))
	}
	clearStack := func() {
		g.stack = g.stack[:base]
	}

	switch mode {
	case 0:
		g.emit("PUSHCONT(push70)", stackop.PUSHCONT(intBody(70)).Serialize())
		g.emit("EXECUTE(push70)", execop.EXECUTE().Serialize())
		pushResult(70)
	case 1:
		g.emit("CALLREF(push71)", execop.CALLREF(intBody(71)).Serialize())
		pushResult(71)
	case 2:
		g.emitPushInt("ifref_true_cond", big.NewInt(-1))
		g.emit("IFREF(push72,true)", execop.IFREF(intBody(72)).Serialize())
		pushResult(72)
	case 3:
		g.emitPushInt("ifref_false_cond", big.NewInt(0))
		g.emit("IFREF(push72,false)", execop.IFREF(intBody(72)).Serialize())
		clearStack()
	case 4:
		g.emitPushInt("ifnotref_true_cond", big.NewInt(0))
		g.emit("IFNOTREF(push73,true)", execop.IFNOTREF(intBody(73)).Serialize())
		pushResult(73)
	case 5:
		g.emitPushInt("ifnotref_false_cond", big.NewInt(-1))
		g.emit("IFNOTREF(push73,false)", execop.IFNOTREF(intBody(73)).Serialize())
		clearStack()
	case 6:
		g.emitPushInt("ifrefelseref_true_cond", big.NewInt(-1))
		g.emit("IFREFELSEREF(true74,false75)", execop.IFREFELSEREF(intBody(74), intBody(75)).Serialize())
		pushResult(74)
	case 7:
		g.emitPushInt("ifrefelseref_false_cond", big.NewInt(0))
		g.emit("IFREFELSEREF(true74,false75)", execop.IFREFELSEREF(intBody(74), intBody(75)).Serialize())
		pushResult(75)
	case 8:
		g.emitPushInt("ifrefelse_true_cond", big.NewInt(-1))
		g.emit("PUSHCONT(ifrefelse_false77)", stackop.PUSHCONT(intBody(77)).Serialize())
		g.emit("IFREFELSE(true76,false77)", execop.IFREFELSE(intBody(76)).Serialize())
		pushResult(76)
	case 9:
		g.emitPushInt("ifelseref_false_cond", big.NewInt(0))
		g.emit("PUSHCONT(ifelseref_true78)", stackop.PUSHCONT(intBody(78)).Serialize())
		g.emit("IFELSEREF(true78,false79)", execop.IFELSEREF(intBody(79)).Serialize())
		pushResult(79)
	case 10:
		body := intBody(80)
		g.emitPushSlice("bless_code", body.MustBeginParse())
		g.emit("BLESS(push80)", execop.BLESS().Serialize())
		g.emit("EXECUTE(blessed_push80)", execop.EXECUTE().Serialize())
		pushResult(80)
	default:
		body := parityProgramCodeCell()
		g.emitPushInt("blessargs_copied", big.NewInt(81))
		g.emitPushSlice("blessargs_code", body.MustBeginParse())
		g.emit("BLESSARGS(1,0)", execop.BLESSARGS(1, 0).Serialize())
		g.emit("EXECUTE(blessargs)", execop.EXECUTE().Serialize())
		pushResult(81)
	}
	return true
}

func (g *parityProgramGenerator) emitFuncParamOp() bool {
	switch g.r.Intn(28) {
	case 0:
		g.emit("NOW", funcsop.NOW().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(crossTestTime.Unix())))
	case 1:
		g.emit("GETPARAM(3)", funcsop.GETPARAM(3).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(crossTestTime.Unix())))
	case 2:
		g.emit("BLOCKLT", funcsop.BLOCKLT().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0)))
	case 3:
		g.emit("LTIME", funcsop.LTIME().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0)))
	case 4:
		g.emit("RANDSEED", funcsop.RANDSEED().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(g.seed)))
	case 5:
		g.emit("BALANCE", funcsop.BALANCE().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{
			kind: parityProgramTuple,
			tuple: []parityProgramStackValue{
				parityProgramIntValue(crossTestBalance),
				{kind: parityProgramNull},
			},
		})
	case 6:
		g.emit("MYADDR", funcsop.MYADDR().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{
			kind:  parityProgramSlice,
			slice: cell.BeginCell().MustStoreAddr(crossTestAddr).ToSlice(),
		})
	case 7:
		g.emit("CONFIGROOT", funcsop.CONFIGROOT().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
	case 8:
		g.emit("CONFIGDICT", funcsop.CONFIGDICT().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(32)))
	case 9:
		g.emit("GETPARAMLONG(6)", funcsop.GETPARAMLONG(6).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(g.seed)))
	case 10:
		return g.emitPrngOp(0)
	case 11:
		return g.emitPrngOp(1)
	case 12:
		return g.emitPrngOp(2)
	case 13:
		return g.emitPrngOp(3)
	case 14:
		return g.emitDataSizeProgramOp(false, false)
	case 15:
		return g.emitDataSizeProgramOp(false, true)
	case 16:
		return g.emitDataSizeProgramOp(true, false)
	case 17:
		return g.emitDataSizeProgramOp(true, true)
	case 18:
		return g.emitRuntimeControlOp()
	case 19:
		return g.emitGlobalVarOp(false)
	case 20:
		return g.emitGlobalVarOp(true)
	case 21:
		return g.emitControlRegisterOp(g.r.Intn(12))
	default:
		return g.emitFuncHashVarIntOp(g.r.Intn(9))
	}
	return true
}

func (g *parityProgramGenerator) emitRuntimeControlOp() bool {
	switch g.r.Intn(12) {
	case 0:
		g.emit("ACCEPT", funcsop.ACCEPT().Serialize())
	case 1:
		base := len(g.stack)
		g.emitPushInt("setgaslimit_limit", big.NewInt(differentialFuzzGasLimit))
		g.emit("SETGASLIMIT", funcsop.SETGASLIMIT().Serialize())
		g.stack = g.stack[:base]
	case 2:
		base := len(g.stack)
		g.emit("GASCONSUMED", funcsop.GASCONSUMED().Serialize())
		g.emit("DROP(gasconsumed)", stackop.DROP().Serialize())
		g.stack = g.stack[:base]
	case 3:
		g.emit("COMMIT", funcsop.COMMIT().Serialize())
	case 4:
		return g.emitGlobalVarOp(false)
	case 5:
		return g.emitGlobalVarOp(true)
	case 6:
		return g.emitSendRawMsgRegisterOp()
	default:
		return g.emitControlRegisterOp(g.r.Intn(12))
	}
	return true
}

func (g *parityProgramGenerator) emitGlobalVarOp(variable bool) bool {
	base := len(g.stack)
	value := big.NewInt(int64(1200 + g.r.Intn(200)))
	if variable {
		g.emitPushInt("setglobvar_value", value)
		g.emitPushInt("setglobvar_idx", big.NewInt(21))
		g.emit("SETGLOBVAR(21)", funcsop.SETGLOBVAR().Serialize())
		g.emitPushInt("getglobvar_idx", big.NewInt(21))
		g.emit("GETGLOBVAR(21)", funcsop.GETGLOBVAR().Serialize())
	} else {
		g.emitPushInt("setglob20_value", value)
		g.emit("SETGLOB(20)", funcsop.SETGLOB(20).Serialize())
		g.emit("GETGLOB(20)", funcsop.GETGLOB(20).Serialize())
	}
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramIntValue(value))
	return true
}

func (g *parityProgramGenerator) emitControlRegisterOp(mode int) bool {
	base := len(g.stack)
	dataCell := cell.BeginCell().MustStoreUInt(uint64(0xA0+mode), 8).EndCell()
	actionCell := cell.BeginCell().MustStoreUInt(uint64(0xB0+mode), 8).EndCell()

	switch mode {
	case 0:
		g.emit("PUSHCTR(4)", execop.PUSHCTR(4).Serialize())
		g.emit("DROP(pushctr4)", stackop.DROP().Serialize())
	case 1:
		g.emit("PUSHCTR(5)", execop.PUSHCTR(5).Serialize())
		g.emit("DROP(pushctr5)", stackop.DROP().Serialize())
	case 2:
		g.emitPushCell("popctr4_data", dataCell)
		g.emit("POPCTR(4)", execop.POPCTR(4).Serialize())
		g.regD[0] = dataCell
	case 3:
		g.emitPushCell("popctr5_actions", actionCell)
		g.emit("POPCTR(5)", execop.POPCTR(5).Serialize())
		g.regD[1] = actionCell
	case 4:
		g.emitPushInt("pushctrx_idx", big.NewInt(4))
		g.emit("PUSHCTRX(4)", execop.PUSHCTRX().Serialize())
		g.emit("DROP(pushctrx4)", stackop.DROP().Serialize())
	case 5:
		g.emitPushCell("popctrx4_data", dataCell)
		g.emitPushInt("popctrx4_idx", big.NewInt(4))
		g.emit("POPCTRX(4)", execop.POPCTRX().Serialize())
		g.regD[0] = dataCell
	case 6:
		g.emit("SAVECTR(4)", execop.SAVECTR(4).Serialize())
	case 7:
		g.emit("SAVEALTCTR(4)", execop.SAVEALTCTR(4).Serialize())
	case 8:
		g.emit("SAVEBOTHCTR(4)", execop.SAVEBOTHCTR(4).Serialize())
	case 9:
		g.emitPushCell("popsavectr4_data", dataCell)
		g.emit("POPSAVECTR(4)", execop.POPSAVECTR(4).Serialize())
		g.regD[0] = dataCell
	case 10:
		g.emitPushCell("setretctr4_data", dataCell)
		g.emit("SETRETCTR(4)", execop.SETRETCTR(4).Serialize())
	default:
		g.emitPushCell("setaltctr4_data", dataCell)
		g.emit("SETALTCTR(4)", execop.SETALTCTR(4).Serialize())
	}

	g.stack = g.stack[:base]
	return true
}

func (g *parityProgramGenerator) emitSendRawMsgRegisterOp() bool {
	base := len(g.stack)
	mode := uint8(g.r.Intn(4))
	msg := parityProgramOutboundInternalMessage()
	nextActions := cell.BeginCell().
		MustStoreRef(g.regD[1]).
		MustStoreUInt(0x0ec3c86d, 32).
		MustStoreUInt(uint64(mode), 8).
		MustStoreRef(msg).
		EndCell()

	g.emitPushCell("sendrawmsg_msg", msg)
	g.emitPushInt("sendrawmsg_mode", big.NewInt(int64(mode)))
	g.emit("SENDRAWMSG", funcsop.SENDRAWMSG().Serialize())
	g.emit("PUSHCTR(5/sendrawmsg)", execop.PUSHCTR(5).Serialize())
	g.emit("HASHCU(sendrawmsg_c5)", cellsliceop.HASHCU().Serialize())

	g.regD[1] = nextActions
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(nextActions.Hash())))
	return true
}

func parityProgramOutboundInternalMessage() *cell.Cell {
	msg, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     crossTestAddr,
		Amount:      tlb.FromNanoTONU(1),
		Body:        cell.BeginCell().MustStoreUInt(0xB0, 8).EndCell(),
	})
	if err != nil {
		panic(err)
	}
	return msg
}

func (g *parityProgramGenerator) emitDataSizeProgramOp(sliceArg, quiet bool) bool {
	base := len(g.stack)
	root := parityProgramStorageStatCell()
	if sliceArg {
		g.emitPushSlice("sdatasize_src", root.MustBeginParse())
	} else {
		g.emitPushCell("cdatasize_src", root)
	}
	g.emitPushInt("datasize_bound", big.NewInt(10))

	name := "CDATASIZE"
	op := funcsop.CDATASIZE().Serialize()
	cells, bits, refs := int64(2), int64(8), int64(1)
	if sliceArg {
		name = "SDATASIZE"
		op = funcsop.SDATASIZE().Serialize()
		cells = 1
	}
	if quiet {
		name += "Q"
		if sliceArg {
			op = funcsop.SDATASIZEQ().Serialize()
		} else {
			op = funcsop.CDATASIZEQ().Serialize()
		}
	}
	g.emit(name, op)

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramIntValue(big.NewInt(cells)),
		parityProgramIntValue(big.NewInt(bits)),
		parityProgramIntValue(big.NewInt(refs)),
	)
	if quiet {
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	}
	return true
}

func (g *parityProgramGenerator) emitPrngOp(mode int) bool {
	switch mode {
	case 0:
		value := g.nextRandU256()
		g.emit("RANDU256", funcsop.RANDU256().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(value))
		g.emitRandSeedProbe()
	case 1:
		bound := big.NewInt(int64(1 + g.r.Intn(1000)))
		value := g.nextRandU256()
		value.Mul(value, bound)
		value.Rsh(value, 256)
		g.emitPushInt("rand_bound", bound)
		g.emit("RAND", funcsop.RAND().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, parityProgramIntValue(value))
		g.emitRandSeedProbe()
	case 2:
		seed := big.NewInt(int64(g.r.Intn(1000)))
		g.emitPushInt("setrand_seed", seed)
		g.emit("SETRAND", funcsop.SETRAND().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.seed = parityProgramUint256Bytes(seed)
		g.emitRandSeedProbe()
	default:
		mix := big.NewInt(int64(g.r.Intn(1000)))
		mixBytes := parityProgramUint256Bytes(mix)
		buf := make([]byte, 64)
		copy(buf, g.seed)
		copy(buf[32:], mixBytes)
		sum := sha256.Sum256(buf)
		g.emitPushInt("addrand_seed", mix)
		g.emit("ADDRAND", funcsop.ADDRAND().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.seed = append(g.seed[:0], sum[:]...)
		g.emitRandSeedProbe()
	}
	return true
}

func (g *parityProgramGenerator) emitRandSeedProbe() {
	g.emit("RANDSEED(after_prng)", funcsop.RANDSEED().Serialize())
	g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(g.seed)))
}

func (g *parityProgramGenerator) nextRandU256() *big.Int {
	sum := sha512.Sum512(g.seed)
	g.seed = append(g.seed[:0], sum[:32]...)
	return new(big.Int).SetBytes(sum[32:])
}

func (g *parityProgramGenerator) emitFuncHashVarIntOp(mode int) bool {
	switch mode {
	case 0:
		data := []byte{0xAB, 0xCD}
		sum := sha256.Sum256(data)
		g.emitPushSlice("sha256u_src", cell.BeginCell().MustStoreSlice(data, uint(len(data)*8)).ToSlice())
		g.emit("SHA256U", funcsop.SHA256U().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(sum[:])))
	case 1:
		builder := cell.BeginCell().MustStoreUInt(0xAB, 8)
		hash := builder.Copy().EndCell().Hash()
		g.emitPushBuilder("hashbu_src", builder)
		g.emit("HASHBU", funcsop.HASHBU().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(hash)))
	case 2:
		data := []byte{0x11, 0x22, 0x33}
		sum := sha256.Sum256(data)
		base := len(g.stack)
		g.emitPushSlice("hashext_src", cell.BeginCell().MustStoreSlice(data, uint(len(data)*8)).ToSlice())
		g.emitPushInt("hashext_count", big.NewInt(1))
		g.emit("HASHEXT(0)", funcsop.HASHEXT(0).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(sum[:])))
	case 3:
		src := parityProgramVarIntSlice(big.NewInt(-2), 4, true)
		base := len(g.stack)
		g.emitPushSlice("ldvarint16_src", src)
		g.emit("LDVARINT16", funcsop.LDVARINT16().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-2)), parityProgramStackValue{kind: parityProgramSlice, slice: cell.BeginCell().EndCell().MustBeginParse()})
	case 4:
		src := parityProgramVarIntSlice(big.NewInt(17), 5, false)
		base := len(g.stack)
		g.emitPushSlice("ldvaruint32_src", src)
		g.emit("LDVARUINT32", funcsop.LDVARUINT32().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(17)), parityProgramStackValue{kind: parityProgramSlice, slice: cell.BeginCell().EndCell().MustBeginParse()})
	case 5:
		src := parityProgramVarIntSlice(big.NewInt(-2), 5, true)
		base := len(g.stack)
		g.emitPushSlice("ldvarint32_src", src)
		g.emit("LDVARINT32", funcsop.LDVARINT32().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-2)), parityProgramStackValue{kind: parityProgramSlice, slice: cell.BeginCell().EndCell().MustBeginParse()})
	case 6:
		base := len(g.stack)
		builder := cell.BeginCell()
		next := builder.Copy()
		next.MustStoreUInt(1, 4).MustStoreInt(-2, 8)
		g.emitPushBuilder("stvarint16_builder", builder)
		g.emitPushInt("stvarint16_value", big.NewInt(-2))
		g.emit("STVARINT16", funcsop.STVARINT16().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	case 7:
		base := len(g.stack)
		builder := cell.BeginCell()
		next := builder.Copy()
		next.MustStoreUInt(1, 5).MustStoreUInt(17, 8)
		g.emitPushBuilder("stvaruint32_builder", builder)
		g.emitPushInt("stvaruint32_value", big.NewInt(17))
		g.emit("STVARUINT32", funcsop.STVARUINT32().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	default:
		base := len(g.stack)
		builder := cell.BeginCell()
		next := builder.Copy()
		next.MustStoreUInt(1, 5).MustStoreInt(-2, 8)
		g.emitPushBuilder("stvarint32_builder", builder)
		g.emitPushInt("stvarint32_value", big.NewInt(-2))
		g.emit("STVARINT32", funcsop.STVARINT32().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	}
	return true
}

func (g *parityProgramGenerator) emitMessageAddressOp() bool {
	switch g.r.Intn(16) {
	case 0:
		return g.emitLoadMsgAddressOp("LDMSGADDR", funcsop.LDMSGADDR().Serialize(), false, false, false)
	case 1:
		return g.emitLoadMsgAddressOp("LDMSGADDRQ", funcsop.LDMSGADDRQ().Serialize(), false, true, false)
	case 2:
		return g.emitLoadMsgAddressOp("LDSTDADDR", funcsop.LDSTDADDR().Serialize(), true, false, false)
	case 3:
		return g.emitLoadMsgAddressOp("LDSTDADDRQ", funcsop.LDSTDADDRQ().Serialize(), true, true, false)
	case 4:
		return g.emitLoadMsgAddressOp("LDOPTSTDADDR", funcsop.LDOPTSTDADDR().Serialize(), true, false, true)
	case 5:
		return g.emitLoadMsgAddressOp("LDOPTSTDADDRQ", funcsop.LDOPTSTDADDRQ().Serialize(), true, true, true)
	case 6:
		return g.emitParseMsgAddressOp(false)
	case 7:
		return g.emitParseMsgAddressOp(true)
	case 8:
		return g.emitRewriteStdAddressOp(false, false)
	case 9:
		return g.emitRewriteStdAddressOp(false, true)
	case 10:
		return g.emitRewriteStdAddressOp(true, false)
	case 11:
		return g.emitRewriteStdAddressOp(true, true)
	case 12:
		return g.emitStoreStdAddressOp(false, false, false)
	case 13:
		return g.emitStoreStdAddressOp(false, true, false)
	case 14:
		return g.emitStoreStdAddressOp(true, false, g.r.Intn(2) == 0)
	default:
		return g.emitStoreStdAddressOp(true, true, g.r.Intn(2) == 0)
	}
}

func (g *parityProgramGenerator) emitLoadMsgAddressOp(name string, op *cell.Builder, stdOnly, quiet, optStd bool) bool {
	base := len(g.stack)
	if optStd && g.r.Intn(3) == 0 {
		src := parityProgramAddrNoneTailSlice()
		g.emitPushSlice(name+"_none", src)
		g.emit(name, op)
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramNull},
			parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramTailSlice()},
		)
		if quiet {
			g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
		}
		return true
	}

	src := parityProgramStdAddrTailSlice()
	g.emitPushSlice(name+"_std", src)
	g.emit(name, op)
	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramStdAddrSlice()},
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramTailSlice()},
	)
	if quiet {
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	}
	return true
}

func (g *parityProgramGenerator) emitParseMsgAddressOp(quiet bool) bool {
	base := len(g.stack)
	name := "PARSEMSGADDR"
	op := funcsop.PARSEMSGADDR().Serialize()
	if quiet {
		name = "PARSEMSGADDRQ"
		op = funcsop.PARSEMSGADDRQ().Serialize()
	}
	g.emitPushSlice(name+"_std", parityProgramStdAddrSlice())
	g.emit(name, op)
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramParsedStdAddrTuple())
	if quiet {
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	}
	return true
}

func (g *parityProgramGenerator) emitRewriteStdAddressOp(varAddr, quiet bool) bool {
	base := len(g.stack)
	name := "REWRITESTDADDR"
	op := funcsop.REWRITESTDADDR().Serialize()
	if varAddr {
		name = "REWRITEVARADDR"
		op = funcsop.REWRITEVARADDR().Serialize()
	}
	if quiet {
		name += "Q"
		if varAddr {
			op = funcsop.REWRITEVARADDRQ().Serialize()
		} else {
			op = funcsop.REWRITESTDADDRQ().Serialize()
		}
	}
	g.emitPushSlice(name+"_std", parityProgramStdAddrSlice())
	g.emit(name, op)
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(crossTestAddr.Workchain()))))
	if varAddr {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramStdAddrDataSlice()})
	} else {
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).SetBytes(crossTestAddr.Data())))
	}
	if quiet {
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	}
	return true
}

func (g *parityProgramGenerator) emitStoreStdAddressOp(opt, quiet, none bool) bool {
	base := len(g.stack)
	name := "STSTDADDR"
	op := funcsop.STSTDADDR().Serialize()
	if opt {
		name = "STOPTSTDADDR"
		op = funcsop.STOPTSTDADDR().Serialize()
	}
	if quiet {
		name += "Q"
		if opt {
			op = funcsop.STOPTSTDADDRQ().Serialize()
		} else {
			op = funcsop.STSTDADDRQ().Serialize()
		}
	}

	builder := cell.BeginCell()
	next := cell.BeginCell()
	if opt && none {
		g.emitPushNull(name + "_none")
		next.MustStoreUInt(0, 2)
	} else {
		g.emitPushSlice(name+"_std", parityProgramStdAddrSlice())
		next.MustStoreBuilder(parityProgramStdAddrSlice().ToBuilder())
	}
	g.emitPushBuilder(name+"_builder", builder)
	g.emit(name, op)

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	if quiet {
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0)))
	}
	return true
}

func (g *parityProgramGenerator) emitPushBuilder(name string, builder *cell.Builder) {
	base := len(g.stack)
	if builder.BitsUsed() == 0 && builder.RefsUsed() == 0 {
		g.emit("NEWC("+name+")", cellsliceop.NEWC().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: cell.BeginCell()})
		return
	}

	g.emitPushSlice(name+"_contents", builder.ToSlice())
	g.emit("NEWC("+name+")", cellsliceop.NEWC().Serialize())
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: cell.BeginCell()})
	g.emit("STSLICE("+name+")", cellsliceop.STSLICE().Serialize())
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: builder.Copy()})
}

func (g *parityProgramGenerator) emitDictKey(name string, kind int, value uint64, bits uint) {
	if kind == 0 {
		g.emitPushSlice(name, parityProgramKeySlice(value, bits))
		return
	}
	g.emitPushInt(name, big.NewInt(int64(value)))
}

func parityProgramVarIntSlice(value *big.Int, lenBits uint, signed bool) *cell.Slice {
	b := cell.BeginCell().MustStoreUInt(1, lenBits)
	if signed {
		b.MustStoreBigInt(value, 8)
	} else {
		b.MustStoreBigUInt(value, 8)
	}
	return b.ToSlice()
}

func parityProgramUint256Bytes(v *big.Int) []byte {
	out := make([]byte, 32)
	v.FillBytes(out)
	return out
}

func (g *parityProgramGenerator) emitCondSelectOp(checked bool) bool {
	base := len(g.stack)
	cond := int64(0)
	selected := parityProgramIntValue(big.NewInt(22))
	if g.r.Intn(2) == 0 {
		cond = -1
		selected = parityProgramIntValue(big.NewInt(11))
	}

	g.emitPushInt("cond", big.NewInt(cond))
	g.emitPushInt("cond_x", big.NewInt(11))
	g.emitPushInt("cond_y", big.NewInt(22))
	if checked {
		g.emit("CONDSELCHK", execop.CONDSELCHK().Serialize())
	} else {
		g.emit("CONDSEL", stackop.CONDSEL().Serialize())
	}

	g.stack = g.stack[:base]
	g.stack = append(g.stack, selected)
	return true
}

func (g *parityProgramGenerator) emitLoadDictOp(preload bool) bool {
	root, _, _ := parityProgramDictRoot(false)
	src := cell.BeginCell().MustStoreMaybeRef(root).EndCell().MustBeginParse()
	base := len(g.stack)
	g.emitPushSlice("dict_container", src)
	if preload {
		g.emit("PLDDICT", parityProgramRawOp(0xF405, 16))
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: root})
		return true
	}
	g.emit("LDDICT", parityProgramRawOp(0xF404, 16))
	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramCell, cell: root},
		parityProgramStackValue{kind: parityProgramSlice, slice: cell.BeginCell().EndCell().MustBeginParse()},
	)
	return true
}

func (g *parityProgramGenerator) emitStoreDictOp() bool {
	root, _, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	builder := cell.BeginCell()
	next := cell.BeginCell().MustStoreMaybeRef(root)

	g.emitPushCell("stdict_root", root)
	g.emitPushBuilder("stdict_builder", builder)
	g.emit("STDICT", parityProgramRawOp(0xF400, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: next})
	return true
}

func (g *parityProgramGenerator) emitSkipDictOp() bool {
	root, _, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	src := cell.BeginCell().MustStoreMaybeRef(root).MustStoreUInt(0xA, 4).ToSlice()
	rest := parityProgramTailSlice()

	g.emitPushSlice("skipdict_src", src)
	g.emit("SKIPDICT", parityProgramRawOp(0xF401, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: rest})
	return true
}

func (g *parityProgramGenerator) emitLoadDictSliceOp(preload, quiet bool) bool {
	root, _, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	dictSlice := cell.BeginCell().MustStoreMaybeRef(root).ToSlice()
	src := cell.BeginCell().MustStoreMaybeRef(root).MustStoreUInt(0xA, 4).ToSlice()
	name := "LDDICTS"
	opcode := uint64(0xF402)
	if preload {
		name = "PLDDICTS"
		opcode = 0xF403
	}

	g.emitPushSlice(strings.ToLower(name)+"_src", src)
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: dictSlice})
	if !preload {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramTailSlice()})
	}
	if quiet {
		g.stack = append(g.stack, parityProgramBoolValue(true))
	}
	return true
}

func (g *parityProgramGenerator) emitLoadDictQuietOp(preload bool) bool {
	root, _, _ := parityProgramDictRoot(false)
	src := cell.BeginCell().MustStoreMaybeRef(root).MustStoreUInt(0xA, 4).ToSlice()
	base := len(g.stack)
	name := "LDDICTQ"
	opcode := uint64(0xF406)
	if preload {
		name = "PLDDICTQ"
		opcode = 0xF407
	}

	g.emitPushSlice(strings.ToLower(name)+"_src", src)
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: root})
	if !preload {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramTailSlice()})
	}
	g.stack = append(g.stack, parityProgramBoolValue(true))
	return true
}

func (g *parityProgramGenerator) emitDictGetOp(byRef, intKey, unsigned bool) bool {
	root, value, ref := parityProgramDictRoot(byRef)
	base := len(g.stack)

	key := uint64(0x12)
	opcode := uint64(0xF40A)
	if byRef {
		opcode++
	}
	switch {
	case intKey && unsigned:
		opcode += 4
		g.emitPushInt("dict_key_u", big.NewInt(int64(key)))
	case intKey:
		opcode += 2
		g.emitPushInt("dict_key_i", big.NewInt(int64(key)))
	default:
		g.emitPushSlice("dict_key", parityProgramKeySlice(key, 8))
	}
	g.emitPushCell("dict_root", root)
	g.emitPushInt("dict_bits", big.NewInt(8))
	g.emit("DICTGET", parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	if byRef {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref})
	} else {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()})
	}
	g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitDictSetOp(byRef, unsigned bool) bool {
	base := len(g.stack)
	value := cell.BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	key := uint64(0x21)
	opcode := uint64(0xF412)
	if byRef {
		opcode++
	}
	if unsigned {
		opcode += 4
	}

	dict := cell.NewDict(8)
	var err error
	if byRef {
		ref := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
		g.emitPushCell("dict_set_ref", ref)
		_, err = dict.SetBuilderWithMode(parityProgramKeyCell(key, 8), cell.BeginCell().MustStoreRef(ref), cell.DictSetModeSet)
	} else {
		g.emitPushSlice("dict_set_value", value.MustBeginParse())
		_, err = dict.SetWithMode(parityProgramKeyCell(key, 8), value, cell.DictSetModeSet)
	}
	if err != nil {
		panic(err)
	}
	if unsigned {
		g.emitPushInt("dict_set_key_u", big.NewInt(int64(key)))
	} else {
		g.emitPushSlice("dict_set_key", parityProgramKeySlice(key, 8))
	}
	g.emitPushNull("dict_set_root")
	g.emitPushInt("dict_set_bits", big.NewInt(8))
	g.emit("DICTSET", parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()))
	return true
}

func (g *parityProgramGenerator) emitDictSetGetOp(byRef bool) bool {
	root, oldValue, oldRef := parityProgramDictRoot(byRef)
	base := len(g.stack)
	newValue := cell.BeginCell().MustStoreUInt(0x99, 8).EndCell()
	newRef := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	key := uint64(0x12)
	opcode := uint64(0xF41A)
	if byRef {
		opcode++
	}

	dict := cell.NewDict(8)
	var err error
	if byRef {
		g.emitPushCell("dict_setget_ref", newRef)
		_, err = dict.SetBuilderWithMode(parityProgramKeyCell(key, 8), cell.BeginCell().MustStoreRef(newRef), cell.DictSetModeSet)
	} else {
		g.emitPushSlice("dict_setget_value", newValue.MustBeginParse())
		_, err = dict.SetWithMode(parityProgramKeyCell(key, 8), newValue, cell.DictSetModeSet)
	}
	if err != nil {
		panic(err)
	}
	g.emitPushSlice("dict_setget_key", parityProgramKeySlice(key, 8))
	g.emitPushCell("dict_setget_root", root)
	g.emitPushInt("dict_setget_bits", big.NewInt(8))
	g.emit("DICTSETGET", parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()))
	if byRef {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: oldRef})
	} else {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: oldValue.MustBeginParse()})
	}
	g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitDictReplaceAddOp(add bool) bool {
	root, _, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	value := cell.BeginCell().MustStoreUInt(0x77, 8).EndCell()
	key := uint64(0x12)
	opcode := uint64(0xF422)
	name := "DICTREPLACE"
	if add {
		key = 0x21
		opcode = 0xF432
		name = "DICTADD"
	}

	dict := cell.NewDict(8)
	if _, err := dict.SetWithMode(parityProgramKeyCell(0x12, 8), cell.BeginCell().MustStoreUInt(0x34, 8).EndCell(), cell.DictSetModeSet); err != nil {
		panic(err)
	}
	if _, err := dict.SetWithMode(parityProgramKeyCell(key, 8), value, cell.DictSetModeSet); err != nil {
		panic(err)
	}

	g.emitPushSlice("dict_replace_add_value", value.MustBeginParse())
	g.emitPushSlice("dict_replace_add_key", parityProgramKeySlice(key, 8))
	g.emitPushCell("dict_replace_add_root", root)
	g.emitPushInt("dict_replace_add_bits", big.NewInt(8))
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()), parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitDictDeleteOp(unsigned bool) bool {
	root, _, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	key := uint64(0x12)
	opcode := uint64(0xF459)
	if unsigned {
		opcode += 2
		g.emitPushInt("dict_del_key_u", big.NewInt(int64(key)))
	} else {
		g.emitPushSlice("dict_del_key", parityProgramKeySlice(key, 8))
	}
	g.emitPushCell("dict_del_root", root)
	g.emitPushInt("dict_del_bits", big.NewInt(8))
	g.emit("DICTDEL", parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitDictGetOptRefOp(unsigned bool) bool {
	root, _, ref := parityProgramDictRoot(true)
	base := len(g.stack)
	key := uint64(0x12)
	opcode := uint64(0xF469)
	if unsigned {
		opcode += 2
		g.emitPushInt("dict_getoptref_key_u", big.NewInt(int64(key)))
	} else {
		g.emitPushSlice("dict_getoptref_key", parityProgramKeySlice(key, 8))
	}
	g.emitPushCell("dict_getoptref_root", root)
	g.emitPushInt("dict_getoptref_bits", big.NewInt(8))
	g.emit("DICTGETOPTREF", parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref})
	return true
}

func (g *parityProgramGenerator) emitDictMinMaxOp(fetchMax bool) bool {
	root, value, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	g.emitPushCell("dict_minmax_root", root)
	g.emitPushInt("dict_minmax_bits", big.NewInt(8))
	name := "DICTMIN"
	opcode := uint64(0xF482)
	key := uint64(0x12)
	if fetchMax {
		name = "DICTMAX"
		opcode = 0xF48A
	}
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()},
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(key, 8)},
		parityProgramIntValue(big.NewInt(-1)),
	)
	return true
}

func (g *parityProgramGenerator) emitDictRemMinMaxOp(fetchMax bool) bool {
	root, value, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	g.emitPushCell("dict_rem_minmax_root", root)
	g.emitPushInt("dict_rem_minmax_bits", big.NewInt(8))
	name := "DICTREMMIN"
	opcode := uint64(0xF492)
	key := uint64(0x12)
	if fetchMax {
		name = "DICTREMMAX"
		opcode = 0xF49A
	}
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramNull},
		parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()},
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(key, 8)},
		parityProgramIntValue(big.NewInt(-1)),
	)
	return true
}

func (g *parityProgramGenerator) emitDictNearOp() bool {
	root, value, _ := parityProgramDictRoot(false)
	base := len(g.stack)
	key := uint64(0x12)
	g.emitPushSlice("dict_near_key", parityProgramKeySlice(key, 8))
	g.emitPushCell("dict_near_root", root)
	g.emitPushInt("dict_near_bits", big.NewInt(8))
	g.emit("DICTGETNEXTEQ", parityProgramRawOp(0xF475, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()},
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(key, 8)},
		parityProgramIntValue(big.NewInt(-1)),
	)
	return true
}

func (g *parityProgramGenerator) emitDictBuilderSetOp(mode cell.DictSetMode, kind int) bool {
	base := len(g.stack)
	key := uint64(0x12)
	root, _, _ := parityProgramDictRoot(false)
	if mode == cell.DictSetModeAdd {
		key = 0x21
	}
	value := cell.BeginCell().MustStoreUInt(0x66, 8)
	dict := parityProgramRootAsDict(root, 8)
	changed, err := dict.SetBuilderWithMode(parityProgramDictKeyCellForKind(kind, key, 8), value.Copy(), mode)
	if err != nil {
		panic(err)
	}

	g.emitPushBuilder("dict_setb_value", value)
	g.emitDictKey("dict_setb_key", kind, key, 8)
	g.emitPushCell("dict_setb_root", root)
	g.emitPushInt("dict_setb_bits", big.NewInt(8))
	name, opcode := parityProgramDictBuilderSetNameOpcode(mode, kind)
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()))
	if mode != cell.DictSetModeSet {
		g.stack = append(g.stack, parityProgramBoolValue(changed))
	}
	return true
}

func (g *parityProgramGenerator) emitDictBuilderSetGetOp(mode cell.DictSetMode, kind int) bool {
	base := len(g.stack)
	key := uint64(0x12)
	root, oldValue, _ := parityProgramDictRoot(false)
	if mode == cell.DictSetModeAdd {
		key = 0x21
	}
	value := cell.BeginCell().MustStoreUInt(0x66, 8)
	dict := parityProgramRootAsDict(root, 8)
	old, _, err := dict.LoadValueAndSetBuilderWithMode(parityProgramDictKeyCellForKind(kind, key, 8), value.Copy(), mode)
	if err != nil {
		panic(err)
	}

	g.emitPushBuilder("dict_setgetb_value", value)
	g.emitDictKey("dict_setgetb_key", kind, key, 8)
	g.emitPushCell("dict_setgetb_root", root)
	g.emitPushInt("dict_setgetb_bits", big.NewInt(8))
	name, opcode := parityProgramDictBuilderSetGetNameOpcode(mode, kind)
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()))
	if old != nil {
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: oldValue.MustBeginParse()},
			parityProgramBoolValue(mode != cell.DictSetModeAdd),
		)
	} else {
		g.stack = append(g.stack, parityProgramBoolValue(mode == cell.DictSetModeAdd))
	}
	return true
}

func (g *parityProgramGenerator) emitDictDeleteGetOp(byRef bool, kind int) bool {
	root, value, ref := parityProgramDictRoot(byRef)
	base := len(g.stack)
	key := uint64(0x12)

	g.emitDictKey("dict_delget_key", kind, key, 8)
	g.emitPushCell("dict_delget_root", root)
	g.emitPushInt("dict_delget_bits", big.NewInt(8))
	name := "DICTDELGET"
	if byRef {
		name += "REF"
	}
	g.emit(name, parityProgramRawOp(parityProgramDictValueOpcode(0xF462, kind, byRef), 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
	if byRef {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: ref})
	} else {
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()})
	}
	g.stack = append(g.stack, parityProgramBoolValue(true))
	return true
}

func (g *parityProgramGenerator) emitDictSetGetOptRefOp(del bool, kind int) bool {
	root, _, oldRef := parityProgramDictRoot(true)
	base := len(g.stack)
	key := uint64(0x12)
	newRef := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	dict := cell.NewDict(8)
	if !del {
		if _, err := dict.SetBuilderWithMode(parityProgramDictKeyCellForKind(kind, key, 8), cell.BeginCell().MustStoreRef(newRef), cell.DictSetModeSet); err != nil {
			panic(err)
		}
	}

	if del {
		g.emitPushNull("dict_setgetoptref_nil")
	} else {
		g.emitPushCell("dict_setgetoptref_new", newRef)
	}
	g.emitDictKey("dict_setgetoptref_key", kind, key, 8)
	g.emitPushCell("dict_setgetoptref_root", root)
	g.emitPushInt("dict_setgetoptref_bits", big.NewInt(8))
	g.emit("DICTSETGETOPTREF", parityProgramRawOp(parityProgramDictScalarOpcode(0xF46D, kind), 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramMaybeCellValue(dict.AsCell()),
		parityProgramStackValue{kind: parityProgramCell, cell: oldRef},
	)
	return true
}

func (g *parityProgramGenerator) emitDictNearExtraOp(mode int) bool {
	base := len(g.stack)
	switch mode {
	case 0:
		root, values := parityProgramMultiDictRoot()
		g.emitPushSlice("dict_getnext_key", parityProgramKeySlice(0x20, 8))
		g.emitPushCell("dict_getnext_root", root)
		g.emitPushInt("dict_getnext_bits", big.NewInt(8))
		g.emit("DICTGETNEXT", parityProgramRawOp(0xF474, 16))
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: values[0x30].MustBeginParse()},
			parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(0x30, 8)},
			parityProgramBoolValue(true),
		)
	case 1:
		root, values := parityProgramMultiDictRoot()
		g.emitPushSlice("dict_getpreveq_key", parityProgramKeySlice(0x20, 8))
		g.emitPushCell("dict_getpreveq_root", root)
		g.emitPushInt("dict_getpreveq_bits", big.NewInt(8))
		g.emit("DICTGETPREVEQ", parityProgramRawOp(0xF477, 16))
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: values[0x20].MustBeginParse()},
			parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(0x20, 8)},
			parityProgramBoolValue(true),
		)
	case 2:
		root, values := parityProgramMultiDictRoot()
		g.emitPushInt("dictu_getnext_key", big.NewInt(0x20))
		g.emitPushCell("dictu_getnext_root", root)
		g.emitPushInt("dictu_getnext_bits", big.NewInt(8))
		g.emit("DICTUGETNEXT", parityProgramRawOp(0xF47C, 16))
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: values[0x30].MustBeginParse()},
			parityProgramIntValue(big.NewInt(0x30)),
			parityProgramBoolValue(true),
		)
	default:
		root, values := parityProgramSignedDictRoot()
		g.emitPushInt("dicti_getnext_key", big.NewInt(-1))
		g.emitPushCell("dicti_getnext_root", root)
		g.emitPushInt("dicti_getnext_bits", big.NewInt(8))
		g.emit("DICTIGETNEXT", parityProgramRawOp(0xF478, 16))
		g.stack = g.stack[:base]
		g.stack = append(g.stack,
			parityProgramStackValue{kind: parityProgramSlice, slice: values[3].MustBeginParse()},
			parityProgramIntValue(big.NewInt(3)),
			parityProgramBoolValue(true),
		)
	}
	return true
}

func (g *parityProgramGenerator) emitSubdictOp(removePrefix bool, kind int) bool {
	root := parityProgramSubdictRoot()
	base := len(g.stack)
	prefixBits := uint(3)
	prefix := uint64(0b101)
	dict := parityProgramRootAsDict(root, 8)
	ok, err := dict.CutPrefixSubdict(parityProgramDictKeyCellForKind(kind, prefix, prefixBits), removePrefix)
	if err != nil || !ok {
		panic("invalid subdict fixture")
	}

	g.emitDictKey("subdict_prefix", kind, prefix, prefixBits)
	g.emitPushInt("subdict_prefix_bits", big.NewInt(int64(prefixBits)))
	g.emitPushCell("subdict_root", root)
	g.emitPushInt("subdict_key_bits", big.NewInt(8))
	name := "SUBDICTGET"
	opcode := uint64(0xF4B1)
	if removePrefix {
		name = "SUBDICTRPGET"
		opcode = 0xF4B5
	}
	g.emit(name, parityProgramRawOp(parityProgramDictScalarOpcode(opcode, kind), 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()))
	return true
}

func (g *parityProgramGenerator) emitPrefixDictGetQOp() bool {
	root, value := parityProgramPrefixDictRoot()
	base := len(g.stack)
	input := parityProgramKeySlice(0b1011, 4)
	g.emitPushSlice("pfx_input", input)
	g.emitPushCell("pfx_root", root)
	g.emitPushInt("pfx_bits", big.NewInt(4))
	g.emit("PFXDICTGETQ", parityProgramRawOp(0xF4A8, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(0b10, 2)},
		parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()},
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(0b11, 2)},
		parityProgramIntValue(big.NewInt(-1)),
	)
	return true
}

func (g *parityProgramGenerator) emitPrefixDictGetOp(quiet bool) bool {
	root, value := parityProgramPrefixDictRoot()
	base := len(g.stack)
	input := parityProgramKeySlice(0b1011, 4)
	name := "PFXDICTGET"
	opcode := uint64(0xF4A9)
	if quiet {
		name = "PFXDICTGETQ"
		opcode = 0xF4A8
	}

	g.emitPushSlice(strings.ToLower(name)+"_input", input)
	g.emitPushCell(strings.ToLower(name)+"_root", root)
	g.emitPushInt(strings.ToLower(name)+"_bits", big.NewInt(4))
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack,
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(0b10, 2)},
		parityProgramStackValue{kind: parityProgramSlice, slice: value.MustBeginParse()},
		parityProgramStackValue{kind: parityProgramSlice, slice: parityProgramKeySlice(0b11, 2)},
	)
	if quiet {
		g.stack = append(g.stack, parityProgramBoolValue(true))
	}
	return true
}

func (g *parityProgramGenerator) emitPrefixDictReplaceAddOp(add bool) bool {
	root, oldValue := parityProgramPrefixDictRoot()
	base := len(g.stack)
	key := uint64(0b10)
	value := cell.BeginCell().MustStoreUInt(0xE, 4).EndCell()
	name := "PFXDICTREPLACE"
	opcode := uint64(0xF471)
	if add {
		key = 0b11
		name = "PFXDICTADD"
		opcode = 0xF472
	}

	dict := cell.NewPrefixDict(4)
	if _, err := dict.SetWithMode(parityProgramKeyCell(0b10, 2), oldValue, cell.DictSetModeSet); err != nil {
		panic(err)
	}
	if _, err := dict.SetWithMode(parityProgramKeyCell(key, 2), value, cell.DictSetModeSet); err != nil {
		panic(err)
	}

	g.emitPushSlice(strings.ToLower(name)+"_value", value.MustBeginParse())
	g.emitPushSlice(strings.ToLower(name)+"_key", parityProgramKeySlice(key, 2))
	g.emitPushCell(strings.ToLower(name)+"_root", root)
	g.emitPushInt(strings.ToLower(name)+"_bits", big.NewInt(4))
	g.emit(name, parityProgramRawOp(opcode, 16))

	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()), parityProgramBoolValue(true))
	return true
}

func (g *parityProgramGenerator) emitPrefixDictSetDelOp(del bool) bool {
	root, value := parityProgramPrefixDictRoot()
	base := len(g.stack)
	key := parityProgramKeySlice(0b10, 2)
	if del {
		g.emitPushSlice("pfx_del_key", key)
		g.emitPushCell("pfx_del_root", root)
		g.emitPushInt("pfx_del_bits", big.NewInt(4))
		g.emit("PFXDICTDEL", parityProgramRawOp(0xF473, 16))
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(-1)))
		return true
	}

	dict := cell.NewPrefixDict(4)
	if _, err := dict.SetWithMode(parityProgramKeyCell(0b10, 2), value, cell.DictSetModeSet); err != nil {
		panic(err)
	}
	g.emitPushSlice("pfx_set_value", value.MustBeginParse())
	g.emitPushSlice("pfx_set_key", key)
	g.emitPushNull("pfx_set_root")
	g.emitPushInt("pfx_set_bits", big.NewInt(4))
	g.emit("PFXDICTSET", parityProgramRawOp(0xF470, 16))
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramMaybeCellValue(dict.AsCell()), parityProgramIntValue(big.NewInt(-1)))
	return true
}

func (g *parityProgramGenerator) emitLoadRefOp() bool {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramSlice {
		return false
	}
	sl := g.stack[len(g.stack)-1].slice.Copy()
	if sl.RefsNum() == 0 {
		return false
	}
	ref, err := sl.LoadRefCell()
	if err != nil {
		return false
	}
	g.emit("LDREF", cellsliceop.LDREF().Serialize())
	g.stack[len(g.stack)-1] = parityProgramStackValue{kind: parityProgramCell, cell: ref}
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: sl})
	return true
}

func (g *parityProgramGenerator) emitLoadIntOp(signed bool) bool {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramSlice {
		return false
	}
	sl := g.stack[len(g.stack)-1].slice.Copy()
	if sl.BitsLeft() < 8 {
		return false
	}
	var val *big.Int
	var err error
	name := "LDU8"
	op := cellsliceop.LDU(8).Serialize()
	if signed {
		name = "LDI8"
		op = cellsliceop.LDI(8).Serialize()
		val, err = sl.LoadBigInt(8)
	} else {
		val, err = sl.LoadBigUInt(8)
	}
	if err != nil {
		return false
	}
	g.emit(name, op)
	g.stack[len(g.stack)-1] = parityProgramIntValue(val)
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: sl})
	return true
}

func (g *parityProgramGenerator) emitStoreIntOp(signed bool) bool {
	if len(g.stack) < 2 || g.stack[len(g.stack)-1].kind != parityProgramBuilder || g.stack[len(g.stack)-2].kind != parityProgramInt {
		return false
	}
	val := g.stack[len(g.stack)-2].int
	b := g.stack[len(g.stack)-1].builder.Copy()
	if !b.CanExtendBy(8, 0) {
		return false
	}

	var err error
	name := "STU8"
	op := cellsliceop.STU(8).Serialize()
	if signed {
		if val.Cmp(big.NewInt(-128)) < 0 || val.Cmp(big.NewInt(127)) > 0 {
			return false
		}
		name = "STI8"
		op = cellsliceop.STI(8).Serialize()
		err = b.StoreBigInt(val, 8)
	} else {
		if val.Sign() < 0 || val.Cmp(big.NewInt(255)) > 0 {
			return false
		}
		err = b.StoreBigUInt(val, 8)
	}
	if err != nil {
		return false
	}
	g.emit(name, op)
	g.stack = g.stack[:len(g.stack)-2]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: b})
	return true
}

func (g *parityProgramGenerator) emitStoreRefOp() bool {
	if len(g.stack) < 2 || g.stack[len(g.stack)-1].kind != parityProgramBuilder || g.stack[len(g.stack)-2].kind != parityProgramCell {
		return false
	}
	b := g.stack[len(g.stack)-1].builder.Copy()
	if !b.CanExtendBy(0, 1) {
		return false
	}
	if err := b.StoreRefUncheckedDepth(g.stack[len(g.stack)-2].cell); err != nil {
		return false
	}
	g.emit("STREF", cellsliceop.STREF().Serialize())
	g.stack = g.stack[:len(g.stack)-2]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: b})
	return true
}

func (g *parityProgramGenerator) emitStoreSliceOp() bool {
	if len(g.stack) < 2 || g.stack[len(g.stack)-1].kind != parityProgramBuilder || g.stack[len(g.stack)-2].kind != parityProgramSlice {
		return false
	}
	b := g.stack[len(g.stack)-1].builder.Copy()
	if err := b.StoreBuilderUncheckedDepth(g.stack[len(g.stack)-2].slice.ToBuilder()); err != nil {
		return false
	}
	g.emit("STSLICE", cellsliceop.STSLICE().Serialize())
	g.stack = g.stack[:len(g.stack)-2]
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramBuilder, builder: b})
	return true
}

func (g *parityProgramGenerator) emitBuilderMetaOp(name string, op *cell.Builder, fn func(*cell.Builder) int64) bool {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramBuilder {
		return false
	}
	val := fn(g.stack[len(g.stack)-1].builder)
	g.emit(name, op)
	g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(val))
	return true
}

func (g *parityProgramGenerator) emitPushValueOp() bool {
	switch g.r.Intn(8) {
	case 0:
		g.emit("PUSHNULL", tupleop.PUSHNULL().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
	case 1:
		g.emit("PUSHNAN", mathop.PUSHNAN().Serialize())
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNaN})
	case 2:
		value := uint8(g.r.Intn(12))
		g.emit(fmt.Sprintf("PUSHPOW2(%d)", value+1), mathop.PUSHPOW2(value).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).Lsh(big.NewInt(1), uint(value+1))))
	case 3:
		value := uint8(g.r.Intn(12))
		g.emit(fmt.Sprintf("PUSHPOW2DEC(%d)", value+1), mathop.PUSHPOW2DEC(value).Serialize())
		v := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(value+1)), big.NewInt(1))
		g.stack = append(g.stack, parityProgramIntValue(v))
	case 4:
		value := uint8(g.r.Intn(12))
		g.emit(fmt.Sprintf("PUSHNEGPOW2(%d)", value+1), mathop.PUSHNEGPOW2(value).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(value+1)))))
	default:
		v := g.smallInt()
		g.emit(fmt.Sprintf("PUSHINT(%s)", v.String()), stackop.PUSHINT(v).Serialize())
		g.stack = append(g.stack, parityProgramIntValue(v))
	}
	return true
}

func (g *parityProgramGenerator) emitStackOp() bool {
	switch g.r.Intn(24) {
	case 0:
		g.emit("NOP", stackop.NOP().Serialize())
		return true
	case 1:
		if len(g.stack) < 1 {
			return false
		}
		g.emit("DROP", stackop.DROP().Serialize())
		g.stack = g.stack[:len(g.stack)-1]
	case 2:
		if len(g.stack) < 1 {
			return false
		}
		g.emit("DUP", stackop.DUP().Serialize())
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1]))
	case 3:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("OVER", stackop.OVER().Serialize())
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-2]))
	case 4:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("SWAP", stackop.SWAP().Serialize())
		g.swapDepth(0, 1)
	case 5:
		if len(g.stack) < 3 {
			return false
		}
		g.emit("ROT", stackop.ROT().Serialize())
		n := len(g.stack)
		a, b, c := g.stack[n-3], g.stack[n-2], g.stack[n-1]
		g.stack[n-3], g.stack[n-2], g.stack[n-1] = b, c, a
	case 6:
		if len(g.stack) < 3 {
			return false
		}
		g.emit("ROTREV", stackop.ROTREV().Serialize())
		n := len(g.stack)
		a, b, c := g.stack[n-3], g.stack[n-2], g.stack[n-1]
		g.stack[n-3], g.stack[n-2], g.stack[n-1] = c, a, b
	case 7:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("2DUP", stackop.DUP2().Serialize())
		n := len(g.stack)
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[n-2]), parityProgramCloneValue(g.stack[n-1]))
	case 8:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("TUCK", stackop.TUCK().Serialize())
		n := len(g.stack)
		a, b := g.stack[n-2], g.stack[n-1]
		g.stack[n-2], g.stack[n-1] = b, a
		g.stack = append(g.stack, parityProgramCloneValue(b))
	case 9:
		if len(g.stack) < 1 {
			return false
		}
		idx := g.randomStackIndex(16)
		g.emit(fmt.Sprintf("PUSH(s%d)", idx), stackop.PUSH(uint8(idx)).Serialize())
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1-idx]))
	case 10:
		if len(g.stack) < 1 {
			return false
		}
		idx := g.randomStackIndex(16)
		g.emit(fmt.Sprintf("POP(s%d)", idx), stackop.POP(uint8(idx)).Serialize())
		g.stack[len(g.stack)-1-idx] = parityProgramCloneValue(g.stack[len(g.stack)-1])
		g.stack = g.stack[:len(g.stack)-1]
	case 11:
		if len(g.stack) < 2 {
			return false
		}
		a := g.randomStackIndex(16)
		b := g.randomStackIndex(16)
		if a == b {
			return false
		}
		g.emit(fmt.Sprintf("XCHG(s%d,s%d)", a, b), stackop.XCHG(uint8(a), uint8(b)).Serialize())
		g.swapDepth(a, b)
	case 12:
		if !g.emitReverseOp() {
			return false
		}
	default:
		return g.emitStackExtraOp()
	}
	return true
}

func (g *parityProgramGenerator) emitStackExtraOp() bool {
	switch g.r.Intn(25) {
	case 0:
		g.emit("DEPTH", stackop.DEPTH().Serialize())
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(len(g.stack)))))
	case 1:
		base := len(g.stack)
		count := base
		if count > 8 {
			count = g.r.Intn(9)
		}
		g.emitPushInt("chkdepth_count", big.NewInt(int64(count)))
		g.emit("CHKDEPTH", stackop.CHKDEPTH().Serialize())
		g.stack = g.stack[:base]
	case 2:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("NIP", stackop.NIP().Serialize())
		top := parityProgramCloneValue(g.stack[len(g.stack)-1])
		g.stack[len(g.stack)-2] = top
		g.stack = g.stack[:len(g.stack)-1]
	case 3:
		if len(g.stack) < 2 {
			return false
		}
		g.emit("2DROP", stackop.DROP2().Serialize())
		g.stack = g.stack[:len(g.stack)-2]
	case 4:
		if len(g.stack) < 4 {
			return false
		}
		g.emit("2OVER", stackop.OVER2().Serialize())
		n := len(g.stack)
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[n-4]), parityProgramCloneValue(g.stack[n-3]))
	case 5:
		if len(g.stack) < 4 {
			return false
		}
		g.emit("2SWAP", stackop.SWAP2().Serialize())
		n := len(g.stack)
		a, b, c, d := g.stack[n-4], g.stack[n-3], g.stack[n-2], g.stack[n-1]
		g.stack[n-4], g.stack[n-3], g.stack[n-2], g.stack[n-1] = c, d, a, b
	case 6:
		if len(g.stack) == 0 {
			return false
		}
		base := len(g.stack)
		idx := g.randomStackIndex(8)
		val := parityProgramCloneValue(g.stack[base-1-idx])
		g.emitPushInt("pick_idx", big.NewInt(int64(idx)))
		g.emit("PICK", stackop.PICK().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, val)
	case 7:
		if len(g.stack) == 0 {
			return false
		}
		base := len(g.stack)
		idx := g.randomStackIndex(8)
		g.emitPushInt("roll_idx", big.NewInt(int64(idx)))
		g.emit("ROLL", stackop.ROLL().Serialize())
		g.stack = g.stack[:base]
		g.moveDepthToTop(idx)
	case 8:
		if len(g.stack) == 0 {
			return false
		}
		base := len(g.stack)
		idx := g.randomStackIndex(8)
		g.emitPushInt("rollrev_idx", big.NewInt(int64(idx)))
		g.emit("ROLLREV", stackop.ROLLREV().Serialize())
		g.stack = g.stack[:base]
		g.moveTopToDepth(idx)
	case 9:
		if len(g.stack) == 0 {
			return false
		}
		base := len(g.stack)
		count := g.r.Intn(base + 1)
		if count > 4 {
			count = g.r.Intn(5)
		}
		g.emitPushInt("dropx_count", big.NewInt(int64(count)))
		g.emit("DROPX", stackop.DROPX().Serialize())
		g.stack = g.stack[:base-count]
	case 10:
		if len(g.stack) == 0 {
			return false
		}
		base := len(g.stack)
		idx := g.randomStackIndex(8)
		g.emitPushInt("xchgx_idx", big.NewInt(int64(idx)))
		g.emit("XCHGX", stackop.XCHGX().Serialize())
		g.stack = g.stack[:base]
		g.swapDepth(0, idx)
	case 11:
		if len(g.stack) < 2 {
			return false
		}
		maxX := len(g.stack)
		if maxX > 5 {
			maxX = 5
		}
		y := g.r.Intn(maxX - 1)
		x := 1 + g.r.Intn(maxX-y)
		if x+y > len(g.stack) {
			return false
		}
		base := len(g.stack)
		g.emitPushInt("revx_x", big.NewInt(int64(x)))
		g.emitPushInt("revx_y", big.NewInt(int64(y)))
		g.emit("REVX", stackop.REVX().Serialize())
		g.stack = g.stack[:base]
		g.reverseDepthRange(x+y, y)
	case 12:
		base := len(g.stack)
		if base > maxSmallIndexForParityProgram {
			return false
		}
		g.emitPushInt("onlytopx_count", big.NewInt(int64(base)))
		g.emit("ONLYTOPX", stackop.ONLYTOPX().Serialize())
		g.stack = g.stack[:base]
	case 13:
		base := len(g.stack)
		if base > maxSmallIndexForParityProgram {
			return false
		}
		g.emitPushInt("onlyx_count", big.NewInt(int64(base)))
		g.emit("ONLYX", stackop.ONLYX().Serialize())
		g.stack = g.stack[:base]
	case 14:
		if len(g.stack) == 0 {
			return false
		}
		max := len(g.stack)
		if max > 4 {
			max = 4
		}
		count := g.r.Intn(max + 1)
		g.emit(fmt.Sprintf("BLKDROP(%d)", count), stackop.BLKDROP(uint8(count)).Serialize())
		g.stack = g.stack[:len(g.stack)-count]
	case 15:
		if len(g.stack) < 2 {
			return false
		}
		i := 1 + g.r.Intn(2)
		j := g.r.Intn(3)
		if i+j > len(g.stack) {
			return false
		}
		g.emit(fmt.Sprintf("BLKDROP2(%d,%d)", i, j), stackop.BLKDROP2(uint8(i), uint8(j)).Serialize())
		g.dropMany(i, j)
	case 16:
		if len(g.stack) == 0 {
			return false
		}
		i := 1 + g.r.Intn(2)
		j := g.randomStackIndex(4)
		g.emit(fmt.Sprintf("BLKPUSH(%d,%d)", i, j), stackop.BLKPUSH(uint8(i), uint8(j)).Serialize())
		for x := 0; x < i; x++ {
			g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1-j]))
		}
	case 17:
		if len(g.stack) < 2 {
			return false
		}
		i := 1 + g.r.Intn(2)
		j := 1 + g.r.Intn(2)
		if i+j > len(g.stack) {
			return false
		}
		g.emit(fmt.Sprintf("BLKSWAP(%d,%d)", i, j), stackop.BLKSWAP(uint8(i), uint8(j)).Serialize())
		g.blockSwap(i, j)
	case 18:
		if len(g.stack) < 2 {
			return false
		}
		i := 1 + g.r.Intn(2)
		j := 1 + g.r.Intn(2)
		if i+j > len(g.stack) {
			return false
		}
		base := len(g.stack)
		g.emitPushInt("blkswx_x", big.NewInt(int64(i)))
		g.emitPushInt("blkswx_y", big.NewInt(int64(j)))
		g.emit("BLKSWX", stackop.BLKSWX().Serialize())
		g.stack = g.stack[:base]
		g.blockSwap(i, j)
	case 19:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		first := parityProgramCloneValue(g.stack[len(g.stack)-1-i])
		second := parityProgramCloneValue(g.stack[len(g.stack)-1-j])
		g.emit(fmt.Sprintf("PUSH2(%d,%d)", i, j), stackop.PUSH2(uint8(i), uint8(j)).Serialize())
		g.stack = append(g.stack, first, second)
	case 20:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("XCHG0(%d)", i), stackop.XCHG0(uint8(i)).Serialize())
		g.swapDepth(0, i)
	case 21:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(32)
		g.emit(fmt.Sprintf("XCHG0L(%d)", i), stackop.XCHG0L(uint8(i)).Serialize())
		g.swapDepth(0, i)
	case 22:
		if len(g.stack) < 2 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("XCHG2(%d,%d)", i, j), stackop.XCHG2(uint8(i), uint8(j)).Serialize())
		g.swapDepth(1, i)
		g.swapDepth(0, j)
	case 23:
		if len(g.stack) < 3 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		k := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("XCHG3(%d,%d,%d)", i, j, k), stackop.XCHG3(uint8(i), uint8(j), uint8(k)).Serialize())
		g.swapDepth(2, i)
		g.swapDepth(1, j)
		g.swapDepth(0, k)
	default:
		return g.emitStackExtraPushExchangeOp()
	}
	return true
}

func (g *parityProgramGenerator) emitStackExtraPushExchangeOp() bool {
	pushDepth := func(depth int) {
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1-depth]))
	}

	switch g.r.Intn(9) {
	case 0:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("XCPU(%d,%d)", i, j), stackop.XCPU(uint8(i), uint8(j)).Serialize())
		g.swapDepth(0, i)
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1-j]))
	case 1:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("PUXC(%d,%d)", i, j), stackop.PUXC(uint8(i), uint8(j)).Serialize())
		val := parityProgramCloneValue(g.stack[len(g.stack)-1-i])
		g.stack = append(g.stack, val)
		g.swapDepth(0, 1)
		g.swapDepth(0, j)
	case 2:
		if len(g.stack) == 0 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		k := g.randomStackIndex(8)
		first := parityProgramCloneValue(g.stack[len(g.stack)-1-i])
		second := parityProgramCloneValue(g.stack[len(g.stack)-1-j])
		third := parityProgramCloneValue(g.stack[len(g.stack)-1-k])
		g.emit(fmt.Sprintf("PUSH3(%d,%d,%d)", i, j, k), stackop.PUSH3(uint8(i), uint8(j), uint8(k)).Serialize())
		g.stack = append(g.stack, first, second, third)
	case 3:
		if len(g.stack) < 2 {
			return false
		}
		i := g.randomStackIndex(8)
		j := g.randomStackIndex(8)
		k := g.randomStackIndex(8)
		g.emit(fmt.Sprintf("XC2PU(%d,%d,%d)", i, j, k), stackop.XC2PU(uint8(i), uint8(j), uint8(k)).Serialize())
		g.swapDepth(1, i)
		g.swapDepth(0, j)
		g.stack = append(g.stack, parityProgramCloneValue(g.stack[len(g.stack)-1-k]))
	case 4:
		if len(g.stack) < 5 {
			return false
		}
		g.emit("XCPUXC(3,4,2)", stackop.XCPUXC(3, 4, 2).Serialize())
		g.swapDepth(1, 3)
		pushDepth(4)
		g.swapDepth(0, 1)
		g.swapDepth(0, 2)
	case 5:
		if len(g.stack) < 5 {
			return false
		}
		g.emit("XCPU2(4,3,2)", stackop.XCPU2(4, 3, 2).Serialize())
		g.swapDepth(0, 4)
		pushDepth(3)
		pushDepth(3)
	case 6:
		if len(g.stack) < 5 {
			return false
		}
		g.emit("PUXC2(4,3,2)", stackop.PUXC2(4, 3, 2).Serialize())
		pushDepth(4)
		g.swapDepth(2, 0)
		g.swapDepth(1, 3)
		g.swapDepth(0, 2)
	case 7:
		if len(g.stack) < 5 {
			return false
		}
		g.emit("PUXCPU(4,3,2)", stackop.PUXCPU(4, 3, 2).Serialize())
		pushDepth(4)
		g.swapDepth(0, 1)
		g.swapDepth(0, 3)
		pushDepth(2)
	default:
		if len(g.stack) < 5 {
			return false
		}
		g.emit("PU2XC(4,3,2)", stackop.PU2XC(4, 3, 2).Serialize())
		pushDepth(4)
		g.swapDepth(1, 0)
		pushDepth(3)
		g.swapDepth(1, 0)
		g.swapDepth(0, 2)
	}
	return true
}

func (g *parityProgramGenerator) emitReverseOp() bool {
	if len(g.stack) < 2 {
		return false
	}
	maxY := len(g.stack) - 2
	if maxY > 4 {
		maxY = 4
	}
	y := g.r.Intn(maxY + 1)
	maxX := len(g.stack) - y
	if maxX > 5 {
		maxX = 5
	}
	if maxX < 2 {
		return false
	}
	x := 2 + g.r.Intn(maxX-1)
	g.emit(fmt.Sprintf("REVERSE(%d,%d)", x, y), stackop.REVERSE(uint8(x), uint8(y)).Serialize())
	start := len(g.stack) - x - y
	end := len(g.stack) - y
	for l, r := start, end-1; l < r; l, r = l+1, r-1 {
		g.stack[l], g.stack[r] = g.stack[r], g.stack[l]
	}
	return true
}

func (g *parityProgramGenerator) emitMathOp() bool {
	if len(g.stack) == 0 {
		return false
	}
	if g.stack[len(g.stack)-1].kind == parityProgramNaN {
		g.emit("ISNAN", mathop.ISNAN().Serialize())
		g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(-1))
		return true
	}

	switch g.r.Intn(24) {
	case 0:
		return g.emitUnaryIntOp("NEGATE", mathop.NEGATE().Serialize(), func(x *big.Int) *big.Int { return new(big.Int).Neg(x) })
	case 1:
		return g.emitUnaryIntOp("INC", mathop.INC().Serialize(), func(x *big.Int) *big.Int { return new(big.Int).Add(x, big.NewInt(1)) })
	case 2:
		return g.emitUnaryIntOp("DEC", mathop.DEC().Serialize(), func(x *big.Int) *big.Int { return new(big.Int).Sub(x, big.NewInt(1)) })
	case 3:
		return g.emitUnaryIntOp("ABS", mathop.ABS().Serialize(), func(x *big.Int) *big.Int { return new(big.Int).Abs(x) })
	case 4:
		return g.emitUnaryIntOp("NOT", mathop.NOT().Serialize(), func(x *big.Int) *big.Int { return new(big.Int).Not(x) })
	case 5:
		return g.emitUnaryIntToSmallOp("BITSIZE", mathop.BITSIZE().Serialize())
	case 6:
		return g.emitUnaryIntToSmallOp("ISNPOS", mathop.ISNPOS().Serialize())
	case 7:
		return g.emitUnaryIntToSmallOp("ISZERO", mathop.ISZERO().Serialize())
	case 8:
		return g.emitUnaryIntToSmallOp("ISPOS", mathop.ISPOS().Serialize())
	case 9:
		return g.emitUnaryIntToSmallOp("ISNEG", mathop.ISNEG().Serialize())
	case 10:
		return g.emitBinaryIntOp("ADD", mathop.SUM().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Add(x, y))}
		})
	case 11:
		return g.emitBinaryIntOp("SUB", mathop.SUB().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Sub(x, y))}
		})
	case 12:
		return g.emitBinaryIntOp("SUBR", mathop.SUBR().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Sub(y, x))}
		})
	case 13:
		return g.emitBinaryIntOp("MUL", mathop.MUL().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Mul(x, y))}
		})
	case 14:
		return g.emitBinaryIntOp("AND", mathop.AND().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).And(x, y))}
		})
	case 15:
		return g.emitBinaryIntOp("OR", mathop.OR().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Or(x, y))}
		})
	case 16:
		return g.emitBinaryIntOp("XOR", mathop.XOR().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			return []parityProgramStackValue{parityProgramIntValue(new(big.Int).Xor(x, y))}
		})
	case 17:
		return g.emitMathCompareOrMinMax()
	default:
		return g.emitMathExtraOp()
	}
}

func (g *parityProgramGenerator) emitMathCompareOrMinMax() bool {
	switch g.r.Intn(10) {
	case 0:
		return g.emitBinaryIntOp("MIN", mathop.MIN().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			if x.Cmp(y) <= 0 {
				return []parityProgramStackValue{parityProgramIntValue(x)}
			}
			return []parityProgramStackValue{parityProgramIntValue(y)}
		})
	case 1:
		return g.emitBinaryIntOp("MAX", mathop.MAX().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			if x.Cmp(y) >= 0 {
				return []parityProgramStackValue{parityProgramIntValue(x)}
			}
			return []parityProgramStackValue{parityProgramIntValue(y)}
		})
	case 2:
		return g.emitBinaryIntOp("MINMAX", mathop.MINMAX().Serialize(), func(x, y *big.Int) []parityProgramStackValue {
			if x.Cmp(y) <= 0 {
				return []parityProgramStackValue{parityProgramIntValue(x), parityProgramIntValue(y)}
			}
			return []parityProgramStackValue{parityProgramIntValue(y), parityProgramIntValue(x)}
		})
	case 3:
		return g.emitBinaryIntToSmallOp("LESS", mathop.LESS().Serialize())
	case 4:
		return g.emitBinaryIntToSmallOp("LEQ", mathop.LEQ().Serialize())
	case 5:
		return g.emitBinaryIntToSmallOp("GREATER", mathop.GREATER().Serialize())
	case 6:
		return g.emitBinaryIntToSmallOp("GEQ", mathop.GEQ().Serialize())
	case 7:
		return g.emitBinaryIntToSmallOp("EQUAL", mathop.EQUAL().Serialize())
	case 8:
		return g.emitBinaryIntToSmallOp("NEQ", mathop.NEQ().Serialize())
	default:
		return g.emitBinaryIntToSmallOp("CMP", mathop.CMP().Serialize())
	}
}

func (g *parityProgramGenerator) emitMathExtraOp() bool {
	base := len(g.stack)
	switch g.r.Intn(7) {
	case 0:
		return g.emitMathCompoundOp(g.r.Intn(18))
	case 1:
		return g.emitMathQuietLogicOp(g.r.Intn(27))
	case 2:
		return g.emitMathShiftModOp(g.r.Intn(14))
	case 3:
		return g.emitMathQuietCompoundOp(g.r.Intn(15))
	}

	switch g.r.Intn(32) {
	case 0:
		g.emitPushInt("addint_x", big.NewInt(12))
		g.emit("ADDINT(-5)", mathop.ADDINT(-5).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(7)))
	case 1:
		g.emitPushInt("mulint_x", big.NewInt(7))
		g.emit("MULINT(-3)", mathop.MULINT(-3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-21)))
	case 2:
		g.emitPushInt("lessint_x", big.NewInt(3))
		g.emit("LESSINT(4)", mathop.LESSINT(4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 3:
		g.emitPushInt("eqint_x", big.NewInt(5))
		g.emit("EQINT(5)", mathop.EQINT(5).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 4:
		g.emitPushInt("gtint_x", big.NewInt(8))
		g.emit("GTINT(3)", mathop.GTINT(3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 5:
		g.emitPushInt("neqint_x", big.NewInt(8))
		g.emit("NEQINT(8)", mathop.NEQINT(8).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0)))
	case 6:
		g.emitPushInt("qadd_x", big.NewInt(10))
		g.emitPushInt("qadd_y", big.NewInt(20))
		g.emit("QADD", mathop.QADD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(30)))
	case 7:
		g.emitPushInt("qsub_x", big.NewInt(10))
		g.emitPushInt("qsub_y", big.NewInt(20))
		g.emit("QSUB", mathop.QSUB().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-10)))
	case 8:
		g.emitPushInt("qsubr_x", big.NewInt(10))
		g.emitPushInt("qsubr_y", big.NewInt(20))
		g.emit("QSUBR", mathop.QSUBR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(10)))
	case 9:
		g.emitPushInt("qnegate_x", big.NewInt(-9))
		g.emit("QNEGATE", mathop.QNEGATE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(9)))
	case 10:
		g.emitPushInt("qinc_x", big.NewInt(9))
		g.emit("QINC", mathop.QINC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(10)))
	case 11:
		g.emitPushInt("qdec_x", big.NewInt(9))
		g.emit("QDEC", mathop.QDEC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(8)))
	case 12:
		g.emitPushInt("qaddint_x", big.NewInt(9))
		g.emit("QADDINT(4)", mathop.QADDINT(4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(13)))
	case 13:
		g.emitPushInt("qmulint_x", big.NewInt(9))
		g.emit("QMULINT(-2)", mathop.QMULINT(-2).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-18)))
	case 14:
		g.emitPushInt("qmul_x", big.NewInt(6))
		g.emitPushInt("qmul_y", big.NewInt(7))
		g.emit("QMUL", mathop.QMUL().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(42)))
	case 15:
		g.emitPushInt("qmin_x", big.NewInt(6))
		g.emitPushInt("qmin_y", big.NewInt(7))
		g.emit("QMIN", mathop.QMIN().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(6)))
	case 16:
		g.emitPushInt("qmax_x", big.NewInt(6))
		g.emitPushInt("qmax_y", big.NewInt(7))
		g.emit("QMAX", mathop.QMAX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(7)))
	case 17:
		g.emitPushInt("qminmax_x", big.NewInt(7))
		g.emitPushInt("qminmax_y", big.NewInt(6))
		g.emit("QMINMAX", mathop.QMINMAX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(6)), parityProgramIntValue(big.NewInt(7)))
	case 18:
		g.emitPushInt("qabs_x", big.NewInt(-7))
		g.emit("QABS", mathop.QABS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(7)))
	case 19:
		g.emitPushInt("sgn_x", big.NewInt(-7))
		g.emit("SGN", mathop.SGN().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 20:
		g.emitPushInt("ubitsize_x", big.NewInt(255))
		g.emit("UBITSIZE", mathop.UBITSIZE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(8)))
	case 21:
		g.emitPushInt("qbitsize_x", big.NewInt(-128))
		g.emit("QBITSIZE", mathop.QBITSIZE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(8)))
	case 22:
		g.emitPushInt("qubitsize_x", big.NewInt(255))
		g.emit("QUBITSIZE", mathop.QUBITSIZE().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(8)))
	case 23:
		g.emitPushInt("fits_x", big.NewInt(63))
		g.emit("FITS(7)", mathop.FITS(6).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(63)))
	case 24:
		g.emitPushInt("ufits_x", big.NewInt(255))
		g.emit("UFITS(8)", mathop.UFITS(7).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(255)))
	case 25:
		g.emitPushInt("fitsx_x", big.NewInt(63))
		g.emitPushInt("fitsx_bits", big.NewInt(7))
		g.emit("FITSX", mathop.FITSX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(63)))
	case 26:
		g.emitPushInt("ufitsx_x", big.NewInt(255))
		g.emitPushInt("ufitsx_bits", big.NewInt(8))
		g.emit("UFITSX", mathop.UFITSX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(255)))
	case 27:
		g.emitPushInt("pow2_bits", big.NewInt(5))
		g.emit("POW2", mathop.POW2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(32)))
	case 28:
		g.emitPushInt("lshift_x", big.NewInt(3))
		g.emitPushInt("lshift_bits", big.NewInt(4))
		g.emit("LSHIFT", mathop.LSHIFT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(48)))
	case 29:
		g.emitPushInt("rshift_x", big.NewInt(48))
		g.emitPushInt("rshift_bits", big.NewInt(4))
		g.emit("RSHIFT", mathop.RSHIFT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(3)))
	case 30:
		g.emitPushInt("div_x", big.NewInt(17))
		g.emitPushInt("div_y", big.NewInt(5))
		g.emit("DIV", mathop.DIV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(3)))
	default:
		g.emitPushInt("divmod_x", big.NewInt(17))
		g.emitPushInt("divmod_y", big.NewInt(5))
		if g.r.Intn(2) == 0 {
			g.emit("MOD", mathop.MOD().Serialize())
			g.stack = g.stack[:base]
			g.stack = append(g.stack, parityProgramIntValue(big.NewInt(2)))
			return true
		}
		g.emit("DIVMOD", mathop.DIVMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(3)), parityProgramIntValue(big.NewInt(2)))
	}
	return true
}

func (g *parityProgramGenerator) emitMathCompoundOp(mode int) bool {
	base := len(g.stack)
	divResult := func(x, y *big.Int, rounding int) (*big.Int, *big.Int) {
		x = new(big.Int).Set(x)
		y = new(big.Int).Set(y)
		switch rounding {
		case 0:
			return ophelpers.DivFloor(x, y)
		case 1:
			q := ophelpers.DivRound(x, y)
			return q, new(big.Int).Sub(x, new(big.Int).Mul(y, q))
		default:
			q := ophelpers.DivCeil(x, y)
			return q, new(big.Int).Sub(x, new(big.Int).Mul(y, q))
		}
	}
	pushDivArgs := func(x, y *big.Int) {
		g.emitPushInt("compound_x", x)
		g.emitPushInt("compound_y", y)
	}
	pushMulDivArgs := func(x, y, z *big.Int) {
		g.emitPushInt("compound_x", x)
		g.emitPushInt("compound_y", y)
		g.emitPushInt("compound_z", z)
	}
	pushAddDivArgs := func(x, w, z *big.Int) {
		g.emitPushInt("compound_x", x)
		g.emitPushInt("compound_w", w)
		g.emitPushInt("compound_z", z)
	}
	pushMulAddDivArgs := func(x, y, w, z *big.Int) {
		g.emitPushInt("compound_x", x)
		g.emitPushInt("compound_y", y)
		g.emitPushInt("compound_w", w)
		g.emitPushInt("compound_z", z)
	}
	qrValues := func(q, r *big.Int) []parityProgramStackValue {
		return []parityProgramStackValue{parityProgramIntValue(q), parityProgramIntValue(r)}
	}

	x := big.NewInt(-17)
	y := big.NewInt(5)
	mx := big.NewInt(-7)
	my := big.NewInt(5)
	z := big.NewInt(4)
	w := big.NewInt(2)

	switch mode {
	case 0:
		q, _ := divResult(x, y, 1)
		pushDivArgs(x, y)
		g.emit("DIVR", mathop.DIVR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 1:
		q, _ := divResult(x, y, 2)
		pushDivArgs(x, y)
		g.emit("DIVC", mathop.DIVC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 2:
		_, r := divResult(x, y, 1)
		pushDivArgs(x, y)
		g.emit("MODR", mathop.MODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	case 3:
		_, r := divResult(x, y, 2)
		pushDivArgs(x, y)
		g.emit("MODC", mathop.MODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	case 4:
		q, r := divResult(x, y, 1)
		pushDivArgs(x, y)
		g.emit("DIVMODR", mathop.DIVMODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 5:
		q, r := divResult(x, y, 2)
		pushDivArgs(x, y)
		g.emit("DIVMODC", mathop.DIVMODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 6:
		product := new(big.Int).Mul(mx, my)
		q, _ := divResult(product, z, 0)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIV", mathop.MULDIV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 7:
		product := new(big.Int).Mul(mx, my)
		q, _ := divResult(product, z, 1)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIVR", mathop.MULDIVR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 8:
		product := new(big.Int).Mul(mx, my)
		q, _ := divResult(product, z, 2)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIVC", mathop.MULDIVC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 9:
		product := new(big.Int).Mul(mx, my)
		q, r := divResult(product, z, 0)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIVMOD", mathop.MULDIVMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 10:
		product := new(big.Int).Mul(mx, my)
		q, r := divResult(product, z, 1)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIVMODR", mathop.MULDIVMODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 11:
		product := new(big.Int).Mul(mx, my)
		q, r := divResult(product, z, 2)
		pushMulDivArgs(mx, my, z)
		g.emit("MULDIVMODC", mathop.MULDIVMODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 12:
		sum := new(big.Int).Add(x, w)
		q, r := divResult(sum, y, 0)
		pushAddDivArgs(x, w, y)
		g.emit("ADDDIVMOD", mathop.ADDDIVMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 13:
		sum := new(big.Int).Add(x, w)
		q, r := divResult(sum, y, 1)
		pushAddDivArgs(x, w, y)
		g.emit("ADDDIVMODR", mathop.ADDDIVMODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 14:
		sum := new(big.Int).Add(x, w)
		q, r := divResult(sum, y, 2)
		pushAddDivArgs(x, w, y)
		g.emit("ADDDIVMODC", mathop.ADDDIVMODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 15:
		sum := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := divResult(sum, z, 0)
		pushMulAddDivArgs(mx, my, w, z)
		g.emit("MULADDDIVMOD", mathop.MULADDDIVMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	case 16:
		sum := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := divResult(sum, z, 1)
		pushMulAddDivArgs(mx, my, w, z)
		g.emit("MULADDDIVMODR", mathop.MULADDDIVMODR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	default:
		sum := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := divResult(sum, z, 2)
		pushMulAddDivArgs(mx, my, w, z)
		g.emit("MULADDDIVMODC", mathop.MULADDDIVMODC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, qrValues(q, r)...)
	}
	return true
}

func (g *parityProgramGenerator) emitMathQuietLogicOp(mode int) bool {
	base := len(g.stack)

	switch mode {
	case 0:
		g.emitPushInt("qnot_x", big.NewInt(5))
		g.emit("QNOT", mathop.QNOT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-6)))
	case 1:
		g.emitPushInt("qand_x", big.NewInt(0x0F))
		g.emitPushInt("qand_y", big.NewInt(0x33))
		g.emit("QAND", mathop.QAND().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0x03)))
	case 2:
		g.emitPushInt("qor_x", big.NewInt(0x0F))
		g.emitPushInt("qor_y", big.NewInt(0x30))
		g.emit("QOR", mathop.QOR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0x3F)))
	case 3:
		g.emitPushInt("qxor_x", big.NewInt(0x0F))
		g.emitPushInt("qxor_y", big.NewInt(0x33))
		g.emit("QXOR", mathop.QXOR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(0x3C)))
	case 4:
		g.emitPushInt("qlshift_x", big.NewInt(3))
		g.emitPushInt("qlshift_bits", big.NewInt(4))
		g.emit("QLSHIFT", mathop.QLSHIFT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(48)))
	case 5:
		g.emitPushInt("qrshift_x", big.NewInt(48))
		g.emitPushInt("qrshift_bits", big.NewInt(4))
		g.emit("QRSHIFT", mathop.QRSHIFT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(3)))
	case 6:
		g.emitPushInt("qlshiftcode_x", big.NewInt(3))
		g.emit("QLSHIFTCODE(3)", mathop.QLSHIFTCODE(3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(24)))
	case 7:
		g.emitPushInt("qrshiftcode_x", big.NewInt(48))
		g.emit("QRSHIFTCODE(3)", mathop.QRSHIFTCODE(3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(6)))
	case 8:
		g.emitPushInt("qpow2_bits", big.NewInt(5))
		g.emit("QPOW2", mathop.QPOW2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(32)))
	case 9:
		g.emitPushInt("qsgn_x", big.NewInt(-7))
		g.emit("QSGN", mathop.QSGN().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 10:
		g.emitPushInt("qless_x", big.NewInt(3))
		g.emitPushInt("qless_y", big.NewInt(7))
		g.emit("QLESS", mathop.QLESS().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 11:
		g.emitPushInt("qequal_x", big.NewInt(5))
		g.emitPushInt("qequal_y", big.NewInt(5))
		g.emit("QEQUAL", mathop.QEQUAL().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 12:
		g.emitPushInt("qleq_x", big.NewInt(5))
		g.emitPushInt("qleq_y", big.NewInt(5))
		g.emit("QLEQ", mathop.QLEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 13:
		g.emitPushInt("qgreater_x", big.NewInt(8))
		g.emitPushInt("qgreater_y", big.NewInt(3))
		g.emit("QGREATER", mathop.QGREATER().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 14:
		g.emitPushInt("qneq_x", big.NewInt(8))
		g.emitPushInt("qneq_y", big.NewInt(3))
		g.emit("QNEQ", mathop.QNEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 15:
		g.emitPushInt("qgeq_x", big.NewInt(8))
		g.emitPushInt("qgeq_y", big.NewInt(8))
		g.emit("QGEQ", mathop.QGEQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 16:
		g.emitPushInt("qcmp_x", big.NewInt(3))
		g.emitPushInt("qcmp_y", big.NewInt(7))
		g.emit("QCMP", mathop.QCMP().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(-1)))
	case 17:
		g.emitPushInt("qeqint_x", big.NewInt(5))
		g.emit("QEQINT(5)", mathop.QEQINT(5).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 18:
		g.emitPushInt("qlessint_x", big.NewInt(3))
		g.emit("QLESSINT(4)", mathop.QLESSINT(4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 19:
		g.emitPushInt("qgtint_x", big.NewInt(5))
		g.emit("QGTINT(4)", mathop.QGTINT(4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 20:
		g.emitPushInt("qneqint_x", big.NewInt(5))
		g.emit("QNEQINT(4)", mathop.QNEQINT(4).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramBoolValue(true))
	case 21:
		g.emitPushInt("qfits_x", big.NewInt(63))
		g.emit("QFITS(7)", mathop.QFITS(6).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(63)))
	case 22:
		g.emitPushInt("qfits_fail_x", big.NewInt(8))
		g.emit("QFITS(3 fail)", mathop.QFITS(2).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNaN})
	case 23:
		g.emitPushInt("qufits_x", big.NewInt(255))
		g.emit("QUFITS(8)", mathop.QUFITS(7).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(255)))
	case 24:
		g.emitPushInt("qfitsx_x", big.NewInt(63))
		g.emitPushInt("qfitsx_bits", big.NewInt(7))
		g.emit("QFITSX", mathop.QFITSX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(63)))
	case 25:
		g.emitPushInt("qufitsx_fail_x", big.NewInt(-1))
		g.emitPushInt("qufitsx_fail_bits", big.NewInt(8))
		g.emit("QUFITSX(fail)", mathop.QUFITSX().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNaN})
	default:
		g.emitPushInt("chknan_x", big.NewInt(7))
		g.emit("CHKNAN", mathop.CHKNAN().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(7)))
	}
	return true
}

func (g *parityProgramGenerator) emitMathShiftModOp(mode int) bool {
	base := len(g.stack)
	x := big.NewInt(-17)
	mx := big.NewInt(-7)
	my := big.NewInt(5)
	shift := big.NewInt(2)
	divisor := func(s *big.Int) *big.Int {
		return new(big.Int).Lsh(big.NewInt(1), uint(s.Uint64()))
	}
	round := func(v, d *big.Int, mode int) (*big.Int, *big.Int) {
		switch mode {
		case 0:
			return ophelpers.DivFloor(new(big.Int).Set(v), new(big.Int).Set(d))
		case 1:
			q := ophelpers.DivRound(new(big.Int).Set(v), new(big.Int).Set(d))
			return q, new(big.Int).Sub(new(big.Int).Set(v), new(big.Int).Mul(new(big.Int).Set(d), q))
		default:
			q := ophelpers.DivCeil(new(big.Int).Set(v), new(big.Int).Set(d))
			return q, new(big.Int).Sub(new(big.Int).Set(v), new(big.Int).Mul(new(big.Int).Set(d), q))
		}
	}
	pushXShift := func(name string) {
		g.emitPushInt(name+"_x", x)
		g.emitPushInt(name+"_shift", shift)
	}
	pushMulShift := func(name string) {
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_y", my)
		g.emitPushInt(name+"_shift", shift)
	}
	pushLShiftDiv := func(name string) {
		g.emitPushInt(name+"_x", mx)
		g.emitPushInt(name+"_y", my)
		g.emitPushInt(name+"_shift", shift)
	}

	switch mode {
	case 0:
		pushXShift("rshiftfloor")
		g.emit("RSHIFTFLOOR", mathop.RSHIFTFLOOR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).Rsh(new(big.Int).Set(x), uint(shift.Uint64()))))
	case 1:
		q, _ := round(x, divisor(shift), 1)
		pushXShift("rshiftr")
		g.emit("RSHIFTR", mathop.RSHIFTR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 2:
		q, _ := round(x, divisor(shift), 2)
		pushXShift("rshiftc")
		g.emit("RSHIFTC", mathop.RSHIFTC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 3:
		_, r := round(x, divisor(shift), 0)
		pushXShift("modpow2")
		g.emit("MODPOW2", mathop.MODPOW2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	case 4:
		_, r := round(x, divisor(shift), 1)
		pushXShift("modpow2r")
		g.emit("MODPOW2R", mathop.MODPOW2R().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	case 5:
		_, r := round(x, divisor(shift), 2)
		pushXShift("modpow2c")
		g.emit("MODPOW2C", mathop.MODPOW2C().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(r))
	case 6:
		g.emitPushInt("rshiftcodefloor_x", x)
		g.emit("RSHIFTCODEFLOOR(3)", mathop.RSHIFTCODEFLOOR(3).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(new(big.Int).Rsh(new(big.Int).Set(x), 3)))
	case 7:
		product := new(big.Int).Mul(mx, my)
		q, _ := round(product, divisor(shift), 0)
		pushMulShift("mulrshift")
		g.emit("MULRSHIFT", mathop.MULRSHIFT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 8:
		product := new(big.Int).Mul(mx, my)
		q, _ := round(product, divisor(shift), 1)
		pushMulShift("mulrshiftr")
		g.emit("MULRSHIFTR", mathop.MULRSHIFTR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 9:
		product := new(big.Int).Mul(mx, my)
		q, _ := round(product, divisor(shift), 2)
		pushMulShift("mulrshiftc")
		g.emit("MULRSHIFTC", mathop.MULRSHIFTC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 10:
		dividend := new(big.Int).Mul(mx, divisor(shift))
		q, _ := round(dividend, my, 0)
		pushLShiftDiv("lshiftdiv")
		g.emit("LSHIFTDIV", mathop.LSHIFTDIV().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 11:
		dividend := new(big.Int).Mul(mx, divisor(shift))
		q, _ := round(dividend, my, 1)
		pushLShiftDiv("lshiftdivr")
		g.emit("LSHIFTDIVR", mathop.LSHIFTDIVR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	case 12:
		dividend := new(big.Int).Mul(mx, divisor(shift))
		q, _ := round(dividend, my, 2)
		pushLShiftDiv("lshiftdivc")
		g.emit("LSHIFTDIVC", mathop.LSHIFTDIVC().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q))
	default:
		dividend := new(big.Int).Mul(mx, divisor(shift))
		q, r := round(dividend, my, 0)
		pushLShiftDiv("lshiftdivmod")
		g.emit("LSHIFTDIVMOD", mathop.LSHIFTDIVMOD().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(q), parityProgramIntValue(r))
	}
	return true
}

func (g *parityProgramGenerator) emitMathQuietCompoundOp(mode int) bool {
	base := len(g.stack)
	x := big.NewInt(-17)
	y := big.NewInt(5)
	mx := big.NewInt(-7)
	my := big.NewInt(5)
	w := big.NewInt(2)
	z := big.NewInt(4)
	shift := big.NewInt(2)
	round := func(v, d *big.Int, args uint8) (*big.Int, *big.Int) {
		switch args & 3 {
		case 0:
			return ophelpers.DivFloor(new(big.Int).Set(v), new(big.Int).Set(d))
		case 1:
			q := ophelpers.DivRound(new(big.Int).Set(v), new(big.Int).Set(d))
			return q, new(big.Int).Sub(new(big.Int).Set(v), new(big.Int).Mul(new(big.Int).Set(d), q))
		default:
			q := ophelpers.DivCeil(new(big.Int).Set(v), new(big.Int).Set(d))
			return q, new(big.Int).Sub(new(big.Int).Set(v), new(big.Int).Mul(new(big.Int).Set(d), q))
		}
	}
	emit := func(name string, prefix uint64, args uint8) {
		g.emit(name, parityProgramRawOp((prefix<<4)|uint64(args), 24))
	}
	pushSelected := func(args uint8, q, r *big.Int) {
		d := (args >> 2) & 3
		if d == 0 {
			d = 3
		}
		if d&1 != 0 {
			g.stack = append(g.stack, parityProgramIntValue(q))
		}
		if d&2 != 0 {
			g.stack = append(g.stack, parityProgramIntValue(r))
		}
	}
	divider := func(bits *big.Int) *big.Int {
		return new(big.Int).Lsh(big.NewInt(1), uint(bits.Uint64()))
	}

	switch mode {
	case 0:
		args := uint8(4)
		q, r := round(x, y, args)
		g.emitPushInt("qdiv_x", x)
		g.emitPushInt("qdiv_y", y)
		emit("QDIV", 0xB7A90, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 1:
		args := uint8(9)
		q, r := round(x, y, args)
		g.emitPushInt("qmodr_x", x)
		g.emitPushInt("qmodr_y", y)
		emit("QMODR", 0xB7A90, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 2:
		args := uint8(2)
		dividend := new(big.Int).Add(new(big.Int).Set(x), w)
		q, r := round(dividend, y, args)
		g.emitPushInt("qadddivmodc_x", x)
		g.emitPushInt("qadddivmodc_w", w)
		g.emitPushInt("qadddivmodc_y", y)
		emit("QADDDIVMODC", 0xB7A90, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 3:
		args := uint8(4)
		q, r := round(x, divider(shift), args)
		g.emitPushInt("qrshift_x", x)
		g.emitPushInt("qrshift_shift", shift)
		emit("QRSHIFT", 0xB7A92, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 4:
		args := uint8(9)
		q, r := round(x, divider(shift), args)
		g.emitPushInt("qmodpow2r_x", x)
		g.emitPushInt("qmodpow2r_shift", shift)
		emit("QMODPOW2R", 0xB7A92, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 5:
		args := uint8(2)
		dividend := new(big.Int).Add(new(big.Int).Set(x), w)
		q, r := round(dividend, divider(shift), args)
		g.emitPushInt("qaddrshiftmodc_x", x)
		g.emitPushInt("qaddrshiftmodc_w", w)
		g.emitPushInt("qaddrshiftmodc_shift", shift)
		emit("QADDRSHIFTMODC", 0xB7A92, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 6:
		args := uint8(4)
		dividend := new(big.Int).Mul(mx, my)
		q, r := round(dividend, z, args)
		g.emitPushInt("qmuldiv_x", mx)
		g.emitPushInt("qmuldiv_y", my)
		g.emitPushInt("qmuldiv_z", z)
		emit("QMULDIV", 0xB7A98, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 7:
		args := uint8(9)
		dividend := new(big.Int).Mul(mx, my)
		q, r := round(dividend, z, args)
		g.emitPushInt("qmulmodr_x", mx)
		g.emitPushInt("qmulmodr_y", my)
		g.emitPushInt("qmulmodr_z", z)
		emit("QMULMODR", 0xB7A98, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 8:
		args := uint8(2)
		dividend := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := round(dividend, z, args)
		g.emitPushInt("qmuladddivmodc_x", mx)
		g.emitPushInt("qmuladddivmodc_y", my)
		g.emitPushInt("qmuladddivmodc_w", w)
		g.emitPushInt("qmuladddivmodc_z", z)
		emit("QMULADDDIVMODC", 0xB7A98, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 9:
		args := uint8(4)
		dividend := new(big.Int).Mul(mx, my)
		q, r := round(dividend, divider(shift), args)
		g.emitPushInt("qmulrshift_x", mx)
		g.emitPushInt("qmulrshift_y", my)
		g.emitPushInt("qmulrshift_shift", shift)
		emit("QMULRSHIFT", 0xB7A9A, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 10:
		args := uint8(9)
		dividend := new(big.Int).Mul(mx, my)
		q, r := round(dividend, divider(shift), args)
		g.emitPushInt("qmulmodpow2r_x", mx)
		g.emitPushInt("qmulmodpow2r_y", my)
		g.emitPushInt("qmulmodpow2r_shift", shift)
		emit("QMULMODPOW2R", 0xB7A9A, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 11:
		args := uint8(2)
		dividend := new(big.Int).Add(new(big.Int).Mul(mx, my), w)
		q, r := round(dividend, divider(shift), args)
		g.emitPushInt("qmuladdrshiftmodc_x", mx)
		g.emitPushInt("qmuladdrshiftmodc_y", my)
		g.emitPushInt("qmuladdrshiftmodc_w", w)
		g.emitPushInt("qmuladdrshiftmodc_shift", shift)
		emit("QMULADDRSHIFTMODC", 0xB7A9A, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 12:
		args := uint8(4)
		dividend := new(big.Int).Lsh(new(big.Int).Set(x), uint(shift.Uint64()))
		q, r := round(dividend, y, args)
		g.emitPushInt("qlshiftdiv_x", x)
		g.emitPushInt("qlshiftdiv_y", y)
		g.emitPushInt("qlshiftdiv_shift", shift)
		emit("QLSHIFTDIV", 0xB7A9C, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	case 13:
		args := uint8(9)
		dividend := new(big.Int).Lsh(new(big.Int).Set(x), uint(shift.Uint64()))
		q, r := round(dividend, y, args)
		g.emitPushInt("qlshiftmodr_x", x)
		g.emitPushInt("qlshiftmodr_y", y)
		g.emitPushInt("qlshiftmodr_shift", shift)
		emit("QLSHIFTMODR", 0xB7A9C, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	default:
		args := uint8(2)
		dividend := new(big.Int).Lsh(new(big.Int).Set(x), uint(shift.Uint64()))
		dividend.Add(dividend, w)
		q, r := round(dividend, y, args)
		g.emitPushInt("qlshiftadddivmodc_x", x)
		g.emitPushInt("qlshiftadddivmodc_w", w)
		g.emitPushInt("qlshiftadddivmodc_y", y)
		g.emitPushInt("qlshiftadddivmodc_shift", shift)
		emit("QLSHIFTADDDIVMODC", 0xB7A9C, args)
		g.stack = g.stack[:base]
		pushSelected(args, q, r)
	}
	return true
}

func (g *parityProgramGenerator) emitTupleOp() bool {
	switch g.r.Intn(20) {
	case 0:
		if len(g.stack) == 0 {
			return false
		}
		g.emit("ISNULL", tupleop.ISNULL().Serialize())
		g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(0))
	case 1:
		if len(g.stack) == 0 {
			return false
		}
		g.emit("ISTUPLE", tupleop.ISTUPLE().Serialize())
		g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(0))
	case 2:
		if len(g.stack) == 0 {
			return false
		}
		g.emit("QTLEN", tupleop.QTLEN().Serialize())
		g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(0))
	case 3:
		n := g.r.Intn(5)
		if n > len(g.stack) {
			n = len(g.stack)
		}
		g.emit(fmt.Sprintf("TUPLE(%d)", n), tupleop.TUPLE(uint8(n)).Serialize())
		items := parityProgramCloneStack(g.stack[len(g.stack)-n:])
		g.stack = g.stack[:len(g.stack)-n]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramTuple, tuple: items})
	case 4:
		top := g.topTuple()
		if top == nil || len(top.tuple) > 15 {
			return false
		}
		n := len(top.tuple)
		g.emit(fmt.Sprintf("UNTUPLE(%d)", n), tupleop.UNTUPLE(uint8(n)).Serialize())
		items := parityProgramCloneStack(top.tuple)
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, items...)
	case 5:
		top := g.topTuple()
		if top == nil {
			return false
		}
		n := 0
		if len(top.tuple) > 0 {
			max := len(top.tuple)
			if max > 15 {
				max = 15
			}
			n = g.r.Intn(max + 1)
		}
		g.emit(fmt.Sprintf("UNPACKFIRST(%d)", n), tupleop.UNPACKFIRST(uint8(n)).Serialize())
		items := parityProgramCloneStack(top.tuple[:n])
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, items...)
	case 6:
		top := g.topTuple()
		if top == nil || len(top.tuple) == 0 {
			return false
		}
		max := len(top.tuple)
		if max > 16 {
			max = 16
		}
		idx := g.r.Intn(max)
		g.emit(fmt.Sprintf("INDEX(%d)", idx), tupleop.INDEX(uint8(idx)).Serialize())
		item := parityProgramCloneValue(top.tuple[idx])
		g.stack[len(g.stack)-1] = item
	case 7:
		if len(g.stack) == 0 {
			return false
		}
		idx := g.r.Intn(6)
		g.emit(fmt.Sprintf("INDEXQ(%d)", idx), tupleop.INDEXQ(uint8(idx)).Serialize())
		top := g.stack[len(g.stack)-1]
		if top.kind == parityProgramTuple && idx < len(top.tuple) {
			g.stack[len(g.stack)-1] = parityProgramCloneValue(top.tuple[idx])
		} else {
			g.stack[len(g.stack)-1] = parityProgramStackValue{kind: parityProgramNull}
		}
	case 8:
		top := g.topTuple()
		if top == nil {
			return false
		}
		g.emit("TLEN", tupleop.TLEN().Serialize())
		g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(int64(len(top.tuple))))
	case 9:
		top := g.topTuple()
		if top == nil || len(top.tuple) == 0 {
			return false
		}
		g.emit("LAST", tupleop.LAST().Serialize())
		g.stack[len(g.stack)-1] = parityProgramCloneValue(top.tuple[len(top.tuple)-1])
	case 10:
		top := g.topTuple()
		if top == nil || len(top.tuple) > 15 {
			return false
		}
		max := len(top.tuple)
		g.emit(fmt.Sprintf("EXPLODE(%d)", max), tupleop.EXPLODE(uint8(max)).Serialize())
		items := parityProgramCloneStack(top.tuple)
		g.stack = g.stack[:len(g.stack)-1]
		g.stack = append(g.stack, items...)
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(int64(max))))
	case 11:
		if len(g.stack) < 2 || g.stack[len(g.stack)-2].kind != parityProgramTuple || len(g.stack[len(g.stack)-2].tuple) >= 254 {
			return false
		}
		g.emit("TPUSH", tupleop.TPUSH().Serialize())
		val := parityProgramCloneValue(g.stack[len(g.stack)-1])
		tup := parityProgramCloneValue(g.stack[len(g.stack)-2])
		tup.tuple = append(tup.tuple, val)
		g.stack = g.stack[:len(g.stack)-2]
		g.stack = append(g.stack, tup)
	case 12:
		top := g.topTuple()
		if top == nil || len(top.tuple) == 0 {
			return false
		}
		g.emit("TPOP", tupleop.TPOP().Serialize())
		tup := parityProgramCloneValue(*top)
		val := parityProgramCloneValue(tup.tuple[len(tup.tuple)-1])
		tup.tuple = tup.tuple[:len(tup.tuple)-1]
		g.stack[len(g.stack)-1] = tup
		g.stack = append(g.stack, val)
	case 13:
		if len(g.stack) < 2 || g.stack[len(g.stack)-2].kind != parityProgramTuple || len(g.stack[len(g.stack)-2].tuple) == 0 {
			return false
		}
		max := len(g.stack[len(g.stack)-2].tuple)
		if max > 16 {
			max = 16
		}
		idx := g.r.Intn(max)
		g.emit(fmt.Sprintf("SETINDEX(%d)", idx), tupleop.SETINDEX(uint8(idx)).Serialize())
		val := parityProgramCloneValue(g.stack[len(g.stack)-1])
		tup := parityProgramCloneValue(g.stack[len(g.stack)-2])
		tup.tuple[idx] = val
		g.stack = g.stack[:len(g.stack)-2]
		g.stack = append(g.stack, tup)
	default:
		return g.emitTupleExtraOp()
	}
	return true
}

func (g *parityProgramGenerator) emitTupleExtraOp() bool {
	base := len(g.stack)
	two := parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(11)),
			parityProgramIntValue(big.NewInt(22)),
		},
	}
	three := parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(11)),
			parityProgramIntValue(big.NewInt(22)),
			parityProgramIntValue(big.NewInt(33)),
		},
	}

	switch g.r.Intn(16) {
	case 0:
		g.emitPushInt("tuplevar_0", big.NewInt(11))
		g.emitPushInt("tuplevar_1", big.NewInt(22))
		g.emitPushInt("tuplevar_count", big.NewInt(2))
		g.emit("TUPLEVAR", tupleop.TUPLEVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, two)
	case 1:
		g.emitPushTuple("tuple_indexvar", two)
		g.emitPushInt("indexvar_idx", big.NewInt(1))
		g.emit("INDEXVAR", tupleop.INDEXVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(22)))
	case 2:
		g.emitPushTuple("tuple_indexvarq", two)
		g.emitPushInt("indexvarq_idx", big.NewInt(1))
		g.emit("INDEXVARQ", tupleop.INDEXVARQ().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(22)))
	case 3:
		g.emitPushTuple("tuple_untuplevar", two)
		g.emitPushInt("untuplevar_count", big.NewInt(2))
		g.emit("UNTUPLEVAR", tupleop.UNTUPLEVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(11)), parityProgramIntValue(big.NewInt(22)))
	case 4:
		g.emitPushTuple("tuple_unpackfirstvar", three)
		g.emitPushInt("unpackfirstvar_count", big.NewInt(2))
		g.emit("UNPACKFIRSTVAR", tupleop.UNPACKFIRSTVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(11)), parityProgramIntValue(big.NewInt(22)))
	case 5:
		g.emitPushTuple("tuple_explodevar", two)
		g.emitPushInt("explodevar_max", big.NewInt(2))
		g.emit("EXPLODEVAR", tupleop.EXPLODEVAR().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(11)), parityProgramIntValue(big.NewInt(22)), parityProgramIntValue(big.NewInt(2)))
	case 6:
		nested := parityProgramStackValue{
			kind:  parityProgramTuple,
			tuple: []parityProgramStackValue{two},
		}
		g.emitPushTuple("tuple_index2", nested)
		g.emit("INDEX2(0,1)", tupleop.INDEX2(0, 1).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(22)))
	case 7:
		nested := parityProgramStackValue{
			kind: parityProgramTuple,
			tuple: []parityProgramStackValue{
				{kind: parityProgramTuple, tuple: []parityProgramStackValue{two}},
			},
		}
		g.emitPushTuple("tuple_index3", nested)
		g.emit("INDEX3(0,0,1)", tupleop.INDEX3(0, 0, 1).Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramIntValue(big.NewInt(22)))
	case 8:
		g.emitPushTuple("tuple_setindexq", two)
		g.emitPushInt("setindexq_value", big.NewInt(99))
		g.emit("SETINDEXQ(1)", tupleop.SETINDEXQ(1).Serialize())
		next := parityProgramCloneValue(two)
		next.tuple[1] = parityProgramIntValue(big.NewInt(99))
		g.stack = g.stack[:base]
		g.stack = append(g.stack, next)
	case 9:
		g.emitPushTuple("tuple_setindexvar", two)
		g.emitPushInt("setindexvar_value", big.NewInt(99))
		g.emitPushInt("setindexvar_idx", big.NewInt(1))
		g.emit("SETINDEXVAR", tupleop.SETINDEXVAR().Serialize())
		next := parityProgramCloneValue(two)
		next.tuple[1] = parityProgramIntValue(big.NewInt(99))
		g.stack = g.stack[:base]
		g.stack = append(g.stack, next)
	case 10:
		g.emitPushTuple("tuple_setindexvarq", two)
		g.emitPushInt("setindexvarq_value", big.NewInt(99))
		g.emitPushInt("setindexvarq_idx", big.NewInt(1))
		g.emit("SETINDEXVARQ", tupleop.SETINDEXVARQ().Serialize())
		next := parityProgramCloneValue(two)
		next.tuple[1] = parityProgramIntValue(big.NewInt(99))
		g.stack = g.stack[:base]
		g.stack = append(g.stack, next)
	case 11:
		g.emitPushInt("nullswapif_cond", big.NewInt(1))
		g.emit("NULLSWAPIF", tupleop.NULLSWAPIF().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(1)))
	case 12:
		g.emitPushInt("nullswapifnot_cond", big.NewInt(0))
		g.emit("NULLSWAPIFNOT", tupleop.NULLSWAPIFNOT().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(0)))
	case 13:
		g.emitPushInt("nullrotrif_payload", big.NewInt(7))
		g.emitPushInt("nullrotrif_cond", big.NewInt(1))
		g.emit("NULLROTRIF", tupleop.NULLROTRIF().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(7)), parityProgramIntValue(big.NewInt(1)))
	case 14:
		g.emitPushInt("nullswapif2_cond", big.NewInt(1))
		g.emit("NULLSWAPIF2", tupleop.NULLSWAPIF2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(1)))
	default:
		g.emitPushInt("nullrotrifnot2_payload", big.NewInt(7))
		g.emitPushInt("nullrotrifnot2_cond", big.NewInt(0))
		g.emit("NULLROTRIFNOT2", tupleop.NULLROTRIFNOT2().Serialize())
		g.stack = g.stack[:base]
		g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull}, parityProgramStackValue{kind: parityProgramNull}, parityProgramIntValue(big.NewInt(7)), parityProgramIntValue(big.NewInt(0)))
	}
	return true
}

func (g *parityProgramGenerator) emitUnaryIntOp(name string, op *cell.Builder, fn func(*big.Int) *big.Int) bool {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramInt {
		return false
	}
	next := parityProgramIntValue(fn(g.stack[len(g.stack)-1].int))
	if !parityProgramValueIsSafe(next) {
		return false
	}
	g.emit(name, op)
	g.stack[len(g.stack)-1] = next
	return true
}

func (g *parityProgramGenerator) emitUnaryIntToSmallOp(name string, op *cell.Builder) bool {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramInt {
		return false
	}
	g.emit(name, op)
	g.stack[len(g.stack)-1] = parityProgramIntValue(big.NewInt(0))
	return true
}

func (g *parityProgramGenerator) emitBinaryIntOp(name string, op *cell.Builder, fn func(x, y *big.Int) []parityProgramStackValue) bool {
	if len(g.stack) < 2 || g.stack[len(g.stack)-1].kind != parityProgramInt || g.stack[len(g.stack)-2].kind != parityProgramInt {
		return false
	}
	x := new(big.Int).Set(g.stack[len(g.stack)-2].int)
	y := new(big.Int).Set(g.stack[len(g.stack)-1].int)
	next := fn(x, y)
	if !parityProgramStackIsSafe(next) {
		return false
	}
	g.emit(name, op)
	g.stack = g.stack[:len(g.stack)-2]
	g.stack = append(g.stack, next...)
	return true
}

func (g *parityProgramGenerator) emitBinaryIntToSmallOp(name string, op *cell.Builder) bool {
	return g.emitBinaryIntOp(name, op, func(_, _ *big.Int) []parityProgramStackValue {
		return []parityProgramStackValue{parityProgramIntValue(big.NewInt(0))}
	})
}

func (g *parityProgramGenerator) emit(name string, op *cell.Builder) {
	g.ops = append(g.ops, op)
	g.trace = append(g.trace, name)
}

func (g *parityProgramGenerator) emitPushInt(name string, v *big.Int) {
	g.emit(fmt.Sprintf("PUSHINT(%s:%s)", name, v.String()), stackop.PUSHINT(v).Serialize())
	g.stack = append(g.stack, parityProgramIntValue(v))
}

func (g *parityProgramGenerator) emitPushNull(name string) {
	g.emit("PUSHNULL("+name+")", tupleop.PUSHNULL().Serialize())
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramNull})
}

func (g *parityProgramGenerator) emitPushCell(name string, cl *cell.Cell) {
	g.emit("PUSHREF("+name+")", stackop.PUSHREF(cl).Serialize())
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramCell, cell: cl})
}

func (g *parityProgramGenerator) emitPushSlice(name string, sl *cell.Slice) {
	g.emit("PUSHSLICE("+name+")", stackop.PUSHSLICE(sl).Serialize())
	g.stack = append(g.stack, parityProgramStackValue{kind: parityProgramSlice, slice: sl.Copy()})
}

func (g *parityProgramGenerator) emitPushTuple(name string, tup parityProgramStackValue) {
	base := len(g.stack)
	for i := range tup.tuple {
		g.emitPushLiteral(fmt.Sprintf("%s_%d", name, i), tup.tuple[i])
	}
	g.emit(fmt.Sprintf("TUPLE(%s:%d)", name, len(tup.tuple)), tupleop.TUPLE(uint8(len(tup.tuple))).Serialize())
	g.stack = g.stack[:base]
	g.stack = append(g.stack, parityProgramCloneValue(tup))
}

func (g *parityProgramGenerator) emitPushLiteral(name string, val parityProgramStackValue) {
	switch val.kind {
	case parityProgramInt:
		g.emitPushInt(name, val.int)
	case parityProgramNull:
		g.emitPushNull(name)
	case parityProgramTuple:
		g.emitPushTuple(name, val)
	default:
		panic("unsupported parity program tuple literal")
	}
}

func (g *parityProgramGenerator) topTuple() *parityProgramStackValue {
	if len(g.stack) == 0 || g.stack[len(g.stack)-1].kind != parityProgramTuple {
		return nil
	}
	return &g.stack[len(g.stack)-1]
}

func (g *parityProgramGenerator) randomStackIndex(limit int) int {
	n := len(g.stack)
	if n > limit {
		n = limit
	}
	return g.r.Intn(n)
}

func (g *parityProgramGenerator) swapDepth(a, b int) {
	ai := len(g.stack) - 1 - a
	bi := len(g.stack) - 1 - b
	g.stack[ai], g.stack[bi] = g.stack[bi], g.stack[ai]
}

func (g *parityProgramGenerator) moveDepthToTop(depth int) {
	idx := len(g.stack) - 1 - depth
	val := g.stack[idx]
	copy(g.stack[idx:], g.stack[idx+1:])
	g.stack[len(g.stack)-1] = val
}

func (g *parityProgramGenerator) moveTopToDepth(depth int) {
	if depth == 0 {
		return
	}
	idx := len(g.stack) - 1 - depth
	val := g.stack[len(g.stack)-1]
	copy(g.stack[idx+1:], g.stack[idx:len(g.stack)-1])
	g.stack[idx] = val
}

func (g *parityProgramGenerator) reverseDepthRange(from, to int) {
	for l, r := len(g.stack)-from, len(g.stack)-to-1; l < r; l, r = l+1, r-1 {
		g.stack[l], g.stack[r] = g.stack[r], g.stack[l]
	}
}

func (g *parityProgramGenerator) dropMany(num, offs int) {
	if num == 0 {
		return
	}
	end := len(g.stack)
	copy(g.stack[end-(num+offs):end-num], g.stack[end-offs:end])
	g.stack = g.stack[:end-num]
}

func (g *parityProgramGenerator) blockSwap(x, y int) {
	if x == 0 || y == 0 {
		return
	}
	g.reverseDepthRange(x+y, y)
	g.reverseDepthRange(y, 0)
	g.reverseDepthRange(x+y, 0)
}

func (g *parityProgramGenerator) smallInt() *big.Int {
	edges := []int64{-17, -8, -3, -1, 0, 1, 2, 3, 7, 15, 31}
	if g.r.Intn(4) == 0 {
		return big.NewInt(int64(g.r.Intn(41) - 20))
	}
	return big.NewInt(edges[g.r.Intn(len(edges))])
}

func (g *parityProgramGenerator) randomCell() *cell.Cell {
	return g.randomBuilder().EndCell()
}

func (g *parityProgramGenerator) randomBuilder() *cell.Builder {
	bits := uint([]int{0, 1, 8, 16, 32}[g.r.Intn(5)])
	b := cell.BeginCell()
	if bits > 0 {
		b.MustStoreSlice(matrixPattern(bits), bits)
	}
	for i := 0; i < g.r.Intn(3); i++ {
		b.MustStoreRef(cell.BeginCell().MustStoreUInt(uint64(g.r.Intn(256)), 8).EndCell())
	}
	return b
}

func parityProgramRawOp(value uint64, bits uint) *cell.Builder {
	return cell.BeginCell().MustStoreUInt(value, bits)
}

func parityProgramCodeCell(builders ...*cell.Builder) *cell.Cell {
	code := cell.BeginCell()
	for _, builder := range builders {
		code.MustStoreBuilder(builder)
	}
	return code.EndCell()
}

func parityProgramKeyCell(value uint64, bits uint) *cell.Cell {
	return cell.BeginCell().MustStoreUInt(value, bits).EndCell()
}

func parityProgramKeySlice(value uint64, bits uint) *cell.Slice {
	return parityProgramKeyCell(value, bits).MustBeginParse()
}

func parityProgramBitsSlice(value uint64, bits uint) *cell.Slice {
	return cell.BeginCell().MustStoreUInt(value, bits).ToSlice()
}

func parityProgramByteSlice(data []byte) *cell.Slice {
	return cell.BeginCell().MustStoreSlice(data, uint(len(data)*8)).ToSlice()
}

func parityProgramDecodeLEInt(data []byte, unsigned bool) *big.Int {
	if unsigned {
		if len(data) == 4 {
			return new(big.Int).SetUint64(uint64(binary.LittleEndian.Uint32(data)))
		}
		return new(big.Int).SetUint64(binary.LittleEndian.Uint64(data))
	}
	if len(data) == 4 {
		return big.NewInt(int64(int32(binary.LittleEndian.Uint32(data))))
	}
	return big.NewInt(int64(binary.LittleEndian.Uint64(data)))
}

func parityProgramEncodeLEInt(v *big.Int, bytesLen int, unsigned bool) []byte {
	out := make([]byte, bytesLen)
	if unsigned {
		if bytesLen == 4 {
			binary.LittleEndian.PutUint32(out, uint32(v.Uint64()))
		} else {
			binary.LittleEndian.PutUint64(out, v.Uint64())
		}
		return out
	}
	if bytesLen == 4 {
		binary.LittleEndian.PutUint32(out, uint32(int32(v.Int64())))
	} else {
		binary.LittleEndian.PutUint64(out, uint64(v.Int64()))
	}
	return out
}

func parityProgramRefSlice() (*cell.Slice, *cell.Cell, *cell.Cell) {
	first := cell.BeginCell().MustStoreUInt(0xA, 4).EndCell()
	second := cell.BeginCell().MustStoreUInt(0xB, 4).EndCell()
	return cell.BeginCell().
		MustStoreUInt(0b101101, 6).
		MustStoreRef(first).
		MustStoreRef(second).
		ToSlice(), first, second
}

func parityProgramDictRoot(byRef bool) (*cell.Cell, *cell.Cell, *cell.Cell) {
	key := parityProgramKeyCell(0x12, 8)
	value := cell.BeginCell().MustStoreUInt(0x34, 8).EndCell()
	ref := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	dict := cell.NewDict(8)

	var err error
	if byRef {
		_, err = dict.SetBuilderWithMode(key, cell.BeginCell().MustStoreRef(ref), cell.DictSetModeSet)
	} else {
		_, err = dict.SetWithMode(key, value, cell.DictSetModeSet)
	}
	if err != nil {
		panic(err)
	}
	return dict.AsCell(), value, ref
}

func parityProgramRootAsDict(root *cell.Cell, bits uint) *cell.Dictionary {
	if root == nil {
		return cell.NewDict(bits)
	}
	return root.AsDict(bits)
}

func parityProgramDictKeyCellForKind(kind int, value uint64, bits uint) *cell.Cell {
	if kind == 1 {
		return cell.BeginCell().MustStoreBigInt(big.NewInt(int64(value)), bits).EndCell()
	}
	return parityProgramKeyCell(value, bits)
}

func parityProgramDictScalarOpcode(base uint64, kind int) uint64 {
	switch kind {
	case 1:
		return base + 1
	case 2:
		return base + 2
	default:
		return base
	}
}

func parityProgramDictValueOpcode(base uint64, kind int, byRef bool) uint64 {
	offset := uint64(0)
	switch kind {
	case 1:
		offset = 2
	case 2:
		offset = 4
	}
	if byRef {
		offset++
	}
	return base + offset
}

func parityProgramDictScalarName(kind int, suffix string) string {
	switch kind {
	case 1:
		return "DICTI" + suffix
	case 2:
		return "DICTU" + suffix
	default:
		return "DICT" + suffix
	}
}

func parityProgramDictBuilderSetNameOpcode(mode cell.DictSetMode, kind int) (string, uint64) {
	switch mode {
	case cell.DictSetModeReplace:
		return parityProgramDictScalarName(kind, "REPLACEB"), parityProgramDictScalarOpcode(0xF449, kind)
	case cell.DictSetModeAdd:
		return parityProgramDictScalarName(kind, "ADDB"), parityProgramDictScalarOpcode(0xF451, kind)
	default:
		return parityProgramDictScalarName(kind, "SETB"), parityProgramDictScalarOpcode(0xF441, kind)
	}
}

func parityProgramDictBuilderSetGetNameOpcode(mode cell.DictSetMode, kind int) (string, uint64) {
	switch mode {
	case cell.DictSetModeReplace:
		return parityProgramDictScalarName(kind, "REPLACEGETB"), parityProgramDictScalarOpcode(0xF44D, kind)
	case cell.DictSetModeAdd:
		return parityProgramDictScalarName(kind, "ADDGETB"), parityProgramDictScalarOpcode(0xF455, kind)
	default:
		return parityProgramDictScalarName(kind, "SETGETB"), parityProgramDictScalarOpcode(0xF445, kind)
	}
}

func parityProgramMultiDictRoot() (*cell.Cell, map[uint64]*cell.Cell) {
	values := map[uint64]*cell.Cell{
		0x10: cell.BeginCell().MustStoreUInt(0x10, 8).EndCell(),
		0x20: cell.BeginCell().MustStoreUInt(0x20, 8).EndCell(),
		0x30: cell.BeginCell().MustStoreUInt(0x30, 8).EndCell(),
	}
	dict := cell.NewDict(8)
	for key, value := range values {
		if _, err := dict.SetWithMode(parityProgramKeyCell(key, 8), value, cell.DictSetModeSet); err != nil {
			panic(err)
		}
	}
	return dict.AsCell(), values
}

func parityProgramSignedDictRoot() (*cell.Cell, map[int64]*cell.Cell) {
	values := map[int64]*cell.Cell{
		-2: cell.BeginCell().MustStoreUInt(0x22, 8).EndCell(),
		3:  cell.BeginCell().MustStoreUInt(0x33, 8).EndCell(),
	}
	dict := cell.NewDict(8)
	for key, value := range values {
		if err := dict.SetIntKey(big.NewInt(key), value); err != nil {
			panic(err)
		}
	}
	return dict.AsCell(), values
}

func parityProgramSubdictRoot() *cell.Cell {
	dict := cell.NewDict(8)
	items := map[uint64]uint64{
		0b01100000: 0x60,
		0b10100000: 0xA0,
		0b10110000: 0xB0,
	}
	for key, value := range items {
		if _, err := dict.SetWithMode(parityProgramKeyCell(key, 8), cell.BeginCell().MustStoreUInt(value, 8).EndCell(), cell.DictSetModeSet); err != nil {
			panic(err)
		}
	}
	return dict.AsCell()
}

func parityProgramPrefixDictRoot() (*cell.Cell, *cell.Cell) {
	value := cell.BeginCell().MustStoreUInt(0xD, 4).EndCell()
	dict := cell.NewPrefixDict(4)
	if _, err := dict.SetWithMode(parityProgramKeyCell(0b10, 2), value, cell.DictSetModeSet); err != nil {
		panic(err)
	}
	return dict.AsCell(), value
}

func parityProgramStorageStatCell() *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0xAB, 8).
		MustStoreRef(cell.BeginCell().EndCell()).
		EndCell()
}

func parityProgramMetaBuilder() *cell.Builder {
	return cell.BeginCell().
		MustStoreUInt(0xAB, 8).
		MustStoreRef(cell.BeginCell().EndCell())
}

func parityProgramFullRefsBuilder() *cell.Builder {
	return cell.BeginCell().
		MustStoreRef(cell.BeginCell().MustStoreUInt(0, 1).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreUInt(0, 1).EndCell()).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell())
}

func parityProgramFullBitsBuilder() *cell.Builder {
	builder := cell.BeginCell()
	if err := builder.StoreSameBit(false, 1023); err != nil {
		panic(err)
	}
	return builder
}

func parityProgramStdAddrSlice() *cell.Slice {
	return cell.BeginCell().MustStoreAddr(crossTestAddr).ToSlice()
}

func parityProgramStdAddrDataSlice() *cell.Slice {
	return cell.BeginCell().MustStoreSlice(crossTestAddr.Data(), 256).ToSlice()
}

func parityProgramTailSlice() *cell.Slice {
	return cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice()
}

func parityProgramStdAddrTailSlice() *cell.Slice {
	return cell.BeginCell().
		MustStoreBuilder(parityProgramStdAddrSlice().ToBuilder()).
		MustStoreUInt(0xA, 4).
		ToSlice()
}

func parityProgramAddrNoneTailSlice() *cell.Slice {
	return cell.BeginCell().MustStoreUInt(0, 2).MustStoreUInt(0xA, 4).ToSlice()
}

func parityProgramParsedStdAddrTuple() parityProgramStackValue {
	return parityProgramStackValue{
		kind: parityProgramTuple,
		tuple: []parityProgramStackValue{
			parityProgramIntValue(big.NewInt(2)),
			{kind: parityProgramNull},
			parityProgramIntValue(big.NewInt(int64(crossTestAddr.Workchain()))),
			{kind: parityProgramSlice, slice: parityProgramStdAddrDataSlice()},
		},
	}
}

func parityProgramIntValue(v *big.Int) parityProgramStackValue {
	return parityProgramStackValue{kind: parityProgramInt, int: new(big.Int).Set(v)}
}

func parityProgramBoolValue(v bool) parityProgramStackValue {
	if v {
		return parityProgramIntValue(big.NewInt(-1))
	}
	return parityProgramIntValue(big.NewInt(0))
}

func parityProgramMaybeCellValue(cl *cell.Cell) parityProgramStackValue {
	if cl == nil {
		return parityProgramStackValue{kind: parityProgramNull}
	}
	return parityProgramStackValue{kind: parityProgramCell, cell: cl}
}

func parityProgramStackIsSafe(stack []parityProgramStackValue) bool {
	for _, val := range stack {
		if !parityProgramValueIsSafe(val) {
			return false
		}
	}
	return true
}

func parityProgramValueIsSafe(val parityProgramStackValue) bool {
	if val.kind == parityProgramInt && val.int.BitLen() > 220 {
		return false
	}
	return true
}

func parityProgramCloneStack(src []parityProgramStackValue) []parityProgramStackValue {
	dst := make([]parityProgramStackValue, len(src))
	for i := range src {
		dst[i] = parityProgramCloneValue(src[i])
	}
	return dst
}

func parityProgramCloneValue(src parityProgramStackValue) parityProgramStackValue {
	dst := parityProgramStackValue{kind: src.kind}
	if src.int != nil {
		dst.int = new(big.Int).Set(src.int)
	}
	if src.tuple != nil {
		dst.tuple = parityProgramCloneStack(src.tuple)
	}
	if src.cell != nil {
		dst.cell = src.cell
	}
	if src.slice != nil {
		dst.slice = src.slice.Copy()
	}
	if src.builder != nil {
		dst.builder = src.builder.Copy()
	}
	return dst
}

func parityProgramHostStack(src []parityProgramStackValue) []any {
	stack := make([]any, len(src))
	for i := range src {
		stack[i] = parityProgramHostValue(src[i])
	}
	return stack
}

func parityProgramHostValue(src parityProgramStackValue) any {
	switch src.kind {
	case parityProgramInt:
		return new(big.Int).Set(src.int)
	case parityProgramNaN:
		return vm.NaN{}
	case parityProgramNull:
		return nil
	case parityProgramTuple:
		items := make([]any, len(src.tuple))
		for i := range src.tuple {
			items[i] = parityProgramHostValue(src.tuple[i])
		}
		return tuple.NewTupleValue(items...)
	case parityProgramCell:
		return src.cell
	case parityProgramSlice:
		return src.slice.Copy()
	case parityProgramBuilder:
		return src.builder.Copy()
	default:
		panic("unknown parity program stack kind")
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
