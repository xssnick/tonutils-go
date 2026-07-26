package tl

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"runtime"
	"testing"
)

type VectorPreflightVariantSmall struct {
	Value uint32 `tl:"int"`
}

type VectorPreflightVariantLarge struct {
	Key   []byte `tl:"int256"`
	Value uint64 `tl:"long"`
}

type VectorPreflightNode struct {
	Variant  any      `tl:"struct boxed [test.vectorPreflight.variantSmall,test.vectorPreflight.variantLarge]"`
	Key      []byte   `tl:"int256"`
	Values   []uint64 `tl:"vector long"`
	Flags    uint32   `tl:"flags"`
	Optional []byte   `tl:"?0 int256"`
}

type VectorPreflightHolder struct {
	Nodes []VectorPreflightNode `tl:"vector struct"`
}

type VectorPreflightManual struct{}

func (*VectorPreflightManual) Parse(data []byte) ([]byte, error) {
	return data, nil
}

func (*VectorPreflightManual) Serialize(_ *bytes.Buffer) error {
	return nil
}

type VectorPreflightManualHolder struct {
	Values []VectorPreflightManual `tl:"vector struct"`
}

type VectorPreflightCycle struct {
	Next  *VectorPreflightCycle `tl:"struct"`
	Value uint32                `tl:"int"`
}

type VectorPreflightCycleHolder struct {
	Values []VectorPreflightCycle `tl:"vector struct"`
}

type VectorPreflightLateVariant struct {
	Value uint64 `tl:"long"`
}

type VectorPreflightLateNode struct {
	Flags    uint32                   `tl:"flags"`
	Optional *VectorPreflightLateNode `tl:"?0 struct"`
	Variant  any                      `tl:"struct boxed [test.vectorPreflight.lateVariant]"`
}

type VectorPreflightLateHolder struct {
	Values []VectorPreflightLateNode `tl:"vector struct"`
}

type VectorPreflightFixed struct {
	Value uint64 `tl:"long"`
}

type VectorPreflightFixedHolder struct {
	Values []VectorPreflightFixed `tl:"vector struct"`
}

func init() {
	Register(VectorPreflightVariantSmall{}, "test.vectorPreflight.variantSmall value:int = test.VectorPreflightVariant")
	Register(VectorPreflightVariantLarge{}, "test.vectorPreflight.variantLarge key:int256 value:long = test.VectorPreflightVariant")
	Register(VectorPreflightNode{}, "")
	Register(VectorPreflightHolder{}, "")
	Register(VectorPreflightManual{}, "")
	Register(VectorPreflightManualHolder{}, "")

	// Register holders before their dependencies to exercise placeholder refresh.
	Register(VectorPreflightCycleHolder{}, "")
	Register(VectorPreflightCycle{}, "")
	Register(VectorPreflightLateHolder{}, "")
	Register(VectorPreflightLateNode{}, "")
	Register(VectorPreflightLateVariant{}, "test.vectorPreflight.lateVariant value:long = test.VectorPreflightLateVariant")
	Register(VectorPreflightFixed{}, "")
	Register(VectorPreflightFixedHolder{}, "")
}

func TestParseVectorStructRejectsTruncatedBeforeAllocation(t *testing.T) {
	const count = uint32(1 << 20)

	data := make([]byte, 4+count)
	binary.LittleEndian.PutUint32(data, count)

	runtime.GC()

	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	var decoded VectorPreflightHolder
	_, err := Parse(&decoded, data, false)

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if err == nil {
		t.Fatal("expected truncated vector error")
	}

	const expected = "failed to parse tl.VectorPreflightHolder type: not enough bytes to parse vector field Nodes of type tl.VectorPreflightHolder with 1048576 elements, need at least 50331648 bytes"
	if err.Error() != expected {
		t.Fatalf("unexpected error:\n got: %s\nwant: %s", err, expected)
	}

	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
		t.Fatalf("truncated vector parse allocated %d bytes, want at most %d", allocated, 8<<20)
	}
}

func TestParseVectorStructPreflightValid(t *testing.T) {
	original := VectorPreflightHolder{
		Nodes: []VectorPreflightNode{
			{
				Variant:  VectorPreflightVariantSmall{Value: 7},
				Key:      bytes.Repeat([]byte{0x11}, 32),
				Values:   []uint64{1, 2, 3},
				Flags:    1,
				Optional: bytes.Repeat([]byte{0x22}, 32),
			},
			{
				Variant: VectorPreflightVariantLarge{
					Key:   bytes.Repeat([]byte{0x33}, 32),
					Value: 9,
				},
				Key:    bytes.Repeat([]byte{0x44}, 32),
				Values: []uint64{},
			},
		},
	}

	data, err := Serialize(&original, false)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	var decoded VectorPreflightHolder
	rest, err := Parse(&decoded, data, false)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing data: %x", rest)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("decoded value mismatch:\n got: %#v\nwant: %#v", decoded, original)
	}
}

