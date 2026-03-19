package tl

import (
	"bytes"
	"testing"
)

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

	b.Reset()
	if err := ToBytesToBuffer(b, make([]byte, 1<<24)); err == nil {
		t.Fatal("should be error")
	}
}
