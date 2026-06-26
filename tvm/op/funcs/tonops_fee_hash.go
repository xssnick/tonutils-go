package funcs

import (
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"math/big"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/sha3"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func init() {
	vm.List = append(vm.List,
		func() vm.OP { return GETGASFEE() },
		func() vm.OP { return GETSTORAGEFEE() },
		func() vm.OP { return GETFORWARDFEE() },
		func() vm.OP { return GETORIGINALFWDFEE() },
		func() vm.OP { return GETGASFEESIMPLE() },
		func() vm.OP { return GETFORWARDFEESIMPLE() },
		func() vm.OP { return GETEXTRABALANCE() },
		func() vm.OP { return SHA256U() },
		func() vm.OP { return HASHEXT(0) },
		func() vm.OP { return HASHBU() },
	)
}

func unpackedConfigSlice(state *vm.State, idx int) (*cell.Slice, error) {
	cfg, err := state.GetUnpackedConfigTuple()
	if err != nil {
		if code, ok := vmerr.ErrorCode(err); ok && code == vmerr.CodeRangeCheck {
			return nil, nil
		}
		return nil, err
	}
	v, err := cfg.Index(idx)
	if err != nil {
		if code, ok := vmerr.ErrorCode(err); ok && code == vmerr.CodeRangeCheck {
			return nil, nil
		}
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	sl, ok := v.(*cell.Slice)
	if !ok {
		return nil, vmerr.Error(vmerr.CodeTypeCheck)
	}
	return sl.Copy(), nil
}

func parseTonStoragePrices(sl *cell.Slice) (*tlb.ConfigStoragePrices, error) {
	if sl == nil {
		return nil, nil
	}

	var prices tlb.ConfigStoragePrices
	if err := tlb.LoadFromCell(&prices, sl.Copy()); err != nil {
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, err.Error())
	}
	return &prices, nil
}

func parseTonGasPrices(sl *cell.Slice) (*tlb.ConfigGasLimitsPrices, error) {
	if sl == nil {
		return nil, vmerr.Error(vmerr.CodeTypeCheck, "intermediate value is not a slice")
	}

	var prices tlb.ConfigGasLimitsPrices
	if err := tlb.LoadFromCell(&prices, sl.Copy()); err != nil {
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, err.Error())
	}
	return &prices, nil
}

func parseTonMsgPrices(sl *cell.Slice) (*tlb.ConfigMsgForwardPrices, error) {
	if sl == nil {
		return nil, vmerr.Error(vmerr.CodeTypeCheck, "intermediate value is not a slice")
	}

	var prices tlb.ConfigMsgForwardPrices
	if err := tlb.LoadFromCell(&prices, sl.Copy()); err != nil {
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, err.Error())
	}
	return &prices, nil
}

func getTonGasPrices(state *vm.State, isMasterchain bool) (*tlb.ConfigGasLimitsPrices, error) {
	idx := 3
	if isMasterchain {
		idx = 2
	}
	sl, err := unpackedConfigSlice(state, idx)
	if err != nil {
		return nil, err
	}
	return parseTonGasPrices(sl)
}

func getTonMsgPrices(state *vm.State, isMasterchain bool) (*tlb.ConfigMsgForwardPrices, error) {
	idx := 5
	if isMasterchain {
		idx = 4
	}
	sl, err := unpackedConfigSlice(state, idx)
	if err != nil {
		return nil, err
	}
	return parseTonMsgPrices(sl)
}

func getTonStoragePrices(state *vm.State) (*tlb.ConfigStoragePrices, error) {
	sl, err := unpackedConfigSlice(state, 0)
	if err != nil {
		return nil, err
	}
	return parseTonStoragePrices(sl)
}

func ceilShiftRight(x *big.Int, bits uint) *big.Int {
	if x.Sign() == 0 {
		return new(big.Int)
	}
	if x.Sign() > 0 {
		q := new(big.Int).Rsh(x, bits)
		for i := uint(0); i < bits; i++ {
			if x.Bit(int(i)) != 0 {
				q.Add(q, funcsBigIntOne)
				break
			}
		}
		return q
	}

	divisor := new(big.Int).Lsh(funcsBigIntOne, bits)
	return new(big.Int).Quo(x, divisor)
}

