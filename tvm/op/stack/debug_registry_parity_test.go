package stack

import (
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestDebugOpcodesTraceDisabledDoNoHostWork(t *testing.T) {
	state := &vm.State{Stack: vm.NewStack()}
	if err := state.Stack.PushSlice(cell.BeginCell().MustStoreSlice([]byte("payload"), 56).EndCell().MustBeginParse()); err != nil {
		t.Fatalf("push slice: %v", err)
	}

	for _, tt := range []struct {
		name string
		op   interface{ Interpret(*vm.State) error }
	}{
		{name: "DUMPSTK", op: DUMPSTK()},
		{name: "DUMP", op: DUMP(0)},
		{name: "DEBUG", op: DEBUG(42)},
		{name: "STRDUMP", op: STRDUMP()},
		{name: "DEBUGSTR", op: DEBUGSTR([]byte("payload"))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var runErr error
			allocs := testing.AllocsPerRun(1000, func() {
				runErr = tt.op.Interpret(state)
			})
			if runErr != nil {
				t.Fatalf("interpret: %v", runErr)
			}
			if allocs != 0 {
				t.Fatalf("trace-disabled execution allocated %.2f times per run", allocs)
			}
		})
	}
}

func TestDumpStackTraceCapsHostWorkAt255Values(t *testing.T) {
	stack := vm.NewStack()
	for i := int64(0); i < 300; i++ {
		if err := stack.PushSmallInt(i); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}

	out := captureTrace(t, stack, func(state *vm.State) {
		if err := DUMPSTK().Interpret(state); err != nil {
			t.Fatalf("DUMPSTK: %v", err)
		}
	})
	if !strings.Contains(out, "stack(300 values) : ... ") {
		t.Fatalf("missing truncated stack marker: %q", out)
	}
	if !strings.Contains(out, ": ... 45 [int]") || strings.Count(out, "[int]") != 255 {
		t.Fatalf("DUMPSTK did not retain exactly the top 255 values: %q", out)
	}
}

func TestSwapOwnsCanonicalCompactPrefix(t *testing.T) {
	var names []string
	for _, getter := range vm.List {
		op := getter()
		for _, prefix := range op.GetPrefixes() {
			if prefix.BitsLeft() != 8 {
				continue
			}
			value, err := prefix.PreloadUInt(8)
			if err == nil && value == 0x01 {
				names = append(names, op.SerializeText())
			}
		}
	}

	if len(names) != 1 || names[0] != "SWAP" {
		t.Fatalf("compact prefix 01 registrations = %v, want [SWAP]", names)
	}
}

func TestPushCtrHasSingleCanonicalRegistration(t *testing.T) {
	var names []string
	for _, getter := range vm.List {
		op := getter()
		for _, prefix := range op.GetPrefixes() {
			if prefix.BitsLeft() != 16 {
				continue
			}
			value, err := prefix.PreloadUInt(16)
			if err == nil && value == 0xED40 {
				names = append(names, op.SerializeText())
			}
		}
	}

	if len(names) != 1 || names[0] != "c0 PUSH" {
		t.Fatalf("ED40 registrations = %v, want one canonical c0 PUSH", names)
	}
}

func BenchmarkDumpStackTraceDisabled(b *testing.B) {
	state := &vm.State{Stack: vm.NewStack()}
	for i := 0; i < 4096; i++ {
		if err := state.Stack.PushSmallInt(1); err != nil {
			b.Fatalf("push: %v", err)
		}
	}
	op := DUMPSTK()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := op.Interpret(state); err != nil {
			b.Fatalf("interpret: %v", err)
		}
	}
}
