package tl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"unsafe"
)

type SerializeAllocationBlockID struct {
	Workchain int32  `tl:"int"`
	Shard     int64  `tl:"long"`
	Seqno     uint32 `tl:"int"`
	RootHash  []byte `tl:"int256"`
	FileHash  []byte `tl:"int256"`
}

type SerializeAllocationDataFull struct {
	ID     SerializeAllocationBlockID `tl:"struct"`
	Proof  []byte                     `tl:"bytes"`
	Block  []byte                     `tl:"bytes"`
	IsLink bool                       `tl:"bool"`
}

type SerializeAllocationPayloadTail struct {
	Source    any    `tl:"struct boxed"`
	DataSize  uint32 `tl:"int"`
	Data      []byte `tl:"bytes"`
	Extra     string `tl:"string"`
	Signature []byte `tl:"bytes"`
}

type ParseAllocationBoxedBytes struct {
	Value any `tl:"bytes struct boxed [test.serializeAllocation.variableBytes,test.serializeAllocation.manualFixedKey]"`
}

func init() {
	Register(SerializeAllocationBlockID{}, "test.serializeAllocation.blockID workchain:int shard:long seqno:int root_hash:int256 file_hash:int256 = test.SerializeAllocationBlockID")
	Register(SerializeAllocationDataFull{}, "test.serializeAllocation.dataFull id:test.serializeAllocation.blockID proof:bytes block:bytes is_link:Bool = test.SerializeAllocationDataFull")
	Register(SerializeAllocationPayloadTail{}, "test.serializeAllocation.payloadTail source:Object data_size:int data:bytes extra:string signature:bytes = test.SerializeAllocationPayloadTail")
	Register(ParseAllocationBoxedBytes{}, "test.parseAllocation.boxedBytes value:bytes = test.ParseAllocationBoxedBytes")
}

func TestSerializeDataFullMatchesBuffer(t *testing.T) {
	for _, size := range []int{0, 1, 253, 254, 64 << 10, 1 << 20, (1 << 24) - 1, 1 << 24} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			value := SerializeAllocationDataFull{
				ID:     SerializeAllocationBlockID{Workchain: -1, Shard: -1 << 63, Seqno: 7},
				Proof:  bytes.Repeat([]byte{0xCA}, 257),
				Block:  bytes.Repeat([]byte{0xFE}, size),
				IsLink: true,
			}

			for _, boxed := range []bool{false, true} {
				var reference bytes.Buffer
				want, err := Serialize(&value, boxed, &reference)
				if err != nil {
					t.Fatal(err)
				}
				got, err := Serialize(&value, boxed)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("Serialize differs from buffer path (boxed=%t)", boxed)
				}

				prefix := []byte{1, 2, 3}
				got, err = Append(prefix, &value, boxed)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got[:len(prefix)], prefix) || !bytes.Equal(got[len(prefix):], want) {
					t.Fatalf("Append differs from buffer path (boxed=%t)", boxed)
				}

				var decoded SerializeAllocationDataFull
				rest, err := ParseNoCopy(&decoded, got[len(prefix):], boxed)
				if err != nil || len(rest) != 0 {
					t.Fatalf("parse: rest=%d, err=%v", len(rest), err)
				}
				if !bytes.Equal(decoded.Proof, value.Proof) || !bytes.Equal(decoded.Block, value.Block) || !decoded.IsLink {
					t.Fatal("decoded fields differ")
				}
			}
		})
	}
}

