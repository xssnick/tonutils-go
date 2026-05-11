package cellslice

import (
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return SEMPTY() },
		func() vm.OP { return SDEMPTY() },
		func() vm.OP { return SREMPTY() },
		func() vm.OP { return SDFIRST() },
		func() vm.OP { return SDCNTLEAD0() },
		func() vm.OP { return SDCNTLEAD1() },
		func() vm.OP { return SDCNTTRAIL0() },
		func() vm.OP { return SDCNTTRAIL1() },
	)
}

func unarySliceBoolOp(name string, prefix helpers.BitPrefix, fn func(*vm.State, *vm.Stack) (bool, error)) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name: name,
		Action: func(state *vm.State) error {
			stk := state.Stack
			ok, err := fn(state, stk)
			if err != nil {
				return err
			}
			return stk.PushBool(ok)
		},
		BitPrefix: prefix,
	}
}

func unarySliceIntOp(name string, prefix helpers.BitPrefix, fn func(*vm.State, *vm.Stack) (int64, error)) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Name: name,
		Action: func(state *vm.State) error {
			stk := state.Stack
			val, err := fn(state, stk)
			if err != nil {
				return err
			}
			return pushInt64(state, val)
		},
		BitPrefix: prefix,
	}
}

func SEMPTY() *helpers.SimpleOP {
	return unarySliceBoolOp("SEMPTY", helpers.BytesPrefix(0xC7, 0x00), func(_ *vm.State, stk *vm.Stack) (bool, error) {
		cs, err := stk.PopSlice()
		if err != nil {
			return false, err
		}
		return cs.BitsLeft() == 0 && cs.RefsNum() == 0, nil
	})
}

func SDEMPTY() *helpers.SimpleOP {
	return unarySliceBoolOp("SDEMPTY", helpers.BytesPrefix(0xC7, 0x01), func(_ *vm.State, stk *vm.Stack) (bool, error) {
		cs, err := stk.PopSlice()
		if err != nil {
			return false, err
		}
		return cs.BitsLeft() == 0, nil
	})
}

func SREMPTY() *helpers.SimpleOP {
	return unarySliceBoolOp("SREMPTY", helpers.BytesPrefix(0xC7, 0x02), func(_ *vm.State, stk *vm.Stack) (bool, error) {
		cs, err := stk.PopSlice()
		if err != nil {
			return false, err
		}
		return cs.RefsNum() == 0, nil
	})
}

func SDFIRST() *helpers.SimpleOP {
	return unarySliceBoolOp("SDFIRST", helpers.BytesPrefix(0xC7, 0x03), func(_ *vm.State, stk *vm.Stack) (bool, error) {
		cs, err := stk.PopSlice()
		if err != nil {
			return false, err
		}
		if cs.BitsLeft() == 0 {
			return false, nil
		}
		return cs.MustPreloadUInt(1) == 1, nil
	})
}

func SDCNTLEAD0() *helpers.SimpleOP {
	return unarySliceIntOp("SDCNTLEAD0", helpers.BytesPrefix(0xC7, 0x10), func(_ *vm.State, stk *vm.Stack) (int64, error) {
		cs, err := stk.PopSlice()
		if err != nil {
			return 0, err
		}
		return int64(cs.CountLeading(false)), nil
	})
}

func SDCNTLEAD1() *helpers.SimpleOP {
	return unarySliceIntOp("SDCNTLEAD1", helpers.BytesPrefix(0xC7, 0x11), func(_ *vm.State, stk *vm.Stack) (int64, error) {
		cs, err := stk.PopSlice()
		if err != nil {
			return 0, err
		}
		return int64(cs.CountLeading(true)), nil
	})
}

func SDCNTTRAIL0() *helpers.SimpleOP {
	return unarySliceIntOp("SDCNTTRAIL0", helpers.BytesPrefix(0xC7, 0x12), func(_ *vm.State, stk *vm.Stack) (int64, error) {
		cs, err := stk.PopSlice()
		if err != nil {
			return 0, err
		}
		return int64(cs.CountTrailing(false)), nil
	})
}

func SDCNTTRAIL1() *helpers.SimpleOP {
	return unarySliceIntOp("SDCNTTRAIL1", helpers.BytesPrefix(0xC7, 0x13), func(_ *vm.State, stk *vm.Stack) (int64, error) {
		cs, err := stk.PopSlice()
		if err != nil {
			return 0, err
		}
		return int64(cs.CountTrailing(true)), nil
	})
}
