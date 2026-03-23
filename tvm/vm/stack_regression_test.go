package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func pushInts(t *testing.T, s *Stack, vals ...int64) {
	t.Helper()

	for _, v := range vals {
		if err := s.PushInt(big.NewInt(v)); err != nil {
			t.Fatalf("push %d: %v", v, err)
		}
	}
}

func assertPopInts(t *testing.T, s *Stack, vals ...int64) {
	t.Helper()

	for _, want := range vals {
		got, err := s.PopInt()
		if err != nil {
			t.Fatalf("pop %d: %v", want, err)
		}
		if got.Int64() != want {
			t.Fatalf("want %d, got %d", want, got.Int64())
		}
	}
}

func TestStackMoveFromMovesTopElements(t *testing.T) {
	from := NewStack()
	pushInts(t, from, 1, 2, 3, 4)

	to := NewStack()
	if err := to.MoveFrom(from, 2); err != nil {
		t.Fatal(err)
	}

	assertPopInts(t, to, 4, 3)
	assertPopInts(t, from, 2, 1)
}

func TestStackSplitTopAllowsWholeStack(t *testing.T) {
	s := NewStack()
	pushInts(t, s, 1, 2, 3)

	top, err := s.SplitTop(3, 0)
	if err != nil {
		t.Fatal(err)
	}

	if s.Len() != 0 {
		t.Fatalf("source stack should be empty, got len=%d", s.Len())
	}

	assertPopInts(t, top, 3, 2, 1)
}

func TestStackSplitTopTakesTopAndDropsNext(t *testing.T) {
	s := NewStack()
	pushInts(t, s, 1, 2, 3, 4, 5)

	top, err := s.SplitTop(2, 1)
	if err != nil {
		t.Fatal(err)
	}

	assertPopInts(t, top, 5, 4)
	assertPopInts(t, s, 2, 1)
}

func TestStackSplitTopRejectsInvalidArgs(t *testing.T) {
	s := NewStack()
	pushInts(t, s, 1, 2, 3)

	if _, err := s.SplitTop(4, 0); err == nil {
		t.Fatal("expected error for top larger than stack")
	}

	if _, err := s.SplitTop(2, 2); err == nil {
		t.Fatal("expected error for invalid drop size")
	}
}

func TestStackPushIntSupportsTVMSigned257BitRange(t *testing.T) {
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	min := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256))
	posOverflow := new(big.Int).Add(new(big.Int).Set(max), big.NewInt(1))
	negOverflow := new(big.Int).Sub(new(big.Int).Set(min), big.NewInt(1))

	s := NewStack()
	if err := s.PushInt(max); err != nil {
		t.Fatalf("max signed 257-bit integer should fit: %v", err)
	}
	if err := s.PushInt(min); err != nil {
		t.Fatalf("min signed 257-bit integer should fit: %v", err)
	}

	gotMin, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}
	if gotMin.Cmp(min) != 0 {
		t.Fatalf("want min %s, got %s", min.String(), gotMin.String())
	}

	gotMax, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}
	if gotMax.Cmp(max) != 0 {
		t.Fatalf("want max %s, got %s", max.String(), gotMax.String())
	}

	if err := s.PushInt(posOverflow); err == nil {
		t.Fatal("expected positive overflow to fail")
	}
	if err := s.PushInt(negOverflow); err == nil {
		t.Fatal("expected negative overflow to fail")
	}
}

func TestStackPushIntQuietKeepsNaNDistinct(t *testing.T) {
	s := NewStack()
	overflow := new(big.Int).Lsh(big.NewInt(1), 257)

	if err := s.PushIntQuiet(overflow); err != nil {
		t.Fatalf("push quiet overflow: %v", err)
	}

	val, err := s.Get(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := val.(NaN); !ok {
		t.Fatalf("expected NaN on stack, got %T", val)
	}

	if _, err = s.PopIntFinite(); err == nil {
		t.Fatal("expected PopIntFinite to reject NaN")
	} else {
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeIntOverflow {
			t.Fatalf("expected int overflow, got %v", err)
		}
	}
}

func TestStateCallArgsPassesTopElements(t *testing.T) {
	s := NewStack()
	pushInts(t, s, 1, 2, 3, 4)

	state := &State{
		CurrentCode: cell.BeginCell().EndCell().BeginParse(),
		Stack:       s,
	}

	cont := &OrdinaryContinuation{
		Data: ControlData{
			NumArgs: 2,
			CP:      CP,
		},
		Code: cell.BeginCell().EndCell().BeginParse(),
	}

	if err := state.CallArgs(cont, 2, -1); err != nil {
		t.Fatal(err)
	}

	assertPopInts(t, state.Stack, 4, 3)

	ret, ok := state.Reg.C[0].(*OrdinaryContinuation)
	if !ok {
		t.Fatalf("expected return continuation, got %T", state.Reg.C[0])
	}

	assertPopInts(t, ret.Data.Stack, 2, 1)
}
