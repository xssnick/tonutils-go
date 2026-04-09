package cell

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
)

type Slice struct {
	special   bool
	levelMask LevelMask
	bitsSz    uint
	loadedSz  uint
	data      []byte
	observer  Observer

	// store it as slice of pointers to make indexing logic cleaner on parse,
	// from outside it should always come as object to not have problems
	refs []*Cell
}

func notifyCellLoad(observer Observer, ref *Cell) {
	if observer == nil || ref == nil {
		return
	}
	if keyObserver, ok := observer.(HashKeyObserver); ok {
		keyObserver.OnCellLoadKey(ref.HashKey())
		return
	}
	observer.OnCellLoad(ref.Hash())
}

func (c *Slice) MustLoadRef() *Slice {
	r, err := c.LoadRef()
	if err != nil {
		panic(err)
	}
	return r
}

func (c *Slice) LoadRef() (*Slice, error) {
	ref, err := c.LoadRefCell()
	if err != nil {
		return nil, err
	}
	return ref.BeginParse().SetObserver(c.observer), nil
}

func (c *Slice) PreloadRef() (*Slice, error) {
	ref, err := c.PreloadRefCell()
	if err != nil {
		return nil, err
	}
	return ref.BeginParse().SetObserver(c.observer), nil
}

func (c *Slice) LoadRefCell() (*Cell, error) {
	if len(c.refs) == 0 {
		return nil, ErrNoMoreRefs
	}
	ref := c.refs[0]
	c.refs = c.refs[1:]
	notifyCellLoad(c.observer, ref)

	return ref, nil
}

func (c *Slice) PreloadRefCell() (*Cell, error) {
	if len(c.refs) == 0 {
		return nil, ErrNoMoreRefs
	}
	ref := c.refs[0]
	notifyCellLoad(c.observer, ref)
	return ref, nil
}

func (c *Slice) PeekRefCell() (*Cell, error) {
	if len(c.refs) == 0 {
		return nil, ErrNoMoreRefs
	}
	return c.refs[0], nil
}

func (c *Slice) MustLoadMaybeRef() *Slice {
	r, err := c.LoadMaybeRef()
	if err != nil {
		panic(err)
	}
	return r
}

func (c *Slice) LoadMaybeRef() (*Slice, error) {
	has, err := c.LoadBoolBit()
	if err != nil {
		return nil, err
	}

	if !has {
		return nil, nil
	}

	if len(c.refs) == 0 {
		return nil, ErrNoMoreRefs
	}
	ref := c.refs[0]
	c.refs = c.refs[1:]
	notifyCellLoad(c.observer, ref)

	return ref.BeginParse().SetObserver(c.observer), nil
}

func (c *Slice) RefsNum() int {
	return len(c.refs)
}

func (c *Slice) BitAt(offset uint) (uint8, error) {
	if offset >= c.BitsLeft() {
		return 0, ErrNotEnoughData(int(c.BitsLeft()), int(offset+1))
	}

	bitPos := (c.loadedSz % 8) + offset
	byteIdx := bitPos / 8
	bitIdx := 7 - (bitPos % 8)
	return (c.data[byteIdx] >> bitIdx) & 1, nil
}

func (c *Slice) Advance(bits uint) error {
	if c.bitsSz-c.loadedSz < bits {
		return ErrNotEnoughData(int(c.bitsSz-c.loadedSz), int(bits))
	}
	if bits == 0 {
		return nil
	}

	startBitOffset := c.loadedSz % 8
	c.loadedSz += bits
	c.data = c.data[(startBitOffset+bits)/8:]
	return nil
}

func (c *Slice) AdvanceExt(bits uint, refs int) error {
	if refs < 0 {
		return ErrNoMoreRefs
	}
	if len(c.refs) < refs {
		return ErrNoMoreRefs
	}
	if err := c.Advance(bits); err != nil {
		return err
	}
	c.refs = c.refs[refs:]
	return nil
}

func (c *Slice) FetchSubslice(bits uint, refs int) (*Slice, error) {
	if refs < 0 {
		return nil, ErrNoMoreRefs
	}
	if len(c.refs) < refs {
		return nil, ErrNoMoreRefs
	}
	data, err := c.LoadSlice(bits)
	if err != nil {
		return nil, err
	}
	outRefs := append([]*Cell(nil), c.refs[:refs]...)
	c.refs = c.refs[refs:]
	return &Slice{
		special:   c.special,
		levelMask: c.levelMask,
		bitsSz:    bits,
		data:      data,
		refs:      outRefs,
		observer:  c.observer,
	}, nil
}

