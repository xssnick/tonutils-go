package cellslice

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return LDIX() },
		func() vm.OP { return LDUX() },
		func() vm.OP { return PLDIX() },
		func() vm.OP { return PLDUX() },
		func() vm.OP { return LDIXQ() },
		func() vm.OP { return LDUXQ() },
		func() vm.OP { return PLDIXQ() },
		func() vm.OP { return PLDUXQ() },
		func() vm.OP { return LDIFIX(1, false, false, false) },
		func() vm.OP { return LDUFIX(1, false, false, false) },
		func() vm.OP { return PLDIFIX(1, false, false, false) },
		func() vm.OP { return PLDUFIX(1, false, false, false) },
		func() vm.OP { return LDIFIX(1, true, false, false) },
		func() vm.OP { return LDUFIX(1, true, false, false) },
		func() vm.OP { return PLDIFIX(1, true, false, false) },
		func() vm.OP { return PLDUFIX(1, true, false, false) },
		func() vm.OP { return PLDUZ(32) },
		func() vm.OP { return PLDSLICEX() },
		func() vm.OP { return LDSLICEXQ() },
		func() vm.OP { return PLDSLICEXQ() },
		func() vm.OP { return LDSLICEFIX(1, false, false) },
		func() vm.OP { return PLDSLICEFIX(1, false, false) },
		func() vm.OP { return LDSLICEFIX(1, true, false) },
		func() vm.OP { return PLDSLICEFIX(1, true, false) },
	)
}

func loadIntCommon(state *vm.State, bits uint, preload, unsigned, quiet bool) error {
	cs, err := state.Stack.PopSlice()
	if err != nil {
		return err
	}

	var val any
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
			if pushErr := state.Stack.PushSlice(cs); pushErr != nil {
				return pushErr
			}
		}
		return state.Stack.PushBool(false)
	}

	if err = state.Stack.PushAny(val); err != nil {
		return err
	}
	if !preload {
		if err = state.Stack.PushSlice(cs); err != nil {
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
			if pushErr := state.Stack.PushSlice(cs); pushErr != nil {
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
			if pushErr := state.Stack.PushSlice(cs); pushErr != nil {
				return pushErr
			}
		}
		return state.Stack.PushBool(false)
	}

	if err = state.Stack.PushSlice(part); err != nil {
		return err
	}
	if !preload {
		if err = state.Stack.PushSlice(cs); err != nil {
			return err
		}
	}
	if quiet {
		return state.Stack.PushBool(true)
	}
	return nil
}

func loadIntXOp(name string, mode uint64) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string { return name },
		BitPrefix:      helpers.UIntPrefix(0xD700>>3, 13),
		FixedSizeBits:  3,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(mode, 3)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			loaded, err := code.LoadUInt(3)
			if err != nil {
				return err
			}
			mode = loaded
			return nil
		},
		Action: func(state *vm.State) error {
			bits, err := state.Stack.PopIntRange(0, 257-int64(mode&1))
			if err != nil {
				return err
			}
			return loadIntCommon(state, uint(bits.Uint64()), mode&2 != 0, mode&1 != 0, mode&4 != 0)
		},
	}
}

func fixedLoadIntOp(name string, prefix uint64, bits uint, quiet, preload, unsigned bool) *helpers.AdvancedOP {
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
	args := (mode << 8) | uint64(bits-1)

	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			actualBits := uint(args&0xFF) + 1
			actualMode := (args >> 8) & 7
			actualName := "LDI"
			if actualMode&1 != 0 {
				actualName = "LDU"
			}
			if actualMode&2 != 0 {
				actualName = "P" + actualName
			}
			if actualMode&4 != 0 {
				actualName += "Q"
			}
			return fmt.Sprintf("%s %d", actualName, actualBits)
		},
		BitPrefix:     helpers.UIntPrefix(prefix, 13),
		FixedSizeBits: 11,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(args, 11)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			loaded, err := code.LoadUInt(11)
			if err != nil {
				return err
			}
			args = loaded
			return nil
		},
		Action: func(state *vm.State) error {
			actualBits := uint(args&0xFF) + 1
			actualMode := (args >> 8) & 7
			return loadIntCommon(state, actualBits, actualMode&2 != 0, actualMode&1 != 0, actualMode&4 != 0)
		},
	}
}

func PLDUZ(bits uint) *helpers.AdvancedOP {
	arg := uint64(bits>>5) - 1
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("PLDUZ %d", bits)
		},
		BitPrefix:     helpers.UIntPrefix(0xD710>>3, 13),
		FixedSizeBits: 3,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(arg, 3)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			loaded, err := code.LoadUInt(3)
			if err != nil {
				return err
			}
			arg = loaded
			return nil
		},
		Action: func(state *vm.State) error {
			actualBits := uint((arg + 1) << 5)
			cs, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			val, err := cs.PreloadBigUInt(actualBits)
			if err != nil {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			if err = state.Stack.PushSlice(cs); err != nil {
				return err
			}
			return state.Stack.PushInt(val)
		},
	}
}

