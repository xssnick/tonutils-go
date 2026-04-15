package tvm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestAdvancedCellOpsGoSemantics(t *testing.T) {
	t.Run("PushRefAndInlinePushSlice", func(t *testing.T) {
		refCell := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		stack, res, err := runRawCode(codeFromBuilders(t, stackop.PUSHREF(refCell).Serialize()))
		if err != nil {
			t.Fatalf("pushref unexpected error: %v", err)
		}
		stack = res.Stack
		gotCell, err := stack.PopCell()
		if err != nil {
			t.Fatalf("pushref pop cell: %v", err)
		}
		if !bytes.Equal(gotCell.Hash(), refCell.Hash()) {
			t.Fatalf("pushref mismatch: got=%s want=%s", gotCell.Dump(), refCell.Dump())
		}

		shortSlice := cell.BeginCell().MustStoreUInt(0b101, 3).EndCell().BeginParse()
		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHSLICEINLINE(shortSlice).Serialize()))
		if err != nil {
			t.Fatalf("pushslice short unexpected error: %v", err)
		}
		stack = res.Stack
		gotShort, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("pushslice short pop: %v", err)
		}
		if got := gotShort.MustLoadUInt(3); got != 0b101 {
			t.Fatalf("pushslice short expected 0b101, got %b", got)
		}

		longSlice := cell.BeginCell().MustStoreSlice(make([]byte, 32), 250).EndCell().BeginParse()
		stack, res, err = runRawCode(codeFromBuilders(t, stackop.PUSHSLICEINLINE(longSlice).Serialize()))
		if err != nil {
			t.Fatalf("pushslice long unexpected error: %v", err)
		}
		stack = res.Stack
		gotLong, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("pushslice long pop: %v", err)
		}
		if gotLong.BitsLeft() != 250 {
			t.Fatalf("pushslice long expected 250 bits, got %d", gotLong.BitsLeft())
		}
	})

	t.Run("LoadRefRTOSAndSliceCuts", func(t *testing.T) {
		child := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
		parent := cell.BeginCell().MustStoreUInt(0xCD, 8).MustStoreRef(child).EndCell()

		stack, res, err := runRawCode(codeFromBuilders(t, cell.BeginCell().MustStoreUInt(0xD5, 8)), parent.BeginParse())
		if err != nil {
			t.Fatalf("ldrefrtos unexpected error: %v", err)
		}
		stack = res.Stack
		childSlice, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("ldrefrtos child pop: %v", err)
		}
		parentRest, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("ldrefrtos parent pop: %v", err)
		}
		if got := childSlice.MustLoadUInt(8); got != 0xAB {
			t.Fatalf("ldrefrtos child expected 0xAB, got %x", got)
		}
		if got := parentRest.MustLoadUInt(8); got != 0xCD {
			t.Fatalf("ldrefrtos parent expected 0xCD, got %x", got)
		}

		src := cell.BeginCell().MustStoreUInt(0b110101, 6).MustStoreRef(cell.BeginCell().EndCell()).EndCell().BeginParse()
		stack, res, err = runRawCode(codeFromOpcodes(t, 0xD720), src, int64(3))
		if err != nil {
			t.Fatalf("sdcutfirst unexpected error: %v", err)
		}
		stack = res.Stack
		cut, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("sdcutfirst pop: %v", err)
		}
		if got := cut.MustLoadUInt(3); got != 0b110 {
			t.Fatalf("sdcutfirst expected 110, got %b", got)
		}

		src = cell.BeginCell().MustStoreUInt(0b110101, 6).MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).EndCell().BeginParse()
		stack, res, err = runRawCode(codeFromOpcodes(t, 0xD733), src, int64(2), int64(1))
		if err != nil {
			t.Fatalf("sskiplast unexpected error: %v", err)
		}
		stack = res.Stack
		trimmed, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("sskiplast pop: %v", err)
		}
		if trimmed.BitsLeft() != 4 || trimmed.RefsNum() != 0 {
			t.Fatalf("sskiplast expected 4 bits/0 refs, got %d bits/%d refs", trimmed.BitsLeft(), trimmed.RefsNum())
		}

		src = cell.BeginCell().MustStoreUInt(0b110101, 6).MustStoreRef(cell.BeginCell().EndCell()).EndCell().BeginParse()
		stack, res, err = runRawCode(codeFromOpcodes(t, 0xD737), src, int64(8), int64(0))
		if err != nil {
			t.Fatalf("splitq unexpected error: %v", err)
		}
		stack = res.Stack
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("splitq ok pop: %v", err)
		}
		if ok {
			t.Fatalf("splitq expected quiet fail")
		}
		orig, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("splitq original pop: %v", err)
		}
		if orig.BitsLeft() != 6 || orig.RefsNum() != 1 {
			t.Fatalf("splitq expected original slice back, got %d bits/%d refs", orig.BitsLeft(), orig.RefsNum())
		}
	})

	t.Run("SliceChecksRefsAndSameRuns", func(t *testing.T) {
		refA := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		refB := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		src := cell.BeginCell().MustStoreUInt(0b111000, 6).MustStoreRef(refA).MustStoreRef(refB).EndCell().BeginParse()

		stack, res, err := runRawCode(codeFromOpcodes(t, 0xD748), src, int64(1))
		if err != nil {
			t.Fatalf("pldrefvar unexpected error: %v", err)
		}
		stack = res.Stack
		gotRef, err := stack.PopCell()
		if err != nil {
			t.Fatalf("pldrefvar pop: %v", err)
		}
		if !bytes.Equal(gotRef.Hash(), refB.Hash()) {
			t.Fatalf("pldrefvar expected second ref")
		}

		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.PLDREFIDX(0).Serialize()), src)
		if err != nil {
			t.Fatalf("pldrefidx unexpected error: %v", err)
		}
		stack = res.Stack
		gotRef, err = stack.PopCell()
		if err != nil {
			t.Fatalf("pldrefidx pop: %v", err)
		}
		if !bytes.Equal(gotRef.Hash(), refA.Hash()) {
			t.Fatalf("pldrefidx expected first ref")
		}

		stack, res, err = runRawCode(codeFromOpcodes(t, 0xD747), src, int64(7), int64(3))
		if err != nil {
			t.Fatalf("schkbitrefsq unexpected error: %v", err)
		}
		stack = res.Stack
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("schkbitrefsq pop: %v", err)
		}
		if ok {
			t.Fatalf("schkbitrefsq expected false")
		}

		stack, res, err = runRawCode(codeFromOpcodes(t, 0xD760), cell.BeginCell().MustStoreUInt(0b000111, 6).EndCell().BeginParse())
		if err != nil {
			t.Fatalf("ldzeroes unexpected error: %v", err)
		}
		stack = res.Stack
		rest, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("ldzeroes rest: %v", err)
		}
		count, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("ldzeroes count: %v", err)
		}
		if count.Int64() != 3 || rest.BitsLeft() != 3 {
			t.Fatalf("ldzeroes expected count=3 rest=3 bits, got count=%d rest=%d", count.Int64(), rest.BitsLeft())
		}

		stack, res, err = runRawCode(codeFromOpcodes(t, 0xD762), cell.BeginCell().MustStoreUInt(0b111000, 6).EndCell().BeginParse(), int64(1))
		if err != nil {
			t.Fatalf("ldsame unexpected error: %v", err)
		}
		stack = res.Stack
		rest, err = stack.PopSlice()
		if err != nil {
			t.Fatalf("ldsame rest: %v", err)
		}
		count, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("ldsame count: %v", err)
		}
		if count.Int64() != 3 || rest.BitsLeft() != 3 {
			t.Fatalf("ldsame expected count=3 rest=3 bits, got count=%d rest=%d", count.Int64(), rest.BitsLeft())
		}
	})

	t.Run("XOpsAndDepths", func(t *testing.T) {
		specialBuilder := cell.BeginCell().MustStoreUInt(uint64(cell.LibraryCellType), 8).MustStoreSlice(make([]byte, 32), 256)
		specialCell, err := specialBuilder.EndCellSpecial(true)
		if err != nil {
			t.Fatalf("special cell build: %v", err)
		}

		stack, res, err := runRawCode(codeFromOpcodes(t, 0xD739), specialCell)
		if err != nil {
			t.Fatalf("xctos unexpected error: %v", err)
		}
		stack = res.Stack
		special, err := stack.PopBool()
		if err != nil {
			t.Fatalf("xctos flag pop: %v", err)
		}
		if !special {
			t.Fatalf("xctos expected special=true")
		}
		gotSlice, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("xctos slice pop: %v", err)
		}
		if !gotSlice.IsSpecial() {
			t.Fatalf("xctos expected special slice")
		}

		stack, res, err = runRawCode(codeFromOpcodes(t, 0xD73B), specialCell)
		if err != nil {
			t.Fatalf("xloadq unexpected error: %v", err)
		}
		stack = res.Stack
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("xloadq bool pop: %v", err)
		}
		if ok {
			t.Fatalf("xloadq expected false without libraries")
		}

		depthCell := cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(cell.BeginCell().MustStoreUInt(2, 2).EndCell()).EndCell()
		stack, res, err = runRawCode(codeFromOpcodes(t, 0xD764), depthCell.BeginParse())
		if err != nil {
			t.Fatalf("sdepth unexpected error: %v", err)
		}
		stack = res.Stack
		depth, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("sdepth pop: %v", err)
		}
		if depth.Int64() != 1 {
			t.Fatalf("sdepth expected 1, got %d", depth.Int64())
		}

		stack, res, err = runRawCode(codeFromOpcodes(t, 0xD765), nil)
		if err != nil {
			t.Fatalf("cdepth unexpected error: %v", err)
		}
		stack = res.Stack
		depth, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("cdepth pop: %v", err)
		}
		if depth.Int64() != 0 {
			t.Fatalf("cdepth expected 0 for nil, got %d", depth.Int64())
		}
	})

	t.Run("XLoadLibraryResolution", func(t *testing.T) {
		target := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		libraryCell := mustLibraryCellForHash(t, target.Hash())
		libraries := mustLibraryCollection(t, target)

		stack := vmcore.NewStack()
		if err := stack.PushCell(libraryCell); err != nil {
			t.Fatalf("push library cell: %v", err)
		}

		res, err := NewTVM().ExecuteDetailedWithLibraries(codeFromOpcodes(t, 0xD73A), cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.GasWithLimit(1_000_000), stack, libraries)
		if err != nil {
			t.Fatalf("xload with libraries unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("xload with libraries exit code: %d", res.ExitCode)
		}

		got, err := res.Stack.PopCell()
		if err != nil {
			t.Fatalf("xload with libraries pop cell: %v", err)
		}
		if !bytes.Equal(got.Hash(), target.Hash()) {
			t.Fatalf("xload with libraries mismatch: got=%s want=%s", got.Dump(), target.Dump())
		}

		stack = vmcore.NewStack()
		if err := stack.PushCell(libraryCell); err != nil {
			t.Fatalf("push library cell for xloadq: %v", err)
		}

		res, err = NewTVM().ExecuteDetailedWithLibraries(codeFromOpcodes(t, 0xD73B), cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.GasWithLimit(1_000_000), stack, libraries)
		if err != nil {
			t.Fatalf("xloadq with libraries unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("xloadq with libraries exit code: %d", res.ExitCode)
		}

		ok, err := res.Stack.PopBool()
		if err != nil {
			t.Fatalf("xloadq with libraries pop bool: %v", err)
		}
		if !ok {
			t.Fatalf("xloadq with libraries expected true")
		}

		got, err = res.Stack.PopCell()
		if err != nil {
			t.Fatalf("xloadq with libraries pop cell: %v", err)
		}
		if !bytes.Equal(got.Hash(), target.Hash()) {
			t.Fatalf("xloadq with libraries mismatch: got=%s want=%s", got.Dump(), target.Dump())
		}
	})

	t.Run("XLoadPrunedCell", func(t *testing.T) {
		pruned := mustPrunedCellForXLoad(t)

		_, res, err := runRawCode(codeFromOpcodes(t, 0xD73A), pruned)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeCellUnderflow {
			t.Fatalf("xload expected cell underflow on pruned cell, got %d", code)
		}

		stack, res, err := runRawCode(codeFromOpcodes(t, 0xD73B), pruned)
		if err != nil {
			t.Fatalf("xloadq on pruned cell unexpected error: %v", err)
		}
		stack = res.Stack
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("xloadq on pruned cell pop bool: %v", err)
		}
		if ok {
			t.Fatalf("xloadq on pruned cell expected false")
		}
	})

	t.Run("BuilderQuietAndMetadata", func(t *testing.T) {
		full := cell.BeginCell()
		full.MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreRef(cell.BeginCell().EndCell())

		stack, res, err := runRawCode(codeFromOpcodes(t, 0xCF18), cell.BeginCell().EndCell(), full)
		if err != nil {
			t.Fatalf("strefq unexpected error: %v", err)
		}
		stack = res.Stack
		status, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("strefq status pop: %v", err)
		}
		if status.Int64() != -1 {
			t.Fatalf("strefq expected quiet fail status -1, got %d", status.Int64())
		}
		gotBuilder, err := stack.PopBuilder()
		if err != nil {
			t.Fatalf("strefq builder pop: %v", err)
		}
		if gotBuilder.RefsUsed() != 4 {
			t.Fatalf("strefq expected full builder back")
		}

		src := cell.BeginCell().MustStoreUInt(0xA, 4)
		dst := cell.BeginCell()
		stack, res, err = runRawCode(codeFromOpcodes(t, 0xCF1B), src, dst)
		if err != nil {
			t.Fatalf("stbq unexpected error: %v", err)
		}
		stack = res.Stack
		status, err = stack.PopIntFinite()
		if err != nil {
			t.Fatalf("stbq status pop: %v", err)
		}
		if status.Int64() != 0 {
			t.Fatalf("stbq expected success status 0, got %d", status.Int64())
		}
		gotBuilder, err = stack.PopBuilder()
		if err != nil {
			t.Fatalf("stbq builder pop: %v", err)
		}
		if gotBuilder.BitsUsed() != 4 {
			t.Fatalf("stbq expected 4 bits, got %d", gotBuilder.BitsUsed())
		}

		libraryBuilder := cell.BeginCell().MustStoreUInt(uint64(cell.LibraryCellType), 8).MustStoreSlice(make([]byte, 32), 256)
		stack, res, err = runRawCode(codeFromOpcodes(t, 0xCF23), libraryBuilder, int64(-1))
		if err != nil {
			t.Fatalf("endxc unexpected error: %v", err)
		}
		stack = res.Stack
		gotCell, err := stack.PopCell()
		if err != nil {
			t.Fatalf("endxc cell pop: %v", err)
		}
		if gotCell.GetType() != cell.LibraryCellType {
			t.Fatalf("endxc expected library cell, got %v", gotCell.GetType())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.BCHKBITSIMM(8, true).Serialize()), cell.BeginCell())
		if err != nil {
			t.Fatalf("bchkbitsq imm unexpected error: %v", err)
		}
		stack = res.Stack
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("bchkbitsq imm bool pop: %v", err)
		}
		if !ok {
			t.Fatalf("bchkbitsq imm expected success")
		}

		stack, res, err = runRawCode(codeFromOpcodes(t, 0xCF3F), cell.BeginCell(), int64(1023), int64(5))
		if err != nil {
			t.Fatalf("bchkbitrefsq unexpected error: %v", err)
		}
		stack = res.Stack
		ok, err = stack.PopBool()
		if err != nil {
			t.Fatalf("bchkbitrefsq bool pop: %v", err)
		}
		if ok {
			t.Fatalf("bchkbitrefsq expected false")
		}

		stack, res, err = runRawCode(codeFromOpcodes(t, 0xCF40), cell.BeginCell(), int64(5))
		if err != nil {
			t.Fatalf("stzeroes unexpected error: %v", err)
		}
		stack = res.Stack
		gotBuilder, err = stack.PopBuilder()
		if err != nil {
			t.Fatalf("stzeroes builder pop: %v", err)
		}
		gotSlice := gotBuilder.ToSlice()
		if got := gotSlice.MustLoadUInt(5); got != 0 {
			t.Fatalf("stzeroes expected zero bits, got %b", got)
		}

		stack, res, err = runRawCode(codeFromOpcodes(t, 0xCF42), cell.BeginCell(), int64(4), int64(1))
		if err != nil {
			t.Fatalf("stsame unexpected error: %v", err)
		}
		stack = res.Stack
		gotBuilder, err = stack.PopBuilder()
		if err != nil {
			t.Fatalf("stsame builder pop: %v", err)
		}
		gotSlice = gotBuilder.ToSlice()
		if got := gotSlice.MustLoadUInt(4); got != 0b1111 {
			t.Fatalf("stsame expected ones, got %b", got)
		}

		btStack := vmcore.NewStack()
		if err = btStack.PushBuilder(cell.BeginCell().MustStoreUInt(0xAB, 8)); err != nil {
			t.Fatalf("btos push builder: %v", err)
		}
		res, err = NewTVM().ExecuteDetailed(codeFromOpcodes(t, 0xCF50), cell.BeginCell().EndCell(), tuple.Tuple{}, vmcore.GasWithLimit(1_000_000), btStack)
		if err != nil {
			t.Fatalf("btos unexpected error: %v", err)
		}
		stack = res.Stack
		gotSlice, err = stack.PopSlice()
		if err != nil {
			t.Fatalf("btos slice pop: %v", err)
		}
		if got := gotSlice.MustLoadUInt(8); got != 0xAB {
			t.Fatalf("btos expected 0xAB, got %x", got)
		}
	})

	t.Run("NonQuietFailures", func(t *testing.T) {
		_, res, err := runRawCode(codeFromOpcodes(t, 0xD741), cell.BeginCell().MustStoreUInt(0, 1).EndCell().BeginParse(), int64(2))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeCellUnderflow {
			t.Fatalf("schkbits expected cell underflow, got %d", code)
		}
	})

	t.Run("CompareFamilyUsesBitSemantics", func(t *testing.T) {
		left := cell.BeginCell().MustStoreUInt(0b1010, 4).MustStoreRef(cell.BeginCell().EndCell()).EndCell().BeginParse()
		right := cell.BeginCell().MustStoreUInt(0b1010, 4).EndCell().BeginParse()

		stack, res, err := runRawCode(codeFromBuilders(t, cellsliceop.SDEQ().Serialize()), left, right)
		if err != nil {
			t.Fatalf("sdeq unexpected error: %v", err)
		}
		stack = res.Stack
		eq, err := stack.PopBool()
		if err != nil {
			t.Fatalf("sdeq pop: %v", err)
		}
		if !eq {
			t.Fatalf("sdeq should ignore refs and compare only bits")
		}

		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.SDLEXCMP().Serialize()),
			cell.BeginCell().MustStoreUInt(0b1010, 4).EndCell().BeginParse(),
			cell.BeginCell().MustStoreUInt(0b1011, 4).EndCell().BeginParse(),
		)
		if err != nil {
			t.Fatalf("sdlexcmp unexpected error: %v", err)
		}
		stack = res.Stack
		cmp, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("sdlexcmp pop: %v", err)
		}
		if cmp.Int64() != -1 {
			t.Fatalf("sdlexcmp expected -1, got %d", cmp.Int64())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.SDPFX().Serialize()),
			cell.BeginCell().MustStoreUInt(0b10, 2).EndCell().BeginParse(),
			cell.BeginCell().MustStoreUInt(0b1011, 4).EndCell().BeginParse(),
		)
		if err != nil {
			t.Fatalf("sdpfx unexpected error: %v", err)
		}
		stack = res.Stack
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("sdpfx pop: %v", err)
		}
		if !ok {
			t.Fatalf("sdpfx expected true")
		}

		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.SDSFX().Serialize()),
			cell.BeginCell().MustStoreUInt(0b011, 3).EndCell().BeginParse(),
			cell.BeginCell().MustStoreUInt(0b1011, 4).EndCell().BeginParse(),
		)
		if err != nil {
			t.Fatalf("sdsfx unexpected error: %v", err)
		}
		stack = res.Stack
		ok, err = stack.PopBool()
		if err != nil {
			t.Fatalf("sdsfx pop: %v", err)
		}
		if !ok {
			t.Fatalf("sdsfx expected true")
		}
	})

	t.Run("SDBeginsAndLittleEndianLoads", func(t *testing.T) {
		stack, res, err := runRawCode(codeFromBuilders(t, cellsliceop.SDBEGINSX().Serialize()),
			cell.BeginCell().MustStoreUInt(0b101101, 6).EndCell().BeginParse(),
			cell.BeginCell().MustStoreUInt(0b101, 3).EndCell().BeginParse(),
		)
		if err != nil {
			t.Fatalf("sdbeginsx unexpected error: %v", err)
		}
		stack = res.Stack
		rest, err := stack.PopSlice()
		if err != nil {
			t.Fatalf("sdbeginsx rest pop: %v", err)
		}
		if got := rest.MustLoadUInt(3); got != 0b101 {
			t.Fatalf("sdbeginsx expected remaining 101, got %b", got)
		}

		orig := cell.BeginCell().MustStoreUInt(0b101101, 6).EndCell().BeginParse()
		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.SDBEGINSXQ().Serialize()),
			orig,
			cell.BeginCell().MustStoreUInt(0b111, 3).EndCell().BeginParse(),
		)
		if err != nil {
			t.Fatalf("sdbeginsxq unexpected error: %v", err)
		}
		stack = res.Stack
		ok, err := stack.PopBool()
		if err != nil {
			t.Fatalf("sdbeginsxq flag pop: %v", err)
		}
		if ok {
			t.Fatalf("sdbeginsxq expected false")
		}
		rest, err = stack.PopSlice()
		if err != nil {
			t.Fatalf("sdbeginsxq slice pop: %v", err)
		}
		if rest.BitsLeft() != orig.BitsLeft() {
			t.Fatalf("sdbeginsxq should preserve original slice")
		}

		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.LDILE4().Serialize()),
			cell.BeginCell().MustStoreSlice([]byte{0xFE, 0xFF, 0xFF, 0xFF}, 32).EndCell().BeginParse(),
		)
		if err != nil {
			t.Fatalf("ldile4 unexpected error: %v", err)
		}
		stack = res.Stack
		rest, err = stack.PopSlice()
		if err != nil {
			t.Fatalf("ldile4 rest pop: %v", err)
		}
		val, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("ldile4 value pop: %v", err)
		}
		if val.Int64() != -2 || rest.BitsLeft() != 0 {
			t.Fatalf("ldile4 expected -2 and empty slice, got %d and %d bits", val.Int64(), rest.BitsLeft())
		}

		short := cell.BeginCell().MustStoreSlice([]byte{0xAA, 0xBB, 0xCC, 0xDD}, 32).EndCell().BeginParse()
		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.PLDULE8Q().Serialize()), short)
		if err != nil {
			t.Fatalf("pldule8q unexpected error: %v", err)
		}
		stack = res.Stack
		ok, err = stack.PopBool()
		if err != nil {
			t.Fatalf("pldule8q flag pop: %v", err)
		}
		if ok {
			t.Fatalf("pldule8q expected false")
		}
	})

	t.Run("LittleEndianStoresAndMetadata", func(t *testing.T) {
		stack, res, err := runRawCode(codeFromBuilders(t, cellsliceop.STULE4().Serialize()), int64(0x11223344), cell.BeginCell())
		if err != nil {
			t.Fatalf("stule4 unexpected error: %v", err)
		}
		stack = res.Stack
		builder, err := stack.PopBuilder()
		if err != nil {
			t.Fatalf("stule4 builder pop: %v", err)
		}
		got, err := builder.ToSlice().LoadSlice(32)
		if err != nil {
			t.Fatalf("stule4 load slice: %v", err)
		}
		if !bytes.Equal(got, []byte{0x44, 0x33, 0x22, 0x11}) {
			t.Fatalf("stule4 expected little-endian bytes, got %x", got)
		}

		_, res, err = runRawCode(codeFromBuilders(t, cellsliceop.STILE4().Serialize()), int64(1<<40), cell.BeginCell())
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("stile4 expected range check, got %d", code)
		}

		builderWithData := cell.BeginCell().MustStoreUInt(0xA, 4).MustStoreRef(cell.BeginCell().EndCell())
		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.BREMBITREFS().Serialize()), builderWithData)
		if err != nil {
			t.Fatalf("brembitrefs unexpected error: %v", err)
		}
		stack = res.Stack
		refsLeft, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("brembitrefs refs pop: %v", err)
		}
		bitsLeft, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("brembitrefs bits pop: %v", err)
		}
		if bitsLeft.Int64() != 1019 || refsLeft.Int64() != 3 {
			t.Fatalf("brembitrefs expected 1019/3, got %d/%d", bitsLeft.Int64(), refsLeft.Int64())
		}
	})

	t.Run("ConstStoreAndIndexedHashDepth", func(t *testing.T) {
		refA := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		refB := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()

		stack, res, err := runRawCode(codeFromBuilders(t, cellsliceop.STREFCONST(refA).Serialize()), cell.BeginCell())
		if err != nil {
			t.Fatalf("strefconst unexpected error: %v", err)
		}
		stack = res.Stack
		builder, err := stack.PopBuilder()
		if err != nil {
			t.Fatalf("strefconst builder pop: %v", err)
		}
		if builder.RefsUsed() != 1 {
			t.Fatalf("strefconst expected one ref, got %d", builder.RefsUsed())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.STREF2CONST(refA, refB).Serialize()), cell.BeginCell())
		if err != nil {
			t.Fatalf("stref2const unexpected error: %v", err)
		}
		stack = res.Stack
		builder, err = stack.PopBuilder()
		if err != nil {
			t.Fatalf("stref2const builder pop: %v", err)
		}
		if builder.RefsUsed() != 2 {
			t.Fatalf("stref2const expected two refs, got %d", builder.RefsUsed())
		}

		constSlice := cell.BeginCell().MustStoreUInt(0b10101, 5).MustStoreRef(refA).EndCell().BeginParse()
		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.STSLICECONST(constSlice).Serialize()), cell.BeginCell())
		if err != nil {
			t.Fatalf("stsliceconst unexpected error: %v", err)
		}
		stack = res.Stack
		builder, err = stack.PopBuilder()
		if err != nil {
			t.Fatalf("stsliceconst builder pop: %v", err)
		}
		if builder.BitsUsed() != 5 || builder.RefsUsed() != 1 {
			t.Fatalf("stsliceconst expected 5 bits and 1 ref, got %d bits and %d refs", builder.BitsUsed(), builder.RefsUsed())
		}

		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.ENDCST().Serialize()),
			cell.BeginCell(),
			cell.BeginCell().MustStoreUInt(0xAB, 8),
		)
		if err != nil {
			t.Fatalf("endcst unexpected error: %v", err)
		}
		stack = res.Stack
		builder, err = stack.PopBuilder()
		if err != nil {
			t.Fatalf("endcst builder pop: %v", err)
		}
		if builder.RefsUsed() != 1 {
			t.Fatalf("endcst expected builder with one ref")
		}

		complex := cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(cell.BeginCell().MustStoreUInt(2, 2).EndCell()).EndCell()
		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.CHASHI(0).Serialize()), complex)
		if err != nil {
			t.Fatalf("chashi unexpected error: %v", err)
		}
		stack = res.Stack
		hashInt, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("chashi pop: %v", err)
		}
		if hashInt.Cmp(new(big.Int).SetBytes(complex.Hash(0))) != 0 {
			t.Fatalf("chashi hash mismatch")
		}

		stack, res, err = runRawCode(codeFromBuilders(t, cellsliceop.CDEPTHIX().Serialize()), complex, int64(0))
		if err != nil {
			t.Fatalf("cdepthix unexpected error: %v", err)
		}
		stack = res.Stack
		depth, err := stack.PopIntFinite()
		if err != nil {
			t.Fatalf("cdepthix pop: %v", err)
		}
		if depth.Int64() != int64(complex.Depth(0)) {
			t.Fatalf("cdepthix expected %d, got %d", complex.Depth(0), depth.Int64())
		}

	})
}

