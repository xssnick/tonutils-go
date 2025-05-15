// string_snake_test.go
package tlb

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestStringSnake_LoadFromCell(t *testing.T) {
	initialValue := "test"
	s := StringSnake{}
	b := cell.BeginCell()
	err := b.StoreStringSnake(initialValue)
	if err != nil {
		t.Fatal(err)
	}
	c := b.EndCell()

	err = s.LoadFromCell(c.BeginParse())
	if err != nil {
		t.Fatal(err)
	}
	if s.Value != initialValue {
		t.Errorf("expected value %s, got '%s'", initialValue, s.Value)
	}
}

func TestStringSnake_ToCell(t *testing.T) {
	initialValue := "test"
	s := StringSnake{Value: initialValue}
	c, err := s.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	str, err := c.BeginParse().LoadStringSnake()
	if err != nil {
		t.Fatal(err)
	}
	if str != initialValue {
		t.Errorf("expected value %s, got '%s'", initialValue, str)
	}
}

func TestStringSnake_ToCellAndBack(t *testing.T) {
	initialValue := "test"
	s := StringSnake{Value: initialValue}
	c, err := s.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var s2 StringSnake
	err = s2.LoadFromCell(c.BeginParse())
	if err != nil {
		t.Fatal(err)
	}
	if s2.Value != initialValue {
		t.Errorf("expected value '%s', got '%s'", initialValue, s2.Value)
	}
}

func TestStringSnake_ComplexStructureSerialization(t *testing.T) {
	type testStruct struct {
		Int32Val  int32       `tlb:"## 32"`
		StringVal StringSnake `tlb:"^"`
		Int64Val  int64       `tlb:"## 64"`
	}

	s := testStruct{
		Int32Val:  123,
		StringVal: StringSnake{Value: "hello"},
		Int64Val:  456,
	}

	c, err := ToCell(s)
	if err != nil {
		t.Fatal(err)
	}

	var s2 testStruct
	err = LoadFromCell(&s2, c.BeginParse())
	if err != nil {
		t.Fatal(err)
	}

	if s2.Int32Val != s.Int32Val {
		t.Errorf("expected Int32Val to be %d, got %d", s.Int32Val, s2.Int32Val)
	}
	if s2.StringVal.Value != s.StringVal.Value {
		t.Errorf("expected StringVal to be '%s', got '%s'", s.StringVal.Value, s2.StringVal.Value)
	}
	if s2.Int64Val != s.Int64Val {
		t.Errorf("expected Int64Val to be %d, got %d", s.Int64Val, s2.Int64Val)
	}
}