func mulUint64(x, y uint64) *big.Int {
	if x == 0 || y == 0 {
		return new(big.Int)
	}
	z := new(big.Int).SetUint64(x)
	return z.Mul(z, new(big.Int).SetUint64(y))
}

func mulBigUint64(x *big.Int, y uint64) *big.Int {
	if x.Sign() == 0 || y == 0 {
		return new(big.Int)
	}
	z := new(big.Int).Set(x)
	return z.Mul(z, new(big.Int).SetUint64(y))
}

func maxBig(x, y *big.Int) *big.Int {
	if x.Cmp(y) >= 0 {
		return x
	}
	return y
}

func popUint64NonNegative(st *vm.Stack) (uint64, error) {
	v, err := st.PopInt()
	if err != nil {
		return 0, err
	}
	if v == nil || v.Sign() < 0 || v.BitLen() > 63 {
		return 0, vmerr.Error(vmerr.CodeRangeCheck, "finite non-negative integer expected")
	}
	return v.Uint64(), nil
}

type getExtraBalanceCheapTrace struct {
	state     *vm.State
	remaining int64
	err       error
	trace     *cell.Trace
}

func newGetExtraBalanceCheapTrace(state *vm.State) *getExtraBalanceCheapTrace {
	return &getExtraBalanceCheapTrace{
		state:     state,
		remaining: vm.GetExtraBalanceCheapMaxGas,
	}
}

func (o *getExtraBalanceCheapTrace) Trace() *cell.Trace {
	if o.trace == nil {
		o.trace = cell.NewTrace(cell.TraceHooks{
			OnLoad: func(c *cell.Cell) {
				o.onCellLoad(c.HashKey())
			},
			OnChild: func(int) *cell.Trace {
				return o.Trace()
			},
			PendingError: o.PendingError,
		})
	}
	return o.trace
}

func (o *getExtraBalanceCheapTrace) onCellLoad(hash cell.Hash) {
	if o.err != nil {
		return
	}

	price := int64(vm.CellReloadGasPrice)
	if o.state.RegisterCellLoadFreeKey(hash) {
		price = vm.CellLoadGasPrice
	}

	paid := price
	if paid > o.remaining {
		paid = o.remaining
	}
	if paid > 0 {
		if err := o.state.ConsumeGas(paid); err != nil {
			o.err = err
			return
		}
		o.remaining -= paid
	}
	if free := price - paid; free > 0 {
		o.state.ConsumeFreeGas(free)
	}
}

func (o *getExtraBalanceCheapTrace) PendingError() error {
	return o.err
}

func GETGASFEE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.GlobalVersion >= 9 && state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			isMasterchain, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			gas, err := popUint64NonNegative(state.Stack)
			if err != nil {
				return err
			}
			prices, err := getTonGasPrices(state, isMasterchain)
			if err != nil {
				return err
			}
			return state.Stack.PushInt(prices.ComputeGasPrice(gas))
		},
		Name:       "GETGASFEE",
		BitPrefix:  helpers.BytesPrefix(0xF8, 0x36),
		MinVersion: 6,
	}
}

func GETSTORAGEFEE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.GlobalVersion >= 9 && state.Stack.Len() < 4 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			isMasterchain, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			delta, err := popUint64NonNegative(state.Stack)
			if err != nil {
				return err
			}
			bits, err := popUint64NonNegative(state.Stack)
			if err != nil {
				return err
			}
			cellsCnt, err := popUint64NonNegative(state.Stack)
			if err != nil {
				return err
			}
			prices, err := getTonStoragePrices(state)
			if err != nil {
				return err
			}
			if prices == nil {
				return state.Stack.PushSmallInt(0)
			}
			return state.Stack.PushInt(prices.ComputeStorageFee(isMasterchain, delta, bits, cellsCnt))
		},
		Name:       "GETSTORAGEFEE",
		BitPrefix:  helpers.BytesPrefix(0xF8, 0x37),
		MinVersion: 6,
	}
}

func GETFORWARDFEE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.GlobalVersion >= 9 && state.Stack.Len() < 3 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			isMasterchain, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			bits, err := popUint64NonNegative(state.Stack)
			if err != nil {
				return err
			}
			cellsCnt, err := popUint64NonNegative(state.Stack)
			if err != nil {
				return err
			}
			prices, err := getTonMsgPrices(state, isMasterchain)
			if err != nil {
				return err
			}
			return state.Stack.PushInt(prices.ComputeForwardFee(cellsCnt, bits))
		},
		Name:       "GETFORWARDFEE",
		BitPrefix:  helpers.BytesPrefix(0xF8, 0x38),
		MinVersion: 6,
	}
}

