package vm

import (
	"math/big"
	"testing"
)

func TestStack_PushInt(t *testing.T) {
	stk := NewStack()
	i := new(big.Int).Lsh(big.NewInt(1), uint(256))
	i.Add(i, big.NewInt(1))
	println(i.String())
	println(i.Neg(i).String())

	// println(cell.BeginCell().MustStoreBigInt(i, 257).EndCell().String())
	println(i.Neg(i).String())
	err := stk.PushInt(i.Neg(i))
	if err == nil {
		t.Fatal("should be err")
	}
}
