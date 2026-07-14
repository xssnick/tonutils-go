package cell

import (
	"crypto/ed25519"
	"encoding/base64"
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
	// typ caches the resolved cell type in the struct padding, see resolveType.
	typ uint8
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
	s := new(Slice)
	if err := c.beginParseIntoWithTrace(s, trace); err != nil {
		return nil, err
	}
	return s, nil
}

func (c *Cell) beginParseIntoWithTrace(dst *Slice, trace *Trace) error {
	// Keep the active trace on the Slice so reference descent does not clone
	// otherwise immutable cells just to attach trace metadata.
	loaded, err := c.loadWithTrace(nil)
	if err != nil {
		return err
	}

	trace.NotifyLoad(loaded)
	*dst = Slice{
		cell:   loaded,
		trace:  trace,
		bitEnd: loaded.bitsSz,
		refEnd: uint8(loaded.refsCount()),
	}
	return nil
}

// BeginParseInto is the value-form of BeginParse for tight descent loops:
// the caller owns dst and reuses it across iterations, so parsing does not
// allocate a new Slice per visited cell. Traces attached to the cell are
// honored the same way BeginParse does.
func (c *Cell) BeginParseInto(dst *Slice) error {
	return c.beginParseIntoWithTrace(dst, c.Trace())
}

// BeginParseIntoWithoutTrace is the value-form of BeginParseWithoutTrace:
// like BeginParseInto, but without attaching any trace to the parsed Slice.
func (c *Cell) BeginParseIntoWithoutTrace(dst *Slice) error {
	return c.beginParseIntoWithTrace(dst, nil)
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
	trace := c.Trace()
	if err != nil || ref == nil || trace == nil {
		return ref, err
	}
	return ref.WithTrace(trace.Child(i)), nil
}

const defaultDumpLimit = uint64(16 << 30)

func (c *Cell) Dump(limitLength ...int) string {
	lim := defaultDumpLimit
	if len(limitLength) > 0 {
		lim = uint64(limitLength[0])
	}
	return c.dump(0, false, lim)
}

func (c *Cell) DumpBits(limitLength ...int) string {
	lim := defaultDumpLimit
	if len(limitLength) > 0 {
		lim = uint64(limitLength[0])
	}
	return c.dump(0, true, lim)
}

func (c *Cell) dump(deep int, bin bool, limitLength uint64) string {
	w := dumpWriter{limit: limitLength}
	c.writeDump(&w, deep, bin)
	return w.builder.String()
}

type dumpWriter struct {
	builder strings.Builder
	limit   uint64
}

func (w *dumpWriter) done() bool {
	return uint64(w.builder.Len()) >= w.limit
}

func (w *dumpWriter) writeByte(v byte) bool {
	if w.done() {
		return false
	}
	w.builder.WriteByte(v)
	return !w.done()
}

func (w *dumpWriter) writeString(v string) bool {
	if w.done() {
		return false
	}

	left := w.limit - uint64(w.builder.Len())
	if uint64(len(v)) > left {
		w.builder.WriteString(v[:int(left)])
		return false
	}
	w.builder.WriteString(v)
	return !w.done()
}

func (w *dumpWriter) writeIndent(deep int) bool {
	const spaces = "                                                                "

	left := deep * 2
	for left > 0 {
		chunk := min(left, len(spaces))
		if !w.writeString(spaces[:chunk]) {
			return false
		}
		left -= chunk
	}
	return true
}

func (c *Cell) writeDump(w *dumpWriter, deep int, bin bool) {
	var s Slice
	if err := c.beginParseIntoWithTrace(&s, nil); err != nil {
		// Keep the legacy root-error behavior: the limit historically applied
		// only after a cell was loaded successfully.
		if deep == 0 && w.builder.Len() == 0 {
			w.builder.WriteString("<failed to load cell: " + err.Error() + ">")
			return
		}
		w.writeIndent(deep)
		w.writeString("<failed to load cell: " + err.Error() + ">")
		return
	}
	base := s.cell
	sz := s.BitsLeft()

	if !w.writeIndent(deep) ||
		!w.writeString(strconv.FormatUint(uint64(sz), 10)) ||
		!w.writeByte('[') {
		return
	}
	if bin {
		for bit := uint(0); bit < sz; bit++ {
			value := (base.data[bit/8] >> (7 - bit%8)) & 1
			if !w.writeByte('0' + value) {
				return
			}
		}
	} else if !writeDumpHex(w, base.data, sz) {
		return
	}
	if !w.writeByte(']') {
		return
	}

	level := base.getLevelMask().GetLevel()
	if level > 0 {
		if !w.writeByte('{') || !w.writeString(strconv.Itoa(level)) || !w.writeByte('}') {
			return
		}
	}
	if base.IsSpecial() && !w.writeByte('*') {
		return
	}

	refCnt := base.refsCount()
	if refCnt == 0 || !w.writeString(" -> {") {
		return
	}

	refs := newCellRefView(base)
	for i := 0; i < refCnt; i++ {
		if !w.writeByte('\n') {
			return
		}
		ref, err := refs.boundaryRef(i)
		if err != nil {
			return
		}
		ref.writeDump(w, deep+1, bin)
		if w.done() {
			return
		}

		if i == refCnt-1 {
			if !w.writeByte('\n') {
				return
			}
		} else if !w.writeByte(',') {
			return
		}
	}
	w.writeIndent(deep)
	w.writeByte('}')
}

func writeDumpHex(w *dumpWriter, data []byte, bits uint) bool {
	const alphabet = "0123456789ABCDEF"

	bytesLen := (bits + 7) / 8
	rem := bits % 8
	for i := uint(0); i < bytesLen; i++ {
		value := data[i]
		if i == bytesLen-1 && rem != 0 {
			value &= byte(0xFF << (8 - rem))
		}
		if !w.writeByte(alphabet[value>>4]) {
			return false
		}
		if i == bytesLen-1 && rem > 0 && rem <= 4 {
			if !w.writeByte('_') {
				return false
			}
			continue
		}
		if !w.writeByte(alphabet[value&0x0F]) {
			return false
		}
	}
	return true
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
	if t := c.typ; t != cellTypeCacheNone {
		return decodeCellTypeCache(t)
	}
	return c.computeType()
}

// resolveType computes the cell type and caches it. It must only be called
// while the cell is exclusively owned (during finalization), never on cells
// already shared for parallel reads.
func (c *Cell) resolveType() Type {
	t := c.computeType()
	c.typ = encodeCellTypeCache(t)
	return t
}

func (c *Cell) computeType() Type {
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
