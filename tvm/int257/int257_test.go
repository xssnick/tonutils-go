package int257

import (
	"math/big"
	"testing"
)

// TestNewInt257 tests creation of Int257 from int64.
func TestNewInt257(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{12345, "12345"},
		{-98765, "-98765"},
	}
	for _, tt := range tests {
		x := NewInt257FromInt64(tt.input)
		if x.String() != tt.expected {
			t.Errorf("NewInt257FromInt64(%d) = %s; expected %s", tt.input, x.String(), tt.expected)
		}
	}
}

// TestArithmetic tests basic arithmetic operations and overflow corner cases.
func TestArithmetic(t *testing.T) {
	a := NewInt257FromInt64(100)
	b := NewInt257FromInt64(50)

	sum, err := a.Add(b)
	if err != nil {
		t.Errorf("Addition error: %v", err)
	} else if sum.String() != "150" {
		t.Errorf("100 + 50 = %s; expected 150", sum.String())
	}

	diff, err := a.Sub(b)
	if err != nil {
		t.Errorf("Subtraction error: %v", err)
	} else if diff.String() != "50" {
		t.Errorf("100 - 50 = %s; expected 50", diff.String())
	}

	prod, err := a.Mul(b)
	if err != nil {
		t.Errorf("Multiplication error: %v", err)
	} else if prod.String() != "5000" {
		t.Errorf("100 * 50 = %s; expected 5000", prod.String())
	}

	div, err := a.Div(b)
	if err != nil {
		t.Errorf("Division error: %v", err)
	} else if div.String() != "2" {
		t.Errorf("100 / 50 = %s; expected 2", div.String())
	}

	mod, err := a.Mod(b)
	if err != nil {
		t.Errorf("Modulo error: %v", err)
	} else if mod.String() != "0" {
		t.Errorf("100 mod 50 = %s; expected 0", mod.String())
	}

	neg, err := a.Neg()
	if err != nil {
		t.Errorf("Negation error: %v", err)
	}
	if neg.String() != "-100" {
		t.Errorf("Negation of 100 = %s; expected -100", neg.String())
	}

	absVal := neg.Abs()
	if absVal.String() != "100" {
		t.Errorf("Abs(-100) = %s; expected 100", absVal.String())
	}

	// Corner case: adding 1 to maxInt257 should overflow.
	maxVal := NewInt257FromBigInt(maxInt257)
	one := NewInt257FromInt64(1)
	_, err = maxVal.Add(one)
	if err == nil {
		t.Error("Expected overflow error when adding 1 to maxInt257")
	}

	// Corner case: subtracting 1 from minInt257 should overflow.
	minVal := NewInt257FromBigInt(minInt257)
	_, err = minVal.Sub(one)
	if err == nil {
		t.Error("Expected overflow error when subtracting 1 from minInt257")
	}
}

// TestBitwise tests basic bitwise operations.
func TestBitwise(t *testing.T) {
	// Test cases (raw two's complement):
	// -1 | -1 = -1, -1 | 1 = -1, -1 & 1 = 1, -1 << 5 = -32, -1 >> 5 = -1.
	minusOne := NewInt257FromInt64(-1)
	one := NewInt257FromInt64(1)

	res := minusOne.Or(minusOne)
	if res.Cmp(minusOne) != 0 {
		t.Errorf("-1 | -1 = %s; expected -1", res.String())
	}

	res = minusOne.Or(one)
	if res.Cmp(minusOne) != 0 {
		t.Errorf("-1 | 1 = %s; expected -1", res.String())
	}

	res = minusOne.And(one)
	if res.Cmp(one) != 0 {
		t.Errorf("-1 & 1 = %s; expected 1", res.String())
	}

	res = minusOne.Lsh(5)
	expected := NewInt257FromInt64(-32)
	if res.Cmp(expected) != 0 {
		t.Errorf("-1 << 5 = %s; expected -32", res.String())
	}

	res = minusOne.Rsh(5)
	if res.Cmp(minusOne) != 0 {
		t.Errorf("-1 >> 5 = %s; expected -1", res.String())
	}

	// Additional tests for positive numbers.
	a := NewInt257FromInt64(5) // binary 101
	b := NewInt257FromInt64(3) // binary  11
	andRes := a.And(b)         // 101 & 011 = 001 = 1
	if andRes.String() != "1" {
		t.Errorf("5 & 3 = %s; expected 1", andRes.String())
	}

	orRes := a.Or(b) // 101 | 011 = 111 = 7
	if orRes.String() != "7" {
		t.Errorf("5 | 3 = %s; expected 7", orRes.String())
	}

	xorRes := a.Xor(b) // 101 ^ 011 = 110 = 6
	if xorRes.String() != "6" {
		t.Errorf("5 ^ 3 = %s; expected 6", xorRes.String())
	}

	notRes := a.Not() // ~5 = -6 in two's complement
	if notRes.String() != "-6" {
		t.Errorf("~5 = %s; expected -6", notRes.String())
	}
}

