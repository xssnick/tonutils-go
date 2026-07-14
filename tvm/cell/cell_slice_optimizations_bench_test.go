package cell

import "testing"

var (
	benchmarkSliceResult  *Slice
	benchmarkBytesResult  []byte
	benchmarkStringResult string
)

func BenchmarkCellSliceLoadRefTraced(b *testing.B) {
	childTrace := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	trace := NewTrace(TraceHooks{OnChild: func(int) *Trace { return childTrace }})
	leaf := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).EndCell().WithTrace(trace)
	base := *root.MustBeginParse()

	b.ReportAllocs()
	for b.Loop() {
		s := base
		loaded, err := s.LoadRef()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSliceResult = loaded
	}
}

func BenchmarkCellSliceLoadBinarySnake(b *testing.B) {
	data := make([]byte, 127*128)
	for i := range data {
		data[i] = byte(i*31 + 7)
	}
	root := BeginCell().MustStoreBinarySnake(data).EndCell()
	base := *root.MustBeginParse()

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		s := base
		loaded, err := s.LoadBinarySnake()
		if err != nil {
			b.Fatal(err)
		}
		benchmarkBytesResult = loaded
	}
}

func BenchmarkCellDumpDeep(b *testing.B) {
	root := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	for range 255 {
		root = BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(root).EndCell()
	}

	b.ReportAllocs()
	for b.Loop() {
		benchmarkStringResult = root.Dump()
	}
}
