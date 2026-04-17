package funcs

import (
	"errors"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return CDATASIZEQ() },
		func() vm.OP { return CDATASIZE() },
		func() vm.OP { return SDATASIZEQ() },
		func() vm.OP { return SDATASIZE() },
		func() vm.OP { return LDVARINT16() },
		func() vm.OP { return STVARINT16() },
		func() vm.OP { return LDVARUINT32() },
		func() vm.OP { return LDVARINT32() },
		func() vm.OP { return STVARUINT32() },
		func() vm.OP { return STVARINT32() },
		func() vm.OP { return LDMSGADDR() },
		func() vm.OP { return LDMSGADDRQ() },
		func() vm.OP { return PARSEMSGADDR() },
		func() vm.OP { return PARSEMSGADDRQ() },
		func() vm.OP { return REWRITESTDADDR() },
		func() vm.OP { return REWRITESTDADDRQ() },
		func() vm.OP { return REWRITEVARADDR() },
		func() vm.OP { return REWRITEVARADDRQ() },
		func() vm.OP { return LDSTDADDR() },
		func() vm.OP { return LDSTDADDRQ() },
		func() vm.OP { return LDOPTSTDADDR() },
		func() vm.OP { return LDOPTSTDADDRQ() },
		func() vm.OP { return STSTDADDR() },
		func() vm.OP { return STSTDADDRQ() },
		func() vm.OP { return STOPTSTDADDR() },
		func() vm.OP { return STOPTSTDADDRQ() },
		func() vm.OP { return RAWRESERVE() },
		func() vm.OP { return RAWRESERVEX() },
		func() vm.OP { return SETCODE() },
		func() vm.OP { return SETLIBCODE() },
		func() vm.OP { return CHANGELIB() },
		func() vm.OP { return SENDMSG() },
	)
}

type storageStat struct {
	limit uint64
	cells uint64
	bits  uint64
	refs  uint64
	seen  map[cell.Hash]struct{}
	state *vm.State
}

func newStorageStat(limit uint64, state *vm.State) *storageStat {
	return &storageStat{
		limit: limit,
		seen:  map[cell.Hash]struct{}{},
		state: state,
	}
}

func (s *storageStat) addCell(cl *cell.Cell) bool {
	if cl == nil {
		return true
	}
	key := cl.HashKey()
	if _, ok := s.seen[key]; ok {
		return true
	}
	if s.cells >= s.limit {
		return false
	}
	if s.state != nil {
		if err := s.state.Cells.RegisterCellLoadKey(key); err != nil {
			return false
		}
	}
	s.seen[key] = struct{}{}
	s.cells++

	sl := cl.BeginParse()
	s.bits += uint64(sl.BitsLeft())
	s.refs += uint64(sl.RefsNum())
	for i := 0; i < sl.RefsNum(); i++ {
		ref, err := sl.PeekRefCellAt(i)
		if err != nil {
			return false
		}
		if !s.addCell(ref) {
			return false
		}
	}
	return true
}

func (s *storageStat) addSlice(sl *cell.Slice) bool {
	if sl == nil {
		return true
	}
	s.bits += uint64(sl.BitsLeft())
	s.refs += uint64(sl.RefsNum())
	for i := 0; i < sl.RefsNum(); i++ {
		ref, err := sl.PeekRefCellAt(i)
		if err != nil {
			return false
		}
		if !s.addCell(ref) {
			return false
		}
	}
	return true
}

