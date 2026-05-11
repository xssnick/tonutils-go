package vm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func Test_StackRotate(t *testing.T) {
	t.Run("123 -> 312", func(t *testing.T) {

		stack := NewStack()

		stack.PushAny(big.NewInt(1))
		stack.PushAny(big.NewInt(2))
		stack.PushAny(big.NewInt(3))

		if err := stack.Rotate(0, 1); err != nil {
			t.Error(err)
		}

		b, err := stack.PopInt()
		if err != nil || b.Cmp(big.NewInt(2)) != 0 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		a, err := stack.PopInt()
		if err != nil || a.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		c, err := stack.PopInt()
		if err != nil || c.Cmp(big.NewInt(3)) != 0 {
			t.Errorf("Expected 3 at c, got %v", c)
		}

	})

	t.Run("123 -> 231", func(t *testing.T) {
		stack := NewStack()

		stack.PushAny(big.NewInt(1))
		stack.PushAny(big.NewInt(2))
		stack.PushAny(big.NewInt(3))

		if err := stack.Rotate(0, 2); err != nil {
			t.Error(err)
		}
		a, err := stack.PopInt()
		if err != nil || a.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		c, err := stack.PopInt()
		if err != nil || c.Cmp(big.NewInt(3)) != 0 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
		b, err := stack.PopInt()
		if err != nil || b.Cmp(big.NewInt(2)) != 0 {
			t.Errorf("Expected 2 at b, got %v", b)
		}

	})

	t.Run("1234 - 3412", func(t *testing.T) {
		stack := NewStack()

		stack.PushAny(big.NewInt(1))
		stack.PushAny(big.NewInt(2))
		stack.PushAny(big.NewInt(3))
		stack.PushAny(big.NewInt(4))

		if err := stack.Rotate(0, 2); err != nil {
			t.Error(err)
		}

		b, err := stack.PopInt()
		if err != nil || b.Cmp(big.NewInt(2)) != 0 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		a, err := stack.PopInt()
		if err != nil || a.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		d, err := stack.PopInt()
		if err != nil || d.Cmp(big.NewInt(4)) != 0 {
			t.Errorf("Expected 4 at d, got %v", d)
		}
		c, err := stack.PopInt()
		if err != nil || c.Cmp(big.NewInt(3)) != 0 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
	})
	t.Run("12 -> 21", func(t *testing.T) {
		stack := NewStack()
		stack.PushAny(big.NewInt(1))
		stack.PushAny(big.NewInt(2))

		if err := stack.Rotate(0, 1); err != nil {
			t.Error(err)
		}

		a, err := stack.PopInt()
		if err != nil || a.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		b, err := stack.PopInt()
		if err != nil || b.Cmp(big.NewInt(2)) != 0 {
			t.Errorf("Expected 2 at b, got %v", b)
		}

	})
	t.Run("12345 -> 34512", func(t *testing.T) {
		stack := NewStack()
		stack.PushAny(big.NewInt(1))
		stack.PushAny(big.NewInt(2))
		stack.PushAny(big.NewInt(3))
		stack.PushAny(big.NewInt(4))
		stack.PushAny(big.NewInt(5))

		if err := stack.Rotate(0, 3); err != nil {
			t.Error(err)
		}

		b, err := stack.PopInt()
		if err != nil {
			t.Error(err)
		}
		a, err := stack.PopInt()
		if err != nil {
			t.Error(err)
		}
		e, err := stack.PopInt()
		if err != nil {
			t.Error(err)
		}
		d, err := stack.PopInt()
		if err != nil {
			t.Error(err)
		}
		c, err := stack.PopInt()
		if err != nil {
			t.Error(err)
		}

		if e.Cmp(big.NewInt(5)) != 0 || d.Cmp(big.NewInt(4)) != 0 ||
			c.Cmp(big.NewInt(3)) != 0 || b.Cmp(big.NewInt(2)) != 0 ||
			a.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("Expected [1 2 3 4 5], got [%v %v %v %v %v]", a, b, c, d, e)
		}
	})
}