func TestSerializePayloadTailIgnoresDeclaredSize(t *testing.T) {
	value := SerializeAllocationPayloadTail{
		Source:    &SerializeAllocationManualFixedKey{Key: make([]byte, 32)},
		Data:      bytes.Repeat([]byte{0xA5}, 4096),
		Extra:     "metadata",
		Signature: make([]byte, 64),
	}
	var capacity int
	for _, declaredSize := range []uint32{0, 1, 4096, ^uint32(0)} {
		value.DataSize = declaredSize
		got, err := Serialize(&value, true)
		if err != nil {
			t.Fatal(err)
		}
		if capacity == 0 {
			capacity = cap(got)
		} else if cap(got) != capacity {
			t.Fatalf("declared size %d changed output capacity from %d to %d", declaredSize, capacity, cap(got))
		}

		var reference bytes.Buffer
		want, err := Serialize(&value, true, &reference)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("declared size %d: wire mismatch", declaredSize)
		}
	}
}

func TestSerializeDataFullAllocations(t *testing.T) {
	value := SerializeAllocationDataFull{Proof: make([]byte, 64<<10), Block: make([]byte, 1<<20)}
	allocations := testing.AllocsPerRun(10, func() {
		var err error
		serializeAllocationBytesSink, err = Serialize(&value, true)
		if err != nil {
			panic(err)
		}
	})
	if allocations != 1 {
		t.Fatalf("serialization allocations = %v, want one output buffer", allocations)
	}

	buffer := make([]byte, 3, 3+len(serializeAllocationBytesSink))
	buffer[0], buffer[1], buffer[2] = 1, 2, 3
	result, err := Append(buffer, &value, true)
	if err != nil {
		t.Fatal(err)
	}
	if &result[0] != &buffer[0] {
		t.Fatal("Append replaced a buffer with sufficient capacity")
	}
}

func TestGrowAppendStructRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if _, err := growAppendStruct(nil, nil, &structInfo{appendFixedSize: maxInt}, true); err == nil {
		t.Fatal("constructor size overflow accepted")
	}
	if _, err := growAppendStruct([]byte{1}, nil, &structInfo{appendFixedSize: maxInt}, false); err == nil {
		t.Fatal("destination size overflow accepted")
	}
	value := SerializeAllocationVariableBytes{Data: []byte{1}}
	info := structInfo{
		appendFixedSize:      maxInt,
		appendVariableFields: _structInfoTableByType[reflect.TypeOf(value)].fields,
	}
	if _, err := growAppendStruct(nil, unsafe.Pointer(&value), &info, false); err == nil {
		t.Fatal("bytes size overflow accepted")
	}
}

func TestParseMessageRejectsUnbackedLengths(t *testing.T) {
	for _, noCopy := range []bool{false, true} {
		for _, header := range [][]byte{
			{0xFE, 0xFF, 0xFF, 0xFF},
			{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0},
		} {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			var value SerializeAllocationVariableBytes
			_, err := parseWithCopyMode(&value, header, false, noCopy)
			runtime.ReadMemStats(&after)
			if err == nil {
				t.Fatal("unbacked bytes length accepted")
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
				t.Fatalf("unbacked bytes length allocated %d bytes", allocated)
			}
		}
	}
}

func TestParseBoxedBytesList(t *testing.T) {
	for _, count := range []int{1, 2, 8} {
		for _, noCopy := range []bool{false, true} {
			t.Run(fmt.Sprintf("count=%d/noCopy=%t", count, noCopy), func(t *testing.T) {
				values := make([]Serializable, count)
				for i := range values {
					values[i] = SerializeAllocationVariableBytes{Data: []byte{byte(i + 1)}}
				}
				wire, err := Serialize(&ParseAllocationBoxedBytes{Value: values}, true)
				if err != nil {
					t.Fatal(err)
				}
				var decoded ParseAllocationBoxedBytes
				rest, err := parseWithCopyMode(&decoded, wire, true, noCopy)
				if err != nil || len(rest) != 0 {
					t.Fatalf("parse: rest=%d, err=%v", len(rest), err)
				}
				got := []Serializable{decoded.Value}
				if count > 1 {
					got = decoded.Value.([]Serializable)
				}
				if !reflect.DeepEqual(got, values) {
					t.Fatalf("decoded %#v, want %#v", got, values)
				}

				for _, v := range got {
					payload := v.(SerializeAllocationVariableBytes).Data
					aliases := false
					for j := range wire {
						if &wire[j] == &payload[0] {
							aliases = true
							break
						}
					}
					if aliases != noCopy {
						t.Fatalf("payload aliases input=%t, want %t", aliases, noCopy)
					}
					if noCopy && cap(payload) != len(payload) {
						t.Fatal("borrowed payload capacity exceeds length")
					}
				}
			})
		}
	}
}