func (c *Slice) PreloadSubslice(bits uint, refs int) (*Slice, error) {
	if refs < 0 {
		return nil, ErrNoMoreRefs
	}
	if len(c.refs) < refs {
		return nil, ErrNoMoreRefs
	}
	data, err := c.PreloadSlice(bits)
	if err != nil {
		return nil, err
	}
	outRefs := append([]*Cell(nil), c.refs[:refs]...)
	return &Slice{
		special:   c.special,
		levelMask: c.levelMask,
		bitsSz:    bits,
		data:      data,
		refs:      outRefs,
		observer:  c.observer,
	}, nil
}

func (c *Slice) MustLoadCoins() uint64 {
	r, err := c.LoadCoins()
	if err != nil {
		panic(err)
	}
	return r
}

func (c *Slice) LoadCoins() (uint64, error) {
	value, err := c.LoadBigCoins()
	if err != nil {
		return 0, err
	}
	return value.Uint64(), nil
}

func (c *Slice) MustLoadBigCoins() *big.Int {
	r, err := c.LoadBigCoins()
	if err != nil {
		panic(err)
	}
	return r
}

func (c *Slice) LoadBigCoins() (*big.Int, error) {
	// varInt 16 https://github.com/ton-blockchain/ton/blob/24dc184a2ea67f9c47042b4104bbb4d82289fac1/crypto/block/block-parse.cpp#L319
	return c.LoadVarUInt(16)
}

func (c *Slice) MustLoadUInt(sz uint) uint64 {
	res, err := c.LoadUInt(sz)
	if err != nil {
		panic(err)
	}
	return res
}

func (c *Slice) MustPreloadUInt(sz uint) uint64 {
	res, err := c.PreloadUInt(sz)
	if err != nil {
		panic(err)
	}
	return res
}

func (c *Slice) LoadUInt(sz uint) (uint64, error) {
	if sz <= 64 {
		return c.loadUintFast(sz, false)
	}

	res, err := c.LoadBigUInt(sz)
	if err != nil {
		return 0, err
	}
	return res.Uint64(), nil
}

func (c *Slice) PreloadUInt(sz uint) (uint64, error) {
	if sz <= 64 {
		return c.loadUintFast(sz, true)
	}

	res, err := c.PreloadBigUInt(sz)
	if err != nil {
		return 0, err
	}
	return res.Uint64(), nil
}

func (c *Slice) MustLoadInt(sz uint) int64 {
	res, err := c.LoadInt(sz)
	if err != nil {
		panic(err)
	}
	return res
}

func (c *Slice) LoadInt(sz uint) (int64, error) {
	if sz <= 64 {
		return c.loadIntFast(sz)
	}

	res, err := c.LoadBigInt(sz)
	if err != nil {
		return 0, err
	}
	return res.Int64(), nil
}

func (c *Slice) MustLoadBoolBit() bool {
	r, err := c.LoadBoolBit()
	if err != nil {
		panic(err)
	}
	return r
}

