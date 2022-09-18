package ton

import (
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/big"
	"reflect"
	"testing"
)

func TestExecutionResult(t *testing.T) {
	data := []any{
		big.NewInt(1),
		cell.BeginCell(),
		cell.BeginCell().EndCell(),
		cell.BeginCell().EndCell().BeginParse(),
		[]any{big.NewInt(2), big.NewInt(3)},
		tlb.StackNaN{},
		nil,
	}

	res := NewExecutionResult(data)

	if _, err := res.Slice(0); err != ErrIncorrectResultType {
		t.Fatal("is slice wrong err")
	}

	if _, err := res.Slice(14); err != ErrResultIndexOutOfRange {
		t.Fatal("is slice wrong err")
	}

	if _, err := res.Int(1); err != ErrIncorrectResultType {
		t.Fatal("is int wrong err")
	}

	if _, err := res.Int(15); err != ErrResultIndexOutOfRange {
		t.Fatal("is int wrong err")
	}

	if _, err := res.Tuple(1); err != ErrIncorrectResultType {
		t.Fatal("is tuple wrong err")
	}

	if _, err := res.Tuple(15); err != ErrResultIndexOutOfRange {
		t.Fatal("is tuple wrong err")
	}

	if _, err := res.Cell(1); err != ErrIncorrectResultType {
		t.Fatal("is cell wrong err")
	}

	if _, err := res.Cell(15); err != ErrResultIndexOutOfRange {
		t.Fatal("is cell wrong err")
	}

	if _, err := res.Builder(2); err != ErrIncorrectResultType {
		t.Fatal("is builder wrong err")
	}

	if _, err := res.Builder(15); err != ErrResultIndexOutOfRange {
		t.Fatal("is builder wrong err")
	}

	if yes, err := res.IsNil(1); yes || err != nil {
		t.Fatal("is nil wrong")
	}

	if _, err := res.IsNil(15); err != ErrResultIndexOutOfRange {
		t.Fatal("is nil err")
	}

	if yes, err := res.IsNil(6); !yes || err != nil {
		t.Fatal("is nil wrong")
	}

	if yes := res.MustIsNil(6); !yes {
		t.Fatal("must is nil wrong")
	}

	if v := res.MustInt(0); v.Uint64() != 1 {
		t.Fatal("must int wrong")
	}

	if v := res.MustSlice(3); v != data[3] {
		t.Fatal("must slice wrong")
	}

	if v := res.MustCell(2); v != data[2] {
		t.Fatal("must cell wrong")
	}

	if v := res.MustBuilder(1); v != data[1] {
		t.Fatal("must builder wrong")
	}

	if v := res.MustTuple(4); v[0].(*big.Int).Uint64() != 2 {
		t.Fatal("must tuple wrong")
	}

	if v := res.AsTuple(); !reflect.DeepEqual(v, data) {
		t.Fatal("as tuple wrong")
	}
}
