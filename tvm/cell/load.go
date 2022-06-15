package cell

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
)

type LoadCell struct {
	special  bool
	level    byte
	bitsSz   int
	loadedSz int
	data     []byte

	// store it as slice of pointers to make indexing logic cleaner on parse,
	// from outside it should always come as object to not have problems
	refs []*LoadCell
}

func (c *LoadCell) MustLoadRef() *LoadCell {
	r, err := c.LoadRef()
	if err != nil {
		panic(err)
	}

	return r
}

func (c *LoadCell) LoadRef() (*LoadCell, error) {
	if len(c.refs) == 0 {
		return nil, ErrNoMoreRefs
	}
	ref := c.refs[0]
	c.refs = c.refs[1:]

	return ref, nil
}

func (c *LoadCell) RefsNum() int {
	return len(c.refs)
}

func (c *LoadCell) LoadCoins() (uint64, error) {
	value, err := c.LoadBigCoins()
	if err != nil {
		return 0, err
	}

	return value.Uint64(), nil
}

func (c *LoadCell) LoadBigCoins() (*big.Int, error) {
	// varInt 16 https://github.com/ton-blockchain/ton/blob/24dc184a2ea67f9c47042b4104bbb4d82289fac1/crypto/block/block-parse.cpp#L319
	ln, err := c.LoadUInt(4)
	if err != nil {
		return nil, err
	}

	value, err := c.LoadBigInt(int(ln * 8))
	if err != nil {
		return nil, err
	}

	return value, nil
}

func (c *LoadCell) MustLoadUInt(sz int) uint64 {
	res, err := c.LoadUInt(sz)
	if err != nil {
		panic(err)
	}
	return res
}

func (c *LoadCell) LoadUInt(sz int) (uint64, error) {
	res, err := c.LoadBigInt(sz)
	if err != nil {
		return 0, err
	}
	return res.Uint64(), nil
}

func (c *LoadCell) LoadBoolBit() (bool, error) {
	res, err := c.LoadBigInt(1)
	if err != nil {
		return false, err
	}
	return res.Uint64() == 1, nil
}

func (c *LoadCell) LoadBigInt(sz int) (*big.Int, error) {
	if sz > 256 {
		return nil, ErrTooBigValue
	}

	b, err := c.LoadSlice(sz)
	if err != nil {
		return nil, err
	}

	// check is value is uses full bytes
	if offset := sz % 8; offset > 0 {
		// move bits to right side of bytes
		for i := len(b) - 1; i >= 0; i-- {
			b[i] >>= 8 - offset // get last bits
			if i > 0 {
				b[i] += b[i-1] << offset
			}
		}
	}

	return new(big.Int).SetBytes(b), nil
}

func (c *LoadCell) LoadVarUInt(sz int) (*big.Int, error) {
	ln, err := c.LoadUInt(big.NewInt(int64(sz - 1)).BitLen())
	if err != nil {
		return nil, err
	}

	value, err := c.LoadBigInt(int(ln * 8))
	if err != nil {
		return nil, err
	}

	return value, nil
}

func (c *LoadCell) MustLoadSlice(sz int) []byte {
	s, err := c.LoadSlice(sz)
	if err != nil {
		panic(err)
	}
	return s
}