func dataSizeOp(name string, prefix helpers.BitPrefix, mode int) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			bound, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if bound.Sign() < 0 {
				return vmerr.Error(vmerr.CodeRangeCheck, "finite non-negative integer expected")
			}
			limit := uint64((1 << 63) - 1)
			if bound.BitLen() <= 63 {
				limit = bound.Uint64()
			}

			stat := newStorageStat(limit, state)
			ok := true
			if mode&2 != 0 {
				sl, popErr := state.Stack.PopSlice()
				if popErr != nil {
					return popErr
				}
				ok = stat.addSlice(sl)
			} else {
				cl, popErr := state.Stack.PopMaybeCell()
				if popErr != nil {
					return popErr
				}
				ok = stat.addCell(cl)
			}

			if ok {
				if err = pushSmallInt(state, int64(stat.cells)); err != nil {
					return err
				}
				if err = pushSmallInt(state, int64(stat.bits)); err != nil {
					return err
				}
				if err = pushSmallInt(state, int64(stat.refs)); err != nil {
					return err
				}
			} else if mode&1 == 0 {
				return vmerr.Error(vmerr.CodeCellOverflow, "scanned too many cells")
			}
			if mode&1 != 0 {
				return state.Stack.PushBool(ok)
			}
			return nil
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func CDATASIZEQ() *helpers.SimpleOP {
	return dataSizeOp("CDATASIZEQ", helpers.BytesPrefix(0xF9, 0x40), 1)
}
func CDATASIZE() *helpers.SimpleOP {
	return dataSizeOp("CDATASIZE", helpers.BytesPrefix(0xF9, 0x41), 0)
}
func SDATASIZEQ() *helpers.SimpleOP {
	return dataSizeOp("SDATASIZEQ", helpers.BytesPrefix(0xF9, 0x42), 3)
}
func SDATASIZE() *helpers.SimpleOP {
	return dataSizeOp("SDATASIZE", helpers.BytesPrefix(0xF9, 0x43), 2)
}

func loadVarIntegerFromSlice(src *cell.Slice, lenBits uint, signed bool) (*big.Int, *cell.Slice, bool) {
	work := src.Copy()
	ln, err := work.LoadUInt(lenBits)
	if err != nil {
		return nil, src, false
	}
	var val *big.Int
	if signed {
		val, err = work.LoadBigInt(uint(ln) * 8)
		if err != nil {
			return nil, src, false
		}
	} else {
		val, err = work.LoadBigUInt(uint(ln) * 8)
		if err != nil {
			return nil, src, false
		}
	}
	return val, work, true
}

func storeVarInteger(dst *cell.Builder, x *big.Int, lenBits uint, signed, quiet bool) (bool, error) {
	if x == nil {
		return false, vmerr.Error(vmerr.CodeTypeCheck)
	}
	maxLen := 1 << lenBits
	var ln int
	if !signed {
		if x.Sign() < 0 {
			return false, vmerr.Error(vmerr.CodeRangeCheck)
		}
		ln = (x.BitLen() + 7) >> 3
	} else {
		ln = 0
		for ; ln < maxLen; ln++ {
			if ln == 0 {
				if x.Sign() == 0 {
					break
				}
				continue
			}
			bits := uint(ln * 8)
			min := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), bits-1))
			max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits-1), big.NewInt(1))
			if x.Cmp(min) >= 0 && x.Cmp(max) <= 0 {
				break
			}
		}
	}
	if ln >= maxLen {
		return false, vmerr.Error(vmerr.CodeRangeCheck)
	}
	if !dst.CanExtendBy(lenBits+uint(ln*8), 0) {
		if quiet {
			return false, nil
		}
		return false, vmerr.Error(vmerr.CodeCellOverflow, "cannot serialize a variable-length integer")
	}
	if err := dst.StoreUInt(uint64(ln), lenBits); err != nil {
		return false, err
	}
	if signed {
		if err := dst.StoreBigInt(x, uint(ln*8)); err != nil {
			return false, err
		}
	} else {
		if err := dst.StoreBigUInt(x, uint(ln*8)); err != nil {
			return false, err
		}
	}
	return true, nil
}

