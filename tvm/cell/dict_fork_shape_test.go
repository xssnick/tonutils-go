package cell

import (
	"errors"
	"testing"
)

// buildForkShapeNode builds a dictionary node cell with a labelLen-bit label,
// payloadBits of payload after it and refs references, then parses it exactly
// as a dictionary walk does.
func buildForkShapeNode(t *testing.T, remaining, labelLen, payloadBits uint, refs int) fixedDictNode {
	t.Helper()

	labelBits := BeginCell()
	if labelLen > 0 {
		if err := labelBits.StoreUInt(0, labelLen); err != nil {
			t.Fatalf("failed to build label: %v", err)
		}
	}

	node := BeginCell()
	if err := storeDictLabel(node, builderSliceView(labelBits), remaining); err != nil {
		t.Fatalf("failed to store label: %v", err)
	}
	if payloadBits > 0 {
		if err := node.StoreUInt(0, payloadBits); err != nil {
			t.Fatalf("failed to store payload: %v", err)
		}
	}
	for i := 0; i < refs; i++ {
		if err := node.StoreRef(BeginCell().EndCell()); err != nil {
			t.Fatalf("failed to store ref %d: %v", i, err)
		}
	}

	parsed, err := parseFixedDictNode(node.EndCell(), remaining)
	if err != nil {
		t.Fatalf("failed to parse node: %v", err)
	}
	if parsed.labelLen != labelLen {
		t.Fatalf("parsed label length %d, want %d", parsed.labelLen, labelLen)
	}
	return parsed
}

// TestValidateForkShapeModes pins the merged validator to the behavior of the
// separate strict and lenient checks it replaced.
func TestValidateForkShapeModes(t *testing.T) {
	const remaining = 4

	cases := []struct {
		name        string
		labelLen    uint
		payloadBits uint
		refs        int
		strictErr   bool
		lenientErr  bool
	}{
		{name: "leaf", labelLen: remaining, payloadBits: 7, refs: 1},
		{name: "empty leaf", labelLen: remaining, payloadBits: 0, refs: 0},
		{name: "fork", labelLen: 1, payloadBits: 0, refs: 2},
		{name: "fork with trailing bits", labelLen: 1, payloadBits: 5, refs: 2, strictErr: true},
		{name: "fork with one ref", labelLen: 1, payloadBits: 0, refs: 1, strictErr: true, lenientErr: true},
		{name: "fork with trailing bits and one ref", labelLen: 1, payloadBits: 5, refs: 1, strictErr: true, lenientErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			node := buildForkShapeNode(t, remaining, c.labelLen, c.payloadBits, c.refs)

			for _, mode := range []struct {
				lenient bool
				wantErr bool
			}{
				{lenient: false, wantErr: c.strictErr},
				{lenient: true, wantErr: c.lenientErr},
			} {
				err := node.validateForkShape(remaining, mode.lenient)
				if mode.wantErr {
					if !errors.Is(err, ErrInvalidDictForkNode) {
						t.Fatalf("lenient=%v: got %v, want %v", mode.lenient, err, ErrInvalidDictForkNode)
					}
					continue
				}
				if err != nil {
					t.Fatalf("lenient=%v: unexpected error: %v", mode.lenient, err)
				}
			}
		})
	}
}
