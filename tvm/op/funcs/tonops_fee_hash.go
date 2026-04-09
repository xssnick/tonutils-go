package funcs

import (
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"math/big"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/sha3"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const hashExtEntryGasPrice = int64(1)

type tonGasPrices struct {
	FlatGasLimit    uint64
	FlatGasPrice    uint64
	GasPrice        uint64
	SpecialGasLimit uint64
	GasLimit        uint64
	GasCredit       uint64
	BlockGasLimit   uint64
	FreezeDueLimit  uint64
	DeleteDueLimit  uint64
}

type tonMsgPrices struct {
	LumpPrice uint64
	BitPrice  uint64
	CellPrice uint64
	IHRFactor uint32
	FirstFrac uint16
	NextFrac  uint16
}

type tonStoragePrices struct {
	ValidSince  uint32
	BitPrice    uint64
	CellPrice   uint64
	MCBitPrice  uint64
	MCCellPrice uint64
}

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
		return nil, err
	}
	v, err := cfg.Index(idx)
	if err != nil {
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

func parseTonStoragePrices(sl *cell.Slice) (*tonStoragePrices, error) {
	if sl == nil {
		return nil, nil
	}
	tag, err := sl.LoadUInt(8)
	if err != nil {
		return nil, err
	}
	if tag != 0xCC {
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, "cannot parse config: invalid storage prices tag")
	}
	validSince, err := sl.LoadUInt(32)
	if err != nil {
		return nil, err
	}
	bitPrice, err := sl.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	cellPrice, err := sl.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	mcBitPrice, err := sl.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	mcCellPrice, err := sl.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	return &tonStoragePrices{
		ValidSince:  uint32(validSince),
		BitPrice:    bitPrice,
		CellPrice:   cellPrice,
		MCBitPrice:  mcBitPrice,
		MCCellPrice: mcCellPrice,
	}, nil
}

func parseTonGasPrices(sl *cell.Slice) (*tonGasPrices, error) {
	if sl == nil {
		return nil, vmerr.Error(vmerr.CodeTypeCheck, "intermediate value is not a slice")
	}
	out := &tonGasPrices{}
	work := sl.Copy()

	tag, err := work.PreloadUInt(8)
	if err != nil {
		return nil, err
	}
	if tag == 0xD1 {
		if _, err = work.LoadUInt(8); err != nil {
			return nil, err
		}
		out.FlatGasLimit, err = work.LoadUInt(64)
		if err != nil {
			return nil, err
		}
		out.FlatGasPrice, err = work.LoadUInt(64)
		if err != nil {
			return nil, err
		}
	}

	mainTag, err := work.LoadUInt(8)
	if err != nil {
		return nil, err
	}
	switch mainTag {
	case 0xDD:
		out.GasPrice, err = work.LoadUInt(64)
		if err != nil {
			return nil, err
		}
		out.GasLimit, err = work.LoadUInt(64)
		if err != nil {
			return nil, err
		}
		out.SpecialGasLimit = out.GasLimit
		out.GasCredit, err = work.LoadUInt(64)
		if err != nil {
			return nil, err
		}
	case 0xDE:
		out.GasPrice, err = work.LoadUInt(64)
		if err != nil {
			return nil, err
		}
		out.GasLimit, err = work.LoadUInt(64)
		if err != nil {
			return nil, err
		}
		out.SpecialGasLimit, err = work.LoadUInt(64)
		if err != nil {
			return nil, err
		}
		out.GasCredit, err = work.LoadUInt(64)
		if err != nil {
			return nil, err
		}
	default:
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, "cannot parse config: invalid gas prices tag")
	}
	out.BlockGasLimit, err = work.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	out.FreezeDueLimit, err = work.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	out.DeleteDueLimit, err = work.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func parseTonMsgPrices(sl *cell.Slice) (*tonMsgPrices, error) {
	if sl == nil {
		return nil, vmerr.Error(vmerr.CodeTypeCheck, "intermediate value is not a slice")
	}
	tag, err := sl.LoadUInt(8)
	if err != nil {
		return nil, err
	}
	if tag != 0xEA {
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, "cannot parse config: invalid msg prices tag")
	}
	lump, err := sl.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	bitPrice, err := sl.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	cellPrice, err := sl.LoadUInt(64)
	if err != nil {
		return nil, err
	}
	ihrFactor, err := sl.LoadUInt(32)
	if err != nil {
		return nil, err
	}
	firstFrac, err := sl.LoadUInt(16)
	if err != nil {
		return nil, err
	}
	nextFrac, err := sl.LoadUInt(16)
	if err != nil {
		return nil, err
	}
	return &tonMsgPrices{
		LumpPrice: lump,
		BitPrice:  bitPrice,
		CellPrice: cellPrice,
		IHRFactor: uint32(ihrFactor),
		FirstFrac: uint16(firstFrac),
		NextFrac:  uint16(nextFrac),
	}, nil
}

