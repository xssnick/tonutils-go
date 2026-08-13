package cellslice

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestDynamicLoadFamiliesHaveSingleRegisteredHandler(t *testing.T) {
	for _, tt := range []struct {
		name  string
		bits  uint
		value uint64
	}{
		{name: "integer", bits: 13, value: 0xD700 >> 3},
		{name: "slice", bits: 14, value: 0xD718 >> 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var names []string
			collect := func(prefixes []*cell.Slice, name string) {
				for _, prefix := range prefixes {
					if prefix.BitsLeft() != tt.bits {
						continue
					}
					value, err := prefix.PreloadUInt(tt.bits)
					if err == nil && value == tt.value {
						names = append(names, name)
					}
				}
			}

			for _, getter := range vm.AllOps() {
				op := getter()
				collect(op.GetPrefixes(), op.SerializeText())
			}

			if len(names) != 1 {
				t.Fatalf("registered handlers = %v, want exactly one", names)
			}
		})
	}
}
