package tl

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type SerializeAllocationFixedKey struct {
	Key []byte `tl:"int256"`
}

type SerializeAllocationNestedFixed struct {
	Key SerializeAllocationFixedKey `tl:"bytes struct boxed"`
}

type SerializeAllocationManualFixedKey struct {
	Key []byte
}

var errSerializeAllocationAfterWrite = errors.New("serialize after write")

type SerializeAllocationWriteError struct{}

func (*SerializeAllocationWriteError) Serialize(buf *bytes.Buffer) error {
	buf.Write([]byte{0x11, 0x22, 0x33, 0x44})
	return errSerializeAllocationAfterWrite
}

func (*SerializeAllocationWriteError) Parse(data []byte) ([]byte, error) {
	return data, nil
}

func (k *SerializeAllocationManualFixedKey) Serialize(buf *bytes.Buffer) error {
	if len(k.Key) != 32 {
		return errors.New("invalid key size")
	}

	buf.Write(k.Key)
	return nil
}

func (k *SerializeAllocationManualFixedKey) Parse(data []byte) ([]byte, error) {
	if len(data) < 32 {
		return nil, errors.New("not enough key data")
	}

	k.Key = append(k.Key[:0], data[:32]...)
	return data[32:], nil
}

type SerializeAllocationVariableBytes struct {
	Data []byte `tl:"bytes"`
}

type SerializeAllocationBytesVector struct {
	Values [][]byte `tl:"vector bytes"`
}

type SerializeAllocationStringVector struct {
	Values []string `tl:"vector string"`
}

func init() {
	Register(SerializeAllocationFixedKey{}, "test.serializeAllocation.fixedKey key:int256 = test.SerializeAllocationFixedKey")
	Register(SerializeAllocationNestedFixed{}, "test.serializeAllocation.nestedFixed key:bytes = test.SerializeAllocationNestedFixed")
	Register(SerializeAllocationManualFixedKey{}, "test.serializeAllocation.manualFixedKey key:int256 = test.SerializeAllocationManualFixedKey")
	Register(SerializeAllocationWriteError{}, "test.serializeAllocation.writeError = test.SerializeAllocationWriteError")
	Register(SerializeAllocationVariableBytes{}, "test.serializeAllocation.variableBytes data:bytes = test.SerializeAllocationVariableBytes")
	Register(SerializeAllocationBytesVector{}, "")
	Register(SerializeAllocationStringVector{}, "")
}

var serializeAllocationBytesSink []byte

func TestSerializeUsesExactFixedCapacity(t *testing.T) {
	tests := []struct {
		name          string
		value         Serializable
		boxed         bool
		fixedWireSize uint64
		serializedLen int
	}{
		{
			name:          "unboxed fixed key",
			value:         &SerializeAllocationFixedKey{Key: bytes.Repeat([]byte{0x11}, 32)},
			fixedWireSize: 32,
			serializedLen: 32,
		},
		{
			name:          "boxed fixed key",
			value:         &SerializeAllocationFixedKey{Key: bytes.Repeat([]byte{0x11}, 32)},
			boxed:         true,
			fixedWireSize: 32,
			serializedLen: 36,
		},
		{
			name: "nested fixed key",
			value: &SerializeAllocationNestedFixed{
				Key: SerializeAllocationFixedKey{Key: bytes.Repeat([]byte{0x11}, 32)},
			},
			boxed:         true,
			fixedWireSize: 40,
			serializedLen: 44,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tp := reflect.TypeOf(test.value).Elem()
			info := _structInfoTableByType[tp]
			if !info.fixedSize {
				t.Fatal("registered struct should have a fixed wire size")
			}
			if info.fixedWireSize != test.fixedWireSize {
				t.Fatalf("fixed wire size = %d, want %d", info.fixedWireSize, test.fixedWireSize)
			}

			got, err := Serialize(test.value, test.boxed)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != test.serializedLen {
				t.Fatalf("serialized length = %d, want %d", len(got), test.serializedLen)
			}
			if cap(got) != len(got) {
				t.Fatalf("serialized capacity = %d, want exact length %d", cap(got), len(got))
			}

			var reference bytes.Buffer
			want, err := Serialize(test.value, test.boxed, &reference)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("serialized data differs from buffer path:\n got: %x\nwant: %x", got, want)
			}
		})
	}
}

func TestSerializeVariableSizeKeepsDefaultCapacity(t *testing.T) {
	value := &SerializeAllocationVariableBytes{Data: []byte{0x11}}
	info := _structInfoTableByType[reflect.TypeOf(*value)]
	if info.fixedSize {
		t.Fatal("bytes field should make the wire size variable")
	}
	if capacity := initialSerializeCapacity(value, true); capacity != DefaultSerializeBufferSize {
		t.Fatalf("initial capacity = %d, want default %d", capacity, DefaultSerializeBufferSize)
	}
}