func (c *LoadCell) LoadSlice(sz int) ([]byte, error) {
	if c.bitsSz-c.loadedSz < sz {
		return nil, ErrNotEnoughData
	}

	if sz <= 0 {
		return []byte{}, nil
	}

	leftSz := sz
	var unusedBits = 0
	if l := c.loadedSz % 8; l > 0 && c.loadedSz > 0 {
		unusedBits = 8 - (c.loadedSz % 8)
	}

	var loadedData []byte

	oneMoreLeft, oneMoreRight := 0, 0
	if unusedBits > 0 && sz > unusedBits {
		oneMoreLeft = 1
	}
	if (sz-unusedBits)%8 != 0 || sz-unusedBits == 0 {
		oneMoreRight = 1
	}

	ln := (sz-unusedBits)/8 + oneMoreLeft + oneMoreRight

	i := oneMoreLeft
	for leftSz > 0 {
		var b byte
		if oneMoreLeft > 0 {
			b = c.data[i-1] << byte(8-unusedBits)
			if i < ln {
				b += c.data[i] >> unusedBits
			}
		} else {
			b = c.data[i]
			if unusedBits > 0 {
				b <<= byte(8 - unusedBits)
			}
		}

		if leftSz < 8 {
			b &= 0xFF << (8 - leftSz)
			leftSz = 0
			loadedData = append(loadedData, b)
			break
		}

		if i < ln {
			loadedData = append(loadedData, b)
		}

		leftSz -= 8
		i++
	}

	usedBytes := (sz - unusedBits) / 8
	if sz >= unusedBits && unusedBits > 0 {
		usedBytes++
	}

	c.data = c.data[usedBytes:]

	c.loadedSz += sz

	return loadedData, nil
}

func (c *LoadCell) MustLoadAddr() *address.Address {
	a, err := c.LoadAddr()
	if err != nil {
		panic(err)
	}
	return a
}

func (c *LoadCell) LoadAddr() (*address.Address, error) {
	typ, err := c.LoadUInt(2)
	if err != nil {
		return nil, err
	}

	switch typ {
	case 2:
		isAnycast, err := c.LoadBoolBit()
		if err != nil {
			return nil, fmt.Errorf("failed to load anycast bit: %w", err)
		}

		if isAnycast {
			depthLen := int(math.Ceil(math.Log2(30)))

			depth, err := c.LoadUInt(depthLen)
			if err != nil {
				return nil, fmt.Errorf("failed to load depth: %w", err)
			}

			pfx, err := c.LoadSlice(int(depth))
			if err != nil {
				return nil, fmt.Errorf("failed to load prefix: %w", err)
			}
			_ = pfx

			// TODO: save anycast
		}

		workchain, err := c.LoadUInt(8)
		if err != nil {
			return nil, fmt.Errorf("failed to load workchain: %w", err)
		}

		data, err := c.LoadSlice(256)
		if err != nil {
			return nil, fmt.Errorf("failed to load addr data: %w", err)
		}

		return address.NewAddress(0, byte(workchain), data), nil
	case 0:
		return &address.Address{}, nil
	default:
		// TODO: support of all types of addresses, currently only std supported, skipping 3 bits
		return nil, errors.New("not supported type of address, currently only std supported")
	}

}

func (c *LoadCell) BitsLeft() int {
	return c.bitsSz - c.loadedSz
}

func (c *LoadCell) RestBits() (int, []byte, error) {
	left := c.bitsSz - c.loadedSz
	data, err := c.LoadSlice(left)
	return left, data, err
}

func (c *LoadCell) MustToCell() *Cell {
	cl, err := c.ToCell()
	if err != nil {
		panic(err)
	}
	return cl
}

func (c *LoadCell) Copy() *LoadCell {
	// copy data
	data := append([]byte{}, c.data...)

	var refs []*LoadCell
	for _, ref := range c.refs {
		refs = append(refs, ref.Copy())
	}

	return &LoadCell{
		bitsSz:   c.bitsSz,
		loadedSz: c.loadedSz,
		data:     data,
		refs:     refs,
	}
}

func (c *LoadCell) ToCell() (*Cell, error) {
	cp := c.Copy()

	left := cp.bitsSz - cp.loadedSz
	data, err := cp.LoadSlice(left)
	if err != nil {
		return nil, err
	}

	var refs []*Cell
	for _, ref := range cp.refs {
		cc, err := ref.ToCell()
		if err != nil {
			return nil, err
		}

		refs = append(refs, cc)
	}

	return &Cell{
		special: c.special,
		level:   c.level,
		bitsSz:  left,
		data:    data,
		refs:    refs,
	}, nil
}
