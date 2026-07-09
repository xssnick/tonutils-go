package tuple

import (
	"math/big"
	"testing"

	tuplepkg "github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func fuzzTupleVersion(raw int64) int {
	version := int(raw % int64(vm.MaxSupportedGlobalVersion+1))
	if version < 0 {
		version = -version
	}
	return version
}

func TestFuzzTupleVersionCoversDefaultRange(t *testing.T) {
	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		if got := fuzzTupleVersion(int64(version)); got != version {
			t.Fatalf("seed version %d mapped to %d", version, got)
		}
	}
	if got := fuzzTupleVersion(-int64(vm.MaxSupportedGlobalVersion)); got != vm.MaxSupportedGlobalVersion {
		t.Fatalf("negative default version mapped to %d, want %d", got, vm.MaxSupportedGlobalVersion)
	}
	for _, raw := range []int64{
		-1,
		-int64(vm.MaxSupportedGlobalVersion) - 1,
		-int64(vm.MaxSupportedGlobalVersion) - 2,
		-123456789,
		123456789,
	} {
		got := fuzzTupleVersion(raw)
		if got < 0 || got > vm.MaxSupportedGlobalVersion {
			t.Fatalf("raw version %d mapped to %d, want within [0, %d]", raw, got, vm.MaxSupportedGlobalVersion)
		}
	}
}

func newTupleVersionedState(version int) *vm.State {
	state := newState()
	state.GlobalVersion = version
	return state
}

func fuzzTupleInts(seed uint64, n int) tuplepkg.Tuple {
	vals := make([]any, n)
	base := int64(seed%4096) - 2048
	for i := range vals {
		vals[i] = big.NewInt(base + int64(i))
	}
	return tuplepkg.NewTupleOwned(vals)
}

func assertTupleVersionErr(t *testing.T, err error, want int64) {
	t.Helper()

	got, ok := vmerr.ErrorCode(err)
	if !ok || got != want {
		t.Fatalf("error code = %d, %v, want %d", got, err, want)
	}
}

func assertTupleFuzzInt(t *testing.T, val any, want int64) {
	t.Helper()

	intVal, ok := val.(*big.Int)
	if !ok {
		t.Fatalf("value type = %T, want *big.Int", val)
	}
	if got := intVal.Int64(); got != want {
		t.Fatalf("value = %d, want %d", got, want)
	}
}

