package exec

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List, func() vm.OP { return CALLREF(nil) })
	vm.List = append(vm.List, func() vm.OP { return JMPREF(nil) })
	vm.List = append(vm.List, func() vm.OP { return JMPREFDATA(nil) })
	vm.List = append(vm.List, func() vm.OP { return IFREF(nil) })
	vm.List = append(vm.List, func() vm.OP { return IFNOTREF(nil) })
	vm.List = append(vm.List, func() vm.OP { return IFJMPREF(nil) })
	vm.List = append(vm.List, func() vm.OP { return IFNOTJMPREF(nil) })
	vm.List = append(vm.List, func() vm.OP { return IFREFELSE(nil) })
	vm.List = append(vm.List, func() vm.OP { return IFELSEREF(nil) })
	vm.List = append(vm.List, func() vm.OP { return IFREFELSEREF(nil, nil) })
}

func CALLREF(code *cell.Cell) vm.OP {
	return bindRefCodeOp(newRefCodeOp("CALLREF", helpers.BytesPrefix(0xDB, 0x3C), 1, func(state *vm.State, refs []*cell.Cell) error {
		cont, err := loadContinuationFromCodeCell(state, refs[0])
		if err != nil {
			return err
		}
		return state.Call(cont)
	}), code)
}

func JMPREF(code *cell.Cell) vm.OP {
	return bindRefCodeOp(newRefCodeOp("JMPREF", helpers.BytesPrefix(0xDB, 0x3D), 1, func(state *vm.State, refs []*cell.Cell) error {
		cont, err := loadContinuationFromCodeCell(state, refs[0])
		if err != nil {
			return err
		}
		return state.Jump(cont)
	}), code)
}

func JMPREFDATA(code *cell.Cell) vm.OP {
	return bindRefCodeOp(newRefCodeOp("JMPREFDATA", helpers.BytesPrefix(0xDB, 0x3E), 1, func(state *vm.State, refs []*cell.Cell) error {
		if err := pushCurrentCode(state); err != nil {
			return err
		}
		cont, err := loadContinuationFromCodeCell(state, refs[0])
		if err != nil {
			return err
		}
		return state.Jump(cont)
	}), code)
}

func IFREF(code *cell.Cell) vm.OP {
	return bindRefCodeOp(newRefCodeOp("IFREF", helpers.BytesPrefix(0xE3, 0x00), 1, func(state *vm.State, refs []*cell.Cell) error {
		cond, err := state.Stack.PopBool()
		if err != nil {
			return err
		}
		if cond {
			cont, err := loadContinuationFromCodeCell(state, refs[0])
			if err != nil {
				return err
			}
			return state.Call(cont)
		}
		return nil
	}), code)
}

func IFNOTREF(code *cell.Cell) vm.OP {
	return bindRefCodeOp(newRefCodeOp("IFNOTREF", helpers.BytesPrefix(0xE3, 0x01), 1, func(state *vm.State, refs []*cell.Cell) error {
		cond, err := state.Stack.PopBool()
		if err != nil {
			return err
		}
		if !cond {
			cont, err := loadContinuationFromCodeCell(state, refs[0])
			if err != nil {
				return err
			}
			return state.Call(cont)
		}
		return nil
	}), code)
}

func IFJMPREF(code *cell.Cell) vm.OP {
	return bindRefCodeOp(newRefCodeOp("IFJMPREF", helpers.BytesPrefix(0xE3, 0x02), 1, func(state *vm.State, refs []*cell.Cell) error {
		cond, err := state.Stack.PopBool()
		if err != nil {
			return err
		}
		if cond {
			cont, err := loadContinuationFromCodeCell(state, refs[0])
			if err != nil {
				return err
			}
			return state.Jump(cont)
		}
		return nil
	}), code)
}

func IFNOTJMPREF(code *cell.Cell) vm.OP {
	return bindRefCodeOp(newRefCodeOp("IFNOTJMPREF", helpers.BytesPrefix(0xE3, 0x03), 1, func(state *vm.State, refs []*cell.Cell) error {
		cond, err := state.Stack.PopBool()
		if err != nil {
			return err
		}
		if !cond {
			cont, err := loadContinuationFromCodeCell(state, refs[0])
			if err != nil {
				return err
			}
			return state.Jump(cont)
		}
		return nil
	}), code)
}

func IFREFELSE(code *cell.Cell) vm.OP {
	return bindRefCodeOp(newRefCodeOp("IFREFELSE", helpers.BytesPrefix(0xE3, 0x0D), 1, func(state *vm.State, refs []*cell.Cell) error {
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			return err
		}
		cond, err := state.Stack.PopBool()
		if err != nil {
			return err
		}
		if cond {
			cont, err = loadContinuationFromCodeCell(state, refs[0])
			if err != nil {
				return err
			}
		}
		return state.Call(cont)
	}), code)
}

func IFELSEREF(code *cell.Cell) vm.OP {
	return bindRefCodeOp(newRefCodeOp("IFELSEREF", helpers.BytesPrefix(0xE3, 0x0E), 1, func(state *vm.State, refs []*cell.Cell) error {
		cont, err := state.Stack.PopContinuation()
		if err != nil {
			return err
		}
		cond, err := state.Stack.PopBool()
		if err != nil {
			return err
		}
		if !cond {
			cont, err = loadContinuationFromCodeCell(state, refs[0])
			if err != nil {
				return err
			}
		}
		return state.Call(cont)
	}), code)
}

func IFREFELSEREF(codeTrue, codeFalse *cell.Cell) vm.OP {
	return bindRefCodeOp(newRefCodeOp("IFREFELSEREF", helpers.BytesPrefix(0xE3, 0x0F), 2, func(state *vm.State, refs []*cell.Cell) error {
		cond, err := state.Stack.PopBool()
		if err != nil {
			return err
		}
		ref := refs[1]
		if cond {
			ref = refs[0]
		}
		cont, err := loadContinuationFromCodeCell(state, ref)
		if err != nil {
			return err
		}
		return state.Call(cont)
	}), codeTrue, codeFalse)
}
