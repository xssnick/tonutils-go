package stack

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func Test_REVERSE(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		st := newStack(1, 2, 3, 4)
		blkpush := REVERSE(4, 0)

		err := blkpush.Interpret(&vm.State{
			Stack: st,
		})
		if err != nil {
			t.Fatal("Failed ROT interpretation:", err.Error())
		}

		if got := popInts(t, st, 4); !equalInts(got, []int64{1, 2, 3, 4}) {
			t.Errorf("Expected [1 2 3 4], got %v", got)
		}
	})
	t.Run("part", func(t *testing.T) {
		st := newStack(1, 2, 3, 4)
		blkpush := REVERSE(2, 1)

		err := blkpush.Interpret(&vm.State{
			Stack: st,
		})
		if err != nil {
			t.Fatal("Failed ROT interpretation:", err.Error())
		}

		if got := popInts(t, st, 4); !equalInts(got, []int64{4, 2, 3, 1}) {
			t.Errorf("Expected [4 2 3 1], got %v", got)
		}
	})
	t.Run("matches cxx opcode range", func(t *testing.T) {
		for encodedX := 0; encodedX <= 15; encodedX++ {
			for y := 0; y <= 15; y++ {
				x := encodedX + 2
				stackLen := x + y + 3

				initial := make([]int64, stackLen)
				for i := range initial {
					initial[i] = int64(i + 1)
				}

				want := reverseLikeCXX(initial, x, y)
				st := newStack(initial...)
				op := REVERSE(uint8(x), uint8(y))
				decoded := REVERSE(2, 0)
				if err := decoded.Deserialize(op.Serialize().EndCell().MustBeginParse()); err != nil {
					t.Fatalf("decode REVERSE(%d,%d): %v", x, y, err)
				}
				if err := decoded.Interpret(&vm.State{Stack: st}); err != nil {
					t.Fatalf("execute REVERSE(%d,%d): %v", x, y, err)
				}
				if got := popInts(t, st, stackLen); !equalInts(got, want) {
					t.Fatalf("REVERSE(%d,%d): expected %v, got %v", x, y, want, got)
				}
			}
		}
	})
}

func reverseLikeCXX(initial []int64, x, y int) []int64 {
	vals := append([]int64(nil), initial...)
	for l, r := len(vals)-x-y, len(vals)-y-1; l < r; l, r = l+1, r-1 {
		vals[l], vals[r] = vals[r], vals[l]
	}

	res := make([]int64, len(vals))
	for i := range vals {
		res[i] = vals[len(vals)-1-i]
	}

	return res
}