func Test_Reverse(t *testing.T) {
	t.Run("123 -> 321", func(t *testing.T) {
		stack := NewStack()

		stack.PushAny(big.NewInt(1))
		stack.PushAny(big.NewInt(2))
		stack.PushAny(big.NewInt(3))
		if err := stack.Reverse(3, 0); err != nil {
			t.Error(err)
		}

		a, err := stack.PopInt()
		if err != nil || a.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		b, err := stack.PopInt()
		if err != nil || b.Cmp(big.NewInt(2)) != 0 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		c, err := stack.PopInt()
		if err != nil || c.Cmp(big.NewInt(3)) != 0 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
	})

	t.Run("1234 -> 4321", func(t *testing.T) {
		stack := NewStack()

		stack.PushAny(big.NewInt(1))
		stack.PushAny(big.NewInt(2))
		stack.PushAny(big.NewInt(3))
		stack.PushAny(big.NewInt(4))

		if err := stack.Reverse(4, 0); err != nil {
			t.Error(err)
		}

		a, err := stack.PopInt()
		if err != nil || a.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		b, err := stack.PopInt()
		if err != nil || b.Cmp(big.NewInt(2)) != 0 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		c, err := stack.PopInt()
		if err != nil || c.Cmp(big.NewInt(3)) != 0 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
		d, err := stack.PopInt()
		if err != nil || d.Cmp(big.NewInt(4)) != 0 {
			t.Errorf("Expected 4 at d, got %v", d)
		}
	})

	t.Run("1234 -> 4231", func(t *testing.T) {
		stack := NewStack()

		stack.PushAny(big.NewInt(1))
		stack.PushAny(big.NewInt(2))
		stack.PushAny(big.NewInt(3))
		stack.PushAny(big.NewInt(4))

		if err := stack.Reverse(3, 1); err != nil {
			t.Error(err)
		}

		d, err := stack.PopInt()
		if err != nil || d.Cmp(big.NewInt(4)) != 0 {
			t.Errorf("Expected 4 at d, got %v", d)
		}
		b, err := stack.PopInt()
		if err != nil || b.Cmp(big.NewInt(2)) != 0 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		c, err := stack.PopInt()
		if err != nil || c.Cmp(big.NewInt(3)) != 0 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
		a, err := stack.PopInt()
		if err != nil || a.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
	})
}

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

func TestStack_PopIntRange(t *testing.T) {
	tests := []struct {
		name string
		val  *big.Int
		min  int64
		max  int64
		code int64
	}{
		{name: "inside", val: big.NewInt(7), min: 0, max: 10},
		{name: "below", val: big.NewInt(-1), min: 0, max: 10, code: vmerr.CodeRangeCheck},
		{name: "above", val: big.NewInt(11), min: 0, max: 10, code: vmerr.CodeRangeCheck},
		{name: "huge_positive", val: new(big.Int).Lsh(big.NewInt(1), 80), min: 0, max: 10, code: vmerr.CodeRangeCheck},
		{name: "huge_negative", val: new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 80)), min: 0, max: 10, code: vmerr.CodeRangeCheck},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stk := NewStack()
			if err := stk.PushAny(tt.val); err != nil {
				t.Fatalf("push failed: %v", err)
			}

			got, err := stk.PopIntRange(tt.min, tt.max)
			if tt.code == 0 {
				if err != nil {
					t.Fatalf("PopIntRange failed: %v", err)
				}
				if got.Cmp(tt.val) != 0 {
					t.Fatalf("PopIntRange value = %v, want %v", got, tt.val)
				}
				return
			}

			if err == nil {
				t.Fatal("expected range error")
			}
			code, ok := vmerr.ErrorCode(err)
			if !ok || code != tt.code {
				t.Fatalf("error code = %d, ok=%v, want %d", code, ok, tt.code)
			}
		})
	}
}