func loadVarIntOp(name string, prefix helpers.BitPrefix, lenBits uint, signed bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			src, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			val, rest, ok := loadVarIntegerFromSlice(src, lenBits, signed)
			if !ok {
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			if err = state.Stack.PushInt(val); err != nil {
				return err
			}
			return state.Stack.PushSlice(rest)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func storeVarIntOp(name string, prefix helpers.BitPrefix, lenBits uint, signed bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			x, err := state.Stack.PopInt()
			if err != nil {
				return err
			}
			dst, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			ok, err := storeVarInteger(dst, x, lenBits, signed, false)
			if err != nil {
				return err
			}
			if !ok {
				return vmerr.Error(vmerr.CodeCellOverflow)
			}
			return state.Stack.PushBuilder(dst)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func LDVARINT16() *helpers.SimpleOP {
	return loadVarIntOp("LDVARINT16", helpers.BytesPrefix(0xFA, 0x01), 4, true)
}
func STVARINT16() *helpers.SimpleOP {
	return storeVarIntOp("STVARINT16", helpers.BytesPrefix(0xFA, 0x03), 4, true)
}
func LDVARUINT32() *helpers.SimpleOP {
	return loadVarIntOp("LDVARUINT32", helpers.BytesPrefix(0xFA, 0x04), 5, false)
}
func LDVARINT32() *helpers.SimpleOP {
	return loadVarIntOp("LDVARINT32", helpers.BytesPrefix(0xFA, 0x05), 5, true)
}
func STVARUINT32() *helpers.SimpleOP {
	return storeVarIntOp("STVARUINT32", helpers.BytesPrefix(0xFA, 0x06), 5, false)
}
func STVARINT32() *helpers.SimpleOP {
	return storeVarIntOp("STVARINT32", helpers.BytesPrefix(0xFA, 0x07), 5, true)
}

type parsedMsgAddress struct {
	Kind      int
	Anycast   *cell.Slice
	Workchain int32
	Addr      *cell.Slice
}

func loadSliceBits(sl *cell.Slice, bits uint) (*cell.Slice, error) {
	data, err := sl.LoadSlice(bits)
	if err != nil {
		return nil, err
	}
	return cell.BeginCell().MustStoreSlice(data, bits).ToSlice(), nil
}

func parseMaybeAnycast(sl *cell.Slice) (*cell.Slice, bool) {
	has, err := sl.LoadBoolBit()
	if err != nil {
		return nil, false
	}
	if !has {
		return nil, true
	}
	return nil, false
}

func parseMessageAddress(src *cell.Slice) (*parsedMsgAddress, *cell.Slice, bool) {
	work := src.Copy()
	typ, err := work.LoadUInt(2)
	if err != nil {
		return nil, src, false
	}
	switch typ {
	case 0:
		return &parsedMsgAddress{Kind: 0}, work, true
	case 1:
		ln, err := work.LoadUInt(9)
		if err != nil {
			return nil, src, false
		}
		addrBits, err := loadSliceBits(work, uint(ln))
		if err != nil {
			return nil, src, false
		}
		return &parsedMsgAddress{Kind: 1, Addr: addrBits}, work, true
	case 2:
		pfx, ok := parseMaybeAnycast(work)
		if !ok {
			return nil, src, false
		}
		wc, err := work.LoadInt(8)
		if err != nil {
			return nil, src, false
		}
		addrBits, err := loadSliceBits(work, 256)
		if err != nil {
			return nil, src, false
		}
		return &parsedMsgAddress{Kind: 2, Anycast: pfx, Workchain: int32(wc), Addr: addrBits}, work, true
	case 3:
		return nil, src, false
	default:
		return nil, src, false
	}
}

func consumedPrefixSlice(src, rest *cell.Slice) (*cell.Slice, error) {
	consumedBits := src.BitsLeft() - rest.BitsLeft()
	consumedRefs := src.RefsNum() - rest.RefsNum()
	return src.Subslice(0, 0, consumedBits, consumedRefs)
}

func pushParsedMessageTuple(state *vm.State, addr *parsedMsgAddress) error {
	var tup *tuple.Tuple
	switch addr.Kind {
	case 0:
		tup = tuple.NewTuple(big.NewInt(0))
	case 1:
		tup = tuple.NewTuple(big.NewInt(1), addr.Addr)
	case 2, 3:
		var anycast any
		if addr.Anycast != nil {
			anycast = addr.Anycast
		}
		tup = tuple.NewTuple(
			big.NewInt(int64(addr.Kind)),
			anycast,
			big.NewInt(int64(addr.Workchain)),
			addr.Addr,
		)
	default:
		return vmerr.Error(vmerr.CodeCellUnderflow, "cannot parse a MsgAddress")
	}
	return state.Stack.PushTuple(*tup)
}

func rewriteAddrBits(addrBits, prefix *cell.Slice) (*cell.Slice, bool) {
	if prefix == nil || prefix.BitsLeft() == 0 {
		return addrBits.Copy(), true
	}
	if prefix.BitsLeft() > addrBits.BitsLeft() {
		return nil, false
	}
	b := cell.BeginCell()
	pfxBytes, err := prefix.PreloadSlice(prefix.BitsLeft())
	if err != nil {
		return nil, false
	}
	if err = b.StoreSlice(pfxBytes, uint(prefix.BitsLeft())); err != nil {
		return nil, false
	}
	rest := addrBits.Copy()
	if !rest.SkipFirst(uint(prefix.BitsLeft()), 0) {
		return nil, false
	}
	restBytes, err := rest.PreloadSlice(rest.BitsLeft())
	if err != nil {
		return nil, false
	}
	if err = b.StoreSlice(restBytes, uint(rest.BitsLeft())); err != nil {
		return nil, false
	}
	return b.ToSlice(), true
}

func loadMessageAddrOp(name string, prefix helpers.BitPrefix, stdOnly, quiet, optStd bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			src, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if optStd {
				if src.BitsLeft() < 2 {
					if quiet {
						if err = state.Stack.PushSlice(src); err != nil {
							return err
						}
						return state.Stack.PushBool(false)
					}
					return vmerr.Error(vmerr.CodeCellUnderflow)
				}
				if tag, _ := src.PreloadUInt(2); tag == 0 {
					rest := src.Copy()
					if err = rest.Advance(2); err != nil {
						return err
					}
					if err = state.Stack.PushAny(nil); err != nil {
						return err
					}
					if err = state.Stack.PushSlice(rest); err != nil {
						return err
					}
					if quiet {
						return state.Stack.PushBool(true)
					}
					return nil
				}
			}
			addr, rest, ok := parseMessageAddress(src)
			if ok && stdOnly && addr.Kind != 2 {
				ok = false
			}
			if !ok {
				if quiet {
					if err = state.Stack.PushSlice(src); err != nil {
						return err
					}
					return state.Stack.PushBool(false)
				}
				return vmerr.Error(vmerr.CodeCellUnderflow)
			}
			addrSlice, err := consumedPrefixSlice(src, rest)
			if err != nil {
				return err
			}
			if err = state.Stack.PushSlice(addrSlice); err != nil {
				return err
			}
			if err = state.Stack.PushSlice(rest); err != nil {
				return err
			}
			if quiet {
				return state.Stack.PushBool(true)
			}
			return nil
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func LDMSGADDR() *helpers.SimpleOP {
	return loadMessageAddrOp("LDMSGADDR", helpers.BytesPrefix(0xFA, 0x40), false, false, false)
}
func LDMSGADDRQ() *helpers.SimpleOP {
	return loadMessageAddrOp("LDMSGADDRQ", helpers.BytesPrefix(0xFA, 0x41), false, true, false)
}
func LDSTDADDR() *helpers.SimpleOP {
	return loadMessageAddrOp("LDSTDADDR", helpers.BytesPrefix(0xFA, 0x48), true, false, false)
}
func LDSTDADDRQ() *helpers.SimpleOP {
	return loadMessageAddrOp("LDSTDADDRQ", helpers.BytesPrefix(0xFA, 0x49), true, true, false)
}
func LDOPTSTDADDR() *helpers.SimpleOP {
	return loadMessageAddrOp("LDOPTSTDADDR", helpers.BytesPrefix(0xFA, 0x50), true, false, true)
}
func LDOPTSTDADDRQ() *helpers.SimpleOP {
	return loadMessageAddrOp("LDOPTSTDADDRQ", helpers.BytesPrefix(0xFA, 0x51), true, true, true)
}

func parseMsgAddrOp(name string, prefix helpers.BitPrefix, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			src, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			addr, rest, ok := parseMessageAddress(src)
			if !ok || rest.BitsLeft() != 0 || rest.RefsNum() != 0 {
				if quiet {
					return state.Stack.PushBool(false)
				}
				return vmerr.Error(vmerr.CodeCellUnderflow, "cannot parse a MsgAddress")
			}
			if err = pushParsedMessageTuple(state, addr); err != nil {
				return err
			}
			if quiet {
				return state.Stack.PushBool(true)
			}
			return nil
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func PARSEMSGADDR() *helpers.SimpleOP {
	return parseMsgAddrOp("PARSEMSGADDR", helpers.BytesPrefix(0xFA, 0x42), false)
}
func PARSEMSGADDRQ() *helpers.SimpleOP {
	return parseMsgAddrOp("PARSEMSGADDRQ", helpers.BytesPrefix(0xFA, 0x43), true)
}

func rewriteMsgAddrOp(name string, prefix helpers.BitPrefix, allowVar, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			src, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			addr, rest, ok := parseMessageAddress(src)
			if !ok || rest.BitsLeft() != 0 || rest.RefsNum() != 0 || (addr.Kind != 2 && addr.Kind != 3) {
				if quiet {
					return state.Stack.PushBool(false)
				}
				return vmerr.Error(vmerr.CodeCellUnderflow, "cannot parse a MsgAddressInt")
			}
			rewritten, ok := rewriteAddrBits(addr.Addr, addr.Anycast)
			if !ok {
				if quiet {
					return state.Stack.PushBool(false)
				}
				return vmerr.Error(vmerr.CodeCellUnderflow, "cannot rewrite address in a MsgAddressInt")
			}
			if !allowVar {
				if rewritten.BitsLeft() != 256 {
					if quiet {
						return state.Stack.PushBool(false)
					}
					return vmerr.Error(vmerr.CodeCellUnderflow, "MsgAddressInt is not a standard 256-bit address")
				}
				data, err := rewritten.PreloadSlice(rewritten.BitsLeft())
				if err != nil {
					return err
				}
				if err = state.Stack.PushInt(big.NewInt(int64(addr.Workchain))); err != nil {
					return err
				}
				if err = state.Stack.PushInt(new(big.Int).SetBytes(data)); err != nil {
					return err
				}
			} else {
				if err = state.Stack.PushInt(big.NewInt(int64(addr.Workchain))); err != nil {
					return err
				}
				if err = state.Stack.PushSlice(rewritten); err != nil {
					return err
				}
			}
			if quiet {
				return state.Stack.PushBool(true)
			}
			return nil
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func REWRITESTDADDR() *helpers.SimpleOP {
	return rewriteMsgAddrOp("REWRITESTDADDR", helpers.BytesPrefix(0xFA, 0x44), false, false)
}
func REWRITESTDADDRQ() *helpers.SimpleOP {
	return rewriteMsgAddrOp("REWRITESTDADDRQ", helpers.BytesPrefix(0xFA, 0x45), false, true)
}
func REWRITEVARADDR() *helpers.SimpleOP {
	return rewriteMsgAddrOp("REWRITEVARADDR", helpers.BytesPrefix(0xFA, 0x46), true, false)
}
func REWRITEVARADDRQ() *helpers.SimpleOP {
	return rewriteMsgAddrOp("REWRITEVARADDRQ", helpers.BytesPrefix(0xFA, 0x47), true, true)
}

func isValidStdMsgAddr(sl *cell.Slice) bool {
	addr, rest, ok := parseMessageAddress(sl)
	return ok && addr.Kind == 2 && rest.BitsLeft() == 0 && rest.RefsNum() == 0 && addr.Addr.BitsLeft() == 256
}

func storeStdAddrOp(name string, prefix helpers.BitPrefix, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			addrSlice, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			isStd := isValidStdMsgAddr(addrSlice)
			if !builder.CanExtendBy(uint(addrSlice.BitsLeft()), uint(addrSlice.RefsNum())) || !isStd {
				if !quiet {
					if !isStd {
						return vmerr.Error(vmerr.CodeCellOverflow, "not a MsgAddressInt")
					}
					return vmerr.Error(vmerr.CodeCellOverflow)
				}
				if err = state.Stack.PushSlice(addrSlice); err != nil {
					return err
				}
				if err = state.Stack.PushBuilder(builder); err != nil {
					return err
				}
				return state.Stack.PushBool(true)
			}
			data, err := addrSlice.PreloadSlice(addrSlice.BitsLeft())
			if err != nil {
				return err
			}
			if err = builder.StoreSlice(data, uint(addrSlice.BitsLeft())); err != nil {
				return err
			}
			if quiet {
				if err = state.Stack.PushBuilder(builder); err != nil {
					return err
				}
				return state.Stack.PushBool(false)
			}
			return state.Stack.PushBuilder(builder)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func STSTDADDR() *helpers.SimpleOP {
	return storeStdAddrOp("STSTDADDR", helpers.BytesPrefix(0xFA, 0x52), false)
}
func STSTDADDRQ() *helpers.SimpleOP {
	return storeStdAddrOp("STSTDADDRQ", helpers.BytesPrefix(0xFA, 0x53), true)
}

func storeOptStdAddrOp(name string, prefix helpers.BitPrefix, quiet bool) *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			raw, err := state.Stack.PopAny()
			if err != nil {
				return err
			}
			if raw == nil {
				if !builder.CanExtendBy(2, 0) {
					if !quiet {
						return vmerr.Error(vmerr.CodeCellOverflow)
					}
					if err = state.Stack.PushAny(nil); err != nil {
						return err
					}
					if err = state.Stack.PushBuilder(builder); err != nil {
						return err
					}
					return state.Stack.PushBool(true)
				}
				if err = builder.StoreUInt(0, 2); err != nil {
					return err
				}
				if quiet {
					if err = state.Stack.PushBuilder(builder); err != nil {
						return err
					}
					return state.Stack.PushBool(false)
				}
				return state.Stack.PushBuilder(builder)
			}
			addrSlice, ok := raw.(*cell.Slice)
			if !ok {
				if !quiet {
					return vmerr.Error(vmerr.CodeTypeCheck, "not a cell slice")
				}
				// Reference TVM restores a null slice here, not the original raw stack entry.
				if err = state.Stack.PushAny(nil); err != nil {
					return err
				}
				if err = state.Stack.PushBuilder(builder); err != nil {
					return err
				}
				return state.Stack.PushBool(true)
			}
			isStd := isValidStdMsgAddr(addrSlice)
			if !builder.CanExtendBy(uint(addrSlice.BitsLeft()), uint(addrSlice.RefsNum())) || !isStd {
				if !quiet {
					if !isStd {
						return vmerr.Error(vmerr.CodeCellOverflow, "not a MsgAddressInt")
					}
					return vmerr.Error(vmerr.CodeCellOverflow)
				}
				if err = state.Stack.PushSlice(addrSlice); err != nil {
					return err
				}
				if err = state.Stack.PushBuilder(builder); err != nil {
					return err
				}
				return state.Stack.PushBool(true)
			}
			data, err := addrSlice.PreloadSlice(addrSlice.BitsLeft())
			if err != nil {
				return err
			}
			if err = builder.StoreSlice(data, uint(addrSlice.BitsLeft())); err != nil {
				return err
			}
			if quiet {
				if err = state.Stack.PushBuilder(builder); err != nil {
					return err
				}
				return state.Stack.PushBool(false)
			}
			return state.Stack.PushBuilder(builder)
		},
		Name:      name,
		BitPrefix: prefix,
	}
}

func STOPTSTDADDR() *helpers.SimpleOP {
	return storeOptStdAddrOp("STOPTSTDADDR", helpers.BytesPrefix(0xFA, 0x54), false)
}
func STOPTSTDADDRQ() *helpers.SimpleOP {
	return storeOptStdAddrOp("STOPTSTDADDRQ", helpers.BytesPrefix(0xFA, 0x55), true)
}

func installAction(state *vm.State, build func(*cell.Builder) error) error {
	b := cell.BeginCell().MustStoreRef(state.Reg.D[1])
	if err := build(b); err != nil {
		if errors.Is(err, cell.ErrNotFit1023) || errors.Is(err, cell.ErrTooMuchRefs) || errors.Is(err, cell.ErrRefCannotBeNil) {
			return vmerr.Error(vmerr.CodeCellOverflow, err.Error())
		}
		return err
	}
	action := b.EndCell()
	if err := state.Cells.RegisterCellCreate(); err != nil {
		return err
	}
	state.Reg.D[1] = action
	return nil
}

func RAWRESERVE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			f, err := state.Stack.PopIntRange(0, 31)
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if x.Sign() < 0 {
				return vmerr.Error(vmerr.CodeRangeCheck, "amount of nanograms must be non-negative")
			}
			return installAction(state, func(b *cell.Builder) error {
				if err = b.StoreUInt(0x36e6b809, 32); err != nil {
					return err
				}
				if err = b.StoreUInt(f.Uint64(), 8); err != nil {
					return err
				}
				if err = b.StoreBigCoins(x); err != nil {
					return err
				}
				return b.StoreMaybeRef(nil)
			})
		},
		Name:      "RAWRESERVE",
		BitPrefix: helpers.BytesPrefix(0xFB, 0x02),
	}
}

func RAWRESERVEX() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			f, err := state.Stack.PopIntRange(0, 31)
			if err != nil {
				return err
			}
			y, err := state.Stack.PopMaybeCell()
			if err != nil {
				return err
			}
			x, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if x.Sign() < 0 {
				return vmerr.Error(vmerr.CodeRangeCheck, "amount of nanograms must be non-negative")
			}
			return installAction(state, func(b *cell.Builder) error {
				if err = b.StoreUInt(0x36e6b809, 32); err != nil {
					return err
				}
				if err = b.StoreUInt(f.Uint64(), 8); err != nil {
					return err
				}
				if err = b.StoreBigCoins(x); err != nil {
					return err
				}
				return b.StoreMaybeRef(y)
			})
		},
		Name:      "RAWRESERVEX",
		BitPrefix: helpers.BytesPrefix(0xFB, 0x03),
	}
}