func FuzzTVMVersionedTupleInvariantEdges(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for kind := uint8(0); kind < 8; kind++ {
			f.Add(version, kind, uint64(version)<<8|uint64(kind))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawKind uint8, seed uint64) {
		state := newTupleVersionedState(fuzzTupleVersion(rawVersion))

		switch rawKind % 8 {
		case 0:
			if err := PUSHNULL().Interpret(state); err != nil {
				t.Fatalf("PUSHNULL failed: %v", err)
			}
			if err := ISNULL().Interpret(state); err != nil {
				t.Fatalf("ISNULL failed: %v", err)
			}
			got, err := state.Stack.PopBool()
			if err != nil {
				t.Fatalf("failed to pop bool: %v", err)
			}
			if !got {
				t.Fatalf("ISNULL on null = false, want true")
			}
		case 1:
			if err := state.Stack.PushTuple(fuzzTupleInts(seed, 2)); err != nil {
				t.Fatalf("failed to push tuple: %v", err)
			}
			if err := INDEXQ(5).Interpret(state); err != nil {
				t.Fatalf("INDEXQ failed: %v", err)
			}
			got, err := state.Stack.PopAny()
			if err != nil {
				t.Fatalf("failed to pop INDEXQ result: %v", err)
			}
			if got != nil {
				t.Fatalf("INDEXQ out-of-range result = %v, want nil", got)
			}
		case 2:
			setValue := int64(seed%2048) - 1024
			if err := state.Stack.PushAny(nil); err != nil {
				t.Fatalf("failed to push nil tuple: %v", err)
			}
			if err := state.Stack.PushInt(big.NewInt(setValue)); err != nil {
				t.Fatalf("failed to push value: %v", err)
			}
			if err := state.Stack.PushSmallInt(3); err != nil {
				t.Fatalf("failed to push index: %v", err)
			}
			if err := SETINDEXVARQ().Interpret(state); err != nil {
				t.Fatalf("SETINDEXVARQ failed: %v", err)
			}

			tup, err := state.Stack.PopTuple()
			if err != nil {
				t.Fatalf("failed to pop tuple: %v", err)
			}
			if tup.Len() != 4 {
				t.Fatalf("tuple length = %d, want 4", tup.Len())
			}
			for i := 0; i < 3; i++ {
				val, err := tup.Index(i)
				if err != nil {
					t.Fatalf("tuple[%d] failed: %v", i, err)
				}
				if val != nil {
					t.Fatalf("tuple[%d] = %v, want nil", i, val)
				}
			}
			val, err := tup.Index(3)
			if err != nil {
				t.Fatalf("tuple[3] failed: %v", err)
			}
			assertTupleFuzzInt(t, val, setValue)
		case 3:
			if err := state.Stack.PushAny(vm.NaN{}); err != nil {
				t.Fatalf("failed to push NaN: %v", err)
			}
			if err := QTLEN().Interpret(state); err != nil {
				t.Fatalf("QTLEN failed: %v", err)
			}
			if got := popInt(t, state); got != -1 {
				t.Fatalf("QTLEN on NaN = %d, want -1", got)
			}
		case 4:
			first := int64(seed%2048) - 1024
			inner0 := tuplepkg.NewTupleValue(big.NewInt(first), big.NewInt(first+1))
			inner1 := tuplepkg.NewTupleValue(big.NewInt(first+2), big.NewInt(first+3))
			if err := state.Stack.PushTuple(tuplepkg.NewTupleValue(inner0, inner1)); err != nil {
				t.Fatalf("failed to push nested tuple: %v", err)
			}
			if err := INDEX2(1, 0).Interpret(state); err != nil {
				t.Fatalf("INDEX2 failed: %v", err)
			}
			if got := popInt(t, state); got != first+2 {
				t.Fatalf("INDEX2 result = %d, want %d", got, first+2)
			}
		case 5:
			if err := state.Stack.PushTuple(fuzzTupleInts(seed, 255)); err != nil {
				t.Fatalf("failed to push tuple: %v", err)
			}
			if err := state.Stack.PushInt(big.NewInt(int64(seed % 1024))); err != nil {
				t.Fatalf("failed to push value: %v", err)
			}
			assertTupleVersionErr(t, TPUSH().Interpret(state), vmerr.CodeTypeCheck)
		case 6:
			if err := state.Stack.PushTuple(fuzzTupleInts(seed, 2)); err != nil {
				t.Fatalf("failed to push tuple: %v", err)
			}
			assertTupleVersionErr(t, UNTUPLE(3).Interpret(state), vmerr.CodeTypeCheck)
		case 7:
			if err := state.Stack.PushAny(nil); err != nil {
				t.Fatalf("failed to push nil tuple: %v", err)
			}
			if err := state.Stack.PushSmallInt(0); err != nil {
				t.Fatalf("failed to push index: %v", err)
			}
			if err := INDEXVARQ().Interpret(state); err != nil {
				t.Fatalf("INDEXVARQ failed: %v", err)
			}
			got, err := state.Stack.PopAny()
			if err != nil {
				t.Fatalf("failed to pop INDEXVARQ result: %v", err)
			}
			if got != nil {
				t.Fatalf("INDEXVARQ nil tuple result = %v, want nil", got)
			}
		}
	})
}

func fuzzTupleQuietIndex(raw uint16, dynamic bool) int {
	if dynamic {
		switch raw {
		case 0, 1, 15, 16, 127, 128, 253, 254:
			return int(raw)
		}
		return int(raw % 255)
	}

	return int(raw % 16)
}

func fuzzTupleLen(raw uint16) int {
	switch raw {
	case 0, 1, 2, 15, 16, 127, 128, 254, 255:
		return int(raw)
	}
	return int(raw % 256)
}

