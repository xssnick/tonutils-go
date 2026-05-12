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

type TraceNode uint32

type Observer interface {
	OnCellLoad(hash Hash)
	OnCellCreate()

	OnRef(parent TraceNode, refIdx int) TraceNode
}

type pendingObserver interface {
	PendingError() error
}

func notifyCellCreate(observer Observer) error {
	if observer == nil {
		return nil
	}
	observer.OnCellCreate()
	if pending, ok := observer.(pendingObserver); ok {
		return pending.PendingError()
	}
	return nil
}

func notifyCellLoad(observer Observer, c *Cell) {
	if observer == nil || c == nil {
		return
	}
	observer.OnCellLoad(c.HashKey())
}

func beginParseObservedNode(c *Cell, observer Observer, node TraceNode) *Slice {
	if observer == nil {
		return c.BeginParse()
	}
	notifyCellLoad(observer, c)
	return &Slice{
		cell:      c,
		observer:  observer,
		traceNode: node,
		bitEnd:    c.bitsSz,
		refEnd:    uint8(c.refsCount()),
	}
}

func (c *Cell) copy() *Cell {
	refCnt := c.refsCount()
	cp := &Cell{
		data:   append([]byte{}, c.data...),
		meta:   cloneCellMeta(c.meta),
		bitsSz: c.bitsSz,
		depth0: c.depth0,
		flags:  c.flags,
	}
	copy(cp.hash0[:], c.hash0[:])
	copy(cp.refs[:], c.refs[:refCnt])
	return cp
}

func (c *Cell) cloneWithRefObserved(i int, ref *Cell, observer Observer) (*Cell, error) {
	cp := c.copy()
	cp.setRef(i, ref)
	if err := notifyCellCreate(observer); err != nil {
		return nil, err
	}
	if err := cp.refreshLevelMaskForRefs(); err != nil {
		return nil, err
	}
	if err := cp.calculateHashes(); err != nil {
		return nil, err
	}
	return cp, nil
}

func (c *Cell) BeginParse() *Slice {
	return &Slice{
		cell:   c,
		bitEnd: c.bitsSz,
		refEnd: uint8(c.refsCount()),
	}
}

func (c *Cell) ToBuilder() *Builder {
	refCnt := c.refsCount()
	var b Builder
	b.bitsSz = uint(c.bitsSz)
	copy(b.data[:], c.data)

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
	return refView.resolvedRef(i)
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
	sz, data, _ := c.BeginParse().RestBits()

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

	level := c.getLevelMask().GetLevel()
	if level > 0 {
		builder.WriteByte('{')
		builder.WriteString(strconv.Itoa(level))
		builder.WriteByte('}')

	}
	if c.IsSpecial() {
		builder.WriteByte('*')
	}
	refCnt := c.refsCount()
	if refCnt > 0 {

		builder.WriteString(" -> {")

		for i, ref := range c.boundaryRefs() {

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
	if len(level) > 0 {
		return append([]byte{}, c.getHash(level[0])...)
	}
	return append([]byte{}, c.getHash(_DataCellMaxLevel)...)
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