func getTonGasPrices(state *vm.State, isMasterchain bool) (*tonGasPrices, error) {
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

func getTonMsgPrices(state *vm.State, isMasterchain bool) (*tonMsgPrices, error) {
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

func getTonStoragePrices(state *vm.State) (*tonStoragePrices, error) {
	sl, err := unpackedConfigSlice(state, 0)
	if err != nil {
		return nil, err
	}
	return parseTonStoragePrices(sl)
}

func ceilShiftRight(x *big.Int, bits uint) *big.Int {
	if x.Sign() == 0 {
		return big.NewInt(0)
	}
	add := new(big.Int).Lsh(big.NewInt(1), bits)
	add.Sub(add, big.NewInt(1))
	y := new(big.Int).Add(x, add)
	return y.Rsh(y, bits)
}

func maxBig(x, y *big.Int) *big.Int {
	if x.Cmp(y) >= 0 {
		return x
	}
	return y
}

func (p *tonGasPrices) computeGasPrice(gasUsed uint64) *big.Int {
	if gasUsed <= p.FlatGasLimit {
		return new(big.Int).SetUint64(p.FlatGasPrice)
	}
	diff := gasUsed - p.FlatGasLimit
	total := ceilShiftRight(new(big.Int).Mul(new(big.Int).SetUint64(p.GasPrice), new(big.Int).SetUint64(diff)), 16)
	return total.Add(total, new(big.Int).SetUint64(p.FlatGasPrice))
}

func (p *tonMsgPrices) computeForwardFee(cells, bits uint64) *big.Int {
	part := new(big.Int).Mul(new(big.Int).SetUint64(p.BitPrice), new(big.Int).SetUint64(bits))
	part.Add(part, new(big.Int).Mul(new(big.Int).SetUint64(p.CellPrice), new(big.Int).SetUint64(cells)))
	part = ceilShiftRight(part, 16)
	return part.Add(part, new(big.Int).SetUint64(p.LumpPrice))
}

func (p *tonStoragePrices) computeStorageFee(isMasterchain bool, delta, bits, cells uint64) *big.Int {
	var bitPrice, cellPrice uint64
	if isMasterchain {
		bitPrice = p.MCBitPrice
		cellPrice = p.MCCellPrice
	} else {
		bitPrice = p.BitPrice
		cellPrice = p.CellPrice
	}
	total := new(big.Int).Mul(new(big.Int).SetUint64(cells), new(big.Int).SetUint64(cellPrice))
	total.Add(total, new(big.Int).Mul(new(big.Int).SetUint64(bits), new(big.Int).SetUint64(bitPrice)))
	total.Mul(total, new(big.Int).SetUint64(delta))
	return ceilShiftRight(total, 16)
}

func popUint64NonNegative(st *vm.Stack) (uint64, error) {
	v, err := st.PopIntFinite()
	if err != nil {
		return 0, err
	}
	if v.Sign() < 0 || v.BitLen() > 63 {
		return 0, vmerr.Error(vmerr.CodeRangeCheck, "finite non-negative integer expected")
	}
	return v.Uint64(), nil
}

func GETGASFEE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			isMasterchain, err := state.Stack.PopBool()
			if err != nil {
				return err
			}
			prices, err := getTonGasPrices(state, isMasterchain)
			if err != nil {
				return err
			}
			gas, err := popUint64NonNegative(state.Stack)
			if err != nil {
				return err
			}
			return state.Stack.PushInt(prices.computeGasPrice(gas))
		},
		Name:      "GETGASFEE",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x36),
	}
}

func GETSTORAGEFEE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
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
				return state.Stack.PushInt(big.NewInt(0))
			}
			return state.Stack.PushInt(prices.computeStorageFee(isMasterchain, delta, bits, cellsCnt))
		},
		Name:      "GETSTORAGEFEE",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x37),
	}
}

func GETFORWARDFEE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
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
			return state.Stack.PushInt(prices.computeForwardFee(cellsCnt, bits))
		},
		Name:      "GETFORWARDFEE",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x38),
	}
}

func GETORIGINALFWDFEE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
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
			num := new(big.Int).Mul(fwdFee, big.NewInt(1<<16))
			den := big.NewInt(int64((1 << 16) - uint64(prices.FirstFrac)))
			return state.Stack.PushInt(num.Div(num, den))
		},
		Name:      "GETORIGINALFWDFEE",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x3A),
	}
}

