package vm

import (
	"math/big"
	"testing"
)

func Test_StackRotate(t *testing.T) {
	t.Run("321 -> 132", func(t *testing.T) {
		stack := NewStack()

		stack.PushAny(1)
		stack.PushAny(2)
		stack.PushAny(3)

		xy, err := stack.FromTop(3)
		if err != nil {
			t.Error(err)
		}
		y, err := stack.FromTop(2)
		if err != nil {
			t.Error(err)
		}
		if err := stack.Rotate(xy, y, stack.Len()); err != nil {
			t.Error(err)
		}

		a, err := stack.PopAny()
		if err != nil || a != 1 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		c, err := stack.PopAny()
		if err != nil || c != 3 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
		b, err := stack.PopAny()
		if err != nil || b != 2 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
	})

	t.Run("321 -> 213", func(t *testing.T) {
		stack := NewStack()

		stack.PushAny(1)
		stack.PushAny(2)
		stack.PushAny(3)

		xy, err := stack.FromTop(3)
		if err != nil {
			t.Error(err)
		}
		y, err := stack.FromTop(1)
		if err != nil {
			t.Error(err)
		}
		if err := stack.Rotate(xy, y, stack.Len()); err != nil {
			t.Error(err)
		}

		b, err := stack.PopAny()
		if err != nil || b != 2 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		a, err := stack.PopAny()
		if err != nil || a != 1 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		c, err := stack.PopAny()
		if err != nil || c != 3 {
			t.Errorf("Expected 3 at c, got %v", c)
		}

	})

	t.Run("4321 - 2143", func(t *testing.T) {
		stack := NewStack()

		stack.PushAny(1)
		stack.PushAny(2)
		stack.PushAny(3)
		stack.PushAny(4)

		xy, err := stack.FromTop(4)
		if err != nil {
			t.Error(err)
		}
		y, err := stack.FromTop(2)
		if err != nil {
			t.Error(err)
		}
		if err := stack.Rotate(xy, y, stack.Len()); err != nil {
			t.Error(err)
		}

		b, err := stack.PopAny()
		if err != nil || b != 2 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		a, err := stack.PopAny()
		if err != nil || a != 1 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		d, err := stack.PopAny()
		if err != nil || d != 4 {
			t.Errorf("Expected 4 at d, got %v", d)
		}
		c, err := stack.PopAny()
		if err != nil || c != 3 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
	})
}

func Test_Reverse(t *testing.T) {
	t.Run("321 -> 123", func(t *testing.T) {
		stack := NewStack()

		stack.PushAny(1)
		stack.PushAny(2)
		stack.PushAny(3)

		if err := stack.Reverse(2, 0); err != nil {
			t.Error(err)
		}

		a, err := stack.PopAny()
		if err != nil || a != 1 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		b, err := stack.PopAny()
		if err != nil || b != 2 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		c, err := stack.PopAny()
		if err != nil || c != 3 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
	})

	t.Run("4321 -> 1234", func(t *testing.T) {
		stack := NewStack()

		stack.PushAny(1)
		stack.PushAny(2)
		stack.PushAny(3)
		stack.PushAny(4)

		if err := stack.Reverse(3, 0); err != nil {
			t.Error(err)
		}

		a, err := stack.PopAny()
		if err != nil || a != 1 {
			t.Errorf("Expected 1 at a, got %v", a)
		}
		b, err := stack.PopAny()
		if err != nil || b != 2 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		c, err := stack.PopAny()
		if err != nil || c != 3 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
		d, err := stack.PopAny()
		if err != nil || d != 4 {
			t.Errorf("Expected 4 at d, got %v", d)
		}
	})

	t.Run("4321 -> 1324", func(t *testing.T) {
		stack := NewStack()

		stack.PushAny(1)
		stack.PushAny(2)
		stack.PushAny(3)
		stack.PushAny(4)

		if err := stack.Reverse(2, 1); err != nil {
			t.Error(err)
		}

		d, err := stack.PopAny()
		if err != nil || d != 4 {
			t.Errorf("Expected 4 at d, got %v", d)
		}

		b, err := stack.PopAny()
		if err != nil || b != 2 {
			t.Errorf("Expected 2 at b, got %v", b)
		}
		c, err := stack.PopAny()
		if err != nil || c != 3 {
			t.Errorf("Expected 3 at c, got %v", c)
		}
		a, err := stack.PopAny()
		if err != nil || a != 1 {
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
