package vm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestStackPushOwnedIntKeepsOwnedValue(t *testing.T) {
	st := NewStack()
	value := new(big.Int).Lsh(big.NewInt(1), 80)
	if err := st.PushOwnedInt(value); err != nil {
		t.Fatal(err)
	}

	got, err := st.PopInt()
	if err != nil {
		t.Fatal(err)
	}
	if got != value {
		t.Fatal("owned non-static integer was copied")
	}
}

func TestStackPushOwnedIntCanonicalizesStaticValues(t *testing.T) {
	st := NewStack()
	value := big.NewInt(1)
	if err := st.PushOwnedInt(value); err != nil {
		t.Fatal(err)
	}

	got, err := st.PopInt()
	if err != nil {
		t.Fatal(err)
	}
	if got == value {
		t.Fatal("static integer should not keep caller-owned pointer")
	}
	if got.Sign() != 1 || got.Int64() != 1 {
		t.Fatalf("unexpected static integer value: %s", got)
	}
}

func TestStackPushOwnedSliceKeepsOwnedValueAndBindsTrace(t *testing.T) {
	st := NewStack()
	trace := cell.NewTrace(cell.TraceHooks{})
	st.SetTrace(trace)

	value := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().MustBeginParse()
	if err := st.PushOwnedSlice(value); err != nil {
		t.Fatal(err)
	}

	got, err := st.Get(0)
	if err != nil {
		t.Fatal(err)
	}
	if got != value {
		t.Fatal("owned slice was copied")
	}
	if value.Trace() != trace {
		t.Fatal("owned slice was not bound to stack trace")
	}
}
