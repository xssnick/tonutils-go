package cellslice

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.ArgList = append(vm.ArgList,
		loadIntXSharedOp,
		fixedLoadIntSharedOp,
		plduzSharedOp,
		loadSliceXSharedOp,
		ldsliceSharedOp,
		fixedLoadSliceSharedOp,
	)
}

var loadIntXNames = [...]string{
	"LDIX",
	"LDUX",
	"PLDIX",
	"PLDUX",
	"LDIXQ",
	"LDUXQ",
	"PLDIXQ",
	"PLDUXQ",
}

var loadSliceXNames = [...]string{
	"LDSLICEX",
	"PLDSLICEX",
	"LDSLICEXQ",
	"PLDSLICEXQ",
}

func loadIntCommon(state *vm.State, bits uint, preload, unsigned, quiet bool) error {
	cs, err := state.Stack.PopSlice()
	if err != nil {
		return err
	}

	var val *big.Int
	if unsigned {
		if preload {
			val, err = cs.PreloadBigUInt(bits)
		} else {
			val, err = cs.LoadBigUInt(bits)
		}
	} else {
		if preload {
			val, err = cs.PreloadBigInt(bits)
		} else {
			val, err = cs.LoadBigInt(bits)
		}
	}

	if err != nil {
		if !quiet {
			return vmerr.Error(vmerr.CodeCellUnderflow)
		}
		if !preload {
			if pushErr := state.Stack.PushOwnedSlice(cs); pushErr != nil {
				return pushErr
			}
		}
		return state.Stack.PushBool(false)
	}

	if err = state.Stack.PushOwnedInt(val); err != nil {
		return err
	}
	if !preload {
		if err = state.Stack.PushOwnedSlice(cs); err != nil {
			return err
		}
	}
	if quiet {
		return state.Stack.PushBool(true)
	}
	return nil
}

func loadSliceCommon(state *vm.State, bits uint, preload, quiet bool) error {
	cs, err := state.Stack.PopSlice()
	if err != nil {
		return err
	}

	if cs.BitsLeft() < bits {
		if !quiet {
			return vmerr.Error(vmerr.CodeCellUnderflow)
		}
		if !preload {
			if pushErr := state.Stack.PushOwnedSlice(cs); pushErr != nil {
				return pushErr
			}
		}
		return state.Stack.PushBool(false)
	}

	var part *cell.Slice
	if preload {
		part, err = cs.PreloadSubslice(bits, 0)
	} else {
		part, err = cs.FetchSubslice(bits, 0)
	}
	if err != nil {
		if !quiet {
			return vmerr.Error(vmerr.CodeCellUnderflow)
		}
		if !preload {
			if pushErr := state.Stack.PushOwnedSlice(cs); pushErr != nil {
				return pushErr
			}
		}
		return state.Stack.PushBool(false)
	}

	if err = state.Stack.PushOwnedSlice(part); err != nil {
		return err
	}
	if !preload {
		if err = state.Stack.PushOwnedSlice(cs); err != nil {
			return err
		}
	}
	if quiet {
		return state.Stack.PushBool(true)
	}
	return nil
}

var loadIntXSharedOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0xD700>>3, 13)),
	ArgBits:  3,
	Name:     func(mode uint64) string { return loadIntXNames[mode&7] },
	Action: func(state *vm.State, mode uint64) error {
		if err := checkStackDepth(state, 2); err != nil {
			return err
		}

		bits, err := state.Stack.PopIntRangeInt64(0, 257-int64(mode&1))
		if err != nil {
			return err
		}
		return loadIntCommon(state, uint(bits), mode&2 != 0, mode&1 != 0, mode&4 != 0)
	},
})

// The 11-bit operand is the mode in bits 8..10 and the width minus one in the
// low byte, exactly as the instruction encodes it.
var fixedLoadIntSharedOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0xD708>>3, 13)),
	ArgBits:  11,
	Name: func(args uint64) string {
		mode := (args >> 8) & 7
		name := "LDI"
		if mode&1 != 0 {
			name = "LDU"
		}
		if mode&2 != 0 {
			name = "P" + name
		}
		if mode&4 != 0 {
			name += "Q"
		}
		return fmt.Sprintf("%s %d", name, uint(args&0xFF)+1)
	},
	Action: func(state *vm.State, args uint64) error {
		mode := (args >> 8) & 7
		return loadIntCommon(state, uint(args&0xFF)+1, mode&2 != 0, mode&1 != 0, mode&4 != 0)
	},
})

