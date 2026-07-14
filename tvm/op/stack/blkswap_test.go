package stack

import (
	"errors"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func Test_BLKSWAP(t *testing.T) {
	st := newStack(1, 2, 3)
	blkswap := BLKSWAP(1, 2)

	if err := blkswap.Interpret(newStackStateWithStack(st)); err != nil {
		t.Fatal("Failed ROT interpretation:", err.Error())
	}
	if got := popInts(t, st, 3); !equalInts(got, []int64{1, 3, 2}) {
		t.Errorf("Expected [1 3 2], got %v", got)
	}

	t.Run("matches cxx opcode range", func(t *testing.T) {
		for encodedX := 0; encodedX <= 15; encodedX++ {
			for encodedY := 0; encodedY <= 15; encodedY++ {
				x, y := encodedX+1, encodedY+1
				stackLen := x + y + 3

				initial := make([]int64, stackLen)
				for i := range initial {
					initial[i] = int64(i + 1)
				}

				want := blockSwapLikeCXX(initial, x, y)
				st := newStack(initial...)
				op := BLKSWAP(uint8(x), uint8(y))
				decoded := BLKSWAP(1, 1)
				if err := decoded.Deserialize(op.Serialize().EndCell().MustBeginParse()); err != nil {
					t.Fatalf("decode BLKSWAP(%d,%d): %v", x, y, err)
				}
				if err := decoded.Interpret(newStackStateWithStack(st)); err != nil {
					t.Fatalf("execute BLKSWAP(%d,%d): %v", x, y, err)
				}
				if got := popInts(t, st, stackLen); !equalInts(got, want) {
					t.Fatalf("BLKSWAP(%d,%d): expected %v, got %v", x, y, want, got)
				}
			}
		}
	})

	t.Run("underflow leaves stack unchanged", func(t *testing.T) {
		st := newStack(1, 2, 3)
		err := BLKSWAP(2, 2).Interpret(newStackStateWithStack(st))
		var tvmErr vmerr.VMError
		if !errors.As(err, &tvmErr) || tvmErr.Code != vmerr.CodeStackUnderflow {
			t.Fatalf("expected stack underflow, got %v", err)
		}
		if got := popInts(t, st, 3); !equalInts(got, []int64{3, 2, 1}) {
			t.Fatalf("unexpected stack after underflow: %v", got)
		}
	})
}

func newStackStateWithStack(st *vm.Stack) *vm.State {
	return &vm.State{
		Stack: st,
		Gas:   vm.NewGas(),
	}
}

func blockSwapLikeCXX(initial []int64, x, y int) []int64 {
	vals := append([]int64(nil), initial...)
	first := len(vals) - x - y
	mid := len(vals) - y

	rotated := append([]int64(nil), vals[:first]...)
	rotated = append(rotated, vals[mid:]...)
	rotated = append(rotated, vals[first:mid]...)

	res := make([]int64, len(rotated))
	for i := range rotated {
		res[i] = rotated[len(rotated)-1-i]
	}

	return res
}
