package dict

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func benchmarkDictRoot(b *testing.B, keyBits uint, count int) *cell.Cell {
	b.Helper()

	d := cell.NewDict(keyBits)
	for i := 0; i < count; i++ {
		key := cell.BeginCell().MustStoreUInt(uint64(i), keyBits).EndCell()
		value := cell.BeginCell().MustStoreUInt(uint64(i^0x5A), 16).EndCell()
		if _, err := d.SetWithMode(key, value, cell.DictSetModeSet); err != nil {
			b.Fatal(err)
		}
	}
	return d.AsCell()
}

func BenchmarkDICTGetInterpret(b *testing.B) {
	const keyBits = 16
	root := benchmarkDictRoot(b, keyBits, 256)
	uintKey := big.NewInt(173)
	sliceKey := cell.BeginCell().MustStoreUInt(173, keyBits).EndCell().MustBeginParse()

	benchmarks := []struct {
		name    string
		variant dictValueVariant
		pushKey func(*vm.Stack) error
	}{
		{
			name:    "uint",
			variant: dictValueVariant{kind: dictKeyUnsignedInt},
			pushKey: func(stack *vm.Stack) error { return stack.PushInt(uintKey) },
		},
		{
			name:    "slice",
			variant: dictValueVariant{kind: dictKeySlice},
			pushKey: func(stack *vm.Stack) error { return stack.PushSlice(sliceKey) },
		},
		{
			name:    "uint_ref",
			variant: dictValueVariant{kind: dictKeyUnsignedInt, byRef: true},
			pushKey: func(stack *vm.Stack) error { return stack.PushInt(uintKey) },
		},
	}

	// The ref variant needs single-reference values, unlike the scalar fixture.
	refDict := cell.NewDict(keyBits)
	refValue := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	for i := 0; i < 256; i++ {
		key := cell.BeginCell().MustStoreUInt(uint64(i), keyBits).EndCell()
		if _, err := refDict.SetBuilderWithMode(key, cell.BeginCell().MustStoreRef(refValue), cell.DictSetModeSet); err != nil {
			b.Fatal(err)
		}
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			action := execDictGet(bm.variant)
			state := &vm.State{}
			benchRoot := root
			if bm.variant.byRef {
				benchRoot = refDict.AsCell()
			}
			b.ReportAllocs()
			for b.Loop() {
				state.Stack = vm.NewStack()
				if err := bm.pushKey(state.Stack); err != nil {
					b.Fatal(err)
				}
				if err := state.Stack.PushMaybeCell(benchRoot); err != nil {
					b.Fatal(err)
				}
				if err := state.Stack.PushSmallInt(keyBits); err != nil {
					b.Fatal(err)
				}
				if err := action(state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDICTUSetInterpret(b *testing.B) {
	const keyBits = 16
	root := benchmarkDictRoot(b, keyBits, 256)
	key := big.NewInt(173)
	value := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell().MustBeginParse()
	action := execDictSet(cell.DictSetModeSet)(dictValueVariant{kind: dictKeyUnsignedInt})
	state := &vm.State{}

	b.ReportAllocs()
	for b.Loop() {
		state.Stack = vm.NewStack()
		if err := state.Stack.PushSlice(value); err != nil {
			b.Fatal(err)
		}
		if err := state.Stack.PushInt(key); err != nil {
			b.Fatal(err)
		}
		if err := state.Stack.PushMaybeCell(root); err != nil {
			b.Fatal(err)
		}
		if err := state.Stack.PushSmallInt(keyBits); err != nil {
			b.Fatal(err)
		}
		if err := action(state); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSUBDICTInterpret(b *testing.B) {
	const (
		keyBits    = 16
		prefixBits = 8
	)
	root := benchmarkDictRoot(b, keyBits, 256)
	intPrefix := big.NewInt(0)
	slicePrefix := cell.BeginCell().MustStoreUInt(0, prefixBits).EndCell().MustBeginParse()

	benchmarks := []struct {
		name    string
		kind    dictKeyKind
		pushKey func(*vm.Stack) error
	}{
		{
			name:    "uint",
			kind:    dictKeyUnsignedInt,
			pushKey: func(stack *vm.Stack) error { return stack.PushInt(intPrefix) },
		},
		{
			name:    "slice",
			kind:    dictKeySlice,
			pushKey: func(stack *vm.Stack) error { return stack.PushSlice(slicePrefix) },
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			action := execSubdict(false)(dictScalarVariant{kind: bm.kind})
			state := &vm.State{}
			b.ReportAllocs()
			for b.Loop() {
				state.Stack = vm.NewStack()
				if err := bm.pushKey(state.Stack); err != nil {
					b.Fatal(err)
				}
				if err := state.Stack.PushSmallInt(prefixBits); err != nil {
					b.Fatal(err)
				}
				if err := state.Stack.PushMaybeCell(root); err != nil {
					b.Fatal(err)
				}
				if err := state.Stack.PushSmallInt(keyBits); err != nil {
					b.Fatal(err)
				}
				if err := action(state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
