package vm

import (
	"math/big"
	"testing"
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