func loadSliceXOp(name string, mode uint64) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string { return name },
		BitPrefix:      helpers.UIntPrefix(0xD718>>2, 14),
		FixedSizeBits:  2,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(mode, 2)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			loaded, err := code.LoadUInt(2)
			if err != nil {
				return err
			}
			mode = loaded
			return nil
		},
		Action: func(state *vm.State) error {
			bits, err := state.Stack.PopIntRange(0, 1023)
			if err != nil {
				return err
			}
			return loadSliceCommon(state, uint(bits.Uint64()), mode&1 != 0, mode&2 != 0)
		},
	}
}

func fixedLoadSliceOp(name string, prefix uint64, bits uint, quiet, preload bool) *helpers.AdvancedOP {
	mode := uint64(0)
	if preload {
		mode |= 1
	}
	if quiet {
		mode |= 2
	}
	args := (mode << 8) | uint64(bits-1)

	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			actualBits := uint(args&0xFF) + 1
			actualMode := (args >> 8) & 3
			actualName := "LDSLICE"
			if actualMode&1 != 0 {
				actualName = "PLDSLICE"
			}
			if actualMode&2 != 0 {
				actualName += "Q"
			}
			return fmt.Sprintf("%s %d", actualName, actualBits)
		},
		BitPrefix:     helpers.UIntPrefix(prefix, 14),
		FixedSizeBits: 10,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(args, 10)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			loaded, err := code.LoadUInt(10)
			if err != nil {
				return err
			}
			args = loaded
			return nil
		},
		Action: func(state *vm.State) error {
			actualBits := uint(args&0xFF) + 1
			actualMode := (args >> 8) & 3
			return loadSliceCommon(state, actualBits, actualMode&1 != 0, actualMode&2 != 0)
		},
	}
}

func LDIX() *helpers.AdvancedOP   { return loadIntXOp("LDIX", 0) }
func LDUX() *helpers.AdvancedOP   { return loadIntXOp("LDUX", 1) }
func PLDIX() *helpers.AdvancedOP  { return loadIntXOp("PLDIX", 2) }
func PLDUX() *helpers.AdvancedOP  { return loadIntXOp("PLDUX", 3) }
func LDIXQ() *helpers.AdvancedOP  { return loadIntXOp("LDIXQ", 4) }
func LDUXQ() *helpers.AdvancedOP  { return loadIntXOp("LDUXQ", 5) }
func PLDIXQ() *helpers.AdvancedOP { return loadIntXOp("PLDIXQ", 6) }
func PLDUXQ() *helpers.AdvancedOP { return loadIntXOp("PLDUXQ", 7) }
func LDIFIX(bits uint, quiet, preload, unsigned bool) *helpers.AdvancedOP {
	name := "LDI"
	if unsigned {
		name = "LDU"
	}
	if preload {
		name = "P" + name
	}
	if quiet {
		name += "Q"
	}
	return fixedLoadIntOp(name, 0xD708>>3, bits, quiet, preload, unsigned)
}

func LDUFIX(bits uint, quiet, preload, unsigned bool) *helpers.AdvancedOP {
	return LDIFIX(bits, quiet, preload, true)
}

func PLDIFIX(bits uint, quiet, preload, unsigned bool) *helpers.AdvancedOP {
	return LDIFIX(bits, quiet, true, false)
}

func PLDUFIX(bits uint, quiet, preload, unsigned bool) *helpers.AdvancedOP {
	return LDIFIX(bits, quiet, true, true)
}

func PLDSLICEX() *helpers.AdvancedOP  { return loadSliceXOp("PLDSLICEX", 1) }
func LDSLICEXQ() *helpers.AdvancedOP  { return loadSliceXOp("LDSLICEXQ", 2) }
func PLDSLICEXQ() *helpers.AdvancedOP { return loadSliceXOp("PLDSLICEXQ", 3) }

func LDSLICEFIX(bits uint, quiet, preload bool) *helpers.AdvancedOP {
	name := "LDSLICE"
	if preload {
		name = "PLDSLICE"
	}
	if quiet {
		name += "Q"
	}
	return fixedLoadSliceOp(name, 0xD71C>>2, bits, quiet, preload)
}

func PLDSLICEFIX(bits uint, quiet, preload bool) *helpers.AdvancedOP {
	return LDSLICEFIX(bits, quiet, true)
}
