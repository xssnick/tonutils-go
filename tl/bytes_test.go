package tl

import (
	"bytes"
	"encoding/hex"
	"strconv"
	"testing"
)

type tlBytesBoundaryCase struct {
	name       string
	payloadLen int
	headerHex  string
	headerLen  int
	padding    int
}

func TestTLBytes(t *testing.T) {
	buf := []byte{0xFF, 0xAA}
	b := &bytes.Buffer{}
	ToBytesToBuffer(b, buf)

	if !bytes.Equal(append([]byte{2}, append(buf, 0)...), b.Bytes()) {
		t.Fatal("not equal small")
		return
	}

	buf = []byte{0xFF, 0xAA, 0xCC}
	b.Reset()
	ToBytesToBuffer(b, buf)
	if !bytes.Equal(append([]byte{3}, buf...), b.Bytes()) {
		t.Fatal("not equal small 2")
		return
	}

	buf = buf[:0]
	for i := 0; i < 254; i++ {
		buf = append(buf, 0xFF)
	}

	b.Reset()
	ToBytesToBuffer(b, buf)

	// corner case + round to 4
	if !bytes.Equal(append([]byte{0xFE, 0xFE, 0x00, 0x00}, append(buf, 0x00, 0x00)...), b.Bytes()) {
		t.Fatal("not equal middle")
		return
	}

	buf = buf[:0]
	for i := 0; i < 1217; i++ {
		buf = append(buf, byte(i%256))
	}
	b.Reset()
	ToBytesToBuffer(b, buf)

	if !bytes.Equal(append([]byte{0xFE, 0xC1, 0x04, 0x00}, append(buf, 0x00, 0x00, 0x00)...), b.Bytes()) {
		t.Fatal("not equal big")
		return
	}
}

func TestAppendBytesMatchesBuffer(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		{0xFF, 0xAA},
		{0xFF, 0xAA, 0xCC},
		bytes.Repeat([]byte{0xFF}, 254),
		bytes.Repeat([]byte{0xAB}, 1217),
	} {
		var buf bytes.Buffer
		if err := ToBytesToBuffer(&buf, data); err != nil {
			t.Fatalf("ToBytesToBuffer len=%d: %v", len(data), err)
		}

		appended, err := AppendBytes(nil, data)
		if err != nil {
			t.Fatalf("AppendBytes len=%d: %v", len(data), err)
		}
		if !bytes.Equal(appended, buf.Bytes()) {
			t.Fatalf("AppendBytes len=%d mismatch", len(data))
		}
	}
}

func TestTLBytesExtendedBoundaryGolden(t *testing.T) {
	tests := []tlBytesBoundaryCase{
		{
			name:       "largest_24_bit",
			payloadLen: (1 << 24) - 1,
			headerHex:  "feffffff",
			headerLen:  4,
			padding:    1,
		},
		{
			name:       "first_extended",
			payloadLen: 1 << 24,
			headerHex:  "ff00000001000000",
			headerLen:  8,
			padding:    0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := bytes.Repeat([]byte{0xA5}, test.payloadLen)
			encoded, err := AppendBytes(nil, payload)
			if err != nil {
				t.Fatal(err)
			}

			header, err := hex.DecodeString(test.headerHex)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded[:test.headerLen], header) {
				t.Fatalf("header = %x, want %x", encoded[:test.headerLen], header)
			}
			if got := len(encoded); got != test.headerLen+test.payloadLen+test.padding {
				t.Fatalf("encoded length = %d, want %d", got, test.headerLen+test.payloadLen+test.padding)
			}
			if !bytes.Equal(encoded[test.headerLen:test.headerLen+test.payloadLen], payload) {
				t.Fatal("encoded payload mismatch")
			}
			for _, value := range encoded[test.headerLen+test.payloadLen:] {
				if value != 0 {
					t.Fatalf("non-zero padding byte: %x", value)
				}
			}

			loaded, rest, err := FromBytesNoCopy(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if len(rest) != 0 {
				t.Fatalf("unexpected trailing bytes: %x", rest)
			}
			if !bytes.Equal(loaded, payload) {
				t.Fatal("decoded payload mismatch")
			}
		})
	}
}

func TestTLBytesExtendedRemap(t *testing.T) {
	const payloadLen = 1 << 24

	payload := bytes.Repeat([]byte{0x5A}, payloadLen)
	slice := append(make([]byte, 4), payload...)
	slice = RemapSliceAsTLBytes(slice, 0)

	wantHeader := []byte{0xFF, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}
	if !bytes.Equal(slice[:8], wantHeader) {
		t.Fatalf("slice header = %x, want %x", slice[:8], wantHeader)
	}
	if !bytes.Equal(slice[8:], payload) {
		t.Fatal("remapped slice payload mismatch")
	}

	var buf bytes.Buffer
	buf.Grow(4 + payloadLen)
	buf.Write(make([]byte, 4))
	buf.Write(payload)
	RemapBufferAsSlice(&buf, 0)

	if !bytes.Equal(buf.Bytes()[:8], wantHeader) {
		t.Fatalf("buffer header = %x, want %x", buf.Bytes()[:8], wantHeader)
	}
	if !bytes.Equal(buf.Bytes()[8:], payload) {
		t.Fatal("remapped buffer payload mismatch")
	}
}

