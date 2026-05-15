package cell

import (
	"errors"
	"fmt"
	"math/big"
	"math/bits"

	"github.com/xssnick/tonutils-go/address"
)

type Slice struct {
	cell  *Cell
	trace *Trace

	bitStart uint16
	bitEnd   uint16
	refStart uint8
	refEnd   uint8
}

func (c *Slice) refCellAt(i int) (*Cell, error) {
	refView := newCellRefView(c.cell)
	return refView.boundaryRef(int(c.refStart) + i)
}

func (c *Slice) boundaryRefCellAt(i int) *Cell {
	refView := newCellRefView(c.cell)
	ref, _ := refView.boundaryRef(int(c.refStart) + i)
	return ref
}

func (c *Slice) peekRefCellAt(i int) (*Cell, error) {
	if i < 0 {
		return nil, ErrNegative
	}
	if i >= c.RefsNum() {
		return nil, ErrNoMoreRefs
	}

	ref, err := c.refCellAt(i)
	if err != nil {
		return nil, err
	}
	return c.withChildTrace(ref, int(c.refStart)+i), nil
}

func (c *Slice) withChildTrace(ref *Cell, refIdx int) *Cell {
	if c.trace == nil || ref == nil {
		return ref
	}
	return ref.WithTrace(c.trace.Child(refIdx))
}

func (c *Slice) loadRefCell(advance bool) (*Cell, error) {
	ref, err := c.peekRefCellAt(0)
	if err != nil {
		return nil, err
	}

	if advance {
		c.refStart++
	}
	return ref, nil
}

func (c *Slice) loadBoundaryRefCell() (*Cell, error) {
	if c.RefsNum() < 1 {
		return nil, ErrNoMoreRefs
	}

	refIdx := int(c.refStart)
	ref := c.withChildTrace(c.boundaryRefCellAt(0), refIdx)

	c.refStart++
	return ref, nil
}

func (c *Slice) MustLoadRef() *Slice {
	r, err := c.LoadRef()
	if err != nil {
		panic(err)
	}
	return r
}

func (c *Slice) LoadRef() (*Slice, error) {
	ref, err := c.loadRefCell(true)
	if err != nil {
		return nil, err
	}
	return ref.BeginParse()
}

func (c *Slice) PreloadRef() (*Slice, error) {
	ref, err := c.loadRefCell(false)
	if err != nil {
		return nil, err
	}
	return ref.BeginParse()
}

func (c *Slice) LoadRefCell() (*Cell, error) {
	return c.loadRefCell(true)
}

func (c *Slice) PreloadRefCell() (*Cell, error) {
	return c.loadRefCell(false)
}

func (c *Slice) PeekRefCell() (*Cell, error) {
	return c.peekRefCellAt(0)
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

	ref, err := c.loadRefCell(true)
	if err != nil {
		return nil, err
	}

	return ref.BeginParse()
}

func (c *Slice) RefsNum() int {
	return int(c.refEnd - c.refStart)
}

func (c *Slice) BitAt(offset uint) (uint8, error) {
	left := c.BitsLeft()
	if offset >= left {
		return 0, ErrNotEnoughData(int(left), int(offset+1))
	}

	bitPos := uint(c.bitStart) + offset
	byteIdx := bitPos / 8
	bitIdx := 7 - (bitPos % 8)
	return (c.cell.data[byteIdx] >> bitIdx) & 1, nil
}

func (c *Slice) SkipBits(bits uint) error {
	left := c.BitsLeft()
	if left < bits {
		return ErrNotEnoughData(int(left), int(bits))
	}

	c.bitStart += uint16(bits)
	return nil
}

func (c *Slice) SkipBitsAndRefs(bits uint, refs int) error {
	if refs < 0 || c.RefsNum() < refs {
		return ErrNoMoreRefs
	}

	left := c.BitsLeft()
	if left < bits {
		return ErrNotEnoughData(int(left), int(bits))
	}

	c.bitStart += uint16(bits)
	c.refStart += uint8(refs)
	return nil
}