func assertTupleQuietSetIndexResult(t *testing.T, state *vm.State, nilTuple, nilValue bool, oldLen, idx int, setValue int64, gasBefore int64) {
	t.Helper()

	wantGas := int64(0)
	wantNil := nilTuple && nilValue
	wantLen := oldLen
	if !wantNil {
		if nilTuple {
			wantLen = idx + 1
		} else if idx >= oldLen && !nilValue {
			wantLen = idx + 1
		}
		if idx < oldLen || !nilValue {
			wantGas = int64(wantLen) * vm.TupleEntryGasPrice
		}
	}

	if got := state.Gas.Used() - gasBefore; got != wantGas {
		t.Fatalf("SETINDEXQ nilTuple=%v nilValue=%v oldLen=%d idx=%d gas=%d, want %d", nilTuple, nilValue, oldLen, idx, got, wantGas)
	}

	if wantNil {
		got, err := state.Stack.PopAny()
		if err != nil {
			t.Fatalf("pop nil SETINDEXQ result: %v", err)
		}
		if got != nil {
			t.Fatalf("SETINDEXQ nil tuple nil value result = %v, want nil", got)
		}
		return
	}

	tup, err := state.Stack.PopTuple()
	if err != nil {
		t.Fatalf("pop SETINDEXQ tuple result: %v", err)
	}
	if tup.Len() != wantLen {
		t.Fatalf("SETINDEXQ tuple len = %d, want %d", tup.Len(), wantLen)
	}
	if idx < tup.Len() {
		got, err := tup.Index(idx)
		if err != nil {
			t.Fatalf("read tuple[%d]: %v", idx, err)
		}
		if nilValue {
			if got != nil {
				t.Fatalf("tuple[%d] = %v, want nil", idx, got)
			}
		} else {
			assertTupleFuzzInt(t, got, setValue)
		}
	}
	if state.Stack.Len() != 0 {
		t.Fatalf("SETINDEXQ left %d stack values, want 0", state.Stack.Len())
	}
}

func FuzzTVMVersionedTupleQuietSetIndexBoundaries(f *testing.F) {
	for version := int64(0); version <= int64(vm.MaxSupportedGlobalVersion); version++ {
		for _, seed := range []struct {
			dynamic  bool
			index    uint16
			length   uint16
			nilTuple bool
			nilValue bool
		}{
			{false, 0, 0, true, true},
			{false, 3, 0, true, false},
			{false, 3, 1, false, true},
			{false, 3, 1, false, false},
			{true, 0, 0, true, true},
			{true, 16, 1, false, true},
			{true, 16, 1, false, false},
			{true, 254, 1, true, false},
			{true, 254, 255, false, true},
			{true, 254, 255, false, false},
		} {
			rawOp := uint8(0)
			if seed.dynamic {
				rawOp = 1
			}
			f.Add(version, rawOp, seed.index, seed.length, seed.nilTuple, seed.nilValue, uint64(version)<<16|uint64(seed.index))
		}
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, rawOp uint8, rawIndex, rawLen uint16, nilTuple, nilValue bool, seed uint64) {
		version := fuzzTupleVersion(rawVersion)
		dynamic := rawOp%2 == 1
		idx := fuzzTupleQuietIndex(rawIndex, dynamic)
		oldLen := fuzzTupleLen(rawLen)
		setValue := int64(seed%4096) - 2048

		state := newTupleVersionedState(version)
		if nilTuple {
			if err := state.Stack.PushAny(nil); err != nil {
				t.Fatalf("push nil tuple: %v", err)
			}
		} else if err := state.Stack.PushTuple(fuzzTupleInts(seed, oldLen)); err != nil {
			t.Fatalf("push tuple: %v", err)
		}
		if nilValue {
			if err := state.Stack.PushAny(nil); err != nil {
				t.Fatalf("push nil value: %v", err)
			}
		} else if err := state.Stack.PushInt(big.NewInt(setValue)); err != nil {
			t.Fatalf("push value: %v", err)
		}
		if dynamic {
			if err := state.Stack.PushSmallInt(int64(idx)); err != nil {
				t.Fatalf("push index: %v", err)
			}
		}

		gasBefore := state.Gas.Used()
		var err error
		if dynamic {
			err = SETINDEXVARQ().Interpret(state)
		} else {
			err = SETINDEXQ(uint8(idx)).Interpret(state)
		}
		if err != nil {
			t.Fatalf("SETINDEXQ version=%d dynamic=%v idx=%d oldLen=%d nilTuple=%v nilValue=%v failed: %v", version, dynamic, idx, oldLen, nilTuple, nilValue, err)
		}
		assertTupleQuietSetIndexResult(t, state, nilTuple, nilValue, oldLen, idx, setValue, gasBefore)
	})
}