func SETCODE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			code, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			return installAction(state, func(b *cell.Builder) error {
				if err = b.StoreUInt(0xAD4DE08E, 32); err != nil {
					return err
				}
				return b.StoreRef(code)
			})
		},
		Name:      "SETCODE",
		BitPrefix: helpers.BytesPrefix(0xFB, 0x04),
	}
}

func popLibMode(state *vm.State) (*big.Int, error) {
	mode, err := state.Stack.PopIntRange(0, 31)
	if err != nil {
		return nil, err
	}
	if (mode.Int64() &^ 16) > 2 {
		return nil, vmerr.Error(vmerr.CodeRangeCheck)
	}
	return mode, nil
}

func SETLIBCODE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			mode, err := popLibMode(state)
			if err != nil {
				return err
			}
			code, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			return installAction(state, func(b *cell.Builder) error {
				if err = b.StoreUInt(0x26FA1DD4, 32); err != nil {
					return err
				}
				if err = b.StoreUInt(uint64(mode.Int64()*2+1), 8); err != nil {
					return err
				}
				return b.StoreRef(code)
			})
		},
		Name:      "SETLIBCODE",
		BitPrefix: helpers.BytesPrefix(0xFB, 0x06),
	}
}

func CHANGELIB() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			mode, err := popLibMode(state)
			if err != nil {
				return err
			}
			hash, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if hash.Sign() < 0 || hash.BitLen() > 256 {
				return vmerr.Error(vmerr.CodeRangeCheck, "library hash must be non-negative")
			}
			return installAction(state, func(b *cell.Builder) error {
				if err = b.StoreUInt(0x26FA1DD4, 32); err != nil {
					return err
				}
				if err = b.StoreUInt(uint64(mode.Int64()*2), 8); err != nil {
					return err
				}
				return b.StoreBigUInt(hash, 256)
			})
		},
		Name:      "CHANGELIB",
		BitPrefix: helpers.BytesPrefix(0xFB, 0x07),
	}
}