func GETGASFEESIMPLE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
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
			total := ceilShiftRight(new(big.Int).Mul(new(big.Int).SetUint64(prices.GasPrice), new(big.Int).SetUint64(gas)), 16)
			return state.Stack.PushInt(total)
		},
		Name:      "GETGASFEESIMPLE",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x3B),
	}
}

func GETFORWARDFEESIMPLE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
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
			part := new(big.Int).Mul(new(big.Int).SetUint64(prices.BitPrice), new(big.Int).SetUint64(bits))
			part.Add(part, new(big.Int).Mul(new(big.Int).SetUint64(prices.CellPrice), new(big.Int).SetUint64(cellsCnt)))
			return state.Stack.PushInt(ceilShiftRight(part, 16))
		},
		Name:      "GETFORWARDFEESIMPLE",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x3C),
	}
}

func GETEXTRABALANCE() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action: func(state *vm.State) error {
			id, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}
			if id.Sign() < 0 || id.BitLen() > 32 {
				return vmerr.Error(vmerr.CodeRangeCheck)
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
			if dictRootAny == nil {
				return pushSmallInt(state, 0)
			}
			dictRoot, ok := dictRootAny.(*cell.Cell)
			if !ok {
				return vmerr.Error(vmerr.CodeTypeCheck)
			}

			key := cell.BeginCell().MustStoreBigUInt(id, 32).EndCell()
			value, err := dictRoot.AsDict(32).SetObserver(&state.Cells).LoadValue(key)
			if err != nil {
				if errors.Is(err, cell.ErrNoSuchKeyInDict) {
					return pushSmallInt(state, 0)
				}
				return err
			}

			amount, err := value.LoadVarUInt(32)
			if err != nil {
				return err
			}
			return state.Stack.PushInt(amount)
		},
		Name:      "GETEXTRABALANCE",
		BitPrefix: helpers.BytesPrefix(0xF8, 0x80),
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
			return state.Stack.PushInt(new(big.Int).SetBytes(sum[:]))
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

func collectHashExtItems(state *vm.State, cnt int, rev, appendMode bool) ([]any, *cell.Builder, error) {
	items := make([]any, cnt)
	for i := 0; i < cnt; i++ {
		val, err := state.Stack.PopAny()
		if err != nil {
			return nil, nil, err
		}
		items[i] = val
	}
	var builder *cell.Builder
	if appendMode {
		var err error
		builder, err = state.Stack.PopBuilder()
		if err != nil {
			return nil, nil, err
		}
	}
	if !rev {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}
	return items, builder, nil
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
		sl := x.WithoutObserver().ToSlice()
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
				v, err := state.Stack.PopIntRange(0, 254)
				if err != nil {
					return err
				}
				hashID = int(v.Int64())
			}
			cntInt, err := state.Stack.PopIntRange(0, int64(state.Stack.Len()))
			if err != nil {
				return err
			}
			cnt := int(cntInt.Int64())
			items, builder, err := collectHashExtItems(state, cnt, rev, appendMode)
			if err != nil {
				return err
			}
			hasher, err := newHashExtHasher(hashID)
			if err != nil {
				return err
			}

			var totalBits int
			buf := make([]byte, 0, 64)
			var gasConsumed int64
			for i, item := range items {
				data, bits, bitsErr := valueBitsForHashExt(item)
				if bitsErr != nil {
					return bitsErr
				}
				buf = appendBits(buf, &totalBits, data, bits)
				gasTotal := int64(i+1)*hashExtEntryGasPrice + int64(totalBits/8)/hasher.BytesPerGasUnit()
				if err = state.ConsumeGas(gasTotal - gasConsumed); err != nil {
					return err
				}
				gasConsumed = gasTotal
			}
			if totalBits%8 != 0 {
				return vmerr.Error(vmerr.CodeCellUnderflow, "hash input does not consist of a whole number of bytes")
			}
			if err = hasher.Append(buf); err != nil {
				return err
			}
			hash := hasher.Finish()
			if appendMode {
				if !builder.CanExtendBy(uint(len(hash)*8), 0) {
					return vmerr.Error(vmerr.CodeCellOverflow)
				}
				if err = builder.StoreSlice(hash, uint(len(hash)*8)); err != nil {
					return vmerr.Error(vmerr.CodeCellOverflow, err.Error())
				}
				return state.Stack.PushBuilder(builder)
			}
			if len(hash) <= 32 {
				return state.Stack.PushInt(new(big.Int).SetBytes(hash))
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
			if err = state.ConsumeTupleGasLen(out.Len()); err != nil {
				return err
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
			cl := builder.WithoutObserver().EndCell()
			return state.Stack.PushInt(new(big.Int).SetBytes(cl.Hash()))
		},
		Name:      "HASHBU",
		BitPrefix: helpers.BytesPrefix(0xF9, 0x16),
	}
}
