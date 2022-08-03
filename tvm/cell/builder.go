package cell

import (
	"encoding/binary"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
)

type Builder struct {
	bitsSz uint
	data   []byte

	// store it as slice of pointers to make indexing logic cleaner on parse,
	// from outside it should always come as object to not have problems
	refs []*Cell
}

func (b *Builder) MustStoreCoins(value uint64) *Builder {
	err := b.StoreCoins(value)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreCoins(value uint64) error {
	return b.StoreBigCoins(new(big.Int).SetUint64(value))
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
	ln := uint((value.BitLen() + 7) >> 3)
	if ln >= 16 {
		return ErrTooBigValue
	}

	if b.bitsSz+4+(ln*8) >= 1024 {
		return ErrNotFit1023
	}

	err := b.StoreUInt(uint64(ln), 4)
	if err != nil {
		return err
	}

	err = b.StoreBigUInt(value, ln*8)
	if err != nil {
		return err
	}

	return nil
}

func (b *Builder) MustStoreUInt(value uint64, sz uint) *Builder {
	err := b.StoreUInt(value, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreUInt(value uint64, sz uint) error {
	if sz > 64 {
		return b.StoreBigUInt(new(big.Int).SetUint64(value), sz)
	}

	value <<= 64 - sz
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, value)

	return b.StoreSlice(buf, sz)
}

func (b *Builder) MustStoreInt(value int64, sz uint) *Builder {
	err := b.StoreInt(value, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreInt(value int64, sz uint) error {
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
	var i uint64
	if value {
		i = 1
	}
	return b.StoreBigUInt(new(big.Int).SetUint64(i), 1)
}

func (b *Builder) MustStoreBigUInt(value *big.Int, sz uint) *Builder {
	err := b.StoreBigUInt(value, sz)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreBigUInt(value *big.Int, sz uint) error {
	if value.BitLen() > 256 {
		return ErrTooBigValue
	}

	if value.Sign() == -1 {
		return ErrNegative
	}

	if sz > 256 {
		return ErrTooBigSize
	}

	return b.storeBig(value, sz)
}

func (b *Builder) storeBig(value *big.Int, sz uint) error {
	bytes := value.Bytes()

	partByte := 0
	if sz%8 != 0 {
		partByte = 1
	}
	bytesToUse := (int(sz) / 8) + partByte

	if len(bytes) < bytesToUse {
		// add zero bits to fit requested size
		bytes = append(make([]byte, bytesToUse-len(bytes)), bytes...)
	}

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
	if value.BitLen() > 256 {
		return ErrTooBigValue
	}

	if sz > 257 {
		return ErrTooBigSize
	}

	one := big.NewInt(1)

	if value.Sign() == -1 {
		// get max value of given sz
		i := new(big.Int).Lsh(one, uint(sz)) // 100000000
		i = i.Sub(i, one)                    // 11111111

		// 11111111 is -1, so we need to add 1 to our negative value first,
		// because we already have -1 in 'i'
		value = value.Add(value, one)
		value = value.Add(value, i)
	}

	return b.storeBig(value, sz)
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
		if b.bitsSz+2+1+8+256 >= 1024 {
			return ErrNotFit1023
		}

		// addr std
		err := b.StoreUInt(0b10, 2)
		if err != nil {
			return err
		}

		// anycast
		err = b.StoreUInt(0b0, 1)
		if err != nil {
			return err
		}

		err = b.StoreUInt(uint64(addr.Workchain()), 8)
		if err != nil {
			return err
		}

		err = b.StoreSlice(addr.Data(), 256)
		if err != nil {
			return err
		}

		return nil
	case address.VarAddress:
		if b.bitsSz+2+1+9+32+addr.BitsLen() >= 1024 {
			return ErrNotFit1023
		}

		// addr var
		err := b.StoreUInt(0b11, 2)
		if err != nil {
			return err
		}

		// anycast
		err = b.StoreUInt(0b0, 1)
		if err != nil {
			return err
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
	var f func(space int) (*Builder, error)
	f = func(space int) (*Builder, error) {
		if len(data) < space {
			space = len(data)
		}

		c := BeginCell()
		err := c.StoreSlice(data, uint(space)*8)
		if err != nil {
			return nil, err
		}

		data = data[space:]

		if len(data) > 0 {
			ref, err := f(127)
			if err != nil {
				return nil, err
			}

			err = c.StoreRef(ref.EndCell())
			if err != nil {
				return nil, err
			}
		}

		return c, nil
	}

	snake, err := f(127 - 4)
	if err != nil {
		return err
	}

	return b.StoreBuilder(snake)
}

func (b *Builder) MustStoreDict(dict *Dictionary) *Builder {
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

func (b *Builder) StoreDict(dict *Dictionary) error {
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
	if ref == nil {
		return b.StoreUInt(0, 1)
	}

	// we need early checks to do 2 stores atomically
	if len(b.refs) >= 4 {
		return ErrTooMuchRefs
	}
	if b.bitsSz+1 >= 1024 {
		return ErrNotFit1023
	}

	b.MustStoreUInt(1, 1).MustStoreRef(ref)
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
	if len(b.refs) >= 4 {
		return ErrTooMuchRefs
	}

	if ref == nil {
		return ErrRefCannotBeNil
	}

	b.refs = append(b.refs, ref)

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

	oneMore := uint(0)
	if sz%8 > 0 {
		oneMore = 1
	}

	if uint(len(bytes)) < sz/8+oneMore {
		return ErrSmallSlice
	}

	if b.bitsSz+sz >= 1024 {
		return ErrNotFit1023
	}

	leftSz := sz
	unusedBits := 8 - (b.bitsSz % 8)

	offset := 0
	for leftSz > 0 {
		bits := uint(8)
		if leftSz < 8 {
			bits = leftSz
		}
		leftSz -= bits

		// if previous byte was not filled, we need to move bits to fill it
		if unusedBits != 8 {
			b.data[len(b.data)-1] += bytes[offset] >> (8 - unusedBits)
			if bits > unusedBits {
				b.data = append(b.data, bytes[offset]<<unusedBits)
			}
			offset++
			continue
		}

		// clear unused part of byte if needed
		b.data = append(b.data, bytes[offset]&(0xFF<<(8-bits)))
		offset++
	}

	b.bitsSz += sz

	return nil
}

func (b *Builder) MustStoreBuilder(builder *Builder) *Builder {
	err := b.StoreBuilder(builder)
	if err != nil {
		panic(err)
	}
	return b
}

func (b *Builder) StoreBuilder(builder *Builder) error {
	if len(b.refs)+len(builder.refs) > 4 {
		return ErrTooMuchRefs
	}

	if b.bitsSz+builder.bitsSz >= 1024 {
		return ErrNotFit1023
	}

	b.refs = append(b.refs, builder.refs...)
	b.MustStoreSlice(builder.data, builder.bitsSz)

	return nil
}

func (b *Builder) RefsUsed() int {
	return len(b.refs)
}

func (b *Builder) BitsUsed() uint {
	return b.bitsSz
}

func (b *Builder) BitsLeft() uint {
	return 1023 - b.bitsSz
}

func (b *Builder) RefsLeft() uint {
	return 4 - uint(len(b.refs))
}

func (b *Builder) Copy() *Builder {
	// copy data
	data := append([]byte{}, b.data...)

	return &Builder{
		bitsSz: b.bitsSz,
		data:   data,
		refs:   b.refs,
	}
}

func BeginCell() *Builder {
	return &Builder{}
}

func (b *Builder) EndCell() *Cell {
	// copy data
	data := append([]byte{}, b.data...)

	return &Cell{
		bitsSz: b.bitsSz,
		data:   data,
		refs:   b.refs,
	}
}
