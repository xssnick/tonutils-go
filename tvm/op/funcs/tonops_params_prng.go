package funcs

import (
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const (
	paramIdxPrevBlocksInfo = 13
	paramIdxUnpackedConfig = 14
	paramIdxDuePayment     = 15
	paramIdxPrecompiledGas = 16
	paramIdxInMsgParams    = 17
	paramIdxRandomSeed     = 6
	inMsgParamBounce       = 0
	inMsgParamBounced      = 1
	inMsgParamSrc          = 2
	inMsgParamFwdFee       = 3
	inMsgParamLT           = 4
	inMsgParamUTime        = 5
	inMsgParamOrigValue    = 6
	inMsgParamValue        = 7
	inMsgParamValueExtra   = 8
	inMsgParamStateInit    = 9
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return PREVBLOCKSINFOTUPLE() },
		func() vm.OP { return UNPACKEDCONFIGTUPLE() },
		func() vm.OP { return DUEPAYMENT() },
		func() vm.OP { return PREVMCBLOCKS() },
		func() vm.OP { return PREVKEYBLOCK() },
		func() vm.OP { return PREVMCBLOCKS_100() },
		func() vm.OP { return INMSGPARAMS() },
		func() vm.OP { return INMSG_BOUNCE() },
		func() vm.OP { return INMSG_BOUNCED() },
		func() vm.OP { return INMSG_SRC() },
		func() vm.OP { return INMSG_FWDFEE() },
		func() vm.OP { return INMSG_LT() },
		func() vm.OP { return INMSG_UTIME() },
		func() vm.OP { return INMSG_ORIGVALUE() },
		func() vm.OP { return INMSG_VALUE() },
		func() vm.OP { return INMSG_VALUEEXTRA() },
		func() vm.OP { return INMSG_STATEINIT() },
		func() vm.OP { return INMSGPARAM(0) },
		func() vm.OP { return GETPRECOMPILEDGAS() },
		func() vm.OP { return RANDU256() },
		func() vm.OP { return RAND() },
		func() vm.OP { return SETRAND() },
		func() vm.OP { return ADDRAND() },
	)
}

func PREVBLOCKSINFOTUPLE() *helpers.SimpleOP {
	return paramAlias("PREVBLOCKSINFOTUPLE", 0xF82D, paramIdxPrevBlocksInfo)
}
func UNPACKEDCONFIGTUPLE() *helpers.SimpleOP {
	return paramAlias("UNPACKEDCONFIGTUPLE", 0xF82E, paramIdxUnpackedConfig)
}
func DUEPAYMENT() *helpers.SimpleOP { return paramAlias("DUEPAYMENT", 0xF82F, paramIdxDuePayment) }

func INMSGPARAMS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			v, err := state.GetParam(paramIdxInMsgParams)
			if err != nil {
				return err
			}
			return pushHostValue(state, v)
		},
		Name:      "INMSGPARAMS",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x81, 0x11),
	}
}

func prevBlocksInfoAlias(name string, opcode byte, idx int) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			tup, err := getPrevBlocksTuple(state)
			if err != nil {
				return err
			}
			v, err := tup.Index(idx)
			if err != nil {
				return err
			}
			return pushHostValue(state, v)
		},
		Name:      name,
		BitPrefix: helpers.BytesPrefix(0xF8, 0x34, opcode),
	}
}

func PREVMCBLOCKS() *helpers.SimpleOP     { return prevBlocksInfoAlias("PREVMCBLOCKS", 0x00, 0) }
func PREVKEYBLOCK() *helpers.SimpleOP     { return prevBlocksInfoAlias("PREVKEYBLOCK", 0x01, 1) }
func PREVMCBLOCKS_100() *helpers.SimpleOP { return prevBlocksInfoAlias("PREVMCBLOCKS_100", 0x02, 2) }

func inMsgParamAlias(name string, opcode byte, idx int) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			return pushInMsgParam(state, idx)
		},
		Name:      name,
		BitPrefix: helpers.BytesPrefix(0xF8, opcode),
	}
}

func INMSG_BOUNCE() *helpers.SimpleOP { return inMsgParamAlias("INMSG_BOUNCE", 0x90, inMsgParamBounce) }
func INMSG_BOUNCED() *helpers.SimpleOP {
	return inMsgParamAlias("INMSG_BOUNCED", 0x91, inMsgParamBounced)
}
func INMSG_SRC() *helpers.SimpleOP    { return inMsgParamAlias("INMSG_SRC", 0x92, inMsgParamSrc) }
func INMSG_FWDFEE() *helpers.SimpleOP { return inMsgParamAlias("INMSG_FWDFEE", 0x93, inMsgParamFwdFee) }
func INMSG_LT() *helpers.SimpleOP     { return inMsgParamAlias("INMSG_LT", 0x94, inMsgParamLT) }
func INMSG_UTIME() *helpers.SimpleOP  { return inMsgParamAlias("INMSG_UTIME", 0x95, inMsgParamUTime) }
func INMSG_ORIGVALUE() *helpers.SimpleOP {
	return inMsgParamAlias("INMSG_ORIGVALUE", 0x96, inMsgParamOrigValue)
}
func INMSG_VALUE() *helpers.SimpleOP { return inMsgParamAlias("INMSG_VALUE", 0x97, inMsgParamValue) }
func INMSG_VALUEEXTRA() *helpers.SimpleOP {
	return inMsgParamAlias("INMSG_VALUEEXTRA", 0x98, inMsgParamValueExtra)
}
func INMSG_STATEINIT() *helpers.SimpleOP {
	return inMsgParamAlias("INMSG_STATEINIT", 0x99, inMsgParamStateInit)
}

