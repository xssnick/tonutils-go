package math

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/op/helpers"
)

func TestImmediateShiftConstructorsCoverByteRange(t *testing.T) {
	constructors := []struct {
		name string
		new  func(int) *helpers.AdvancedOP
	}{
		{name: "LSHIFT#", new: LSHIFTCODE},
		{name: "RSHIFT#", new: RSHIFTCODE},
		{name: "QLSHIFT#", new: QLSHIFTCODE},
		{name: "QRSHIFT#", new: QRSHIFTCODE},
		{name: "RSHIFTR#", new: RSHIFTRCODE},
		{name: "RSHIFTC#", new: RSHIFTCCODE},
		{name: "RSHIFT# floor", new: RSHIFTCODEFLOOR},
		{name: "MULRSHIFT#", new: MULRSHIFTCODE},
		{name: "MULRSHIFTR#", new: MULRSHIFTRCODE},
		{name: "MULRSHIFTC#", new: MULRSHIFTCCODE},
		{name: "MODPOW2#", new: MODPOW2CODE},
		{name: "MODPOW2R#", new: MODPOW2RCODE},
		{name: "MODPOW2C#", new: MODPOW2CCODE},
		{name: "MULMODPOW2#", new: MULMODPOW2CODE},
		{name: "MULMODPOW2R#", new: MULMODPOW2RCODE},
		{name: "MULMODPOW2C#", new: MULMODPOW2CCODE},
		{name: "RSHIFT#MOD", new: RSHIFTCODEMOD},
		{name: "RSHIFTR#MOD", new: RSHIFTRCODEMOD},
		{name: "RSHIFTC#MOD", new: RSHIFTCCODEMOD},
		{name: "MULRSHIFT#MOD", new: MULRSHIFTCODEMOD},
		{name: "MULRSHIFTR#MOD", new: MULRSHIFTRCODEMOD},
		{name: "MULRSHIFTC#MOD", new: MULRSHIFTCCODEMOD},
		{name: "ADDRSHIFT#MOD", new: ADDRSHIFTCODEMOD},
		{name: "ADDRSHIFTR#MOD", new: ADDRSHIFTRCODEMOD},
		{name: "ADDRSHIFTC#MOD", new: ADDRSHIFTCCODEMOD},
		{name: "MULADDRSHIFT#MOD", new: MULADDRSHIFTCODEMOD},
		{name: "MULADDRSHIFTR#MOD", new: MULADDRSHIFTRCODEMOD},
		{name: "MULADDRSHIFTC#MOD", new: MULADDRSHIFTCCODEMOD},
	}

	for _, constructor := range constructors {
		t.Run(constructor.name, func(t *testing.T) {
			for _, shift := range []int{1, 127, 128, 255, 256} {
				src := constructor.new(shift)
				encoded := src.Serialize().EndCell()
				slice := encoded.MustBeginParse()
				if err := slice.SkipBits(slice.BitsLeft() - 8); err != nil {
					t.Fatalf("shift %d: skip opcode prefix: %v", shift, err)
				}
				if suffix := slice.MustLoadUInt(8); suffix != uint64(shift-1) {
					t.Fatalf("shift %d: suffix = %d, want %d", shift, suffix, shift-1)
				}

				dst := constructor.new(1)
				if err := dst.Deserialize(encoded.MustBeginParse()); err != nil {
					t.Fatalf("shift %d: deserialize: %v", shift, err)
				}
				if got := dst.Serialize().EndCell().Hash(); string(got) != string(encoded.Hash()) {
					t.Fatalf("shift %d: constructor round trip changed bytecode", shift)
				}
			}
		})
	}
}
