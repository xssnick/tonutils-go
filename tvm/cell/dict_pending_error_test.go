package cell

import (
	"errors"
	"math/big"
	"testing"
)

func pendingErrorAfterLoads(limit int, pendingErr error) (*Trace, func() int) {
	loads := 0
	var trace *Trace
	trace = NewTrace(TraceHooks{
		OnLoad: func(*Cell) {
			loads++
		},
		OnChild: func(int) *Trace {
			return trace
		},
		PendingError: func() error {
			if loads >= limit {
				return pendingErr
			}
			return nil
		},
	})
	return trace, func() int { return loads }
}

func TestDictionaryDeleteMergeStopsAtPendingLoadError(t *testing.T) {
	pendingErr := errors.New("pending cell load error")
	malformedSurvivor := BeginCell().EndCell()

	tests := []struct {
		name string
		run  func(*Trace) error
	}{
		{
			name: "fixed dictionary",
			run: func(trace *Trace) error {
				leaf := BeginCell().
					MustStoreUInt(0, 2). // hml_short with an empty label
					MustStoreUInt(0xAA, 8).
					EndCell()
				root := BeginCell().
					MustStoreUInt(0, 2). // hml_short with an empty label
					MustStoreRef(leaf).
					MustStoreRef(malformedSurvivor).
					EndCell()

				_, err := root.AsDictWithTrace(1, trace).DeleteByIntKey(big.NewInt(0))
				return err
			},
		},
		{
			name: "prefix dictionary",
			run: func(trace *Trace) error {
				leaf := BeginCell().
					MustStoreUInt(0, 2).     // hml_short with an empty label
					MustStoreBoolBit(false). // leaf constructor
					MustStoreUInt(0xAA, 8).
					EndCell()
				root := BeginCell().
					MustStoreUInt(0, 2).    // hml_short with an empty label
					MustStoreBoolBit(true). // fork constructor
					MustStoreRef(leaf).
					MustStoreRef(malformedSurvivor).
					EndCell()
				key := BeginCell().MustStoreUInt(0, 1).EndCell().MustBeginParse()

				_, err := root.AsPrefixDictWithTrace(1, trace).LoadValueAndDeleteBySliceKey(key)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			trace, loads := pendingErrorAfterLoads(3, pendingErr)
			err := test.run(trace)
			if !errors.Is(err, pendingErr) {
				t.Fatalf("delete error = %v, want pending load error", err)
			}
			if got := loads(); got != 3 {
				t.Fatalf("load notifications = %d, want 3", got)
			}
		})
	}
}

func TestDictionaryLoadAllStopsAtPendingLoadError(t *testing.T) {
	pendingErr := errors.New("pending cell load error")
	dict := NewDict(8)
	if err := dict.SetIntKey(big.NewInt(0x12), BeginCell().MustStoreUInt(0xAA, 8).EndCell()); err != nil {
		t.Fatal(err)
	}

	trace, loads := pendingErrorAfterLoads(1, pendingErr)
	values, err := dict.AsCell().WithoutTrace().AsDictWithTrace(8, trace).LoadAll()
	if !errors.Is(err, pendingErr) {
		t.Fatalf("LoadAll error = %v, want pending load error", err)
	}
	if values != nil {
		t.Fatalf("LoadAll returned %d values after pending load error", len(values))
	}
	if got := loads(); got != 1 {
		t.Fatalf("load notifications = %d, want 1", got)
	}
}

var benchmarkDictionaryLoadAllSink []DictKV

func BenchmarkDictionaryLoadAll(b *testing.B) {
	dict := NewDict(16)
	for i := int64(0); i < 256; i++ {
		if err := dict.SetIntKey(big.NewInt(i), BeginCell().MustStoreUInt(uint64(i), 16).EndCell()); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		values, err := dict.LoadAll()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkDictionaryLoadAllSink = values
	}
}