func TestTLBytesRejectsInvalidLengths(t *testing.T) {
	if _, err := tlBytesEncodedSize(-1); err == nil {
		t.Fatal("negative length was accepted")
	}

	if strconv.IntSize == 64 {
		maximum := (uint64(1) << 32) - 1
		size, err := tlBytesEncodedSize(int(maximum))
		if err != nil {
			t.Fatalf("maximum uint32 length: %v", err)
		}
		if want := int((uint64(1) << 32) + 8); size != want {
			t.Fatalf("maximum uint32 encoded size = %d, want %d", size, want)
		}

		tooLarge := uint64(1) << 32
		if _, err := tlBytesEncodedSize(int(tooLarge)); err == nil {
			t.Fatal("length 1<<32 was accepted")
		}
	}
}

func TestTLFromBytesRejectsMalformedExtendedHeader(t *testing.T) {
	header := []byte{0xFF, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}
	for size := 1; size < len(header); size++ {
		if _, _, err := FromBytesNoCopy(header[:size]); err == nil {
			t.Fatalf("truncated extended header of %d bytes was accepted", size)
		}
	}

	oversized := []byte{0xFF, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}
	if _, _, err := FromBytesNoCopy(oversized); err == nil {
		t.Fatal("extended length above uint32 was accepted")
	}

	if _, _, err := FromBytesNoCopy(header); err == nil {
		t.Fatal("truncated extended payload was accepted")
	}
}

func TestTLFromBytesTerminalPadding(t *testing.T) {
	loaded, rest, err := FromBytes([]byte{2, 0xFF, 0xAA, 0})
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(loaded, []byte{0xFF, 0xAA}) {
		t.Fatalf("loaded bytes mismatch: %x", loaded)
	}

	if rest != nil {
		t.Fatalf("rest should be nil, got %x", rest)
	}
}

// A TL bytes field always occupies a multiple of 4 bytes, so the alignment
// must be present even at the very end of the buffer -- td::TlParser::
// fetch_string consumes sizeof(int32)+result_aligned_len unconditionally.
func TestTLFromBytesRejectsMissingPadding(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "missing_empty_padding",
			data: []byte{0},
		},
		{
			name: "missing_terminal_padding",
			data: []byte{2, 0xFF, 0xAA},
		},
		{
			name: "truncated_terminal_padding",
			data: []byte{1, 0xFF, 0},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := FromBytesNoCopy(test.data); err == nil {
				t.Fatal("TL bytes without alignment padding was accepted")
			}
		})
	}
}

// The reference skips the alignment bytes without inspecting them, so a
// non-canonical writer must not make us drop an otherwise valid frame.
func TestTLFromBytesAcceptsNonZeroPadding(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		payload []byte
		rest    []byte
	}{
		{
			name:    "non_zero_first_padding_byte",
			data:    []byte{1, 0xFF, 1, 0},
			payload: []byte{0xFF},
		},
		{
			name:    "non_zero_last_padding_byte",
			data:    []byte{1, 0xFF, 0, 1},
			payload: []byte{0xFF},
		},
		{
			name:    "non_zero_padding_before_trailing_data",
			data:    []byte{2, 0xFF, 0xAA, 1, 0xCC},
			payload: []byte{0xFF, 0xAA},
			rest:    []byte{0xCC},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded, rest, err := FromBytesNoCopy(test.data)
			if err != nil {
				t.Fatalf("valid TL bytes rejected: %v", err)
			}
			if !bytes.Equal(loaded, test.payload) {
				t.Fatalf("payload = %x, want %x", loaded, test.payload)
			}
			if !bytes.Equal(rest, test.rest) {
				t.Fatalf("rest = %x, want %x", rest, test.rest)
			}
		})
	}
}

func TestTLFromBytesReturnsDataAfterCanonicalPadding(t *testing.T) {
	loaded, rest, err := FromBytesNoCopy([]byte{2, 0xFF, 0xAA, 0, 0xCC})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded, []byte{0xFF, 0xAA}) {
		t.Fatalf("loaded bytes = %x, want ffaa", loaded)
	}
	if !bytes.Equal(rest, []byte{0xCC}) {
		t.Fatalf("rest = %x, want cc", rest)
	}
}
