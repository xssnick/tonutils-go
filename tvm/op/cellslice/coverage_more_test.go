package cellslice

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestParseLoadedCellSliceBranches(t *testing.T) {
	state := newCellSliceState()

	if _, err := parseLoadedCellSlice(state, nil); err == nil {
		t.Fatal("expected nil cell parse to fail")
	}

	ordinary := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	sl, err := parseLoadedCellSlice(state, ordinary)
	if err != nil {
		t.Fatalf("failed to parse ordinary cell: %v", err)
	}
	if sl.BitsLeft() != 8 || sl.MustLoadUInt(8) != 0xAB {
		t.Fatal("unexpected ordinary slice")
	}

	if _, err = parseLoadedCellSlice(state, mustLibraryCell(t)); err == nil {
		t.Fatal("expected library cell parse to fail")
	}
	if _, err = parseLoadedCellSlice(state, mustPrunedCell(t)); err == nil {
		t.Fatal("expected pruned cell parse to fail")
	}
}

func TestRegisteredCellSliceOpsInstantiate(t *testing.T) {
	if len(vm.List) == 0 {
		t.Fatal("expected cellslice package to register op getters")
	}

	for i, getter := range vm.List {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("registered getter %d panicked: %v", i, r)
				}
			}()
			if op := getter(); op == nil {
				t.Fatalf("registered getter %d returned nil", i)
			}
		}()
	}
}