func (c *Slice) FetchSubslice(bits uint, refs int) (*Slice, error) {
	if refs < 0 || c.RefsNum() < refs {
		return nil, ErrNoMoreRefs
	}

	left := c.BitsLeft()
	if left < bits {
		return nil, ErrNotEnoughData(int(left), int(bits))
	}
	out := &Slice{
		cell:     c.cell,
		trace:    c.trace,
		bitStart: c.bitStart,
		bitEnd:   c.bitStart + uint16(bits),
		refStart: c.refStart,
		refEnd:   c.refStart + uint8(refs),
	}
	c.bitStart += uint16(bits)
	c.refStart += uint8(refs)
	return out, nil
}

func (c *Slice) PreloadSubslice(bits uint, refs int) (*Slice, error) {
	if refs < 0 || c.RefsNum() < refs {
		return nil, ErrNoMoreRefs
	}

	left := c.BitsLeft()
	if left < bits {
		return nil, ErrNotEnoughData(int(left), int(bits))
	}
	return &Slice{
		cell:     c.cell,
		trace:    c.trace,
		bitStart: c.bitStart,
		bitEnd:   c.bitStart + uint16(bits),
		refStart: c.refStart,
		refEnd:   c.refStart + uint8(refs),
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
	cp := *c
	ln, err := cp.LoadUInt(4)
	if err != nil {
		return 0, err
	}
	if ln >= 16 {
		return 0, ErrTooBigValue
	}

	if ln <= 8 {
		value, err := cp.LoadUInt(uint(ln * 8))
		if err != nil {
			return 0, err
		}
		c.bitStart = cp.bitStart
		c.refStart = cp.refStart
		return value, nil
	}

	value, err := cp.LoadBigUInt(uint(ln * 8))
	if err != nil {
		return 0, err
	}
	if !value.IsUint64() {
		return 0, ErrTooBigValue
	}
	c.bitStart = cp.bitStart
	c.refStart = cp.refStart
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

	cp := c.Copy()
	res, err := cp.LoadBigUInt(sz)
	if err != nil {
		return 0, err
	}
	if !res.IsUint64() {
		return 0, ErrTooBigValue
	}
	c.bitStart = cp.bitStart
	c.refStart = cp.refStart
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
	if !res.IsUint64() {
		return 0, ErrTooBigValue
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

	cp := c.Copy()
	res, err := cp.LoadBigInt(sz)
	if err != nil {
		return 0, err
	}
	if !res.IsInt64() {
		return 0, ErrTooBigValue
	}
	c.bitStart = cp.bitStart
	c.refStart = cp.refStart
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
	if c.bitStart >= c.bitEnd {
		return false, ErrNotEnoughData(0, 1)
	}

	bitPos := c.bitStart
	bit := (c.cell.data[bitPos/8] >> (7 - (bitPos % 8))) & 1
	c.bitStart++
	return bit == 1, nil
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
	left := c.BitsLeft()
	if left < sz {
		return 0, ErrNotEnoughData(int(left), int(sz))
	}

	if sz == 0 {
		return 0, nil
	}

	startBit := uint(c.bitStart)
	if sz == 1 {
		value := uint64((c.cell.data[startBit/8] >> (7 - (startBit % 8))) & 1)
		if !preload {
			c.bitStart++
		}
		return value, nil
	}

	startBitOffset := startBit % 8
	byteStart := int(startBit / 8)
	bytesNeeded := int((startBitOffset + sz + 7) / 8)
	data := c.cell.data[byteStart : byteStart+bytesNeeded]

	var value uint64
	if startBitOffset == 0 && sz%8 == 0 {
		for _, b := range data {
			value = (value << 8) | uint64(b)
		}
		if !preload {
			c.bitStart += uint16(sz)
		}
		return value, nil
	}

	if bytesNeeded <= 8 {
		for _, b := range data {
			value = (value << 8) | uint64(b)
		}
		value >>= uint(bytesNeeded*8) - startBitOffset - sz
		if sz < 64 {
			value &= (uint64(1) << sz) - 1
		}
	} else {
		for _, b := range data[:8] {
			value = (value << 8) | uint64(b)
		}
		value <<= startBitOffset
		value |= uint64(data[8] >> (8 - startBitOffset))
		if sz < 64 {
			value >>= 64 - sz
		}
	}

	if !preload {
		c.bitStart += uint16(sz)
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
	if sz == 0 {
		return big.NewInt(0), nil
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
	if sz == 0 {
		return nil, ErrInvalidSize
	}

	lnBits := uint(bits.Len64(uint64(sz - 1)))
	ln, err := c.LoadUInt(lnBits)
	if err != nil {
		return nil, err
	}
	if ln >= uint64(sz) {
		return nil, ErrTooBigValue
	}

	value, err := c.LoadBigUInt(uint(ln * 8))
	if err != nil {
		return nil, err
	}

	return value, nil
}

func (c *Slice) LoadVarInt(sz uint) (*big.Int, error) {
	if sz == 0 {
		return nil, ErrInvalidSize
	}

	lnBits := uint(bits.Len64(uint64(sz - 1)))
	ln, err := c.LoadUInt(lnBits)
	if err != nil {
		return nil, err
	}
	if ln >= uint64(sz) {
		return nil, ErrTooBigValue
	}

	value, err := c.LoadBigInt(uint(ln * 8))
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

func (c *Slice) MustLoadVarInt(sz uint) *big.Int {
	s, err := c.LoadVarInt(sz)
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
	left := c.BitsLeft()
	if left < sz {
		return nil, ErrNotEnoughData(int(left), int(sz))
	}

	if sz == 0 {
		return []byte{}, nil
	}

	startBit := uint(c.bitStart)
	startBitOffset := startBit % 8
	data := c.cell.data[startBit/8:]
	outLen := int((sz + 7) / 8)
	loadedData := make([]byte, outLen)

	if startBitOffset == 0 {
		copy(loadedData, data[:outLen])
	} else {
		shift := uint8(startBitOffset)
		invShift := uint8(8 - startBitOffset)
		last := outLen - 1
		sourceBytesNeeded := int((startBitOffset + sz + 7) / 8)

		for i := 0; i < last; i++ {
			loadedData[i] = data[i]<<shift | data[i+1]>>invShift
		}

		loadedData[last] = data[last] << shift
		if sourceBytesNeeded > outLen {
			loadedData[last] |= data[last+1] >> invShift
		}
	}

	if rem := sz % 8; rem != 0 {
		loadedData[outLen-1] &= byte(0xFF << (8 - rem))
	}

	if !preload {
		c.bitStart += uint16(sz)
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
		var anycast *address.Anycast
		isAnycast, err := c.LoadBoolBit()
		if err != nil {
			return nil, fmt.Errorf("failed to load anycast bit: %w", err)
		}

		if isAnycast {
			depth, err := c.LoadUInt(5)
			if err != nil {
				return nil, fmt.Errorf("failed to load depth: %w", err)
			}
			if depth == 0 || depth > 30 {
				return nil, fmt.Errorf("invalid anycast depth: %d", depth)
			}

			pfx, err := c.LoadSlice(uint(depth))
			if err != nil {
				return nil, fmt.Errorf("failed to load prefix: %w", err)
			}
			anycast = address.NewAnycast(uint(depth), pfx)
		}

		workchain, err := c.LoadUInt(8)
		if err != nil {
			return nil, fmt.Errorf("failed to load workchain: %w", err)
		}

		data, err := c.LoadSlice(256)
		if err != nil {
			return nil, fmt.Errorf("failed to load addr data: %w", err)
		}

		return address.NewAddress(0, byte(workchain), data).WithAnycast(anycast), nil
	case 3:
		var anycast *address.Anycast
		isAnycast, err := c.LoadBoolBit()
		if err != nil {
			return nil, fmt.Errorf("failed to load anycast bit: %w", err)
		}

		if isAnycast {
			depth, err := c.LoadUInt(5)
			if err != nil {
				return nil, fmt.Errorf("failed to load depth: %w", err)
			}
			if depth == 0 || depth > 30 {
				return nil, fmt.Errorf("invalid anycast depth: %d", depth)
			}

			pfx, err := c.LoadSlice(uint(depth))
			if err != nil {
				return nil, fmt.Errorf("failed to load prefix: %w", err)
			}
			anycast = address.NewAnycast(uint(depth), pfx)
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

		return address.NewAddressVar(0, int32(workchain), uint(ln), data).WithAnycast(anycast), nil
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
	return c.cell.IsSpecial()
}

func (c *Slice) BitsLeft() uint {
	return uint(c.bitEnd - c.bitStart)
}

func (c *Slice) RestBits() (uint, []byte, error) {
	left := c.BitsLeft()
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
	return &Slice{
		cell:     c.cell,
		trace:    c.trace,
		bitStart: c.bitStart,
		bitEnd:   c.bitEnd,
		refStart: c.refStart,
		refEnd:   c.refEnd,
	}
}

func (c *Slice) BaseCell() *Cell {
	if c.trace != nil {
		return c.cell.WithTrace(c.trace)
	}
	return c.cell
}

func (c *Slice) BitRange() (start, end uint) {
	return uint(c.bitStart), uint(c.bitEnd)
}

func (c *Slice) RefRange() (start, end int) {
	return int(c.refStart), int(c.refEnd)
}

func (c *Slice) ToBuilder() *Builder {
	left := c.BitsLeft()
	var b Builder
	b.bitsSz = left
	b.trace = c.trace

	copy(b.data[:], c.MustPreloadSlice(left))

	b.refsNum = uint8(c.RefsNum())
	for i := uint8(0); i < b.refsNum; i++ {
		refIdx := int(c.refStart) + int(i)
		b.refs[i] = c.withChildTrace(c.boundaryRefCellAt(int(i)), refIdx)
	}

	return &b
}

func (c *Slice) ToCell() (*Cell, error) {
	left := c.BitsLeft()
	data, err := c.PreloadSlice(left)
	if err != nil {
		return nil, err
	}

	refs := make([]*Cell, c.RefsNum())
	for i := range refs {
		refs[i] = c.withChildTrace(c.boundaryRefCellAt(i), int(c.refStart)+i)
	}

	cl := &Cell{
		bitsSz: uint16(left),
		data:   data,
	}
	cl.setRefs(refs)
	fullCell := c.bitStart == 0 &&
		c.bitEnd == c.cell.bitsSz &&
		c.refStart == 0 &&
		int(c.refEnd) == c.cell.refsCount()
	if c.cell.IsSpecial() && fullCell {
		cl.setSpecial(true)
		if err = refreshSpecialCellLevelMask(cl); err != nil {
			return nil, err
		}
	} else if fullCell {
		cl.setLevelMask(c.cell.getLevelMask())
	} else {
		cl.setLevelMask(ordinaryLevelMask(refs))
	}
	if err = c.trace.NotifyCreate(); err != nil {
		return nil, err
	}
	if err := validateCellRefDepthLimit(refs); err != nil {
		return nil, err
	}
	if err := validateBoundaryCell(cl); err != nil {
		return nil, err
	}
	if err := cl.calculateHashes(); err != nil {
		return nil, err
	}
	return cl, nil
}

func (c *Slice) String() string {
	cl, err := c.WithoutTrace().ToCell()
	if err != nil {
		return "<invalid>"
	}
	return cl.String()
}

func (c *Slice) SetTrace(trace *Trace) *Slice {
	c.trace = trace
	return c
}

func (c *Slice) Trace() *Trace {
	if c == nil {
		return nil
	}
	return c.trace
}

func (c *Slice) WithoutTrace() *Slice {
	cp := c.Copy()
	cp.trace = nil
	return cp
}
