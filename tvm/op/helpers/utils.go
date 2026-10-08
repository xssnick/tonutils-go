package helpers

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func Builder(b []byte) *cell.Builder {
	return cell.BeginCell().MustStoreSlice(b, uint(len(b)*8))
}

// PeekZeroPaddedOpcode drains what is left of a truncated instruction and
// returns it zero-padded to bits. That padded value is the opcode-table entry
// the reference VM has already selected and charged for, so gas for a
// truncated instruction follows it rather than the undecoded default.
// Callers reach this only after reading bits failed, which for sz <= 64 happens
// only on underflow, so the remainder is always shorter than bits.
func PeekZeroPaddedOpcode(code *cell.Slice, bits uint) uint64 {
	rest := code.BitsLeft()
	if rest == 0 {
		return 0
	}

	raw, _ := code.LoadUInt(rest)
	return raw << (bits - rest)
}
