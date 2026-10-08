package cell

import (
	"math/big"
	"testing"
)

func newDictionaryViewLoadTrace() (*Trace, func() int) {
	loads := 0
	var trace *Trace
	trace = NewTrace(TraceHooks{
		OnLoad: func(*Cell) {
			loads++
		},
		OnChild: func(int) *Trace {
			return trace
		},
	})
	return trace, func() int { return loads }
}

func dictionaryViewKey(value uint64, bits uint) *Cell {
	return BeginCell().MustStoreUInt(value, bits).EndCell()
}

func TestAsDictWithTraceLegacyPathsMatchSetTrace(t *testing.T) {
	base := NewDict(8)
	value := BeginCell().MustStoreUInt(5, 3).EndCell()
	if err := base.SetIntKey(big.NewInt(0x12), value); err != nil {
		t.Fatal(err)
	}
	if err := base.SetIntKey(big.NewInt(0x13), value); err != nil {
		t.Fatal(err)
	}
	root := base.AsCell().WithoutTrace()
	common := dictionaryViewKey(1, 4)
	extract := dictionaryViewKey(0, 1)

	tests := []struct {
		name string
		run  func(*Dictionary) error
	}{
		{
			name: "range",
			run: func(dict *Dictionary) error {
				_, err := dict.Range(false, false)
				return err
			},
		},
		{
			name: "has common prefix",
			run: func(dict *Dictionary) error {
				_, err := dict.HasCommonPrefix(common)
				return err
			},
		},
		{
			name: "get common prefix",
			run: func(dict *Dictionary) error {
				_, err := dict.GetCommonPrefix()
				return err
			},
		},
		{
			name: "shuffle foreach",
			run: func(dict *Dictionary) error {
				_, err := dict.CheckForEach(func(*Slice, *Cell) (bool, error) {
					return true, nil
				}, false, true)
				return err
			},
		},
		{
			name: "load all",
			run: func(dict *Dictionary) error {
				_, err := dict.LoadAll()
				return err
			},
		},
		{
			name: "set",
			run: func(dict *Dictionary) error {
				_, err := dict.SetBuilderByIntKeyWithMode(big.NewInt(0x14), BeginCell().MustStoreUInt(6, 3), DictSetModeSet)
				return err
			},
		},
		{
			name: "delete",
			run: func(dict *Dictionary) error {
				_, err := dict.DeleteByIntKey(big.NewInt(0x12))
				return err
			},
		},
		{
			name: "delete min",
			run: func(dict *Dictionary) error {
				_, _, err := dict.LoadMinMaxAndDelete(false, false)
				return err
			},
		},
		{
			name: "load and set",
			run: func(dict *Dictionary) error {
				_, _, err := dict.LoadValueAndSetBuilderByIntKeyWithMode(big.NewInt(0x12), BeginCell().MustStoreUInt(7, 3), DictSetModeSet)
				return err
			},
		},
		{
			name: "load and delete",
			run: func(dict *Dictionary) error {
				_, err := dict.LoadValueAndDeleteByIntKey(big.NewInt(0x12))
				return err
			},
		},
		{
			name: "as cell",
			run: func(dict *Dictionary) error {
				_, err := dict.AsCell().BeginParse()
				return err
			},
		},
		{
			name: "to cell",
			run: func(dict *Dictionary) error {
				root, err := dict.ToCell()
				if err != nil {
					return err
				}
				_, err = root.BeginParse()
				return err
			},
		},
		{
			name: "extract unchanged root",
			run: func(dict *Dictionary) error {
				root, err := dict.ExtractPrefixSubdictRoot(extract, false)
				if err != nil {
					return err
				}
				_, err = root.BeginParse()
				return err
			},
		},
		{
			name: "cut direct slice prefix",
			run: func(dict *Dictionary) error {
				prefix := extract.MustBeginParse()
				_, err := dict.CutPrefixSubdictBySlice(prefix, false)
				return err
			},
		},
		{
			name: "cut direct integer prefix",
			run: func(dict *Dictionary) error {
				_, err := dict.CutPrefixSubdictByInt(big.NewInt(0), 1, false)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			trace, setTraceLoads := newDictionaryViewLoadTrace()
			materialized := root.AsDict(8).SetTrace(trace)
			if err := test.run(materialized); err != nil {
				t.Fatalf("SetTrace path failed: %v", err)
			}

			trace, viewLoads := newDictionaryViewLoadTrace()
			view := root.AsDictWithTrace(8, trace)
			if err := test.run(view); err != nil {
				t.Fatalf("AsDictWithTrace path failed: %v", err)
			}

			want, got := setTraceLoads(), viewLoads()
			if want == 0 {
				t.Fatal("SetTrace path did not load any cells")
			}
			if got != want {
				t.Fatalf("load notifications = %d, want %d", got, want)
			}
		})
	}
}

func TestAsPrefixDictWithTraceMutationAndBoundaryMatchSetTrace(t *testing.T) {
	base := NewPrefixDict(8)
	value := BeginCell().MustStoreUInt(5, 3).EndCell()
	left := dictionaryViewKey(0, 2)
	right := dictionaryViewKey(1, 2)
	if err := base.Set(left, value); err != nil {
		t.Fatal(err)
	}
	if err := base.Set(right, value); err != nil {
		t.Fatal(err)
	}
	root := base.AsCell().WithoutTrace()

	tests := []struct {
		name string
		run  func(*PrefixDictionary) error
	}{
		{
			name: "set",
			run: func(dict *PrefixDictionary) error {
				_, err := dict.SetBuilderBySliceKeyWithMode(left.MustBeginParse(), BeginCell().MustStoreUInt(6, 3), DictSetModeSet)
				return err
			},
		},
		{
			name: "load and delete",
			run: func(dict *PrefixDictionary) error {
				_, err := dict.LoadValueAndDeleteBySliceKey(left.MustBeginParse())
				return err
			},
		},
		{
			name: "as cell",
			run: func(dict *PrefixDictionary) error {
				_, err := dict.AsCell().BeginParse()
				return err
			},
		},
		{
			name: "to cell",
			run: func(dict *PrefixDictionary) error {
				root, err := dict.ToCell()
				if err != nil {
					return err
				}
				_, err = root.BeginParse()
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			trace, setTraceLoads := newDictionaryViewLoadTrace()
			materialized := root.AsPrefixDict(8).SetTrace(trace)
			if err := test.run(materialized); err != nil {
				t.Fatalf("SetTrace path failed: %v", err)
			}

			trace, viewLoads := newDictionaryViewLoadTrace()
			view := root.AsPrefixDictWithTrace(8, trace)
			if err := test.run(view); err != nil {
				t.Fatalf("AsPrefixDictWithTrace path failed: %v", err)
			}

			want, got := setTraceLoads(), viewLoads()
			if want == 0 {
				t.Fatal("SetTrace path did not load any cells")
			}
			if got != want {
				t.Fatalf("load notifications = %d, want %d", got, want)
			}
		})
	}
}