func INMSGPARAM(idx uint8) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			return fmt.Sprintf("INMSGPARAM %d", idx&15)
		},
		BitPrefix:     helpers.UIntPrefix(0xF89, 12),
		FixedSizeBits: 4,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(idx&15), 4)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(4)
			if err != nil {
				return err
			}
			idx = uint8(v)
			return nil
		},
		Action: func(state *vm.State) error {
			return pushInMsgParam(state, int(idx))
		},
	}
}

func GETPRECOMPILEDGAS() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			v, err := state.GetParam(paramIdxPrecompiledGas)
			if err != nil {
				return err
			}
			return pushHostValue(state, v)
		},
		Name:      "GETPRECOMPILEDGAS",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x39),
	}
}

func getPrevBlocksTuple(state *vm.State) (tuple.Tuple, error) {
	v, err := state.GetParam(paramIdxPrevBlocksInfo)
	if err != nil {
		return tuple.Tuple{}, err
	}
	tup, ok := v.(tuple.Tuple)
	if !ok {
		return tuple.Tuple{}, vmerr.Error(vmerr.CodeTypeCheck)
	}
	return tup, nil
}

func getInMsgParamsTuple(state *vm.State) (tuple.Tuple, error) {
	v, err := state.GetParam(paramIdxInMsgParams)
	if err != nil {
		return tuple.Tuple{}, err
	}
	tup, ok := v.(tuple.Tuple)
	if !ok {
		return tuple.Tuple{}, vmerr.Error(vmerr.CodeTypeCheck)
	}
	return tup, nil
}

func pushInMsgParam(state *vm.State, idx int) error {
	tup, err := getInMsgParamsTuple(state)
	if err != nil {
		return err
	}
	v, err := tup.Index(idx)
	if err != nil {
		return err
	}
	return pushHostValue(state, v)
}

func getRandSeed(state *vm.State) (*big.Int, error) {
	v, err := state.GetParam(paramIdxRandomSeed)
	if err != nil {
		return nil, err
	}
	seed, ok := v.(*big.Int)
	if !ok {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	}
	if seed.Sign() < 0 || seed.BitLen() > 256 {
		return nil, vmerr.Error(vmerr.CodeRangeCheck, "random seed out of range")
	}
	return new(big.Int).Set(seed), nil
}

func setRandSeed(state *vm.State, seed *big.Int) error {
	if seed == nil || seed.Sign() < 0 || seed.BitLen() > 256 {
		return vmerr.Error(vmerr.CodeRangeCheck, "random seed out of range")
	}

	top := state.Reg.C7.Copy()
	innerVal, err := top.Index(0)
	if err != nil {
		return err
	}
	inner, ok := innerVal.(tuple.Tuple)
	if !ok {
		return vmerr.Error(vmerr.CodeTypeCheck, "intermediate value is not a tuple")
	}
	inner = inner.Copy()
	if err = inner.Set(paramIdxRandomSeed, new(big.Int).Set(seed)); err != nil {
		return err
	}
	if err = state.ConsumeTupleGasLen(inner.Len()); err != nil {
		return err
	}
	if err = top.Set(0, inner); err != nil {
		return err
	}
	if err = state.ConsumeTupleGasLen(top.Len()); err != nil {
		return err
	}
	return state.SetC7(top)
}

func randSeedBytes(state *vm.State) ([]byte, error) {
	seed, err := getRandSeed(state)
	if err != nil {
		return nil, err
	}
	return exportUnsignedBytes(seed, 32, "random seed out of range")
}

func generateRandU256(state *vm.State) (*big.Int, error) {
	seed, err := randSeedBytes(state)
	if err != nil {
		return nil, err
	}
	sum := sha512.Sum512(seed)
	nextSeed := new(big.Int).SetBytes(sum[:32])
	if err = setRandSeed(state, nextSeed); err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(sum[32:]), nil
}

func RANDU256() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			val, err := generateRandU256(state)
			if err != nil {
				return err
			}
			return state.Stack.PushInt(val)
		},
		Name:      "RANDU256",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x10),
	}
}

func RAND() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			y, err := generateRandU256(state)
			if err != nil {
				return err
			}
			res := new(big.Int).Mul(x, y)
			res.Rsh(res, 256)
			return state.Stack.PushInt(res)
		},
		Name:      "RAND",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x11),
	}
}

func setRandOp(name string, opcode byte, mix bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if x.Sign() < 0 || x.BitLen() > 256 {
				return vmerr.Error(vmerr.CodeRangeCheck, "new random seed out of range")
			}
			next := new(big.Int).Set(x)
			if mix {
				seed, seedErr := randSeedBytes(state)
				if seedErr != nil {
					return seedErr
				}
				mixBytes, exportErr := exportUnsignedBytes(x, 32, "mixed seed value out of range")
				if exportErr != nil {
					return exportErr
				}
				buf := make([]byte, 64)
				copy(buf, seed)
				copy(buf[32:], mixBytes)
				sum := sha256.Sum256(buf)
				next = new(big.Int).SetBytes(sum[:])
			}
			return setRandSeed(state, next)
		},
		Name:      name,
		BitPrefix: helpers.BytesPrefix(0xF8, opcode),
	}
}

func SETRAND() *helpers.SimpleOP { return setRandOp("SETRAND", 0x14, false) }
func ADDRAND() *helpers.SimpleOP { return setRandOp("ADDRAND", 0x15, true) }