func TestParseVectorManualZeroWireSize(t *testing.T) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 3)

	var decoded VectorPreflightManualHolder
	rest, err := Parse(&decoded, data, false)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing data: %x", rest)
	}
	if len(decoded.Values) != 3 {
		t.Fatalf("decoded %d elements, want 3", len(decoded.Values))
	}
}

func TestParseVectorMinimumWireSizeCycle(t *testing.T) {
	data := make([]byte, 4)

	info := _structInfoTableByType[reflect.TypeOf(VectorPreflightCycleHolder{})]
	if size := info.fields[0].structInfo.minimumWireSize; size != 4 {
		t.Fatalf("minimum wire size = %d, want 4", size)
	}

	var decoded VectorPreflightCycleHolder
	rest, err := Parse(&decoded, data, false)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("unexpected trailing data: %x", rest)
	}
	if len(decoded.Values) != 0 {
		t.Fatalf("decoded %d elements, want 0", len(decoded.Values))
	}
}

func TestVectorMinimumWireSizeRegistrationOrder(t *testing.T) {
	info := _structInfoTableByType[reflect.TypeOf(VectorPreflightLateHolder{})]
	if size := info.fields[0].structInfo.minimumWireSize; size != 16 {
		t.Fatalf("minimum wire size = %d, want 16", size)
	}
}

var vectorPreflightMinimumSink uint64
var vectorPreflightLengthSink int

func parseVectorPreflightFixed(data []byte) {
	var decoded VectorPreflightFixedHolder
	rest, err := Parse(&decoded, data, false)
	if err != nil {
		panic(err)
	}
	if len(rest) != 0 {
		panic("unexpected trailing data")
	}

	vectorPreflightLengthSink = len(decoded.Values)
}

func TestVectorMinimumWireSizeCachedAllocations(t *testing.T) {
	info := _structInfoTableByType[reflect.TypeOf(VectorPreflightHolder{})]
	elementInfo := info.fields[0].structInfo

	allocations := testing.AllocsPerRun(1000, func() {
		vectorPreflightMinimumSink = elementInfo.minimumWireSize
	})
	if allocations != 0 {
		t.Fatalf("cached minimum wire size allocations = %v, want 0", allocations)
	}
	if vectorPreflightMinimumSink != 48 {
		t.Fatalf("minimum wire size = %d, want 48", vectorPreflightMinimumSink)
	}
}

func TestParseVectorCachedPreflightAllocations(t *testing.T) {
	const count = uint32(3)

	emptyData := make([]byte, 4)
	emptyAllocations := testing.AllocsPerRun(1000, func() {
		parseVectorPreflightFixed(emptyData)
	})

	data := make([]byte, 4+count*8)
	binary.LittleEndian.PutUint32(data, count)

	allocations := testing.AllocsPerRun(1000, func() {
		parseVectorPreflightFixed(data)
	})

	if allocations != emptyAllocations+1 {
		t.Fatalf("valid vector parse allocations = %v, empty vector = %v, want one result slice difference", allocations, emptyAllocations)
	}
	if vectorPreflightLengthSink != int(count) {
		t.Fatalf("decoded %d elements, want %d", vectorPreflightLengthSink, count)
	}
}

func BenchmarkVectorMinimumWireSizeRecursive(b *testing.B) {
	info := _structInfoTableByType[reflect.TypeOf(VectorPreflightHolder{})]
	elementInfo := info.fields[0].structInfo

	b.ReportAllocs()
	for b.Loop() {
		vectorPreflightMinimumSink = calculateMinimumWireSize(elementInfo, map[*structInfo]bool{})
	}

	if vectorPreflightMinimumSink != 48 {
		b.Fatalf("minimum wire size = %d, want 48", vectorPreflightMinimumSink)
	}
}

func BenchmarkVectorMinimumWireSizeCached(b *testing.B) {
	info := _structInfoTableByType[reflect.TypeOf(VectorPreflightHolder{})]
	elementInfo := info.fields[0].structInfo

	b.ReportAllocs()
	for b.Loop() {
		vectorPreflightMinimumSink = elementInfo.minimumWireSize
	}

	if vectorPreflightMinimumSink != 48 {
		b.Fatalf("minimum wire size = %d, want 48", vectorPreflightMinimumSink)
	}
}

func benchmarkParseVectorCachedPreflight(b *testing.B, count uint32) {
	data := make([]byte, 4+count*8)
	binary.LittleEndian.PutUint32(data, count)

	b.ReportAllocs()
	for b.Loop() {
		var decoded VectorPreflightFixedHolder
		rest, err := Parse(&decoded, data, false)
		if err != nil {
			b.Fatal(err)
		}
		if len(rest) != 0 {
			b.Fatalf("unexpected trailing data: %x", rest)
		}

		vectorPreflightLengthSink = len(decoded.Values)
	}
}

func BenchmarkParseVectorCachedPreflight(b *testing.B) {
	b.Run("empty", func(b *testing.B) {
		benchmarkParseVectorCachedPreflight(b, 0)
	})
	b.Run("three_elements", func(b *testing.B) {
		benchmarkParseVectorCachedPreflight(b, 3)
	})
}