func GETORIGINALFWDFEE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.GlobalVersion >= 9 && state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			isMasterchain, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			fwdFee, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if fwdFee.Sign() < 0 {
				return vmerr.Error(vmerr.CodeRangeCheck, "fwd_fee is negative")
			}
			prices, err := getTonMsgPrices(state, isMasterchain)
			if err != nil {
				return err
			}
			num := new(big.Int).Lsh(fwdFee, 16)
			den := new(big.Int).SetUint64((1 << 16) - uint64(prices.FirstFrac))
			return state.Stack.PushOwnedInt(num.Div(num, den))
		},
		Name:       "GETORIGINALFWDFEE",
		BitPrefix:  helpers.BytesPrefix(0xF8, 0x3A),
		MinVersion: 6,
	}
}

func GETGASFEESIMPLE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.GlobalVersion >= 9 && state.Stack.Len() < 2 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			isMasterchain, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			gas, err := popUint64NonNegative(state.Stack)
			if err != nil {
				return err
			}
			prices, err := getTonGasPrices(state, isMasterchain)
			if err != nil {
				return err
			}
			total := ceilShiftRight(mulUint64(prices.GasPrice, gas), 16)
			return state.Stack.PushOwnedInt(total)
		},
		Name:       "GETGASFEESIMPLE",
		BitPrefix:  helpers.BytesPrefix(0xF8, 0x3B),
		MinVersion: 6,
	}
}

func GETFORWARDFEESIMPLE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			if state.GlobalVersion >= 9 && state.Stack.Len() < 3 {
				return vmerr.Error(vmerr.CodeStackUnderflow)
			}
			isMasterchain, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			bits, err := popUint64NonNegative(state.Stack)
			if err != nil {
				return err
			}
			cellsCnt, err := popUint64NonNegative(state.Stack)
			if err != nil {
				return err
			}
			prices, err := getTonMsgPrices(state, isMasterchain)
			if err != nil {
				return err
			}
			part := mulUint64(prices.BitPrice, bits)
			part.Add(part, mulUint64(prices.CellPrice, cellsCnt))
			return state.Stack.PushOwnedInt(ceilShiftRight(part, 16))
		},
		Name:       "GETFORWARDFEESIMPLE",
		BitPrefix:  helpers.BytesPrefix(0xF8, 0x3C),
		MinVersion: 6,
	}
}

func GETEXTRABALANCE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			id, err := state.Stack.PopIntRangeInt64(0, (1<<32)-1)
			if err != nil {
				return err
			}

			balanceAny, err := state.GetParam(7)
			if err != nil {
				return err
			}
			balance, ok := balanceAny.(tuple.Tuple)
			if !ok || balance.Len() < 2 {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}

			dictRootAny, err := balance.Index(1)
			if err != nil {
				return err
			}
			var dictRoot *cell.Cell
			if dictRootAny != nil {
				var ok bool
				dictRoot, ok = dictRootAny.(*cell.Cell)
				if !ok {
					return vmerr.Error(vmerr.CodeTypeCheck)
				}
			}
			cheap := state.RegisterGetExtraBalanceCall()
			if dictRoot == nil {
				return pushSmallInt(state, 0)
			}

			key := cell.BeginCell().MustStoreUInt(uint64(id), 32).EndCell()
			trace := state.Cells.Trace()
			var cheapTrace *getExtraBalanceCheapTrace
			if cheap {
				cheapTrace = newGetExtraBalanceCheapTrace(state)
				trace = cheapTrace.Trace()
			}

			value, err := dictRoot.AsDict(32).SetTrace(trace).LoadValue(key)
			if cheapTrace != nil {
				if gasErr := cheapTrace.PendingError(); gasErr != nil {
					return gasErr
				}
			}
			if gasErr := state.CheckGas(); gasErr != nil {
				return gasErr
			}
			if err != nil {
				if errors.Is(err, cell.ErrNoSuchKeyInDict) {
					return pushSmallInt(state, 0)
				}
				return vmerr.Error(vmerr.CodeCellUnderflow, err.Error())
			}

			amount, err := value.LoadVarUInt(32)
			if err != nil {
				return vmerr.Error(vmerr.CodeCellUnderflow, err.Error())
			}
			return state.Stack.PushInt(amount)
		},
		Name:       "GETEXTRABALANCE",
		BitPrefix:  helpers.BytesPrefix(0xF8, 0x80),
		MinVersion: 10,
	}
}

