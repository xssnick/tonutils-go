package cell

import (
	"errors"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
)

type LoadCell struct {
	bitsSz   int
	loadedSz int
	data     []byte

	// store it as slice of pointers to make indexing logic cleaner on parse,
	// from outside it should always come as object to not have problems
	refs []*LoadCell
}

func (c *LoadCell) LoadRef() (*LoadCell, error) {
	if len(c.refs) == 0 {
		return nil, ErrNoMoreRefs
	}
	ref := c.refs[0]
	c.refs = c.refs[1:]

	return ref, nil
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

	for i := oneMoreLeft; i < ln+oneMoreRight; i++ {
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

		if leftSz == 0 {
			break
		} else if leftSz < 8 {
			b &= 0xFF << (8 - leftSz)
			leftSz = 0
			loadedData = append(loadedData, b)
			break
		}

		if i < ln {
			loadedData = append(loadedData, b)
		}
		leftSz -= 8
	}

	usedBytes := (sz - unusedBits) / 8
	if (sz-unusedBits == 0) || ((sz-unusedBits)%8 > 0 && sz > unusedBits && unusedBits > 0) {
		usedBytes++
	}
	c.data = c.data[usedBytes:]

	c.loadedSz += sz

	return loadedData, nil
}

func (c *LoadCell) LoadAddr() (*address.Address, error) {
	typ, err := c.LoadUInt(2)
	if err != nil {
		return nil, err
	}

	// TODO: support of all types of addresses, currently only std supported, skipping 3 bits
	if typ != 2 {
		return nil, errors.New("not supported type of address, currently only std supported")
	}

	isAnycast, err := c.LoadUInt(1)
	if err != nil {
		return nil, err
	}

	if isAnycast == 1 {
		return nil, errors.New("currently anycast addr is not supported")
	}

	workchain, err := c.LoadSlice(8)
	if err != nil {
		return nil, err
	}

	data, err := c.LoadSlice(256)
	if err != nil {
		return nil, err
	}

	return address.NewAddress(0, workchain[0], data), nil
}

func (c *LoadCell) RestBits() (int, []byte, error) {
	left := c.bitsSz - c.loadedSz
	data, err := c.LoadSlice(left)
	return left, data, err
}

func (c *LoadCell) ToCell() (*Cell, error) {
	left := c.bitsSz - c.loadedSz
	data, err := c.LoadSlice(left)

	var refs []*Cell
	for _, ref := range c.refs {
		cc, err := ref.ToCell()
		if err != nil {
			return nil, err
		}

		refs = append(refs, cc)
	}

	return &Cell{
		bitsSz: left,
		data:   data,
		refs:   refs,
	}, err
}
