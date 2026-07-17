package dict

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestDICTIGETJMPZHasSingleCanonicalRegistration(t *testing.T) {
	var names []string
	for _, getter := range vm.List {
		op := getter()
		for _, prefix := range op.GetPrefixes() {
			if prefix.BitsLeft() != 16 {
				continue
			}
			value, err := prefix.PreloadUInt(16)
			if err == nil && value == 0xF4BC {
				names = append(names, op.SerializeText())
			}
		}
	}

	if len(names) != 1 || names[0] != "DICTIGETJMPZ" {
		t.Fatalf("F4BC registrations = %v, want one canonical DICTIGETJMPZ", names)
	}
}