// TestShifts tests shift operations.
func TestShifts(t *testing.T) {
	// Lsh: logical left shift on raw representation.
	a := NewInt257FromInt64(5)
	res := a.Lsh(2)
	if res.String() != "20" {
		t.Errorf("5 << 2 = %s; expected 20", res.String())
	}

	// Rsh: arithmetic right shift (sign extension).
	b := NewInt257FromInt64(5)
	res = b.Rsh(1)
	if res.String() != "2" {
		t.Errorf("5 >> 1 = %s; expected 2", res.String())
	}

	// For negative numbers, Rsh should preserve the sign.
	negSeven := NewInt257FromInt64(-7)
	res = negSeven.Rsh(4)
	expected := NewInt257FromInt64(-1) // arithmetic shift: -7 >> 4 = -1
	if res.Cmp(expected) != 0 {
		t.Errorf("Rsh(-7, 4) = %s; expected -1", res.String())
	}
}

// TestCornerCases tests additional edge cases.
func TestCornerCases(t *testing.T) {
	// Division and modulo by zero.
	a := NewInt257FromInt64(100)
	zero := NewInt257FromInt64(0)
	_, err := a.Div(zero)
	if err == nil {
		t.Error("Expected division by zero error")
	}
	_, err = a.Mod(zero)
	if err == nil {
		t.Error("Expected modulo by zero error")
	}

	// Check wrap-around: maxInt257 + 1 should overflow.
	maxVal := NewInt257FromBigInt(maxInt257)
	one := NewInt257FromInt64(1)
	_, err = maxVal.Add(one)
	if err == nil {
		t.Error("Expected addition overflow when adding 1 to maxInt257")
	}

	// Check wrap-around: minInt257 - 1 should overflow.
	minVal := NewInt257FromBigInt(minInt257)
	_, err = minVal.Sub(one)
	if err == nil {
		t.Error("Expected subtraction overflow when subtracting 1 from minInt257")
	}

	// Comparison tests.
	if NewInt257FromInt64(100).Cmp(NewInt257FromInt64(50)) != 1 {
		t.Error("Expected 100 > 50")
	}
	if NewInt257FromInt64(-100).Cmp(NewInt257FromInt64(50)) != -1 {
		t.Error("Expected -100 < 50")
	}
	if NewInt257FromInt64(12345).Cmp(NewInt257FromInt64(12345)) != 0 {
		t.Error("Expected 12345 == 12345")
	}
}

// FuzzArithmetic fuzz tests arithmetic operations.
func FuzzArithmetic(f *testing.F) {
	f.Add(int64(0), int64(0))
	f.Add(int64(123), int64(-456))
	f.Fuzz(func(t *testing.T, a int64, b int64) {
		x := NewInt257FromInt64(a)
		y := NewInt257FromInt64(b)
		sum, err := x.Add(y)
		if err != nil {
			return // skip cases that overflow
		}
		expected := new(big.Int).Add(big.NewInt(a), big.NewInt(b))
		expected = normalize(expected)
		if sum.ToBigInt().Cmp(expected) != 0 {
			t.Errorf("Add(%d, %d) = %s; expected %s", a, b, sum.String(), expected.String())
		}
	})
}

