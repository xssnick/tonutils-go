package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestAdvancedBuilderStoreOps(t *testing.T) {
	ref := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	t.Run("ReferenceStores", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreUInt(0x11, 8))
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STBREF().Interpret(state); err != nil {
			t.Fatalf("STBREF failed: %v", err)
		}
		if builder := popCellSliceBuilder(t, state); builder.RefsUsed() != 1 {
			t.Fatal("expected STBREF to store one ref")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreUInt(0x22, 8))
		if err := STBREFR().Interpret(state); err != nil {
			t.Fatalf("STBREFR failed: %v", err)
		}
		if builder := popCellSliceBuilder(t, state); builder.RefsUsed() != 1 {
			t.Fatal("expected STBREFR to store one ref")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceCell(t, state, ref)
		if err := STREFR().Interpret(state); err != nil {
			t.Fatalf("STREFR failed: %v", err)
		}
		if builder := popCellSliceBuilder(t, state); builder.RefsUsed() != 1 {
			t.Fatal("expected STREFR to store one ref")
		}
	})

	t.Run("QuietReferenceFailuresPreserveInputs", func(t *testing.T) {
		state := newCellSliceState()
		dst := mustFullBuilder(t)
		pushCellSliceCell(t, state, ref)
		pushCellSliceBuilder(t, state, dst.Copy())
		if err := STREFQ().Interpret(state); err != nil {
			t.Fatalf("STREFQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != -1 {
			t.Fatalf("unexpected STREFQ status: %d", status)
		}
		if builder := popCellSliceBuilder(t, state); builder.RefsUsed() != dst.RefsUsed() {
			t.Fatal("expected STREFQ to preserve builder")
		}
		if gotRef := popCellSliceCell(t, state); !sameCellHash(gotRef, ref) {
			t.Fatal("expected STREFQ to preserve ref")
		}

		state = newCellSliceState()
		src := cell.BeginCell().MustStoreUInt(0x33, 8)
		pushCellSliceBuilder(t, state, src.Copy())
		pushCellSliceBuilder(t, state, dst.Copy())
		if err := STBREFQ().Interpret(state); err != nil {
			t.Fatalf("STBREFQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != -1 {
			t.Fatalf("unexpected STBREFQ status: %d", status)
		}
		if builder := popCellSliceBuilder(t, state); builder.RefsUsed() != dst.RefsUsed() {
			t.Fatal("expected STBREFQ to preserve dst")
		}
		if builder := popCellSliceBuilder(t, state); builder.EndCell().BeginParse().MustLoadUInt(8) != 0x33 {
			t.Fatal("expected STBREFQ to preserve src")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, dst.Copy())
		pushCellSliceBuilder(t, state, src.Copy())
		if err := STBREFRQ().Interpret(state); err != nil {
			t.Fatalf("STBREFRQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != -1 {
			t.Fatalf("unexpected STBREFRQ status: %d", status)
		}
		if builder := popCellSliceBuilder(t, state); builder.EndCell().BeginParse().MustLoadUInt(8) != 0x33 {
			t.Fatal("expected STBREFRQ to preserve src")
		}
		if builder := popCellSliceBuilder(t, state); builder.RefsUsed() != dst.RefsUsed() {
			t.Fatal("expected STBREFRQ to preserve dst")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, dst.Copy())
		pushCellSliceCell(t, state, ref)
		if err := STREFRQ().Interpret(state); err != nil {
			t.Fatalf("STREFRQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != -1 {
			t.Fatalf("unexpected STREFRQ status: %d", status)
		}
		if gotRef := popCellSliceCell(t, state); !sameCellHash(gotRef, ref) {
			t.Fatal("expected STREFRQ to preserve ref")
		}
		if builder := popCellSliceBuilder(t, state); builder.RefsUsed() != dst.RefsUsed() {
			t.Fatal("expected STREFRQ to preserve dst")
		}
	})

	t.Run("QuietReferenceSuccessesReturnZero", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceCell(t, state, ref)
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STREFQ().Interpret(state); err != nil {
			t.Fatalf("STREFQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != 0 {
			t.Fatalf("unexpected STREFQ success status: %d", status)
		}
		if builder := popCellSliceBuilder(t, state); builder.RefsUsed() != 1 {
			t.Fatal("expected STREFQ to store ref on success")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreUInt(0x44, 8))
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STBREFQ().Interpret(state); err != nil {
			t.Fatalf("STBREFQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != 0 {
			t.Fatalf("unexpected STBREFQ success status: %d", status)
		}
		if builder := popCellSliceBuilder(t, state); builder.RefsUsed() != 1 {
			t.Fatal("expected STBREFQ to store ref on success")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreUInt(0x55, 8))
		if err := STBREFRQ().Interpret(state); err != nil {
			t.Fatalf("STBREFRQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != 0 {
			t.Fatalf("unexpected STBREFRQ success status: %d", status)
		}
		if builder := popCellSliceBuilder(t, state); builder.RefsUsed() != 1 {
			t.Fatal("expected STBREFRQ to store ref on success")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceCell(t, state, ref)
		if err := STREFRQ().Interpret(state); err != nil {
			t.Fatalf("STREFRQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != 0 {
			t.Fatalf("unexpected STREFRQ success status: %d", status)
		}
		if builder := popCellSliceBuilder(t, state); builder.RefsUsed() != 1 {
			t.Fatal("expected STREFRQ to store ref on success")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		if err := STSLICERQ().Interpret(state); err != nil {
			t.Fatalf("STSLICERQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != 0 {
			t.Fatalf("unexpected STSLICERQ success status: %d", status)
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadUInt(8); got != 0xAB {
			t.Fatalf("unexpected STSLICERQ stored value: %x", got)
		}
	})

	t.Run("BuilderAndSliceStores", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STSLICEQ().Interpret(state); err != nil {
			t.Fatalf("STSLICEQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != 0 {
			t.Fatalf("unexpected STSLICEQ status: %d", status)
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadUInt(8); got != 0xAB {
			t.Fatalf("unexpected STSLICEQ value: %x", got)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreUInt(0xCD, 8))
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STBQ().Interpret(state); err != nil {
			t.Fatalf("STBQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != 0 {
			t.Fatalf("unexpected STBQ status: %d", status)
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadUInt(8); got != 0xCD {
			t.Fatalf("unexpected STBQ value: %x", got)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreUInt(0xEF, 8))
		if err := STBR().Interpret(state); err != nil {
			t.Fatalf("STBR failed: %v", err)
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadUInt(8); got != 0xEF {
			t.Fatalf("unexpected STBR value: %x", got)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreUInt(0x55, 8))
		if err := STBRQ().Interpret(state); err != nil {
			t.Fatalf("STBRQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != 0 {
			t.Fatalf("unexpected STBRQ status: %d", status)
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadUInt(8); got != 0x55 {
			t.Fatalf("unexpected STBRQ value: %x", got)
		}
	})

	t.Run("QuietBuilderFailuresPreserveInputs", func(t *testing.T) {
		fullBits := cell.BeginCell().MustStoreSlice(make([]byte, 128), 1016)

		state := newCellSliceState()
		srcSlice := cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice()
		pushCellSliceSlice(t, state, srcSlice)
		pushCellSliceBuilder(t, state, fullBits.Copy())
		if err := STSLICEQ().Interpret(state); err != nil {
			t.Fatalf("STSLICEQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != -1 {
			t.Fatalf("unexpected STSLICEQ failure status: %d", status)
		}
		if builder := popCellSliceBuilder(t, state); builder.BitsUsed() != fullBits.BitsUsed() {
			t.Fatal("expected STSLICEQ to preserve destination builder")
		}
		if sl := popCellSliceSlice(t, state); !sameSliceHash(sl, srcSlice) {
			t.Fatal("expected STSLICEQ to preserve source slice")
		}

		state = newCellSliceState()
		srcBuilder := cell.BeginCell().MustStoreUInt(0xCD, 8)
		pushCellSliceBuilder(t, state, srcBuilder.Copy())
		pushCellSliceBuilder(t, state, fullBits.Copy())
		if err := STBQ().Interpret(state); err != nil {
			t.Fatalf("STBQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != -1 {
			t.Fatalf("unexpected STBQ failure status: %d", status)
		}
		if builder := popCellSliceBuilder(t, state); builder.BitsUsed() != fullBits.BitsUsed() {
			t.Fatal("expected STBQ to preserve destination builder")
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadUInt(8); got != 0xCD {
			t.Fatalf("unexpected preserved STBQ source: %x", got)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, fullBits.Copy())
		pushCellSliceBuilder(t, state, srcBuilder.Copy())
		if err := STBRQ().Interpret(state); err != nil {
			t.Fatalf("STBRQ failed: %v", err)
		}
		if status := popCellSliceInt(t, state); status != -1 {
			t.Fatalf("unexpected STBRQ failure status: %d", status)
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadUInt(8); got != 0xCD {
			t.Fatalf("unexpected preserved STBRQ source: %x", got)
		}
		if builder := popCellSliceBuilder(t, state); builder.BitsUsed() != fullBits.BitsUsed() {
			t.Fatal("expected STBRQ to preserve destination builder")
		}
	})

	t.Run("NonQuietOverflowPaths", func(t *testing.T) {
		fullRefs := mustFullBuilder(t)

		state := newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreUInt(0x11, 8))
		pushCellSliceBuilder(t, state, fullRefs.Copy())
		if err := STBREF().Interpret(state); err == nil {
			t.Fatal("expected STBREF overflow")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, fullRefs.Copy())
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreUInt(0x22, 8))
		if err := STBREFR().Interpret(state); err == nil {
			t.Fatal("expected STBREFR overflow")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, fullRefs.Copy())
		pushCellSliceCell(t, state, ref)
		if err := STREFR().Interpret(state); err == nil {
			t.Fatal("expected STREFR overflow")
		}

		fullBits := cell.BeginCell().MustStoreSlice(make([]byte, 128), 1016)

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, fullBits.Copy())
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		if err := STSLICER().Interpret(state); err == nil {
			t.Fatal("expected STSLICER overflow")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, fullBits.Copy())
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreUInt(0xCD, 8))
		if err := STBR().Interpret(state); err == nil {
			t.Fatal("expected STBR overflow")
		}
	})

	t.Run("InputErrors", func(t *testing.T) {
		ops := []struct {
			name string
			op   interface{ Interpret(*vm.State) error }
		}{
			{name: "STBREF", op: STBREF()},
			{name: "STBREFR", op: STBREFR()},
			{name: "STREFR", op: STREFR()},
			{name: "STREFQ", op: STREFQ()},
			{name: "STBREFQ", op: STBREFQ()},
			{name: "STSLICEQ", op: STSLICEQ()},
			{name: "STBQ", op: STBQ()},
			{name: "STSLICER", op: STSLICER()},
			{name: "STBR", op: STBR()},
			{name: "STBREFRQ", op: STBREFRQ()},
			{name: "STREFRQ", op: STREFRQ()},
			{name: "STBRQ", op: STBRQ()},
			{name: "STSLICERQ", op: STSLICERQ()},
		}

		for _, tt := range ops {
			t.Run(tt.name, func(t *testing.T) {
				if err := tt.op.Interpret(newCellSliceState()); err == nil {
					t.Fatalf("expected %s input error", tt.name)
				}
			})
		}
	})
}

func TestAdvancedBuilderMetricsAndChecks(t *testing.T) {
	t.Run("EndxcMetricsAndBtos", func(t *testing.T) {
		builder := cell.BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).EndCell())

		state := newCellSliceState()
		pushCellSliceBuilder(t, state, builder.Copy())
		pushCellSliceBool(t, state, false)
		if err := ENDXC().Interpret(state); err != nil {
			t.Fatalf("ENDXC failed: %v", err)
		}
		if cl := popCellSliceCell(t, state); cl.Depth() != 2 {
			t.Fatalf("unexpected ENDXC cell depth: %d", cl.Depth())
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, builder.Copy())
		if err := BDEPTH().Interpret(state); err != nil {
			t.Fatalf("BDEPTH failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 2 {
			t.Fatalf("unexpected BDEPTH: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, builder.Copy())
		if err := BBITS().Interpret(state); err != nil {
			t.Fatalf("BBITS failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 8 {
			t.Fatalf("unexpected BBITS: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, builder.Copy())
		if err := BREFS().Interpret(state); err != nil {
			t.Fatalf("BREFS failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 1 {
			t.Fatalf("unexpected BREFS: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, builder.Copy())
		if err := BBITREFS().Interpret(state); err != nil {
			t.Fatalf("BBITREFS failed: %v", err)
		}
		if refs := popCellSliceInt(t, state); refs != 1 {
			t.Fatalf("unexpected BBITREFS refs: %d", refs)
		}
		if bits := popCellSliceInt(t, state); bits != 8 {
			t.Fatalf("unexpected BBITREFS bits: %d", bits)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, builder.Copy())
		if err := BTOS().Interpret(state); err != nil {
			t.Fatalf("BTOS failed: %v", err)
		}
		if sl := popCellSliceSlice(t, state); sl.BitsLeft() != 8 || sl.RefsNum() != 1 {
			t.Fatal("unexpected BTOS slice")
		}
	})

	t.Run("BuilderChecksAndSameBits", func(t *testing.T) {
		decodedImm := BCHKBITSIMM(1, false)
		if err := decodedImm.Deserialize(BCHKBITSIMM(8, false).Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("BCHKBITSIMM deserialize failed: %v", err)
		}
		if got := decodedImm.SerializeText(); got != "BCHKBITS 8" {
			t.Fatalf("unexpected BCHKBITSIMM text: %q", got)
		}
		if got := decodedImm.InstructionBits(); got != 24 {
			t.Fatalf("unexpected BCHKBITSIMM bits: %d", got)
		}

		decodedQuietImm := BCHKBITSIMM(1, true)
		if err := decodedQuietImm.Deserialize(BCHKBITSIMM(8, true).Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("BCHKBITSIMMQ deserialize failed: %v", err)
		}
		if got := decodedQuietImm.SerializeText(); got != "BCHKBITSQ 8" {
			t.Fatalf("unexpected BCHKBITSIMMQ text: %q", got)
		}

		state := newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := BCHKBITSIMM(2, false).Interpret(state); err != nil {
			t.Fatalf("BCHKBITSIMM failed: %v", err)
		}

		fullBits := cell.BeginCell().MustStoreSlice(make([]byte, 128), 1022)
		state = newCellSliceState()
		pushCellSliceBuilder(t, state, fullBits.Copy())
		if err := BCHKBITSIMM(2, true).Interpret(state); err != nil {
			t.Fatalf("BCHKBITSIMMQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected BCHKBITSIMM quiet failure")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceInt(t, state, 1)
		if err := BCHKBITS().Interpret(state); err != nil {
			t.Fatalf("BCHKBITS failed: %v", err)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceInt(t, state, 1)
		if err := BCHKBITSQ().Interpret(state); err != nil {
			t.Fatalf("BCHKBITSQ failed: %v", err)
		}
		if !popCellSliceBool(t, state) {
			t.Fatal("expected BCHKBITSQ success flag")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceInt(t, state, 0)
		if err := BCHKREFS().Interpret(state); err != nil {
			t.Fatalf("BCHKREFS failed: %v", err)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceInt(t, state, 1)
		pushCellSliceInt(t, state, 0)
		if err := BCHKBITREFS().Interpret(state); err != nil {
			t.Fatalf("BCHKBITREFS failed: %v", err)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, mustFullBuilder(t))
		pushCellSliceInt(t, state, 1)
		if err := BCHKREFSQ().Interpret(state); err != nil {
			t.Fatalf("BCHKREFSQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected BCHKREFSQ to report false")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, mustFullBuilder(t))
		pushCellSliceInt(t, state, 0)
		pushCellSliceInt(t, state, 1)
		if err := BCHKBITREFSQ().Interpret(state); err != nil {
			t.Fatalf("BCHKBITREFSQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected BCHKBITREFSQ to report false")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, fullBits.Copy())
		pushCellSliceInt(t, state, 8)
		if err := BCHKBITSQ().Interpret(state); err != nil {
			t.Fatalf("BCHKBITSQ quiet failure failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected BCHKBITSQ failure flag")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceInt(t, state, 3)
		if err := STZEROES().Interpret(state); err != nil {
			t.Fatalf("STZEROES failed: %v", err)
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadUInt(3); got != 0 {
			t.Fatalf("unexpected STZEROES bits: %b", got)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceInt(t, state, 3)
		if err := STONES().Interpret(state); err != nil {
			t.Fatalf("STONES failed: %v", err)
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadUInt(3); got != 0x7 {
			t.Fatalf("unexpected STONES bits: %b", got)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceInt(t, state, 3)
		pushCellSliceInt(t, state, 1)
		if err := STSAME().Interpret(state); err != nil {
			t.Fatalf("STSAME failed: %v", err)
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadUInt(3); got != 0x7 {
			t.Fatalf("unexpected STSAME bits: %b", got)
		}
	})
}
