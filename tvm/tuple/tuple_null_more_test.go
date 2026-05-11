package tuple

import "testing"

func TestTupleIsNullDistinguishesUndefinedFromEmpty(t *testing.T) {
	var zero Tuple
	if !zero.IsNull() {
		t.Fatal("zero tuple should be null")
	}

	empty := NewTupleValue()
	if empty.IsNull() {
		t.Fatal("explicit empty tuple should not be null")
	}
	if empty.Len() != 0 {
		t.Fatalf("empty tuple len = %d, want 0", empty.Len())
	}
}