func TestSimpleCellSliceOps(t *testing.T) {
	t.Run("NewcEndcCtosAndEnds", func(t *testing.T) {
		state := newCellSliceState()
		if err := NEWC().Interpret(state); err != nil {
			t.Fatalf("NEWC failed: %v", err)
		}
		builder := popCellSliceBuilder(t, state)
		builder.MustStoreUInt(0xAB, 8)
		pushCellSliceBuilder(t, state, builder)
		if err := ENDC().Interpret(state); err != nil {
			t.Fatalf("ENDC failed: %v", err)
		}
		cl := popCellSliceCell(t, state)
		pushCellSliceCell(t, state, cl)
		if err := CTOS().Interpret(state); err != nil {
			t.Fatalf("CTOS failed: %v", err)
		}
		sl := popCellSliceSlice(t, state)
		if sl.BitsLeft() != 8 || sl.MustLoadUInt(8) != 0xAB {
			t.Fatal("unexpected CTOS slice")
		}

		pushCellSliceSlice(t, state, cell.BeginCell().EndCell().BeginParse())
		if err := ENDS().Interpret(state); err != nil {
			t.Fatalf("ENDS failed on empty slice: %v", err)
		}

		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(1, 1).ToSlice())
		if err := ENDS().Interpret(state); err == nil {
			t.Fatal("expected ENDS to fail on remaining bits")
		}
	})

	t.Run("HashOpsMatchUnderlyingHashes", func(t *testing.T) {
		cl := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
		state := newCellSliceState()
		pushCellSliceCell(t, state, cl)
		if err := HASHCU().Interpret(state); err != nil {
			t.Fatalf("HASHCU failed: %v", err)
		}
		gotCU, err := state.Stack.PopInt()
		if err != nil {
			t.Fatalf("failed to pop HASHCU result: %v", err)
		}
		if gotCU.Cmp(new(big.Int).SetBytes(cl.Hash())) != 0 {
			t.Fatal("unexpected HASHCU value")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cl.BeginParse())
		if err := HASHSU().Interpret(state); err != nil {
			t.Fatalf("HASHSU failed: %v", err)
		}
		gotSU, err := state.Stack.PopInt()
		if err != nil {
			t.Fatalf("failed to pop HASHSU result: %v", err)
		}
		if gotSU.Cmp(new(big.Int).SetBytes(cl.Hash())) != 0 {
			t.Fatal("unexpected HASHSU value")
		}
	})

	t.Run("CoinAndIntegerLoadStoreWrappers", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceInt(t, state, 17)
		if err := STGRAMS().Interpret(state); err != nil {
			t.Fatalf("STGRAMS failed: %v", err)
		}
		builder := popCellSliceBuilder(t, state)
		pushCellSliceSlice(t, state, builder.ToSlice())
		if err := LDGRAMS().Interpret(state); err != nil {
			t.Fatalf("LDGRAMS failed: %v", err)
		}
		sl := popCellSliceSlice(t, state)
		if sl.BitsLeft() != 0 {
			t.Fatalf("expected all grams data to be consumed, got %d bits", sl.BitsLeft())
		}
		if got := popCellSliceInt(t, state); got != 17 {
			t.Fatalf("unexpected grams value: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceInt(t, state, 250)
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STU(8).Interpret(state); err != nil {
			t.Fatalf("STU failed: %v", err)
		}
		builder = popCellSliceBuilder(t, state)
		pushCellSliceSlice(t, state, builder.ToSlice())
		if err := LDU(8).Interpret(state); err != nil {
			t.Fatalf("LDU failed: %v", err)
		}
		sl = popCellSliceSlice(t, state)
		if sl.BitsLeft() != 0 {
			t.Fatalf("expected LDU to consume all bits, got %d", sl.BitsLeft())
		}
		if got := popCellSliceInt(t, state); got != 250 {
			t.Fatalf("unexpected LDU value: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceInt(t, state, -2)
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STI(8).Interpret(state); err != nil {
			t.Fatalf("STI failed: %v", err)
		}
		builder = popCellSliceBuilder(t, state)
		pushCellSliceSlice(t, state, builder.ToSlice())
		if err := LDI(8).Interpret(state); err != nil {
			t.Fatalf("LDI failed: %v", err)
		}
		sl = popCellSliceSlice(t, state)
		if sl.BitsLeft() != 0 {
			t.Fatalf("expected LDI to consume all bits, got %d", sl.BitsLeft())
		}
		if got := popCellSliceInt(t, state); got != -2 {
			t.Fatalf("unexpected LDI value: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xCC, 8).ToSlice())
		if err := PLDU(8).Interpret(state); err != nil {
			t.Fatalf("PLDU failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 0xCC {
			t.Fatalf("unexpected PLDU value: %d", got)
		}
	})

	t.Run("ReferenceAndSliceWrappers", func(t *testing.T) {
		ref := cell.BeginCell().MustStoreUInt(0x44, 8).EndCell()
		src := cell.BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(ref).EndCell().BeginParse()
		state := newCellSliceState()
		pushCellSliceSlice(t, state, src)
		if err := LDREF().Interpret(state); err != nil {
			t.Fatalf("LDREF failed: %v", err)
		}
		rest := popCellSliceSlice(t, state)
		gotRef := popCellSliceCell(t, state)
		if !sameCellHash(gotRef, ref) {
			t.Fatal("unexpected loaded ref")
		}
		if rest.BitsLeft() != 8 || rest.RefsNum() != 0 {
			t.Fatal("unexpected remainder after LDREF")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceInt(t, state, 4)
		if err := LDSLICEX().Interpret(state); err != nil {
			t.Fatalf("LDSLICEX failed: %v", err)
		}
		rest = popCellSliceSlice(t, state)
		part := popCellSliceSlice(t, state)
		if rest.BitsLeft() != 4 || rest.MustLoadUInt(4) != 0xB {
			t.Fatal("unexpected LDSLICEX remainder")
		}
		if part.BitsLeft() != 4 || part.MustLoadUInt(4) != 0xA {
			t.Fatal("unexpected LDSLICEX part")
		}

		state = newCellSliceState()
		pushCellSliceCell(t, state, ref)
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STREF().Interpret(state); err != nil {
			t.Fatalf("STREF failed: %v", err)
		}
		builder := popCellSliceBuilder(t, state)
		if builder.RefsUsed() != 1 {
			t.Fatal("expected STREF to store one ref")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STSLICE().Interpret(state); err != nil {
			t.Fatalf("STSLICE failed: %v", err)
		}
		builder = popCellSliceBuilder(t, state)
		if got := builder.EndCell().BeginParse().MustLoadUInt(8); got != 0xAB {
			t.Fatalf("unexpected STSLICE value: %x", got)
		}

		state = newCellSliceState()
		srcBuilder := cell.BeginCell().MustStoreUInt(0xCD, 8)
		pushCellSliceBuilder(t, state, srcBuilder)
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := STB().Interpret(state); err != nil {
			t.Fatalf("STB failed: %v", err)
		}
		builder = popCellSliceBuilder(t, state)
		if got := builder.EndCell().BeginParse().MustLoadUInt(8); got != 0xCD {
			t.Fatalf("unexpected STB value: %x", got)
		}
	})
}

func TestAdvancedDeserializeOps(t *testing.T) {
	t.Run("LdRefRtosAndSliceCuts", func(t *testing.T) {
		ref := cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()
		state := newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(ref).ToSlice())
		if err := LDREFRTOS().Interpret(state); err != nil {
			t.Fatalf("LDREFRTOS failed: %v", err)
		}
		child := popCellSliceSlice(t, state)
		parent := popCellSliceSlice(t, state)
		if child.BitsLeft() != 8 || child.MustLoadUInt(8) != 0xCD {
			t.Fatal("unexpected child slice")
		}
		if parent.BitsLeft() != 8 || parent.RefsNum() != 0 {
			t.Fatal("unexpected parent remainder")
		}

		tests := []struct {
			name string
			op   interface{ Interpret(*vm.State) error }
			push func(*vm.State)
			want uint64
		}{
			{
				name: "SDCUTFIRST",
				op:   SDCUTFIRST(),
				push: func(st *vm.State) {
					pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
					pushCellSliceInt(t, st, 4)
				},
				want: 0xA,
			},
			{
				name: "SDCUTLAST",
				op:   SDCUTLAST(),
				push: func(st *vm.State) {
					pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
					pushCellSliceInt(t, st, 4)
				},
				want: 0xB,
			},
			{
				name: "SDSKIPLAST",
				op:   SDSKIPLAST(),
				push: func(st *vm.State) {
					pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
					pushCellSliceInt(t, st, 4)
				},
				want: 0xA,
			},
			{
				name: "SDSUBSTR",
				op:   SDSUBSTR(),
				push: func(st *vm.State) {
					pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
					pushCellSliceInt(t, st, 2)
					pushCellSliceInt(t, st, 3)
				},
				want: 0x5,
			},
			{
				name: "SCUTFIRST",
				op:   SCUTFIRST(),
				push: func(st *vm.State) {
					pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
					pushCellSliceInt(t, st, 4)
					pushCellSliceInt(t, st, 0)
				},
				want: 0xA,
			},
			{
				name: "SSKIPFIRST",
				op:   SSKIPFIRST(),
				push: func(st *vm.State) {
					pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
					pushCellSliceInt(t, st, 4)
					pushCellSliceInt(t, st, 0)
				},
				want: 0xB,
			},
			{
				name: "SCUTLAST",
				op:   SCUTLAST(),
				push: func(st *vm.State) {
					pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
					pushCellSliceInt(t, st, 4)
					pushCellSliceInt(t, st, 0)
				},
				want: 0xB,
			},
			{
				name: "SSKIPLAST",
				op:   SSKIPLAST(),
				push: func(st *vm.State) {
					pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
					pushCellSliceInt(t, st, 4)
					pushCellSliceInt(t, st, 0)
				},
				want: 0xA,
			},
			{
				name: "SUBSLICE",
				op:   SUBSLICE(),
				push: func(st *vm.State) {
					pushCellSliceSlice(t, st, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
					pushCellSliceInt(t, st, 2)
					pushCellSliceInt(t, st, 0)
					pushCellSliceInt(t, st, 3)
					pushCellSliceInt(t, st, 0)
				},
				want: 0x5,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				st := newCellSliceState()
				tt.push(st)
				if err := tt.op.Interpret(st); err != nil {
					t.Fatalf("%s failed: %v", tt.name, err)
				}
				sl := popCellSliceSlice(t, st)
				if sl.MustLoadUInt(sl.BitsLeft()) != tt.want {
					t.Fatalf("unexpected %s result", tt.name)
				}
			})
		}
	})

	t.Run("SplitAndXLoadVariants", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceInt(t, state, 4)
		pushCellSliceInt(t, state, 0)
		if err := SPLIT().Interpret(state); err != nil {
			t.Fatalf("SPLIT failed: %v", err)
		}
		rest := popCellSliceSlice(t, state)
		first := popCellSliceSlice(t, state)
		if rest.MustLoadUInt(4) != 0xB || first.MustLoadUInt(4) != 0xA {
			t.Fatal("unexpected split result")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		pushCellSliceInt(t, state, 16)
		pushCellSliceInt(t, state, 0)
		if err := SPLITQ().Interpret(state); err != nil {
			t.Fatalf("SPLITQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected SPLITQ failure flag")
		}
		if sl := popCellSliceSlice(t, state); sl.BitsLeft() != 8 {
			t.Fatal("expected SPLITQ failure to preserve source")
		}

		ordinary := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
		state = newCellSliceState()
		pushCellSliceCell(t, state, ordinary)
		if err := XCTOS().Interpret(state); err != nil {
			t.Fatalf("XCTOS failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("ordinary XCTOS should not be special")
		}
		if sl := popCellSliceSlice(t, state); sl.MustLoadUInt(8) != 0xAB {
			t.Fatal("unexpected XCTOS slice")
		}

		state = newCellSliceState()
		pushCellSliceCell(t, state, mustPrunedCell(t))
		if err := XCTOS().Interpret(state); err != nil {
			t.Fatalf("XCTOS special failed: %v", err)
		}
		if !popCellSliceBool(t, state) {
			t.Fatal("special XCTOS should report special")
		}
		popCellSliceSlice(t, state)

		state = newCellSliceState()
		pushCellSliceCell(t, state, ordinary)
		if err := XLOAD().Interpret(state); err != nil {
			t.Fatalf("XLOAD failed: %v", err)
		}
		if !sameCellHash(popCellSliceCell(t, state), ordinary) {
			t.Fatal("unexpected XLOAD result")
		}

		state = newCellSliceState()
		pushCellSliceCell(t, state, mustPrunedCell(t))
		if err := XLOADQ().Interpret(state); err != nil {
			t.Fatalf("XLOADQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected XLOADQ failure flag")
		}

		state = newCellSliceState()
		pushCellSliceCell(t, state, mustPrunedCell(t))
		if err := XLOAD().Interpret(state); err == nil {
			t.Fatal("expected XLOAD on pruned cell to fail")
		}
	})

	t.Run("SliceChecksReferenceLoadersAndCounts", func(t *testing.T) {
		withRef := cell.BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(cell.BeginCell().EndCell()).ToSlice()
		state := newCellSliceState()
		pushCellSliceSlice(t, state, withRef.Copy())
		pushCellSliceInt(t, state, 8)
		if err := SCHKBITS().Interpret(state); err != nil {
			t.Fatalf("SCHKBITS failed: %v", err)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, withRef.Copy())
		pushCellSliceInt(t, state, 1)
		if err := SCHKREFS().Interpret(state); err != nil {
			t.Fatalf("SCHKREFS failed: %v", err)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, withRef.Copy())
		pushCellSliceInt(t, state, 16)
		if err := SCHKBITSQ().Interpret(state); err != nil {
			t.Fatalf("SCHKBITSQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected SCHKBITSQ to report false")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, withRef.Copy())
		pushCellSliceInt(t, state, 2)
		if err := SCHKREFSQ().Interpret(state); err != nil {
			t.Fatalf("SCHKREFSQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected SCHKREFSQ to report false")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, withRef.Copy())
		pushCellSliceInt(t, state, 8)
		pushCellSliceInt(t, state, 1)
		if err := SCHKBITREFS().Interpret(state); err != nil {
			t.Fatalf("SCHKBITREFS failed: %v", err)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, withRef.Copy())
		pushCellSliceInt(t, state, 16)
		pushCellSliceInt(t, state, 1)
		if err := SCHKBITREFSQ().Interpret(state); err != nil {
			t.Fatalf("SCHKBITREFSQ failed: %v", err)
		}
		if popCellSliceBool(t, state) {
			t.Fatal("expected SCHKBITREFSQ to report false")
		}

		ref0 := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
		ref1 := cell.BeginCell().MustStoreUInt(2, 2).EndCell()
		refSlice := cell.BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(ref0).MustStoreRef(ref1).ToSlice()

		state = newCellSliceState()
		pushCellSliceSlice(t, state, refSlice.Copy())
		pushCellSliceInt(t, state, 1)
		if err := PLDREFVAR().Interpret(state); err != nil {
			t.Fatalf("PLDREFVAR failed: %v", err)
		}
		if !sameCellHash(popCellSliceCell(t, state), ref1) {
			t.Fatal("unexpected PLDREFVAR ref")
		}

		op := PLDREFIDX(0)
		decoded := PLDREFIDX(1)
		if err := decoded.Deserialize(op.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("PLDREFIDX deserialize failed: %v", err)
		}
		state = newCellSliceState()
		pushCellSliceSlice(t, state, refSlice.Copy())
		if err := decoded.Interpret(state); err != nil {
			t.Fatalf("PLDREFIDX failed: %v", err)
		}
		if !sameCellHash(popCellSliceCell(t, state), ref0) {
			t.Fatal("unexpected PLDREFIDX ref")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, refSlice.Copy())
		if err := SBITS().Interpret(state); err != nil {
			t.Fatalf("SBITS failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 8 {
			t.Fatalf("unexpected SBITS: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, refSlice.Copy())
		if err := SREFS().Interpret(state); err != nil {
			t.Fatalf("SREFS failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 2 {
			t.Fatalf("unexpected SREFS: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, refSlice.Copy())
		if err := SBITREFS().Interpret(state); err != nil {
			t.Fatalf("SBITREFS failed: %v", err)
		}
		if refs := popCellSliceInt(t, state); refs != 2 {
			t.Fatalf("unexpected SBITREFS refs: %d", refs)
		}
		if bits := popCellSliceInt(t, state); bits != 8 {
			t.Fatalf("unexpected SBITREFS bits: %d", bits)
		}
	})

	t.Run("SameBitAndDepthOps", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreSlice([]byte{0x3F}, 6).ToSlice())
		if err := LDZEROES().Interpret(state); err != nil {
			t.Fatalf("LDZEROES failed: %v", err)
		}
		sl := popCellSliceSlice(t, state)
		if got := popCellSliceInt(t, state); got != 2 {
			t.Fatalf("unexpected LDZEROES count: %d", got)
		}
		if sl.BitsLeft() != 4 {
			t.Fatal("expected LDZEROES to leave remainder")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreSlice([]byte{0xE0}, 3).ToSlice())
		if err := LDONES().Interpret(state); err != nil {
			t.Fatalf("LDONES failed: %v", err)
		}
		popCellSliceSlice(t, state)
		if got := popCellSliceInt(t, state); got != 3 {
			t.Fatalf("unexpected LDONES count: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreSlice([]byte{0xE0}, 3).ToSlice())
		pushCellSliceInt(t, state, 1)
		if err := LDSAME().Interpret(state); err != nil {
			t.Fatalf("LDSAME failed: %v", err)
		}
		popCellSliceSlice(t, state)
		if got := popCellSliceInt(t, state); got != 3 {
			t.Fatalf("unexpected LDSAME count: %d", got)
		}

		deep := cell.BeginCell().MustStoreRef(cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).EndCell()).EndCell()
		state = newCellSliceState()
		pushCellSliceSlice(t, state, deep.BeginParse())
		if err := SDEPTH().Interpret(state); err != nil {
			t.Fatalf("SDEPTH failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 2 {
			t.Fatalf("unexpected SDEPTH: %d", got)
		}

		state = newCellSliceState()
		if err := state.Stack.PushAny(nil); err != nil {
			t.Fatalf("failed to push nil maybe-cell: %v", err)
		}
		if err := CDEPTH().Interpret(state); err != nil {
			t.Fatalf("CDEPTH nil failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 0 {
			t.Fatalf("unexpected nil CDEPTH: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceCell(t, state, deep)
		if err := CDEPTH().Interpret(state); err != nil {
			t.Fatalf("CDEPTH failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 2 {
			t.Fatalf("unexpected CDEPTH: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceCell(t, state, deep)
		if err := CLEVEL().Interpret(state); err != nil {
			t.Fatalf("CLEVEL failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 0 {
			t.Fatalf("unexpected CLEVEL: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceCell(t, state, deep)
		if err := CLEVELMASK().Interpret(state); err != nil {
			t.Fatalf("CLEVELMASK failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 0 {
			t.Fatalf("unexpected CLEVELMASK: %d", got)
		}
	})
}

func TestConstStoreHelpersAndOps(t *testing.T) {
	t.Run("RefConstRoundTripAndInterpret", func(t *testing.T) {
		ref0 := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		ref1 := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
		op := STREF2CONST(ref0, ref1)
		decoded := STREFCONST(nil)
		if err := decoded.Deserialize(op.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("STREFCONST deserialize failed: %v", err)
		}
		if got := decoded.SerializeText(); got != "STREF2CONST" {
			t.Fatalf("unexpected STREFCONST text: %q", got)
		}
		if got := decoded.InstructionBits(); got != 16 {
			t.Fatalf("unexpected STREFCONST bits: %d", got)
		}

		state := newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := decoded.Interpret(state); err != nil {
			t.Fatalf("STREFCONST failed: %v", err)
		}
		builder := popCellSliceBuilder(t, state)
		if builder.RefsUsed() != 2 {
			t.Fatal("expected STREFCONST to store two refs")
		}
	})

	t.Run("SliceConstRoundTripAndRemainderOps", func(t *testing.T) {
		value := cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice()
		op := STSLICECONST(value)
		decoded := STSLICECONST(cell.BeginCell().EndCell().BeginParse())
		if err := decoded.Deserialize(op.Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("STSLICECONST deserialize failed: %v", err)
		}
		if got := decoded.SerializeText(); got == "" {
			t.Fatal("expected non-empty STSLICECONST text")
		}
		if got := decoded.InstructionBits(); got != 14 {
			t.Fatalf("unexpected STSLICECONST bits: %d", got)
		}

		state := newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := decoded.Interpret(state); err != nil {
			t.Fatalf("STSLICECONST failed: %v", err)
		}
		builder := popCellSliceBuilder(t, state)
		if got := builder.EndCell().BeginParse().MustLoadUInt(4); got != 0xA {
			t.Fatalf("unexpected STSLICECONST value: %x", got)
		}

		if got := paddedConstStoreSliceBits(3); got != 10 {
			t.Fatalf("unexpected padded const bits: %d", got)
		}
		payload := encodeConstStoreSlicePayload(cell.BeginCell().MustStoreUInt(1, 1).ToSlice(), 10).EndCell().BeginParse()
		if payload.BitsLeft() != 10 {
			t.Fatalf("unexpected const payload bits: %d", payload.BitsLeft())
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, cell.BeginCell())
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreUInt(1, 1))
		if err := ENDCST().Interpret(state); err != nil {
			t.Fatalf("ENDCST failed: %v", err)
		}
		builder = popCellSliceBuilder(t, state)
		if builder.RefsUsed() != 1 {
			t.Fatal("expected ENDCST to store one ref")
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, builder)
		if err := BREMBITS().Interpret(state); err != nil {
			t.Fatalf("BREMBITS failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 1023 {
			t.Fatalf("unexpected BREMBITS remainder: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, builder)
		if err := BREMREFS().Interpret(state); err != nil {
			t.Fatalf("BREMREFS failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 3 {
			t.Fatalf("unexpected BREMREFS remainder: %d", got)
		}

		state = newCellSliceState()
		pushCellSliceBuilder(t, state, builder)
		if err := BREMBITREFS().Interpret(state); err != nil {
			t.Fatalf("BREMBITREFS failed: %v", err)
		}
		if refs := popCellSliceInt(t, state); refs != 3 {
			t.Fatalf("unexpected BREMBITREFS refs: %d", refs)
		}
	})
}

func TestLittleEndianHelpers(t *testing.T) {
	if got := decodeLEInt([]byte{0x34, 0x12, 0x00, 0x00}, true); got.Uint64() != 0x1234 {
		t.Fatal("unexpected unsigned little-endian decode")
	}
	if got, err := encodeLEInt(big.NewInt(-2), 8, false); err != nil || len(got) != 8 {
		t.Fatalf("unexpected encoded LE length: %d", len(got))
	}
	if !fitsUnsignedBits(big.NewInt(255), 8) {
		t.Fatal("expected unsigned fit")
	}
	if !fitsSignedBits(big.NewInt(-1), 8) {
		t.Fatal("expected signed fit")
	}
	if fitsSignedBits(big.NewInt(128), 8) {
		t.Fatal("did not expect signed fit")
	}
}

func TestFixedWidthLoadStoreRoundTripsAndErrors(t *testing.T) {
	t.Run("RoundTrips", func(t *testing.T) {
		state := newCellSliceState()
		ldu := LDU(1)
		if err := ldu.Deserialize(LDU(8).Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("LDU deserialize failed: %v", err)
		}
		if got := ldu.SerializeText(); got != "8 LDU" {
			t.Fatalf("unexpected LDU text: %q", got)
		}
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice())
		if err := ldu.Interpret(state); err != nil {
			t.Fatalf("LDU failed: %v", err)
		}
		if popCellSliceSlice(t, state).BitsLeft() != 0 || popCellSliceInt(t, state) != 0xAB {
			t.Fatal("unexpected LDU round-trip result")
		}

		state = newCellSliceState()
		ldi := LDI(1)
		if err := ldi.Deserialize(LDI(8).Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("LDI deserialize failed: %v", err)
		}
		if got := ldi.SerializeText(); got != "8 LDI" {
			t.Fatalf("unexpected LDI text: %q", got)
		}
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreInt(-2, 8).ToSlice())
		if err := ldi.Interpret(state); err != nil {
			t.Fatalf("LDI failed: %v", err)
		}
		if popCellSliceSlice(t, state).BitsLeft() != 0 || popCellSliceInt(t, state) != -2 {
			t.Fatal("unexpected LDI round-trip result")
		}

		state = newCellSliceState()
		pldu := PLDU(1)
		if err := pldu.Deserialize(PLDU(8).Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("PLDU deserialize failed: %v", err)
		}
		if got := pldu.SerializeText(); got != "8 PLDU" {
			t.Fatalf("unexpected PLDU text: %q", got)
		}
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xCD, 8).ToSlice())
		if err := pldu.Interpret(state); err != nil {
			t.Fatalf("PLDU failed: %v", err)
		}
		if got := popCellSliceInt(t, state); got != 0xCD {
			t.Fatalf("unexpected PLDU value: %d", got)
		}

		state = newCellSliceState()
		stu := STU(1)
		if err := stu.Deserialize(STU(8).Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("STU deserialize failed: %v", err)
		}
		if got := stu.SerializeText(); got != "8 STU" {
			t.Fatalf("unexpected STU text: %q", got)
		}
		pushCellSliceInt(t, state, 0xEE)
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := stu.Interpret(state); err != nil {
			t.Fatalf("STU failed: %v", err)
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadUInt(8); got != 0xEE {
			t.Fatalf("unexpected STU value: %x", got)
		}

		state = newCellSliceState()
		sti := STI(1)
		if err := sti.Deserialize(STI(8).Serialize().EndCell().BeginParse()); err != nil {
			t.Fatalf("STI deserialize failed: %v", err)
		}
		if got := sti.SerializeText(); got != "8 STI" {
			t.Fatalf("unexpected STI text: %q", got)
		}
		pushCellSliceInt(t, state, -3)
		pushCellSliceBuilder(t, state, cell.BeginCell())
		if err := sti.Interpret(state); err != nil {
			t.Fatalf("STI failed: %v", err)
		}
		if got := popCellSliceBuilder(t, state).EndCell().BeginParse().MustLoadInt(8); got != -3 {
			t.Fatalf("unexpected STI value: %d", got)
		}
	})

	t.Run("ErrorBranches", func(t *testing.T) {
		state := newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xF, 4).ToSlice())
		if err := LDU(8).Interpret(state); err == nil {
			t.Fatal("expected LDU underflow")
		}

		state = newCellSliceState()
		pushCellSliceSlice(t, state, cell.BeginCell().MustStoreUInt(0xF, 4).ToSlice())
		if err := PLDU(8).Interpret(state); err == nil {
			t.Fatal("expected PLDU underflow")
		}

		state = newCellSliceState()
		pushCellSliceInt(t, state, 1)
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreSlice(make([]byte, 128), 1020))
		if err := STU(8).Interpret(state); err == nil {
			t.Fatal("expected STU overflow error")
		}

		state = newCellSliceState()
		pushCellSliceInt(t, state, 1)
		pushCellSliceBuilder(t, state, cell.BeginCell().MustStoreSlice(make([]byte, 128), 1020))
		if err := STI(8).Interpret(state); err == nil {
			t.Fatal("expected STI overflow error")
		}
	})
}

func TestConstAndBeginsConstMetadata(t *testing.T) {
	ref0 := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	ref1 := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	if got := len(STREF2CONST(ref0, ref1).GetPrefixes()); got == 0 {
		t.Fatal("expected STREFCONST prefixes")
	}

	value := cell.BeginCell().MustStoreUInt(0xA, 4).ToSlice()
	if got := len(STSLICECONST(value).GetPrefixes()); got == 0 {
		t.Fatal("expected STSLICECONST prefixes")
	}

	begins := SDBEGINSCONST(value, true)
	if got := len(begins.GetPrefixes()); got == 0 {
		t.Fatal("expected SDBEGINSCONST prefixes")
	}
	if got := begins.SerializeText(); got == "" {
		t.Fatal("expected SDBEGINSCONST text")
	}
	if got := begins.InstructionBits(); got != 21 {
		t.Fatalf("unexpected SDBEGINSCONST bits: %d", got)
	}
}