func SHA256U() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			sl, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if sl.BitsLeft()%8 != 0 {
				return vmerr.Error(vmerr.CodeCellUnderflow, "slice does not consist of a whole number of bytes")
			}
			data, err := sl.PreloadSlice(sl.BitsLeft())
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			return state.Stack.PushOwnedInt(new(big.Int).SetBytes(sum[:]))
		},
		Name:      "SHA256U",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x02),
	}
}

type hashExtHasher interface {
	Append([]byte) error
	Finish() []byte
	BytesPerGasUnit() int64
}

type singleHashHasher struct {
	appendFn        func([]byte) error
	finishFn        func() []byte
	bytesPerGasUnit int64
}

func (h *singleHashHasher) Append(data []byte) error { return h.appendFn(data) }
func (h *singleHashHasher) Finish() []byte           { return h.finishFn() }
func (h *singleHashHasher) BytesPerGasUnit() int64   { return h.bytesPerGasUnit }

func newHashExtHasher(hashID int) (hashExtHasher, error) {
	switch hashID {
	case 0:
		h := sha256.New()
		return &singleHashHasher{
			appendFn:        func(data []byte) error { _, err := h.Write(data); return err },
			finishFn:        func() []byte { return h.Sum(nil) },
			bytesPerGasUnit: 33,
		}, nil
	case 1:
		h := sha512.New()
		return &singleHashHasher{
			appendFn:        func(data []byte) error { _, err := h.Write(data); return err },
			finishFn:        func() []byte { return h.Sum(nil) },
			bytesPerGasUnit: 16,
		}, nil
	case 2:
		h, err := blake2b.New512(nil)
		if err != nil {
			return nil, err
		}
		return &singleHashHasher{
			appendFn:        func(data []byte) error { _, err := h.Write(data); return err },
			finishFn:        func() []byte { return h.Sum(nil) },
			bytesPerGasUnit: 19,
		}, nil
	case 3:
		h := sha3.NewLegacyKeccak256()
		return &singleHashHasher{
			appendFn:        func(data []byte) error { _, err := h.Write(data); return err },
			finishFn:        func() []byte { return h.Sum(nil) },
			bytesPerGasUnit: 11,
		}, nil
	case 4:
		h := sha3.NewLegacyKeccak512()
		return &singleHashHasher{
			appendFn:        func(data []byte) error { _, err := h.Write(data); return err },
			finishFn:        func() []byte { return h.Sum(nil) },
			bytesPerGasUnit: 6,
		}, nil
	default:
		return nil, vmerr.Error(vmerr.CodeRangeCheck, "unknown hashext hash id")
	}
}

func appendBits(dst []byte, dstBits *int, src []byte, bits int) []byte {
	if *dstBits%8 == 0 && bits%8 == 0 {
		dst = append(dst, src[:bits/8]...)
		*dstBits += bits
		return dst
	}

	for i := 0; i < bits; i++ {
		srcBit := (src[i/8] >> (7 - uint(i%8))) & 1
		if *dstBits%8 == 0 {
			dst = append(dst, 0)
		}
		if srcBit != 0 {
			dst[*dstBits/8] |= 1 << (7 - uint(*dstBits%8))
		}
		*dstBits++
	}
	return dst
}

func valueBitsForHashExt(val any) ([]byte, int, error) {
	switch x := val.(type) {
	case *cell.Slice:
		data, err := x.PreloadSlice(x.BitsLeft())
		if err != nil {
			return nil, 0, err
		}
		return data, int(x.BitsLeft()), nil
	case *cell.Builder:
		sl := x.WithoutTrace().ToSlice()
		data, err := sl.PreloadSlice(sl.BitsLeft())
		if err != nil {
			return nil, 0, err
		}
		return data, int(sl.BitsLeft()), nil
	default:
		return nil, 0, vmerr.Error(vmerr.CodeTypeCheck, "expected slice or builder")
	}
}