func TestAppendShortVariableVectorPreflightIsOrderIndependent(t *testing.T) {
	largeBytes := bytes.Repeat([]byte{0x22}, 4096)
	largeString := strings.Repeat("x", 4096)

	tests := []struct {
		name  string
		value Serializable
	}{
		{
			name: "bytes small first",
			value: &SerializeAllocationBytesVector{
				Values: [][]byte{{0x11}, largeBytes},
			},
		},
		{
			name: "bytes large first",
			value: &SerializeAllocationBytesVector{
				Values: [][]byte{largeBytes, {0x11}},
			},
		},
		{
			name: "strings small first",
			value: &SerializeAllocationStringVector{
				Values: []string{"x", largeString},
			},
		},
		{
			name: "strings large first",
			value: &SerializeAllocationStringVector{
				Values: []string{largeString, "x"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Append(nil, test.value, false)
			if err != nil {
				t.Fatal(err)
			}
			if cap(got) != len(got) {
				t.Fatalf("serialized capacity = %d, want exact length %d", cap(got), len(got))
			}

			allocations := testing.AllocsPerRun(20, func() {
				var err error
				serializeAllocationBytesSink, err = Append(nil, test.value, false)
				if err != nil {
					panic(err)
				}
			})
			if allocations != 1 {
				t.Fatalf("serialization allocations = %v, want 1", allocations)
			}
		})
	}
}

func TestAddVectorEncodedSizeRejectsOverflow(t *testing.T) {
	if size, err := addVectorEncodedSize(4, 3, 8); err != nil || size != 28 {
		t.Fatalf("valid size = %d, %v, want 28, nil", size, err)
	}

	maxInt := int(^uint(0) >> 1)
	if _, err := addVectorEncodedSize(4, maxInt, 8); err == nil {
		t.Fatal("expected vector size overflow error")
	}
}

func TestHashBufferReuseAfterErrorAndOversizedValue(t *testing.T) {
	if _, err := Hash(&SerializeAllocationWriteError{}); !errors.Is(err, errSerializeAllocationAfterWrite) {
		t.Fatalf("hash error = %v, want %v", err, errSerializeAllocationAfterWrite)
	}

	key := &SerializeAllocationFixedKey{Key: bytes.Repeat([]byte{0x5A}, 32)}
	wire, err := Serialize(key, true)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(wire)
	assertHash := func() {
		got, err := Hash(key)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want[:]) {
			t.Fatalf("hash = %x, want %x", got, want)
		}
	}
	assertHash()

	large := &SerializeAllocationVariableBytes{Data: make([]byte, maxPooledHashBufferSize+1)}
	if _, err = Hash(large); err != nil {
		t.Fatal(err)
	}
	assertHash()
}

func BenchmarkHashFixedSize(b *testing.B) {
	key := SerializeAllocationFixedKey{Key: bytes.Repeat([]byte{0x11}, 32)}

	b.ReportAllocs()
	for b.Loop() {
		var err error
		serializeAllocationBytesSink, err = Hash(key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHashFixedSizePointer(b *testing.B) {
	key := SerializeAllocationFixedKey{Key: bytes.Repeat([]byte{0x11}, 32)}

	b.ReportAllocs()
	for b.Loop() {
		var err error
		serializeAllocationBytesSink, err = Hash(&key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHashManualFixedSize(b *testing.B) {
	key := SerializeAllocationManualFixedKey{Key: bytes.Repeat([]byte{0x11}, 32)}

	b.ReportAllocs()
	for b.Loop() {
		var err error
		serializeAllocationBytesSink, err = Hash(key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHashManualFixedSizePointer(b *testing.B) {
	key := SerializeAllocationManualFixedKey{Key: bytes.Repeat([]byte{0x11}, 32)}

	b.ReportAllocs()
	for b.Loop() {
		var err error
		serializeAllocationBytesSink, err = Hash(&key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSerializeFixedSize(b *testing.B) {
	key := SerializeAllocationFixedKey{Key: bytes.Repeat([]byte{0x11}, 32)}

	b.ReportAllocs()
	for b.Loop() {
		var err error
		serializeAllocationBytesSink, err = Serialize(&key, true)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAppendShortVariableVector(b *testing.B) {
	largeBytes := bytes.Repeat([]byte{0x22}, 64<<10)
	largeString := strings.Repeat("x", 64<<10)

	b.Run("bytes_small_first", func(b *testing.B) {
		value := SerializeAllocationBytesVector{
			Values: [][]byte{{0x11}, largeBytes, largeBytes, largeBytes, largeBytes, largeBytes, largeBytes},
		}

		b.ReportAllocs()
		for b.Loop() {
			var err error
			serializeAllocationBytesSink, err = Append(nil, &value, false)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("bytes_large_first", func(b *testing.B) {
		value := SerializeAllocationBytesVector{
			Values: [][]byte{largeBytes, largeBytes, largeBytes, largeBytes, largeBytes, largeBytes, {0x11}},
		}

		b.ReportAllocs()
		for b.Loop() {
			var err error
			serializeAllocationBytesSink, err = Append(nil, &value, false)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("strings_small_first", func(b *testing.B) {
		value := SerializeAllocationStringVector{
			Values: []string{"x", largeString, largeString, largeString, largeString, largeString, largeString},
		}

		b.ReportAllocs()
		for b.Loop() {
			var err error
			serializeAllocationBytesSink, err = Append(nil, &value, false)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("strings_large_first", func(b *testing.B) {
		value := SerializeAllocationStringVector{
			Values: []string{largeString, largeString, largeString, largeString, largeString, largeString, "x"},
		}

		b.ReportAllocs()
		for b.Loop() {
			var err error
			serializeAllocationBytesSink, err = Append(nil, &value, false)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
