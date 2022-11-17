package discmath

import (
	"encoding/hex"
	"testing"
)

func TestGF256MulAdd(t *testing.T) {
	row, _ := hex.DecodeString("00006ef24e3a3f6717e9e91ae61c")
	add, _ := hex.DecodeString("00000165499dbdf1b63ce3f3c0bc")
	mul := uint8(110)

	g := GF256{data: row}
	v := GF256{data: add}

	g.AddMul(&v, mul)

	println(g.String())
}
