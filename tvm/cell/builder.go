package cell

import (
	"encoding/binary"
	"github.com/xssnick/tonutils-go/address"
	"math/big"
	"math/bits"
)

var bigIntOne = big.NewInt(1)

type Builder struct {
	trace   *Trace
	data    [maxCellDataBytes]byte
	refs    [4]*Cell
	bitsSz  uint
	refsNum uint8
}

type SerializableToCell interface {
	ToCell() (*Cell, error)
}

func (b *Builder) usedBytes() int {
	return int((b.bitsSz + 7) / 8)
}

func (b *Builder) dataSlice() []byte {
	return b.data[:b.usedBytes()]
}

func (b *Builder) rawRefs() []*Cell {
	return b.refs[:b.refsNum:b.refsNum]
}

func validateCellRefDepthLimit(refs []*Cell) error {
	for _, ref := range refs {
		if ref.Depth() >= maxDepth {
			return ErrCellDepthLimit
		}
	}
	return nil
}

func (b *Builder) MustStoreCoins(value uint64) *Builder {
	err := b.StoreCoins(value)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreCoins(value uint64) error {
	return b.StoreVarUInt(value, 16)
}

func (b *Builder) MustStoreBigCoins(value *big.Int) *Builder {
	err := b.StoreBigCoins(value)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreBigCoins(value *big.Int) error {
	// varInt 16 https://github.com/ton-blockchain/ton/blob/24dc184a2ea67f9c47042b4104bbb4d82289fac1/crypto/block/block-parse.cpp#L319
	return b.StoreBigVarUInt(value, 16)
}

func (b *Builder) StoreVarUInt(val uint64, sz uint) error {
	if sz == 0 {
		return ErrInvalidSize
	}

	ln := uint((bits.Len64(val) + 7) >> 3)
	if ln >= sz {
		return ErrTooBigValue
	}

	szLen := uint(bits.Len64(uint64(sz - 1)))
	if b.bitsSz+szLen+(ln*8) >= 1024 {
		return ErrNotFit1023
	}

	if err := b.StoreUInt(uint64(ln), szLen); err != nil {
		return err
	}
	return b.StoreUInt(val, ln*8)
}

func (b *Builder) StoreVarInt(val int64, sz uint) error {
	return b.StoreBigVarInt(big.NewInt(val), sz)
}

func (b *Builder) StoreBigVarUInt(val *big.Int, sz uint) error {
	if val == nil {
		return ErrNilBigInt
	}
	if sz == 0 {
		return ErrInvalidSize
	}
	if val.Sign() < 0 {
		return ErrNegative
	}

	ln := uint((val.BitLen() + 7) >> 3) // bytes required for value
	if ln >= sz {
		return ErrTooBigValue
	}

	szLen := uint(bits.Len64(uint64(sz - 1)))
	if b.bitsSz+szLen+(ln*8) >= 1024 {
		return ErrNotFit1023
	}

	err := b.StoreUInt(uint64(ln), szLen)
	if err != nil {
		return err
	}

	err = b.StoreBigUInt(val, ln*8)
	if err != nil {
		return err
	}
	return nil
}

func (b *Builder) StoreBigVarInt(val *big.Int, sz uint) error {
	if val == nil {
		return ErrNilBigInt
	}
	if sz == 0 {
		return ErrInvalidSize
	}

	ln := varIntBytes(val)
	if ln >= sz {
		return ErrTooBigValue
	}
	if ln > 32 {
		return ErrTooBigSize
	}

	szLen := uint(bits.Len64(uint64(sz - 1)))
	if b.bitsSz+szLen+(ln*8) >= 1024 {
		return ErrNotFit1023
	}

	err := b.StoreUInt(uint64(ln), szLen)
	if err != nil {
		return err
	}

	err = b.StoreBigInt(val, ln*8)
	if err != nil {
		return err
	}
	return nil
}

func (b *Builder) MustStoreBigVarUInt(val *big.Int, sz uint) *Builder {
	err := b.StoreBigVarUInt(val, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) MustStoreBigVarInt(val *big.Int, sz uint) *Builder {
	err := b.StoreBigVarInt(val, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) MustStoreVarUInt(val uint64, sz uint) *Builder {
	err := b.StoreVarUInt(val, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) MustStoreVarInt(val int64, sz uint) *Builder {
	err := b.StoreVarInt(val, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) MustStoreUInt(value uint64, sz uint) *Builder {
	err := b.StoreUInt(value, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreUInt(value uint64, sz uint) error {
	if sz == 0 {
		if value != 0 {
			return ErrTooBigValue
		}
		return nil
	}
	if sz > 64 {
		return b.StoreBigUInt(new(big.Int).SetUint64(value), sz)
	}
	if sz < 64 && value>>sz != 0 {
		return ErrTooBigValue
	}
	if sz == 1 {
		return b.StoreBoolBit(value == 1)
	}
	if b.bitsSz%8 == 0 && sz%8 == 0 {
		if b.bitsSz+sz >= 1024 {
			return ErrNotFit1023
		}

		dst := int(b.bitsSz / 8)
		switch sz {
		case 8:
			b.data[dst] = byte(value)
		case 16:
			binary.BigEndian.PutUint16(b.data[dst:dst+2], uint16(value))
		case 32:
			binary.BigEndian.PutUint32(b.data[dst:dst+4], uint32(value))
		case 64:
			binary.BigEndian.PutUint64(b.data[dst:dst+8], value)
		default:
			value <<= 64 - sz
			var buf [8]byte
			binary.BigEndian.PutUint64(buf[:], value)
			copy(b.data[dst:dst+int(sz/8)], buf[:int(sz/8)])
		}
		b.bitsSz += sz
		return nil
	}

	if b.bitsSz+sz >= 1024 {
		return ErrNotFit1023
	}

	idx := b.bitsSz / 8
	if idx+8 > uint(len(b.data)) {
		// too close to the array end for a word-wide write
		value <<= 64 - sz
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], value)
		return b.StoreSlice(buf[:], sz)
	}

	// single read-modify-write of the destination word: bits past bitsSz are
	// zero by the builder invariant, so OR places the value in one shot
	off := b.bitsSz % 8
	v := value << (64 - sz)
	cur := binary.BigEndian.Uint64(b.data[idx:])
	binary.BigEndian.PutUint64(b.data[idx:], cur|v>>off)
	if off+sz > 64 {
		b.data[idx+8] = byte(v << (64 - off) >> 56)
	}

	b.bitsSz += sz
	return nil
}

func (b *Builder) MustStoreInt(value int64, sz uint) *Builder {
	err := b.StoreInt(value, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreInt(value int64, sz uint) error {
	if sz > 257 {
		return ErrTooBigSize
	}
	if sz == 0 {
		if value != 0 {
			return ErrTooBigValue
		}
		return nil
	}

	if sz <= 64 {
		if !fitsSignedInt64(value, sz) {
			return ErrTooBigValue
		}
		if value >= 0 {
			return b.StoreUInt(uint64(value), sz)
		}

		u := uint64(value)
		if sz < 64 {
			u &= (uint64(1) << sz) - 1
		}
		return b.StoreUInt(u, sz)
	}

	return b.StoreBigInt(new(big.Int).SetInt64(value), sz)
}

func (b *Builder) MustStoreBoolBit(value bool) *Builder {
	err := b.StoreBoolBit(value)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreBoolBit(value bool) error {
	if b.bitsSz+1 >= 1024 {
		return ErrNotFit1023
	}

	byteIdx := b.bitsSz / 8
	bitMask := byte(1 << (7 - (b.bitsSz % 8)))
	if value {
		b.data[byteIdx] |= bitMask
	} else {
		b.data[byteIdx] &^= bitMask
	}

	b.bitsSz++
	return nil
}

func (b *Builder) MustStoreBigUInt(value *big.Int, sz uint) *Builder {
	err := b.StoreBigUInt(value, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreBigUInt(value *big.Int, sz uint) error {
	if value == nil {
		return ErrNilBigInt
	}

	if value.Sign() == -1 {
		return ErrNegative
	}

	if sz > 256 {
		return ErrTooBigSize
	}
	if sz == 0 {
		if value.Sign() != 0 {
			return ErrTooBigValue
		}
		return nil
	}
	if value.BitLen() > int(sz) {
		return ErrTooBigValue
	}

	return b.storeBig(value, sz)
}

func (b *Builder) storeBig(value *big.Int, sz uint) error {
	bytesToUse := int((sz + 7) / 8)
	var buf [33]byte
	bytes := value.FillBytes(buf[:bytesToUse])

	// check is value uses full bytes
	if offset := sz % 8; offset > 0 {
		add := byte(0)
		// move bits to left side of bytes to fit into size
		for i := len(bytes) - 1; i >= 0; i-- {
			toMove := bytes[i] & (0xFF << offset) // get last bits
			bytes[i] <<= 8 - offset               // move first bits to last
			bytes[i] += add
			add = toMove >> offset
		}
	}

	return b.StoreSlice(bytes, sz)
}

func (b *Builder) MustStoreBigInt(value *big.Int, sz uint) *Builder {
	err := b.StoreBigInt(value, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreBigInt(value *big.Int, sz uint) error {
	if value == nil {
		return ErrNilBigInt
	}

	if sz > 257 {
		return ErrTooBigSize
	}
	if sz == 0 {
		if value.Sign() != 0 {
			return ErrTooBigValue
		}
		return nil
	}
	if value.Sign() >= 0 {
		if value.BitLen() > int(sz) {
			return ErrTooBigValue
		}
		return b.storeBig(value, sz)
	}
	if !fitsNegativeBigInt(value, sz) {
		return ErrTooBigValue
	}

	encoded := new(big.Int).Lsh(bigIntOne, sz)
	encoded.Add(encoded, value)
	return b.storeBig(encoded, sz)
}

func (b *Builder) MustStoreAddr(addr *address.Address) *Builder {
	err := b.StoreAddr(addr)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreAddr(addr *address.Address) error {
	if addr == nil || addr.IsAddrNone() {
		if b.bitsSz+2 >= 1024 {
			return ErrNotFit1023
		}
		return b.StoreUInt(0, 2)
	}

	anycast := addr.Anycast()
	anycastBits := uint(0)
	if anycast != nil {
		if anycast.Depth() == 0 || anycast.Depth() > 30 {
			return ErrTooBigSize
		}
		if uint(len(anycast.Prefix())*8) < anycast.Depth() {
			return ErrSmallSlice
		}
		anycastBits = 5 + anycast.Depth()
	}

	switch addr.Type() {
	case address.ExtAddress:
		if b.bitsSz+2+9+addr.BitsLen() >= 1024 {
			return ErrNotFit1023
		}

		// addr ext
		err := b.StoreUInt(0b01, 2)
		if err != nil {
			return err
		}

		err = b.StoreUInt(uint64(addr.BitsLen()), 9)
		if err != nil {
			return err
		}

		err = b.StoreSlice(addr.Data(), addr.BitsLen())
		if err != nil {
			return err
		}

		return nil
	case address.StdAddress:
		if b.bitsSz+2+1+anycastBits+8+256 >= 1024 {
			return ErrNotFit1023
		}

		// addr std
		err := b.StoreUInt(0b10, 2)
		if err != nil {
			return err
		}

		if anycast == nil {
			err = b.StoreUInt(0b0, 1)
			if err != nil {
				return err
			}
		} else {
			err = b.StoreUInt(0b1, 1)
			if err != nil {
				return err
			}
			err = b.StoreUInt(uint64(anycast.Depth()), 5)
			if err != nil {
				return err
			}
			err = b.StoreSlice(anycast.Prefix(), anycast.Depth())
			if err != nil {
				return err
			}
		}

		err = b.StoreUInt(uint64(uint8(addr.Workchain())), 8)
		if err != nil {
			return err
		}

		err = b.StoreSlice(addr.Data(), 256)
		if err != nil {
			return err
		}

		return nil
	case address.VarAddress:
		if b.bitsSz+2+1+anycastBits+9+32+addr.BitsLen() >= 1024 {
			return ErrNotFit1023
		}

		// addr var
		err := b.StoreUInt(0b11, 2)
		if err != nil {
			return err
		}

		if anycast == nil {
			err = b.StoreUInt(0b0, 1)
			if err != nil {
				return err
			}
		} else {
			err = b.StoreUInt(0b1, 1)
			if err != nil {
				return err
			}
			err = b.StoreUInt(uint64(anycast.Depth()), 5)
			if err != nil {
				return err
			}
			err = b.StoreSlice(anycast.Prefix(), anycast.Depth())
			if err != nil {
				return err
			}
		}

		err = b.StoreUInt(uint64(addr.BitsLen()), 9)
		if err != nil {
			return err
		}

		err = b.StoreInt(int64(addr.Workchain()), 32)
		if err != nil {
			return err
		}

		err = b.StoreSlice(addr.Data(), addr.BitsLen())
		if err != nil {
			return err
		}

		return nil
	}

	return ErrAddressTypeNotSupported
}

func fitsSignedInt64(value int64, sz uint) bool {
	if sz == 0 {
		return value == 0
	}
	if sz >= 64 {
		return true
	}

	max := int64(uint64(1)<<(sz-1)) - 1
	min := -int64(uint64(1) << (sz - 1))
	return value >= min && value <= max
}

func fitsNegativeBigInt(value *big.Int, sz uint) bool {
	if sz == 0 {
		return value.Sign() == 0
	}
	if value.Sign() >= 0 {
		return false
	}

	absMinusOne := new(big.Int).Neg(value)
	absMinusOne.Sub(absMinusOne, bigIntOne)
	return absMinusOne.BitLen() <= int(sz-1)
}

func varIntBytes(val *big.Int) uint {
	if val.Sign() == 0 {
		return 0
	}
	if val.Sign() > 0 {
		return uint(val.BitLen()+8) >> 3
	}

	n := new(big.Int).Neg(val)
	n.Sub(n, bigIntOne)
	return uint(n.BitLen()+8) >> 3
}

func (b *Builder) MustStoreStringSnake(str string) *Builder {
	err := b.StoreStringSnake(str)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) MustStoreBinarySnake(data []byte) *Builder {
	err := b.StoreBinarySnake(data)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreStringSnake(str string) error {
	return b.StoreBinarySnake([]byte(str))
}

func (b *Builder) StoreBinarySnake(data []byte) error {
	var makeSnake func(data []byte, space int) (*Builder, error)
	makeSnake = func(data []byte, space int) (*Builder, error) {
		if len(data) < space {
			space = len(data)
		}

		c := BeginCell()
		if err := c.StoreSlice(data[:space], uint(space)*8); err != nil {
			return nil, err
		}

		if len(data) > space {
			ref, err := makeSnake(data[space:], 127)
			if err != nil {
				return nil, err
			}

			child, err := ref.EndCellSpecial(false)
			if err != nil {
				return nil, err
			}

			if err = c.StoreRef(child); err != nil {
				return nil, err
			}
		}

		return c, nil
	}

	snake, err := makeSnake(data, int(b.BitsLeft()/8))
	if err != nil {
		return err
	}

	return b.StoreBuilder(snake)
}

func (b *Builder) MustStoreDict(dict SerializableToCell) *Builder {
	err := b.StoreDict(dict)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) MustStoreMaybeRef(ref *Cell) *Builder {
	err := b.StoreMaybeRef(ref)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreDict(dict SerializableToCell) error {
	if dict == nil {
		return b.StoreMaybeRef(nil)
	}

	c, err := dict.ToCell()
	if err != nil {
		return err
	}
	return b.StoreMaybeRef(c)
}

func (b *Builder) StoreMaybeRef(ref *Cell) error {
	return b.storeMaybeRef(ref, true)
}

func (b *Builder) StoreMaybeRefUncheckedDepth(ref *Cell) error {
	return b.storeMaybeRef(ref, false)
}

func (b *Builder) storeMaybeRef(ref *Cell, checkDepth bool) error {
	if ref == nil {
		return b.StoreUInt(0, 1)
	}

	// Keep these checks before storing the presence bit, so the method is atomic.
	if b.refsNum >= 4 {
		return ErrTooMuchRefs
	}
	if b.bitsSz+1 >= 1024 {
		return ErrNotFit1023
	}
	if checkDepth && ref.Depth() >= maxDepth {
		return ErrCellDepthLimit
	}

	if err := b.StoreUInt(1, 1); err != nil {
		return err
	}
	b.refs[b.refsNum] = ref
	b.refsNum++
	return nil
}

func (b *Builder) MustStoreRef(ref *Cell) *Builder {
	err := b.StoreRef(ref)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreRef(ref *Cell) error {
	return b.storeRef(ref, true)
}

func (b *Builder) StoreRefUncheckedDepth(ref *Cell) error {
	return b.storeRef(ref, false)
}

func (b *Builder) storeRef(ref *Cell, checkDepth bool) error {
	if b.refsNum >= 4 {
		return ErrTooMuchRefs
	}
	if ref == nil {
		return ErrRefCannotBeNil
	}
	if checkDepth && ref.Depth() >= maxDepth {
		return ErrCellDepthLimit
	}
	b.refs[b.refsNum] = ref
	b.refsNum++

	return nil
}

func (b *Builder) MustStoreSlice(bytes []byte, sz uint) *Builder {
	err := b.StoreSlice(bytes, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreSlice(bytes []byte, sz uint) error {
	if sz == 0 {
		return nil
	}

	if b.bitsSz+sz >= 1024 {
		return ErrNotFit1023
	}

	if uint(len(bytes)*8) < sz {
		return ErrSmallSlice
	}

	bytesNeeded := int((sz + 7) / 8)
	dstByte := int(b.bitsSz / 8)
	dstBitOffset := b.bitsSz % 8

	if dstBitOffset == 0 {
		copy(b.data[dstByte:dstByte+bytesNeeded], bytes[:bytesNeeded])
	} else {
		shift := dstBitOffset
		invShift := 8 - dstBitOffset

		// word-wise path: 8 source bytes per step; the last byte of each
		// written word is partial and is completed by the next step or by
		// the tail loop below
		i := 0
		for ; i+8 <= bytesNeeded && uint((i+7)*8)+invShift < sz && dstByte+i+9 <= len(b.data); i += 8 {
			v := binary.BigEndian.Uint64(bytes[i:])
			b.data[dstByte+i] |= byte(v >> (56 + shift))
			binary.BigEndian.PutUint64(b.data[dstByte+i+1:], v<<invShift)
		}

		for ; i < bytesNeeded; i++ {
			src := bytes[i]
			b.data[dstByte+i] |= src >> shift
			if sz > uint(i*8)+invShift {
				b.data[dstByte+i+1] = src << invShift
			}
		}
	}

	b.bitsSz += sz
	if rem := b.bitsSz % 8; rem != 0 {
		b.data[b.bitsSz/8] &= byte(0xFF << (8 - rem))
	}

	return nil
}

func (b *Builder) storeSliceFromSlice(slice *Slice, sz uint) error {
	var data [maxCellDataBytes]byte
	if err := slice.loadSliceInto(data[:], sz, false); err != nil {
		return err
	}
	return b.StoreSlice(data[:], sz)
}

func (b *Builder) MustStoreBuilder(builder *Builder) *Builder {
	err := b.StoreBuilder(builder)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreBuilder(builder *Builder) error {
	return b.storeBuilder(builder, true)
}

func (b *Builder) StoreBuilderUncheckedDepth(builder *Builder) error {
	return b.storeBuilder(builder, false)
}

func (b *Builder) storeBuilder(builder *Builder, checkDepth bool) error {
	if int(b.refsNum)+int(builder.refsNum) > 4 {
		return ErrTooMuchRefs
	}
	if b.bitsSz+builder.bitsSz >= 1024 {
		return ErrNotFit1023
	}

	refs := builder.rawRefs()
	if checkDepth {
		if err := validateCellRefDepthLimit(refs); err != nil {
			return err
		}
	}

	for _, ref := range refs {
		b.refs[b.refsNum] = ref
		b.refsNum++
	}

	return b.StoreSlice(builder.dataSlice(), builder.bitsSz)
}

func (b *Builder) RefsUsed() int {
	return int(b.refsNum)
}

func (b *Builder) BitsUsed() uint {
	return b.bitsSz
}

func (b *Builder) BitsLeft() uint {
	return 1023 - b.bitsSz
}

func (b *Builder) RefsLeft() uint {
	return 4 - uint(b.refsNum)
}

// truncateBits rolls the builder back to an earlier length for backtracking
// tree walks. The whole rolled-back range is zeroed to restore the builder
// invariant that bits past bitsSz are zero: stores rely on it by extending
// partially written bytes with OR.
func (b *Builder) truncateBits(sz uint) {
	if sz >= b.bitsSz {
		return
	}

	from := sz / 8
	if rem := sz % 8; rem != 0 {
		b.data[from] &= byte(0xFF << (8 - rem))
		from++
	}
	to := (b.bitsSz + 7) / 8
	if from < to {
		clear(b.data[from:to])
	}
	b.bitsSz = sz
}

func (b *Builder) Copy() *Builder {
	cp := &Builder{
		trace:   b.trace,
		bitsSz:  b.bitsSz,
		refsNum: b.refsNum,
	}
	copy(cp.data[:], b.dataSlice())
	copy(cp.refs[:], b.rawRefs())
	return cp
}

func BeginCell() *Builder {
	return &Builder{}
}

func (b *Builder) EndCell() *Cell {
	if err := b.trace.NotifyCreate(); err != nil {
		panic(err)
	}
	c, err := finalizeCellFromBuilder(b, false)
	if err != nil {
		panic(err)
	}
	return c
}

func (b *Builder) ToSlice() *Slice {
	return newSliceFromCell(b.EndCell(), b.trace)
}

func (b *Builder) String() string {
	return b.WithoutTrace().EndCell().String()
}

func (b *Builder) SetTrace(trace *Trace) *Builder {
	b.trace = trace
	return b
}

func (b *Builder) Trace() *Trace {
	if b == nil {
		return nil
	}
	return b.trace
}

func (b *Builder) WithoutTrace() *Builder {
	cp := b.Copy()
	cp.trace = nil
	return cp
}
