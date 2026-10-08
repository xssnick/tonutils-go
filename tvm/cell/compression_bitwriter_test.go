package cell

import (
	"bytes"
	"testing"
)

func TestBitWriterBulkParity(t *testing.T) {
	lengths := []int{0, 1, 2, 7, 8, 9, 31, 32, 63, 64, 65, 127, 128, 255, 256, 511, 768, 1023}
	for prefix := 0; prefix < 8; prefix++ {
		for sourceOffset := 0; sourceOffset < 8; sourceOffset++ {
			for _, bitLen := range lengths {
				data := make([]byte, (sourceOffset+bitLen+7)/8)
				for i := range data {
					data[i] = byte(i*73 + prefix*29 + sourceOffset*11 + bitLen)
				}
				span := bitSpan{data: data, bitOffset: sourceOffset, bitLen: bitLen}

				var got, want bitWriter
				for i := 0; i < prefix; i++ {
					bit := byte(i & 1)
					got.WriteBit(bit)
					want.WriteBit(bit)
				}
				got.AppendSpan(span)
				for i := 0; i < span.Len(); i++ {
					want.WriteBit(span.Bit(i))
				}

				if got.Len() != want.Len() || !bytes.Equal(got.Bytes(), want.Bytes()) {
					t.Fatalf("AppendSpan mismatch at prefix=%d source-offset=%d bits=%d: got=%08b want=%08b", prefix, sourceOffset, bitLen, got.Bytes(), want.Bytes())
				}
			}
		}
	}

	values := []uint64{0, 1, 0xA5, 0x1122334455667788, ^uint64(0)}
	for prefix := 0; prefix < 8; prefix++ {
		for width := 0; width <= 64; width++ {
			for _, value := range values {
				var got, want bitWriter
				for i := 0; i < prefix; i++ {
					got.WriteBit(1)
					want.WriteBit(1)
				}
				got.WriteUint(value, width)
				for i := width - 1; i >= 0; i-- {
					want.WriteBit(byte(value >> uint(i) & 1))
				}

				if got.Len() != want.Len() || !bytes.Equal(got.Bytes(), want.Bytes()) {
					t.Fatalf("WriteUint mismatch at prefix=%d width=%d value=%x: got=%08b want=%08b", prefix, width, value, got.Bytes(), want.Bytes())
				}
			}
		}
	}
}