// FuzzBitwise fuzz tests bitwise operations.
func FuzzBitwise(f *testing.F) {
	f.Add(int64(5), int64(3))
	f.Add(int64(-7), int64(2))
	f.Fuzz(func(t *testing.T, a int64, b int64) {
		x := NewInt257FromInt64(a)
		y := NewInt257FromInt64(b)

		// AND
		andRes := x.And(y)
		ax := new(big.Int)
		if big.NewInt(a).Sign() < 0 {
			ax.Add(big.NewInt(a), mod257)
		} else {
			ax.SetInt64(a)
		}
		ay := new(big.Int)
		if big.NewInt(b).Sign() < 0 {
			ay.Add(big.NewInt(b), mod257)
		} else {
			ay.SetInt64(b)
		}
		expected := new(big.Int).And(ax, ay)
		expected.And(expected, mask257)
		if expected.Cmp(half257) >= 0 {
			expected.Sub(expected, mod257)
		}
		if andRes.ToBigInt().Cmp(expected) != 0 {
			t.Errorf("And(%d, %d) = %s; expected %s", a, b, andRes.String(), expected.String())
		}

		// OR
		orRes := x.Or(y)
		expected = new(big.Int).Or(ax, ay)
		expected.And(expected, mask257)
		if expected.Cmp(half257) >= 0 {
			expected.Sub(expected, mod257)
		}
		if orRes.ToBigInt().Cmp(expected) != 0 {
			t.Errorf("Or(%d, %d) = %s; expected %s", a, b, orRes.String(), expected.String())
		}

		// XOR
		xorRes := x.Xor(y)
		expected = new(big.Int).Xor(ax, ay)
		expected.And(expected, mask257)
		if expected.Cmp(half257) >= 0 {
			expected.Sub(expected, mod257)
		}
		if xorRes.ToBigInt().Cmp(expected) != 0 {
			t.Errorf("Xor(%d, %d) = %s; expected %s", a, b, xorRes.String(), expected.String())
		}
	})
}

// FuzzBitwiseNot fuzz tests bitwise operations.
func FuzzBitwiseNot(f *testing.F) {
	f.Add(int64(5))
	f.Add(int64(-7))
	f.Fuzz(func(t *testing.T, a int64) {
		// Fuzz test for NOT.
		x := NewInt257FromInt64(a)
		res := x.Not()
		ax := new(big.Int)
		if big.NewInt(a).Sign() < 0 {
			ax.Add(big.NewInt(a), mod257)
		} else {
			ax.SetInt64(a)
		}
		expected := new(big.Int).Xor(ax, mask257)
		if expected.Cmp(half257) >= 0 {
			expected.Sub(expected, mod257)
		}
		if res.ToBigInt().Cmp(expected) != 0 {
			t.Errorf("Not(%d) = %s; expected %s", a, res.String(), expected.String())
		}
	})
}

// FuzzShifts fuzz tests shift operations.
func FuzzShifts(f *testing.F) {
	f.Add(int64(5), uint(3))
	f.Add(int64(-7), uint(4))
	f.Fuzz(func(t *testing.T, a int64, shift uint) {
		// Limit the shift amount to 300 bits.
		s := shift % 300
		x := NewInt257FromInt64(a)

		// Lsh: expected value is computed on the raw representation.
		res := x.Lsh(s)
		expected := new(big.Int).Lsh(x.raw(), s)
		expected.Mod(expected, mod257)
		if expected.Cmp(half257) >= 0 {
			expected.Sub(expected, mod257)
		}
		if res.ToBigInt().Cmp(expected) != 0 {
			t.Errorf("Lsh(%d, %d) = %s; expected %s", a, s, res.String(), expected.String())
		}

		// Rsh: expected value is computed as arithmetic right shift on x.value.
		res = x.Rsh(s)
		expected = new(big.Int).Rsh(x.value, s)
		if res.ToBigInt().Cmp(expected) != 0 {
			t.Errorf("Rsh(%d, %d) = %s; expected %s", a, s, res.String(), expected.String())
		}
	})
}
