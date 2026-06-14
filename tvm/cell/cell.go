package cell

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

type Type uint8

const (
	OrdinaryCellType     Type = 0x00
	PrunedCellType       Type = 0x01
	LibraryCellType      Type = 0x02
	MerkleProofCellType  Type = 0x03
	MerkleUpdateCellType Type = 0x04
	UnknownCellType      Type = 0xFF
)

type Hash [hashSize]byte

const maxDepth = 1024
const maxCellDataBytes = 128

type Cell struct {
	data  []byte
	hash0 Hash
	refs  [4]*Cell
	meta  *cellMeta

	bitsSz uint16
	depth0 uint16
	flags  uint8
}

func (c *Cell) copy() *Cell {
	cp := *c
	cp.meta = cloneCellMeta(c.meta)
	return &cp
}

func (c *Cell) load() (*Cell, error) {
	return c.loadWithTrace(c.Trace())
}

func (c *Cell) loadWithTrace(trace *Trace) (*Cell, error) {
	if c == nil || !c.IsLazy() {
		return c, nil
	}
	return loadLazyPrunedRefWithTrace(c, trace)
}

func (c *Cell) BeginParse() (*Slice, error) {
	return c.beginParseWithTrace(c.Trace())
}

// Prewarm prepares the lazy Cell by loading its data and returns the loaded Cell or an error on failure.
func (c *Cell) Prewarm() (*Cell, error) {
	loaded, err := c.loadWithTrace(c.Trace())
	if err != nil {
		return nil, err
	}
	return loaded, nil
}

func (c *Cell) BeginParseWithoutTrace() (*Slice, error) {
	return c.beginParseWithTrace(nil)
}

// BeginParseWithTrace parses the cell using trace instead of the trace attached
// to the cell wrapper.
func (c *Cell) BeginParseWithTrace(trace *Trace) (*Slice, error) {
	return c.beginParseWithTrace(trace)
}

func (c *Cell) beginParseWithTrace(trace *Trace) (*Slice, error) {
	loaded, err := c.loadWithTrace(trace)
	if err != nil {
		return nil, err
	}

	trace.NotifyLoad(loaded)
	return newSliceFromCell(loaded, trace), nil
}

func (c *Cell) MustBeginParse() *Slice {
	s, err := c.BeginParse()
	if err != nil {
		panic(err)
	}
	return s
}

func (c *Cell) Trace() *Trace {
	if c == nil || c.meta == nil {
		return nil
	}
	return c.meta.trace
}

func (c *Cell) WithTrace(trace *Trace) *Cell {
	if c == nil {
		return nil
	}
	if c.Trace() == trace {
		return c
	}
	cp := c.copy()
	if trace != nil {
		cp.ensureMeta().trace = trace
		return cp
	}
	if cp.meta != nil {
		cp.meta.trace = nil
		cp.clearMetaIfEmpty()
	}
	return cp
}

func (c *Cell) WithoutTrace() *Cell {
	return c.WithTrace(nil)
}

func (c *Cell) withTraceCombined(trace *Trace) *Cell {
	if c == nil || trace == nil {
		return c
	}
	current := c.Trace()
	combined := CombineTraces(current, trace)
	if combined == current {
		return c
	}
	return c.WithTrace(combined)
}

func (c *Cell) ToBuilder() *Builder {
	refCnt := c.refsCount()
	var b Builder
	b.bitsSz = uint(c.bitsSz)
	copy(b.data[:], c.data)
	if rem := b.bitsSz % 8; rem != 0 {
		b.data[b.bitsSz/8] &= byte(0xFF << (8 - rem))
	}

	b.refsNum = uint8(refCnt)
	copy(b.refs[:], c.refs[:refCnt])

	return &b
}

func (c *Cell) BitsSize() uint {
	return uint(c.bitsSz)
}

func (c *Cell) RefsNum() uint {
	return uint(c.refsCount())
}

func (c *Cell) MustPeekRef(i int) *Cell {
	ref, err := c.PeekRef(i)
	if err != nil {
		panic(err)
	}
	return ref
}

func (c *Cell) PeekRef(i int) (*Cell, error) {
	if i >= c.refsCount() {
		return nil, ErrNoMoreRefs
	}
	if i < 0 {
		return nil, ErrNegative
	}

	refView := newCellRefView(c)
	ref, err := refView.boundaryRef(i)
	if err != nil || ref == nil || c.Trace() == nil {
		return ref, err
	}
	return ref.WithTrace(c.Trace().Child(i)), nil
}

func (c *Cell) Dump(limitLength ...int) string {
	var lim = uint64(1024<<20) * 16
	if len(limitLength) > 0 {
		// 16 MB default lim
		lim = uint64(limitLength[0])
	}
	return c.dump(0, false, lim)
}

func (c *Cell) DumpBits(limitLength ...int) string {
	var lim = uint64(1024<<20) * 16
	if len(limitLength) > 0 {
		// 16 MB default lim
		lim = uint64(limitLength[0])
	}
	return c.dump(0, true, lim)
}