func HASHEXT(args uint16) *helpers.AdvancedOP {
	return &helpers.AdvancedOP{
		NameSerializer: func() string {
			rev := (args>>8)&1 != 0
			appendMode := (args>>9)&1 != 0
			hashID := args & 0xFF
			rendered := int(hashID)
			if hashID == 255 {
				rendered = -1
			}
			name := "HASHEXT"
			if appendMode {
				name += "A"
			}
			if rev {
				name += "R"
			}
			return fmt.Sprintf("%s %d", name, rendered)
		},
		BitPrefix:     helpers.UIntPrefix(0x3E41, 14),
		FixedSizeBits: 10,
		MinVersion:    4,
		SerializeSuffix: func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(args), 10)
		},
		DeserializeSuffix: func(code *cell.Slice) error {
			v, err := code.LoadUInt(10)
			if err != nil {
				return err
			}
			args = uint16(v)
			return nil
		},
		Action: func(state *vm.State) error {
			rev := (args>>8)&1 != 0
			appendMode := (args>>9)&1 != 0
			hashID := int(args & 0xFF)
			if hashID == 255 {
				if state.GlobalVersion >= 9 && state.Stack.Len() < 2 {
					return vmerr.Error(vmerr.CodeStackUnderflow)
				}
				v, err := state.Stack.PopIntRangeInt64(0, 254)
				if err != nil {
					return err
				}
				hashID = int(v)
			}
			maxCnt := state.Stack.Len() - 1
			if state.GlobalVersion >= 9 && appendMode {
				maxCnt--
			}
			cntInt, err := state.Stack.PopIntRangeInt64(0, int64(maxCnt))
			if err != nil {
				return err
			}
			cnt := int(cntInt)
			hasher, err := newHashExtHasher(hashID)
			if err != nil {
				return err
			}

			var totalBits int
			buf := make([]byte, 0, 64)
			var gasConsumed int64
			for i := 0; i < cnt; i++ {
				idx := i
				if !rev {
					idx = cnt - 1 - i
				}
				item, getErr := state.Stack.Get(idx)
				if getErr != nil {
					return getErr
				}
				data, bits, bitsErr := valueBitsForHashExt(item)
				if bitsErr != nil {
					if dropErr := state.Stack.Drop(cnt); dropErr != nil {
						return dropErr
					}
					return bitsErr
				}
				nextTotalBits := totalBits + bits
				gasTotal := int64(i+1)*vm.HashExtEntryGasPrice + int64(nextTotalBits/8)/hasher.BytesPerGasUnit()
				if err = state.ConsumeGas(gasTotal - gasConsumed); err != nil {
					return err
				}
				gasConsumed = gasTotal
				buf = appendBits(buf, &totalBits, data, bits)
			}
			if err = state.Stack.Drop(cnt); err != nil {
				return err
			}
			if totalBits%8 != 0 {
				return vmerr.Error(vmerr.CodeCellUnderflow, "hash input does not consist of a whole number of bytes")
			}
			if err = hasher.Append(buf); err != nil {
				return err
			}
			hash := hasher.Finish()
			if appendMode {
				builder, err := state.Stack.PopBuilder()
				if err != nil {
					return err
				}
				if !builder.CanExtendBy(uint(len(hash)*8), 0) {
					return vmerr.Error(vmerr.CodeCellOverflow)
				}
				if err = builder.StoreSlice(hash, uint(len(hash)*8)); err != nil {
					return vmerr.Error(vmerr.CodeCellOverflow, err.Error())
				}
				return state.Stack.PushOwnedBuilder(builder)
			}
			if len(hash) <= 32 {
				return state.Stack.PushOwnedInt(new(big.Int).SetBytes(hash))
			}
			out := tuple.NewTupleSized((len(hash) + 31) / 32)
			for i := 0; i < out.Len(); i++ {
				start := i * 32
				end := start + 32
				if end > len(hash) {
					end = len(hash)
				}
				if err = out.Set(i, new(big.Int).SetBytes(hash[start:end])); err != nil {
					return err
				}
			}
			return state.Stack.PushTuple(out)
		},
	}
}

func HASHBU() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			builder, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			cl := builder.WithoutTrace().EndCell()
			hash := cl.HashKey()
			return state.Stack.PushOwnedInt(new(big.Int).SetBytes(hash[:]))
		},
		Name:       "HASHBU",
		BitPrefix:  helpers.BytesPrefix(0xF9, 0x16),
		MinVersion: 12,
	}
}