func addressFromSlice(sl *cell.Slice) (*address.Address, error) {
	if sl == nil {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	}
	return sl.Copy().LoadAddr()
}

func getMyAddr(state *vm.State) (*address.Address, error) {
	v, err := state.GetParam(8)
	if err != nil {
		return nil, err
	}
	sl, ok := v.(*cell.Slice)
	if !ok {
		return nil, vmerr.Error(vmerr.CodeTypeCheck, "invalid param MYADDR")
	}
	return addressFromSlice(sl)
}

func getSizeLimitsMaxMsgCells(state *vm.State) (uint64, error) {
	sl, err := unpackedConfigSlice(state, 6)
	if err != nil {
		return 0, err
	}
	if sl == nil {
		return 1 << 13, nil
	}
	tag, err := sl.LoadUInt(8)
	if err != nil {
		return 0, err
	}
	switch tag {
	case 0x01:
		_, err = sl.LoadUInt(32)
		if err != nil {
			return 0, err
		}
		maxCells, err := sl.LoadUInt(32)
		if err != nil {
			return 0, err
		}
		return maxCells, nil
	case 0x02:
		_, err = sl.LoadUInt(32)
		if err != nil {
			return 0, err
		}
		maxCells, err := sl.LoadUInt(32)
		if err != nil {
			return 0, err
		}
		return maxCells, nil
	default:
		return 0, vmerr.Error(vmerr.CodeCellUnderflow, "configuration parameter 43 is invalid")
	}
}