func TestParseBoxedBytesManualAndErrors(t *testing.T) {
	first, err := Serialize(&SerializeAllocationVariableBytes{Data: []byte{1}}, true)
	if err != nil {
		t.Fatal(err)
	}
	manual, err := Serialize(&SerializeAllocationManualFixedKey{Key: make([]byte, 32)}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, noCopy := range []bool{false, true} {
		for _, prefix := range [][]byte{nil, first} {
			for _, test := range []struct {
				name  string
				body  []byte
				valid bool
			}{
				{name: "manual", body: manual, valid: true},
				{name: "truncated manual", body: manual[:len(manual)-1]},
				{name: "unknown constructor", body: []byte{0, 0, 0, 0}},
				{name: "truncated constructor", body: []byte{0}},
				{name: "disallowed constructor", body: binary.LittleEndian.AppendUint32(nil, _structInfoTableByType[reflect.TypeOf(SerializeAllocationFixedKey{})].idNum)},
			} {
				body := append(append([]byte(nil), prefix...), test.body...)
				wire, err := AppendBytes(nil, body)
				if err != nil {
					t.Fatal(err)
				}
				var decoded ParseAllocationBoxedBytes
				_, err = parseWithCopyMode(&decoded, wire, false, noCopy)
				if (err == nil) != test.valid {
					t.Fatalf("%s, noCopy=%t, prefix=%d: %v", test.name, noCopy, len(prefix), err)
				}
			}
		}
		var decoded ParseAllocationBoxedBytes
		if _, err := parseWithCopyMode(&decoded, []byte{0, 0, 0, 0}, false, noCopy); err == nil {
			t.Fatal("empty boxed bytes accepted")
		}
	}
}

func BenchmarkSerializeDataFull(b *testing.B) {
	for _, blockSize := range []int{240, 1 << 20} {
		b.Run(fmt.Sprint(blockSize), func(b *testing.B) {
			value := SerializeAllocationDataFull{
				Proof: make([]byte, 64<<10),
				Block: make([]byte, blockSize),
			}

			b.ReportAllocs()
			for b.Loop() {
				var err error
				serializeAllocationBytesSink, err = Serialize(&value, true)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSerializePayloadTail(b *testing.B) {
	for _, payloadSize := range []int{240, 64 << 10, 1 << 20} {
		b.Run(fmt.Sprint(payloadSize), func(b *testing.B) {
			value := SerializeAllocationPayloadTail{
				Source:    &SerializeAllocationManualFixedKey{Key: make([]byte, 32)},
				Data:      make([]byte, payloadSize),
				Signature: make([]byte, 64),
			}

			b.ReportAllocs()
			for b.Loop() {
				var err error
				serializeAllocationBytesSink, err = Serialize(&value, true)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParseBoxedBytes(b *testing.B) {
	for _, count := range []int{1, 2, 8} {
		for _, noCopy := range []bool{false, true} {
			b.Run(fmt.Sprintf("count=%d/noCopy=%t", count, noCopy), func(b *testing.B) {
				values := make([]Serializable, count)
				for i := range values {
					values[i] = &SerializeAllocationVariableBytes{Data: bytes.Repeat([]byte{byte(i + 1)}, 16)}
				}
				wire, err := Serialize(&ParseAllocationBoxedBytes{Value: values}, true)
				if err != nil {
					b.Fatal(err)
				}

				b.ReportAllocs()
				for b.Loop() {
					var value ParseAllocationBoxedBytes
					if _, err := parseWithCopyMode(&value, wire, true, noCopy); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