// The operand is the width in 32-bit units minus one.
var plduzSharedOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0xD710>>3, 13)),
	ArgBits:  3,
	Name: func(arg uint64) string {
		return fmt.Sprintf("PLDUZ %d", (arg+1)<<5)
	},
	Action: func(state *vm.State, arg uint64) error {
		actualBits := uint((arg + 1) << 5)
		cs, err := state.Stack.PopSlice()
		if err != nil {
			return err
		}
		loadBits := actualBits
		if left := cs.BitsLeft(); left < loadBits {
			loadBits = left
		}
		val := new(big.Int)
		if loadBits > 0 {
			val, err = cs.PreloadBigUInt(loadBits)
			if err != nil {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
		}
		if loadBits < actualBits {
			val.Lsh(val, actualBits-loadBits)
		}
		if err = state.Stack.PushOwnedSlice(cs); err != nil {
			return err
		}
		return state.Stack.PushOwnedInt(val)
	},
})

var loadSliceXSharedOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0xD718>>2, 14)),
	ArgBits:  2,
	Name:     func(mode uint64) string { return loadSliceXNames[mode&3] },
	Action: func(state *vm.State, mode uint64) error {
		if err := checkStackDepth(state, 2); err != nil {
			return err
		}

		bits, err := state.Stack.PopIntRangeInt64(0, 1023)
		if err != nil {
			return err
		}
		return loadSliceCommon(state, uint(bits), mode&1 != 0, mode&2 != 0)
	},
})

// The operand is the width minus one, exactly as it sits in the code.
var ldsliceSharedOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.BytesPrefix(0xD6)),
	ArgBits:  8,
	Name: func(args uint64) string {
		return fmt.Sprintf("LDSLICE %d", uint(args)+1)
	},
	Action: func(state *vm.State, args uint64) error {
		return loadSliceCommon(state, uint(args)+1, false, false)
	},
})

// The 10-bit operand is the mode in bits 8..9 and the width minus one in the
// low byte, exactly as the instruction encodes it.
var fixedLoadSliceSharedOp = helpers.NewArgOP(&helpers.ArgOP{
	Prefixed: helpers.SinglePrefixed(helpers.UIntPrefix(0xD71C>>2, 14)),
	ArgBits:  10,
	Name: func(args uint64) string {
		mode := (args >> 8) & 3
		name := "LDSLICE"
		if mode&1 != 0 {
			name = "PLDSLICE"
		}
		if mode&2 != 0 {
			name += "Q"
		}
		return fmt.Sprintf("%s %d", name, uint(args&0xFF)+1)
	},
	Action: func(state *vm.State, args uint64) error {
		mode := (args >> 8) & 3
		return loadSliceCommon(state, uint(args&0xFF)+1, mode&1 != 0, mode&2 != 0)
	},
})

func loadIntXOp(mode uint64) vm.OP { return vm.Bind(loadIntXSharedOp, mode) }

func loadSliceXOp(mode uint64) vm.OP { return vm.Bind(loadSliceXSharedOp, mode) }

func LDIX() vm.OP   { return loadIntXOp(0) }
func LDUX() vm.OP   { return loadIntXOp(1) }
func PLDIX() vm.OP  { return loadIntXOp(2) }
func PLDUX() vm.OP  { return loadIntXOp(3) }
func LDIXQ() vm.OP  { return loadIntXOp(4) }
func LDUXQ() vm.OP  { return loadIntXOp(5) }
func PLDIXQ() vm.OP { return loadIntXOp(6) }
func PLDUXQ() vm.OP { return loadIntXOp(7) }

func PLDUZ(bits uint) vm.OP {
	return vm.Bind(plduzSharedOp, uint64(bits>>5)-1)
}

func LDIFIX(bits uint, quiet, preload, unsigned bool) vm.OP {
	mode := uint64(0)
	if unsigned {
		mode |= 1
	}
	if preload {
		mode |= 2
	}
	if quiet {
		mode |= 4
	}
	return vm.Bind(fixedLoadIntSharedOp, (mode<<8)|uint64(bits-1))
}

func LDUFIX(bits uint, quiet, preload, unsigned bool) vm.OP {
	return LDIFIX(bits, quiet, preload, true)
}

func PLDIFIX(bits uint, quiet, preload, unsigned bool) vm.OP {
	return LDIFIX(bits, quiet, true, false)
}

func PLDUFIX(bits uint, quiet, preload, unsigned bool) vm.OP {
	return LDIFIX(bits, quiet, true, true)
}

func PLDSLICEX() vm.OP  { return loadSliceXOp(1) }
func LDSLICEXQ() vm.OP  { return loadSliceXOp(2) }
func PLDSLICEXQ() vm.OP { return loadSliceXOp(3) }

func LDSLICE(bits uint) vm.OP {
	return vm.Bind(ldsliceSharedOp, uint64(bits-1))
}

func LDSLICEFIX(bits uint, quiet, preload bool) vm.OP {
	mode := uint64(0)
	if preload {
		mode |= 1
	}
	if quiet {
		mode |= 2
	}
	return vm.Bind(fixedLoadSliceSharedOp, (mode<<8)|uint64(bits-1))
}

func PLDSLICEFIX(bits uint, quiet, preload bool) vm.OP {
	return LDSLICEFIX(bits, quiet, true)
}