func addMessageTailStorage(stat *storageStat, msgCell *cell.Cell, skipFirstRefs int) bool {
	if stat.state != nil {
		if err := stat.state.Cells.RegisterCellLoad(msgCell); err != nil {
			return false
		}
	}
	root := msgCell.BeginParse()
	if err := root.Advance(root.BitsLeft()); err != nil {
		return false
	}
	if skipFirstRefs > 0 {
		if err := root.AdvanceExt(0, skipFirstRefs); err != nil {
			return false
		}
	}
	return stat.addSlice(root)
}

func SENDMSG() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			modeVal, err := state.Stack.PopIntRange(0, 2047)
			if err != nil {
				return err
			}
			send := modeVal.Int64()&1024 == 0
			mode := modeVal.Int64() &^ 1024
			if mode >= 256 {
				return vmerr.Error(vmerr.CodeRangeCheck)
			}
			msgCell, err := state.Stack.PopCell()
			if err != nil {
				return err
			}
			if err = state.Cells.RegisterCellLoad(msgCell); err != nil {
				return err
			}

			var msg tlb.Message
			if err = tlb.LoadFromCell(&msg, msgCell.BeginParse()); err != nil {
				return vmerr.Error(vmerr.CodeUnknown, "invalid message")
			}

			myAddr, err := getMyAddr(state)
			if err != nil {
				return err
			}
			dest := msg.Msg.DestAddr()
			isMasterchain := myAddr != nil && myAddr.Workchain() == -1
			if msg.MsgType == tlb.MsgTypeInternal && dest != nil && dest.Workchain() == -1 {
				isMasterchain = true
			}
			prices, err := getTonMsgPrices(state, isMasterchain)
			if err != nil {
				return err
			}
			maxCells, err := getSizeLimitsMaxMsgCells(state)
			if err != nil {
				return err
			}
			stat := newStorageStat(maxCells, state)
			skipRefs := 0
			if intMsg, ok := msg.Msg.(*tlb.InternalMessage); ok && intMsg.ExtraCurrencies != nil && intMsg.ExtraCurrencies.AsCell() != nil {
				skipRefs = 1
			}
			if !addMessageTailStorage(stat, msgCell, skipRefs) {
				return vmerr.Error(vmerr.CodeCellOverflow, "scanned too many cells")
			}

			fwd := prices.computeForwardFee(stat.cells, stat.bits)
			ihr := big.NewInt(0)
			if intMsg, ok := msg.Msg.(*tlb.InternalMessage); ok {
				userFwd := intMsg.FwdFee.Nano()
				fwd = maxBig(fwd, userFwd)
				ihrDisabled := true
				if !ihrDisabled {
					part := ceilShiftRight(new(big.Int).Mul(fwd, new(big.Int).SetUint64(uint64(prices.IHRFactor))), 16)
					ihr = part
				}
			}
			totalFee := new(big.Int).Add(fwd, ihr)
			if err = state.Stack.PushInt(totalFee); err != nil {
				return err
			}
			if !send {
				return nil
			}
			return installAction(state, func(b *cell.Builder) error {
				if err = b.StoreUInt(0x0EC3C86D, 32); err != nil {
					return err
				}
				if err = b.StoreUInt(uint64(mode), 8); err != nil {
					return err
				}
				return b.StoreRef(msgCell)
			})
		},
		Name:      "SENDMSG",
		BitPrefix: helpers.BytesPrefix(0xFB, 0x08),
	}
}