func (c *Cell) dump(deep int, bin bool, limitLength uint64) string {
	s, err := c.WithoutTrace().BeginParse()
	if err != nil {
		return strings.Repeat("  ", deep) + "<failed to load cell: " + err.Error() + ">"
	}
	base := s.BaseCell()
	sz, data, _ := s.RestBits()

	builder := strings.Builder{}

	if bin {
		for _, n := range data {
			builder.WriteString(fmt.Sprintf("%08b", n))
		}
		if sz%8 != 0 {
			tmp := builder.String()
			builder.Reset()
			builder.WriteString(tmp[:uint(len(tmp))-(8-(sz%8))])
		}
	} else {
		tmp := make([]byte, len(data)*2)
		hex.Encode(tmp, data)
		builder.WriteString(strings.ToUpper(string(tmp)))

		if sz%8 <= 4 && sz%8 > 0 {
			tmp := builder.String()
			builder.Reset()
			builder.WriteString(tmp[:len(tmp)-1])
			builder.WriteByte('_')

		}
	}

	val := builder.String()
	builder.Reset()
	builder.WriteString(strings.Repeat("  ", deep))
	builder.WriteString(strconv.FormatUint(uint64(sz), 10))
	builder.WriteByte('[')
	builder.WriteString(val)
	builder.WriteByte(']')

	level := base.getLevelMask().GetLevel()
	if level > 0 {
		builder.WriteByte('{')
		builder.WriteString(strconv.Itoa(level))
		builder.WriteByte('}')

	}
	if base.IsSpecial() {
		builder.WriteByte('*')
	}
	refCnt := base.refsCount()
	if refCnt > 0 {

		builder.WriteString(" -> {")

		for i, ref := range base.boundaryRefs() {

			builder.WriteByte('\n')
			builder.WriteString(ref.dump(deep+1, bin, limitLength))

			if i == refCnt-1 {
				builder.WriteByte('\n')
			} else {
				builder.WriteByte(',')
			}

			if uint64(builder.Len()) > limitLength {
				break
			}
		}
		builder.WriteString(strings.Repeat("  ", deep))
		builder.WriteByte('}')
	}

	if uint64(builder.Len()) > limitLength {
		tmp := builder.String()
		builder.Reset()
		builder.WriteString(tmp[:limitLength])
	}

	return builder.String()
}

const _DataCellMaxLevel = 3

// Hash - calculates a hash of cell recursively
// Once calculated, it is cached and can be reused cheap.
func (c *Cell) Hash(level ...int) []byte {
	hash := make([]byte, hashSize)
	if len(level) > 0 {
		copy(hash, c.getHash(level[0]))
		return hash
	}
	copy(hash, c.getHash(_DataCellMaxLevel))
	return hash
}

func (c *Cell) HashKey(level ...int) Hash {
	var key Hash
	if len(level) > 0 {
		copy(key[:], c.getHash(level[0]))
		return key
	}
	copy(key[:], c.getHash(_DataCellMaxLevel))
	return key
}

func (c *Cell) Depth(level ...int) uint16 {
	if len(level) > 0 {
		return c.getDepth(level[0])
	}
	return c.getDepth(_DataCellMaxLevel)
}

func (c *Cell) Sign(key ed25519.PrivateKey) []byte {
	return ed25519.Sign(key, c.Hash())
}

func (c *Cell) Verify(key ed25519.PublicKey, signature []byte) bool {
	return ed25519.Verify(key, c.Hash(), signature)
}

func (c *Cell) GetType() Type {
	if !c.IsSpecial() {
		return OrdinaryCellType
	}
	if c.BitsSize() < 8 {
		return UnknownCellType
	}

	switch Type(c.data[0]) {
	case PrunedCellType:
		if c.BitsSize() >= 288 {
			msk := LevelMask{c.data[1]}
			lvl := msk.GetLevel()
			if c.IsLazy() && lvl == 0 && c.RefsNum() == 0 && c.BitsSize() == 16+(256+16) {
				return PrunedCellType
			}
			if lvl > 0 && lvl <= 3 && c.BitsSize() >= 16+(256+16)*(uint(msk.Apply(lvl-1).getHashIndex()+1)) {
				return PrunedCellType
			}
		}
	case MerkleProofCellType:
		if c.RefsNum() == 1 && c.BitsSize() == 280 {
			return MerkleProofCellType
		}
	case MerkleUpdateCellType:
		if c.RefsNum() == 2 && c.BitsSize() == 552 {
			return MerkleUpdateCellType
		}
	case LibraryCellType:
		if c.BitsSize() == 8+256 {
			return LibraryCellType
		}
	}
	return UnknownCellType
}

func (c *Cell) UnmarshalJSON(bytes []byte) error {
	if len(bytes) < 2 || bytes[0] != '"' || bytes[len(bytes)-1] != '"' {
		return fmt.Errorf("invalid cell data")
	}
	bytes = bytes[1 : len(bytes)-1]

	if len(bytes) == 0 {
		return nil
	}

	data := make([]byte, base64.StdEncoding.DecodedLen(len(bytes)))

	n, err := base64.StdEncoding.Decode(data, bytes)
	if err != nil {
		return err
	}
	data = data[:n]

	cl, err := FromBOC(data)
	if err != nil {
		return err
	}
	*c = *cl
	return nil
}

func (c *Cell) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(base64.StdEncoding.EncodeToString(c.ToBOC()))), nil
}

func (c *Cell) String() string {
	return c.Dump()
}