func (c *Slice) LoadBoolBit() (bool, error) {
	res, err := c.LoadUInt(1)
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

func (c *Slice) MustLoadBigUInt(sz uint) *big.Int {
	r, err := c.LoadBigUInt(sz)
	if err != nil {
		panic(err)
	}
	return r
}

func (c *Slice) MustPreloadBigUInt(sz uint) *big.Int {
	r, err := c.PreloadBigUInt(sz)
	if err != nil {
		panic(err)
	}
	return r
}

func (c *Slice) loadUintFast(sz uint, preload bool) (uint64, error) {
	if c.bitsSz-c.loadedSz < sz {
		return 0, ErrNotEnoughData(int(c.bitsSz-c.loadedSz), int(sz))
	}

	if sz == 0 {
		return 0, nil
	}

	var value uint64
	data := c.data
	startBitOffset := c.loadedSz % 8
	bitOffset := startBitOffset

	for i := uint(0); i < sz; i++ {
		value <<= 1
		value |= uint64((data[0] >> (7 - bitOffset)) & 1)

		bitOffset++
		if bitOffset == 8 {
			bitOffset = 0
			data = data[1:]
		}
	}

	if !preload {
		consumedBytes := (startBitOffset + sz) / 8
		c.loadedSz += sz
		c.data = c.data[consumedBytes:]
	}

	return value, nil
}

func (c *Slice) loadIntFast(sz uint) (int64, error) {
	u, err := c.loadUintFast(sz, false)
	if err != nil {
		return 0, err
	}

	if sz == 0 {
		return 0, nil
	}

	if sz == 64 {
		return int64(u), nil
	}

	signBit := uint64(1) << (sz - 1)
	if u&signBit != 0 {
		u |= ^((uint64(1) << sz) - 1)
	}
	return int64(u), nil
}

func (c *Slice) LoadBigUInt(sz uint) (*big.Int, error) {
	if sz > 256 {
		return nil, ErrTooBigSize
	}

	return c.loadBigNumber(sz)
}

func (c *Slice) PreloadBigUInt(sz uint) (*big.Int, error) {
	if sz > 256 {
		return nil, ErrTooBigSize
	}

	return c.preloadBigNumber(sz)
}

func (c *Slice) loadBigNumber(sz uint) (*big.Int, error) {
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

func (c *Slice) preloadBigNumber(sz uint) (*big.Int, error) {
	b, err := c.PreloadSlice(sz)
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

func (c *Slice) LoadBigInt(sz uint) (*big.Int, error) {
	if sz > 257 {
		return nil, ErrTooBigSize
	}

	u, err := c.loadBigNumber(sz)
	if err != nil {
		return nil, err
	}

	one := big.NewInt(1)

	// check is last bit = 1
	isNegative := new(big.Int).And(u, new(big.Int).Lsh(one, sz-1)).Cmp(big.NewInt(0)) != 0

	if isNegative {
		// get max value of given sz
		i := new(big.Int).Lsh(one, sz)
		i = i.Sub(i, one)

		val := u.Sub(u, i)

		// finally, we are subtracting 1, because 0xFF = -1,
		// and when we do u-i we get value which is +1 from actual
		return val.Sub(val, one), nil
	}

	return u, nil
}

func (c *Slice) PreloadBigInt(sz uint) (*big.Int, error) {
	if sz > 257 {
		return nil, ErrTooBigSize
	}

	cp := c.Copy()
	return cp.LoadBigInt(sz)
}

func (c *Slice) LoadVarUInt(sz uint) (*big.Int, error) {
	ln, err := c.LoadUInt(uint(big.NewInt(int64(sz - 1)).BitLen()))
	if err != nil {
		return nil, err
	}

	value, err := c.LoadBigUInt(uint(ln * 8))
	if err != nil {
		return nil, err
	}

	return value, nil
}

func (c *Slice) MustLoadVarUInt(sz uint) *big.Int {
	s, err := c.LoadVarUInt(sz)
	if err != nil {
		panic(err)
	}
	return s
}

func (c *Slice) MustLoadSlice(sz uint) []byte {
	s, err := c.LoadSlice(sz)
	if err != nil {
		panic(err)
	}
	return s
}

func (c *Slice) MustPreloadSlice(sz uint) []byte {
	s, err := c.PreloadSlice(sz)
	if err != nil {
		panic(err)
	}
	return s
}

func (c *Slice) LoadSlice(sz uint) ([]byte, error) {
	return c.loadSlice(sz, false)
}

func (c *Slice) PreloadSlice(sz uint) ([]byte, error) {
	return c.loadSlice(sz, true)
}

func (c *Slice) loadSlice(sz uint, preload bool) ([]byte, error) {
	if c.bitsSz-c.loadedSz < sz {
		return nil, ErrNotEnoughData(int(c.bitsSz-c.loadedSz), int(sz))
	}

	if sz == 0 {
		return []byte{}, nil
	}

	startBitOffset := c.loadedSz % 8
	outLen := int((sz + 7) / 8)
	loadedData := make([]byte, outLen)

	if startBitOffset == 0 {
		copy(loadedData, c.data[:outLen])
	} else {
		shift := uint8(startBitOffset)
		invShift := uint8(8 - startBitOffset)
		last := outLen - 1
		sourceBytesNeeded := int((startBitOffset + sz + 7) / 8)

		for i := 0; i < last; i++ {
			loadedData[i] = c.data[i]<<shift | c.data[i+1]>>invShift
		}

		loadedData[last] = c.data[last] << shift
		if sourceBytesNeeded > outLen {
			loadedData[last] |= c.data[last+1] >> invShift
		}
	}

	if rem := sz % 8; rem != 0 {
		loadedData[outLen-1] &= byte(0xFF << (8 - rem))
	}

	if !preload {
		c.loadedSz += sz
		c.data = c.data[(startBitOffset+sz)/8:]
	}

	return loadedData, nil
}

func (c *Slice) MustLoadAddr() *address.Address {
	a, err := c.LoadAddr()
	if err != nil {
		panic(err)
	}
	return a
}

func (c *Slice) LoadAddr() (*address.Address, error) {
	typ, err := c.LoadUInt(2)
	if err != nil {
		return nil, err
	}

	switch typ {
	case 0:
		return address.NewAddressNone(), nil
	case 1:
		ln, err := c.LoadUInt(9)
		if err != nil {
			return nil, fmt.Errorf("failed to load len: %w", err)
		}

		data, err := c.LoadSlice(uint(ln))
		if err != nil {
			return nil, fmt.Errorf("failed to load addr data: %w", err)
		}

		return address.NewAddressExt(0, uint(ln), data), nil
	case 2:
		isAnycast, err := c.LoadBoolBit()
		if err != nil {
			return nil, fmt.Errorf("failed to load anycast bit: %w", err)
		}

		if isAnycast {
			depthLen := uint(math.Ceil(math.Log2(30)))

			depth, err := c.LoadUInt(depthLen)
			if err != nil {
				return nil, fmt.Errorf("failed to load depth: %w", err)
			}

			pfx, err := c.LoadSlice(uint(depth))
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
	case 3:
		isAnycast, err := c.LoadBoolBit()
		if err != nil {
			return nil, fmt.Errorf("failed to load anycast bit: %w", err)
		}

		if isAnycast {
			depthLen := uint(math.Ceil(math.Log2(30)))

			depth, err := c.LoadUInt(depthLen)
			if err != nil {
				return nil, fmt.Errorf("failed to load depth: %w", err)
			}

			pfx, err := c.LoadSlice(uint(depth))
			if err != nil {
				return nil, fmt.Errorf("failed to load prefix: %w", err)
			}
			_ = pfx

			// TODO: save anycast
		}

		ln, err := c.LoadUInt(9)
		if err != nil {
			return nil, fmt.Errorf("failed to load len: %w", err)
		}

		workchain, err := c.LoadInt(32)
		if err != nil {
			return nil, fmt.Errorf("failed to load workchain: %w", err)
		}

		data, err := c.LoadSlice(uint(ln))
		if err != nil {
			return nil, fmt.Errorf("failed to load addr data: %w", err)
		}

		return address.NewAddressVar(0, int32(workchain), uint(ln), data), nil
	default:
		return nil, errors.New("not supported type of address")
	}
}

func (c *Slice) MustLoadStringSnake() string {
	a, err := c.LoadStringSnake()
	if err != nil {
		panic(err)
	}
	return a
}

func (c *Slice) MustLoadBinarySnake() []byte {
	a, err := c.LoadBinarySnake()
	if err != nil {
		panic(err)
	}
	return a
}

func (c *Slice) LoadStringSnake() (string, error) {
	a, err := c.LoadBinarySnake()
	if err != nil {
		return "", err
	}
	return string(a), nil
}

func (c *Slice) LoadBinarySnake() ([]byte, error) {
	var data []byte

	ref := c
	for ref != nil {
		b, err := ref.LoadSlice(ref.BitsLeft())
		if err != nil {
			return nil, err
		}
		data = append(data, b...)

		if ref.RefsNum() > 1 {
			return nil, fmt.Errorf("more than one ref, it is not snake string")
		}

		if ref.RefsNum() == 1 {
			ref = ref.MustLoadRef()
			continue
		}
		ref = nil
	}

	return data, nil
}

func (c *Slice) IsSpecial() bool {
	return c.special
}

func (c *Slice) BitsLeft() uint {
	return c.bitsSz - c.loadedSz
}

func (c *Slice) RestBits() (uint, []byte, error) {
	left := c.bitsSz - c.loadedSz
	data, err := c.LoadSlice(left)
	return left, data, err
}

func (c *Slice) MustToCell() *Cell {
	cl, err := c.ToCell()
	if err != nil {
		panic(err)
	}
	return cl
}

func (c *Slice) Copy() *Slice {
	// Copy byte storage to preserve snapshot semantics even if the originating
	// cell was constructed through RawUnsafe helpers and its backing bytes are
	// mutated later.
	data := append([]byte{}, c.data...)

	return &Slice{
		special:   c.special,
		levelMask: c.levelMask,
		bitsSz:    c.bitsSz,
		loadedSz:  c.loadedSz,
		data:      data,
		refs:      c.refs,
		observer:  c.observer,
	}
}

func (c *Slice) ToBuilder() *Builder {
	left := c.bitsSz - c.loadedSz
	return &Builder{
		bitsSz:   left,
		data:     c.MustPreloadSlice(left),
		refs:     c.refs,
		observer: c.observer,
	}
}

func (c *Slice) ToCell() (*Cell, error) {
	left := c.bitsSz - c.loadedSz
	data, err := c.PreloadSlice(left)
	if err != nil {
		return nil, err
	}

	cl := &Cell{
		special:   c.special,
		levelMask: c.levelMask,
		bitsSz:    left,
		data:      data,
		refs:      c.refs,
	}
	cl.calculateHashes()
	if c.observer != nil {
		c.observer.OnCellCreate()
	}
	return cl, nil
}

func (c *Slice) String() string {
	cl, err := c.WithoutObserver().ToCell()
	if err != nil {
		return "<invalid>"
	}
	return cl.String()
}

func (c *Slice) SetObserver(observer Observer) *Slice {
	c.observer = observer
	return c
}

func (c *Slice) WithoutObserver() *Slice {
	cp := c.Copy()
	cp.observer = nil
	return cp
}
