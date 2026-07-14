package tl

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type NoCopyFields struct {
	Key  []byte `tl:"int256"`
	Data []byte `tl:"bytes"`
}

type NoCopyCellFields struct {
	Cell *cell.Cell `tl:"cell"`
}

func init() {
	Register(NoCopyFields{}, "nocopy.fields key:int256 data:bytes = NoCopyFields")
	Register(NoCopyCellFields{}, "nocopy.cellFields cell:cell = NoCopyCellFields")
}

func TestAppendMatchesSerialize(t *testing.T) {
	tst, want := parse()

	got, err := Append(nil, &tst, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("append output does not match serialize output")
	}

	prefix := []byte{0xAA, 0xBB}
	got, err = Append(prefix, &tst, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[:len(prefix)], prefix) {
		t.Fatal("append should preserve prefix")
	}
	if !bytes.Equal(got[len(prefix):], want) {
		t.Fatal("append output after prefix does not match serialize output")
	}
}

func TestParseNoCopyAliasesBytes(t *testing.T) {
	src := NoCopyFields{
		Key:  bytes.Repeat([]byte{0x11}, 32),
		Data: []byte{0x22, 0x33, 0x44},
	}

	data, err := Serialize(&src, true)
	if err != nil {
		t.Fatal(err)
	}

	var copied NoCopyFields
	if _, err = Parse(&copied, data, true); err != nil {
		t.Fatal(err)
	}

	var noCopy NoCopyFields
	if _, err = ParseNoCopy(&noCopy, data, true); err != nil {
		t.Fatal(err)
	}

	if cap(noCopy.Key) != len(noCopy.Key) {
		t.Fatalf("no-copy fixed bytes cap should be limited, got cap %d len %d", cap(noCopy.Key), len(noCopy.Key))
	}
	if cap(noCopy.Data) != len(noCopy.Data) {
		t.Fatalf("no-copy bytes cap should be limited, got cap %d len %d", cap(noCopy.Data), len(noCopy.Data))
	}

	keyOff := 4
	dataOff := keyOff + 32 + 1
	data[keyOff] = 0x99
	data[dataOff] = 0x88

	if copied.Key[0] == 0x99 || copied.Data[0] == 0x88 {
		t.Fatal("regular Parse should copy byte fields")
	}
	if noCopy.Key[0] != 0x99 {
		t.Fatalf("ParseNoCopy fixed bytes should alias input, got %x", noCopy.Key[0])
	}
	if noCopy.Data[0] != 0x88 {
		t.Fatalf("ParseNoCopy bytes should alias input, got %x", noCopy.Data[0])
	}
}

func TestParseNoCopyAliasesCellPayload(t *testing.T) {
	src := NoCopyCellFields{
		Cell: cell.BeginCell().MustStoreUInt(0x1234, 16).EndCell(),
	}

	data, err := Serialize(&src, true)
	if err != nil {
		t.Fatal(err)
	}

	var parsed NoCopyCellFields
	if _, err = ParseNoCopy(&parsed, data, true); err != nil {
		t.Fatal(err)
	}

	value, err := parsed.Cell.MustBeginParse().LoadUInt(16)
	if err != nil {
		t.Fatal(err)
	}
	if value != 0x1234 {
		t.Fatalf("unexpected parsed cell value: %x", value)
	}

	payloadOff := bytes.Index(data, []byte{0x12, 0x34})
	if payloadOff < 0 {
		t.Fatal("serialized cell payload not found")
	}
	data[payloadOff] = 0xAB
	data[payloadOff+1] = 0xCD

	value, err = parsed.Cell.MustBeginParse().LoadUInt(16)
	if err != nil {
		t.Fatal(err)
	}
	if value != 0xABCD {
		t.Fatalf("ParseNoCopy cell payload should alias input, got %x", value)
	}
}