func mustLibraryCellForHash(t *testing.T, hash []byte) *cell.Cell {
	t.Helper()
	cl, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(hash, 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("failed to build library cell: %v", err)
	}
	return cl
}

func mustLibraryCollection(t *testing.T, entries ...*cell.Cell) *cell.Cell {
	t.Helper()
	dict := cell.NewDict(256)
	for _, entry := range entries {
		key := cell.BeginCell().MustStoreSlice(entry.Hash(), 256).EndCell()
		if _, err := dict.SetRefWithMode(key, entry, cell.DictSetModeSet); err != nil {
			t.Fatalf("failed to add library entry: %v", err)
		}
	}
	return dict.AsCell()
}

func mustPrunedCellForXLoad(t *testing.T) *cell.Cell {
	t.Helper()

	branch := cell.BeginCell().
		MustStoreUInt(0xBEEF, 16).
		MustStoreRef(cell.BeginCell().MustStoreUInt(1, 1).EndCell()).
		EndCell()
	root := cell.BeginCell().
		MustStoreUInt(0, 1).
		MustStoreRef(branch).
		EndCell()

	proof, err := root.CreateProof(cell.CreateProofSkeleton())
	if err != nil {
		t.Fatalf("failed to build proof for pruned cell: %v", err)
	}
	body, err := cell.UnwrapProof(proof, root.Hash())
	if err != nil {
		t.Fatalf("failed to unwrap proof for pruned cell: %v", err)
	}
	pruned, err := body.PeekRef(0)
	if err != nil {
		t.Fatalf("failed to get pruned ref: %v", err)
	}
	if !pruned.IsSpecial() || pruned.GetType() != cell.PrunedCellType {
		t.Fatalf("expected pruned cell, got special=%v type=%v", pruned.IsSpecial(), pruned.GetType())
	}
	return pruned
}
