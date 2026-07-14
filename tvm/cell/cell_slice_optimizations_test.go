package cell

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"testing"
)

func TestSliceBitsEqualSameOffsetExhaustive(t *testing.T) {
	for offset := uint(0); offset < 8; offset++ {
		for bitLen := uint(0); bitLen <= 1023; bitLen++ {
			dataLen := int((offset + bitLen + 7) / 8)
			leftData := make([]byte, dataLen)
			for i := range leftData {
				leftData[i] = byte(i*73 + int(offset)*29 + int(bitLen)*11)
			}
			rightData := bytes.Clone(leftData)

			left := rawBitSlice(leftData, offset, bitLen)
			right := rawBitSlice(rightData, offset, bitLen)

			// Bits outside the selected range must not affect equality.
			if offset > 0 {
				rightData[0] ^= 0x80
			}
			if end := offset + bitLen; bitLen > 0 && end%8 != 0 {
				rightData[len(rightData)-1] ^= 0x01
			}
			if got, want := left.BitsEqual(right), slowBitsEqual(left, right); got != want || !got {
				t.Fatalf("equal range mismatch at offset=%d bits=%d: got=%v want=%v", offset, bitLen, got, want)
			}

			if bitLen == 0 {
				continue
			}
			for _, pos := range uniqueBitPositions(bitLen) {
				absolute := offset + pos
				rightData[absolute/8] ^= 1 << (7 - absolute%8)
				if got, want := left.BitsEqual(right), slowBitsEqual(left, right); got != want || got {
					t.Fatalf("mismatch not found at offset=%d bits=%d pos=%d: got=%v want=%v", offset, bitLen, pos, got, want)
				}
				rightData[absolute/8] ^= 1 << (7 - absolute%8)
			}
		}
	}
}

func TestSliceBitsEqualDifferentOffsetsFallback(t *testing.T) {
	lengths := []uint{0, 1, 2, 7, 8, 9, 15, 16, 31, 32, 63, 64, 65, 127, 128, 255, 256, 511, 512, 1023}
	for leftOffset := uint(0); leftOffset < 8; leftOffset++ {
		for rightOffset := uint(0); rightOffset < 8; rightOffset++ {
			for _, bitLen := range lengths {
				leftData := make([]byte, (leftOffset+bitLen+7)/8)
				rightData := make([]byte, (rightOffset+bitLen+7)/8)
				for bit := uint(0); bit < bitLen; bit++ {
					value := byte((bit*17 + bitLen*3 + leftOffset*5 + rightOffset) % 11)
					if value < 5 {
						setRawBit(leftData, leftOffset+bit)
						setRawBit(rightData, rightOffset+bit)
					}
				}

				left := rawBitSlice(leftData, leftOffset, bitLen)
				right := rawBitSlice(rightData, rightOffset, bitLen)
				if got, want := left.BitsEqual(right), slowBitsEqual(left, right); got != want || !got {
					t.Fatalf("equal range mismatch at offsets=%d/%d bits=%d: got=%v want=%v", leftOffset, rightOffset, bitLen, got, want)
				}
				if bitLen == 0 {
					continue
				}

				pos := bitLen / 2
				absolute := rightOffset + pos
				rightData[absolute/8] ^= 1 << (7 - absolute%8)
				if got, want := left.BitsEqual(right), slowBitsEqual(left, right); got != want || got {
					t.Fatalf("mismatch not found at offsets=%d/%d bits=%d: got=%v want=%v", leftOffset, rightOffset, bitLen, got, want)
				}
			}
		}
	}
}

func TestSliceIntoAPIs(t *testing.T) {
	data := make([]byte, 130)
	for i := range data {
		data[i] = byte(i*37 + 13)
	}
	lengths := []uint{0, 1, 4, 7, 8, 9, 31, 64, 65, 255, 256, 511, 768, 1023}

	for offset := uint(0); offset < 8; offset++ {
		for _, bitLen := range lengths {
			t.Run(sliceIntoTestName(offset, bitLen), func(t *testing.T) {
				s := rawBitSlice(data, offset, bitLen)
				want := packRawBits(data, offset, bitLen)
				preloaded := make([]byte, len(want))
				before := *s
				if err := s.PreloadSliceInto(preloaded, bitLen); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(preloaded, want) {
					t.Fatalf("unexpected preloaded data: got=%x want=%x", preloaded, want)
				}
				if *s != before {
					t.Fatal("preload advanced the slice")
				}

				loaded := make([]byte, len(want))
				if err := s.LoadSliceInto(loaded, bitLen); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(loaded, want) {
					t.Fatalf("unexpected loaded data: got=%x want=%x", loaded, want)
				}
				if s.BitsLeft() != 0 {
					t.Fatalf("load left %d bits", s.BitsLeft())
				}
			})
		}
	}

	s := rawBitSlice(data, 3, 17)
	before := *s
	short := []byte{0xAA, 0xBB}
	if err := s.LoadSliceInto(short, 17); !errors.Is(err, io.ErrShortBuffer) {
		t.Fatalf("unexpected short buffer error: %v", err)
	}
	if *s != before || !bytes.Equal(short, []byte{0xAA, 0xBB}) {
		t.Fatal("short destination changed cursor or destination")
	}

	dst := []byte{0xAA, 0xBB, 0xCC}
	if err := s.LoadSliceInto(dst, 18); !IsNotEnoughDataError(err) {
		t.Fatalf("unexpected short source error: %v", err)
	}
	if *s != before || !bytes.Equal(dst, []byte{0xAA, 0xBB, 0xCC}) {
		t.Fatal("short source changed cursor or destination")
	}
	if err := s.PreloadSliceInto(nil, 0); err != nil {
		t.Fatalf("zero-bit preload failed: %v", err)
	}
}

func TestSliceBigUIntInto(t *testing.T) {
	data := make([]byte, 33)
	for i := range data {
		data[i] = byte(i*41 + 0x53)
	}
	widths := []uint{0, 1, 7, 8, 9, 63, 64, 65, 127, 128, 255, 256}

	for _, offset := range []uint{0, 3, 7} {
		for _, width := range widths {
			t.Run(sliceIntoTestName(offset, width), func(t *testing.T) {
				s := rawBitSlice(data, offset, width)
				want := bigIntFromRawBits(data, offset, width)
				before := *s

				preloaded := big.NewInt(123)
				if err := s.PreloadBigUIntInto(preloaded, width); err != nil {
					t.Fatal(err)
				}
				if preloaded.Cmp(want) != 0 {
					t.Fatalf("unexpected preloaded value: got=%s want=%s", preloaded, want)
				}
				if *s != before {
					t.Fatal("preload advanced the slice")
				}

				loaded := big.NewInt(456)
				if err := s.LoadBigUIntInto(loaded, width); err != nil {
					t.Fatal(err)
				}
				if loaded.Cmp(want) != 0 {
					t.Fatalf("unexpected loaded value: got=%s want=%s", loaded, want)
				}
				if s.BitsLeft() != 0 {
					t.Fatalf("load left %d bits", s.BitsLeft())
				}
			})
		}
	}

	s := rawBitSlice(data, 3, 8)
	before := *s
	dst := big.NewInt(777)
	if err := s.LoadBigUIntInto(dst, 257); !errors.Is(err, ErrTooBigSize) {
		t.Fatalf("unexpected size error: %v", err)
	}
	if *s != before || dst.Int64() != 777 {
		t.Fatal("size error changed cursor or destination")
	}
	if err := s.LoadBigUIntInto(dst, 9); !IsNotEnoughDataError(err) {
		t.Fatalf("unexpected source error: %v", err)
	}
	if *s != before || dst.Int64() != 777 {
		t.Fatal("source error changed cursor or destination")
	}
}

func TestSliceBigUIntIntoReusesStorage(t *testing.T) {
	data := make([]byte, 32)
	for i := range data {
		data[i] = byte(i*19 + 1)
	}
	base := *rawBitSlice(data, 0, 256)
	var dst big.Int
	dst.SetBytes(make([]byte, 32))
	if err := (&base).PreloadBigUIntInto(&dst, 256); err != nil {
		t.Fatal(err)
	}

	var loadErr error
	allocs := testing.AllocsPerRun(1000, func() {
		s := base
		loadErr = s.LoadBigUIntInto(&dst, 256)
	})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if allocs != 0 {
		t.Fatalf("reused destination allocated: %.2f allocs/op", allocs)
	}

	first, err := (&base).PreloadBigUInt(256)
	if err != nil {
		t.Fatal(err)
	}
	second, err := (&base).PreloadBigUInt(256)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("owned API returned the same big.Int")
	}
	first.SetUint64(0)
	if second.Sign() == 0 {
		t.Fatal("owned API results share mutable storage")
	}
}

func TestSliceRefParsingCarriesTraceWithoutCellClone(t *testing.T) {
	childLoads := 0
	childTrace := NewTrace(TraceHooks{OnLoad: func(*Cell) { childLoads++ }})
	parentTrace := NewTrace(TraceHooks{OnChild: func(refIdx int) *Trace {
		if refIdx != 0 {
			t.Fatalf("unexpected child index: %d", refIdx)
		}
		return childTrace
	}})
	leaf := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).EndCell().WithTrace(parentTrace)

	s := root.MustBeginParse()
	loaded, err := s.LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.cell != leaf {
		t.Fatal("parse path cloned the referenced cell")
	}
	if loaded.Trace() != childTrace || loaded.cell.Trace() != nil {
		t.Fatal("child trace was not kept exclusively on the slice")
	}
	if childLoads != 1 {
		t.Fatalf("unexpected child load notifications: %d", childLoads)
	}
	if base := loaded.BaseCell(); base.Trace() != childTrace || base == leaf {
		t.Fatal("BaseCell did not materialize the traced cell view")
	}
	if leaf.Trace() != nil {
		t.Fatal("source leaf trace was mutated")
	}

	preloadSource := root.MustBeginParse()
	preloaded, err := preloadSource.PreloadRef()
	if err != nil {
		t.Fatal(err)
	}
	if preloadSource.RefsNum() != 1 || preloaded.cell != leaf {
		t.Fatal("PreloadRef advanced or cloned the referenced cell")
	}
	cellView, err := preloadSource.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if cellView.Trace() != childTrace || cellView == leaf {
		t.Fatal("LoadRefCell did not preserve its traced-cell API")
	}

	maybe := BeginCell().MustStoreMaybeRef(leaf).EndCell().WithTrace(parentTrace).MustBeginParse()
	maybeLoaded, err := maybe.LoadMaybeRef()
	if err != nil {
		t.Fatal(err)
	}
	if maybeLoaded.cell != leaf || maybeLoaded.Trace() != childTrace {
		t.Fatal("LoadMaybeRef cloned the cell or lost its child trace")
	}
}

func TestSliceRefParsingCombinedAndUsageTraces(t *testing.T) {
	loadsA, loadsB := 0, 0
	childA := NewTrace(TraceHooks{OnLoad: func(*Cell) { loadsA++ }})
	childB := NewTrace(TraceHooks{OnLoad: func(*Cell) { loadsB++ }})
	traceA := NewTrace(TraceHooks{OnChild: func(int) *Trace { return childA }})
	traceB := NewTrace(TraceHooks{OnChild: func(int) *Trace { return childB }})
	combined := CombineTraces(traceA, traceB)
	leaf := BeginCell().MustStoreUInt(0xCD, 8).EndCell()
	root := BeginCell().MustStoreRef(leaf).EndCell().WithTrace(combined)

	loaded, err := root.MustBeginParse().LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Trace() == nil || loadsA != 1 || loadsB != 1 {
		t.Fatalf("combined child trace was not notified: a=%d b=%d", loadsA, loadsB)
	}

	tree := NewCellUsageTree()
	lazyRoot := cellWithLazyRefsFromCell(
		BeginCell().MustStoreRef(leaf).EndCell(),
		func(Hash) (*Cell, error) { return leaf, nil },
	).WithTrace(tree.RootTrace())
	lazyLoaded, err := lazyRoot.MustBeginParse().LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	childNode := tree.GetChild(tree.RootNode(), 0)
	if childNode == 0 || !tree.IsLoaded(childNode) {
		t.Fatal("lazy child was not recorded in the usage tree")
	}
	if lazyLoaded.cell != leaf || lazyLoaded.cell.Trace() != nil {
		t.Fatal("lazy parse path cloned the loaded cell")
	}
	if node, ok := tree.NodeForCell(lazyLoaded.BaseCell()); !ok || node != childNode {
		t.Fatalf("BaseCell lost usage node identity: got=%d want=%d ok=%v", node, childNode, ok)
	}
}

func TestSliceRefParsingLazyVirtualPrunedTrace(t *testing.T) {
	base := BeginCell().
		MustStoreUInt(0x11, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0x22, 8).EndCell()).
		EndCell()
	pruned, err := createPrunedBranchFromCell(base, 2)
	if err != nil {
		t.Fatal(err)
	}
	loader := &testLazyLoader{cells: map[Hash]*Cell{pruned.HashKey(pruned.Level()): pruned}}
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(pruned), loader.LoadCell).Virtualize(0)
	childTrace := NewTrace(TraceHooks{OnLoad: func(*Cell) {}})
	rootTrace := NewTrace(TraceHooks{OnChild: func(int) *Trace { return childTrace }})
	root := BeginCell().MustStoreRef(lazy).EndCell().WithTrace(rootTrace)

	loaded, err := root.MustBeginParse().LoadRef()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.cell.IsVirtualized() || loaded.cell.GetType() != PrunedCellType {
		t.Fatalf("lazy virtual pruned semantics changed: virtual=%v type=%v", loaded.cell.IsVirtualized(), loaded.cell.GetType())
	}
	if loaded.cell.Trace() != nil || loaded.Trace() != childTrace {
		t.Fatal("lazy virtual parse attached the trace to the cell")
	}
	if loaded.BaseCell().Trace() != childTrace {
		t.Fatal("lazy virtual BaseCell lost the child trace")
	}
	if loader.calls != 1 {
		t.Fatalf("unexpected lazy loads: %d", loader.calls)
	}
}

func TestSliceLoadBinarySnakeDirect(t *testing.T) {
	child := BeginCell().MustStoreUInt(0b11011, 5).EndCell()
	root := BeginCell().MustStoreUInt(0b101, 3).MustStoreRef(child).EndCell()
	got, err := root.MustBeginParse().LoadBinarySnake()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0b10100000, 0b11011000}) {
		t.Fatalf("unexpected non-byte-aligned snake: %08b", got)
	}
	longData := make([]byte, 127*5+37)
	for i := range longData {
		longData[i] = byte(i*23 + 9)
	}
	longGot, err := BeginCell().MustStoreBinarySnake(longData).EndCell().MustBeginParse().LoadBinarySnake()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(longGot, longData) {
		t.Fatal("multi-cell snake data changed")
	}

	fullRoot := BeginCell().MustStoreSlice([]byte("a"), 8).
		MustStoreRef(BeginCell().MustStoreSlice([]byte("b"), 8).EndCell()).
		EndCell()
	lazyRoot := cellWithLazyRefsFromCell(fullRoot)
	if _, err := lazyRoot.MustBeginParse().LoadBinarySnake(); !errors.Is(err, ErrLazyLoaderNotSet) {
		t.Fatalf("expected lazy loader error, got %v", err)
	}
}

func TestCellDumpGoldenAndLimits(t *testing.T) {
	grandchild := BeginCell().EndCell()
	right := BeginCell().MustStoreUInt(1, 1).MustStoreRef(grandchild).EndCell()
	root := BeginCell().
		MustStoreUInt(0b101, 3).
		MustStoreRef(BeginCell().MustStoreUInt(0xAB, 8).EndCell()).
		MustStoreRef(right).
		EndCell()

	wantHex := "3[A_] -> {\n  8[AB],\n  1[8_] -> {\n    0[]\n  }\n}"
	wantBits := "3[101] -> {\n  8[10101011],\n  1[1] -> {\n    0[]\n  }\n}"
	if got := root.Dump(); got != wantHex {
		t.Fatalf("unexpected hex dump:\n%s\nwant:\n%s", got, wantHex)
	}
	if got := root.DumpBits(); got != wantBits {
		t.Fatalf("unexpected bit dump:\n%s\nwant:\n%s", got, wantBits)
	}
	if got := root.Dump(-1); got != wantHex {
		t.Fatalf("negative legacy limit changed: got=%q want=%q", got, wantHex)
	}

	for limit := 0; limit <= len(wantHex)+3; limit++ {
		want := wantHex
		if limit < len(want) {
			want = want[:limit]
		}
		if got := root.Dump(limit); got != want {
			t.Fatalf("unexpected dump at limit %d: got=%q want=%q", limit, got, want)
		}
	}
	for limit := 0; limit <= len(wantBits)+3; limit++ {
		want := wantBits
		if limit < len(want) {
			want = want[:limit]
		}
		if got := root.DumpBits(limit); got != want {
			t.Fatalf("unexpected bit dump at limit %d: got=%q want=%q", limit, got, want)
		}
	}
}

func TestCellDumpAllBitLengthsMatchLegacyFormat(t *testing.T) {
	for bitLen := uint(0); bitLen <= 1023; bitLen++ {
		data := make([]byte, (bitLen+7)/8)
		for i := range data {
			data[i] = byte(i*67 + int(bitLen)*13)
		}
		cell := &Cell{data: data, bitsSz: uint16(bitLen)}

		if got, want := cell.Dump(), legacyLeafDump(data, bitLen, false); got != want {
			t.Fatalf("hex dump mismatch at %d bits: got=%q want=%q", bitLen, got, want)
		}
		if got, want := cell.DumpBits(), legacyLeafDump(data, bitLen, true); got != want {
			t.Fatalf("binary dump mismatch at %d bits: got=%q want=%q", bitLen, got, want)
		}
	}
}

func TestCellDumpDeepChain(t *testing.T) {
	root := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	for range 511 {
		root = BeginCell().MustStoreUInt(0xAB, 8).MustStoreRef(root).EndCell()
	}

	full := root.Dump()
	deepest := strings.Repeat("  ", 511) + "8[AB]\n"
	if !strings.HasPrefix(full, "8[AB] -> {\n  8[AB]") ||
		!strings.Contains(full, deepest) ||
		!strings.HasSuffix(full, "}") ||
		strings.Count(full, "8[AB]") != 512 ||
		strings.Count(full, " -> {") != 511 {
		t.Fatal("deep dump structure is malformed")
	}
	const limit = 4096
	if got := root.Dump(limit); got != full[:limit] {
		t.Fatal("deep limited dump is not an exact prefix")
	}
}

func TestCellDumpLazyLoadErrorsKeepLegacyLimits(t *testing.T) {
	source := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	lazy := mustCreateLazyPrunedRef(t, lazyRefFromCell(source))
	const loadError = "<failed to load cell: lazy pruned ref loader is not set>"

	for _, limit := range []int{0, 1, 8, len(loadError) - 1} {
		if got := lazy.Dump(limit); got != loadError {
			t.Fatalf("root load error changed at limit %d: got=%q want=%q", limit, got, loadError)
		}
	}

	root := BeginCell().MustStoreRef(lazy).EndCell()
	want := "0[] -> {\n  " + loadError + "\n}"
	for limit := 0; limit <= len(want)+1; limit++ {
		prefix := want
		if limit < len(prefix) {
			prefix = prefix[:limit]
		}
		if got := root.Dump(limit); got != prefix {
			t.Fatalf("nested load error changed at limit %d: got=%q want=%q", limit, got, prefix)
		}
	}
}

func rawBitSlice(data []byte, offset, bitLen uint) *Slice {
	return &Slice{
		cell:     &Cell{data: data, bitsSz: uint16(offset + bitLen)},
		bitStart: uint16(offset),
		bitEnd:   uint16(offset + bitLen),
	}
}

func slowBitsEqual(left, right *Slice) bool {
	if left.BitsLeft() != right.BitsLeft() {
		return false
	}
	for bit := uint(0); bit < left.BitsLeft(); bit++ {
		if left.bitAt(bit) != right.bitAt(bit) {
			return false
		}
	}
	return true
}

func uniqueBitPositions(bitLen uint) []uint {
	positions := []uint{0, bitLen / 2, bitLen - 1}
	out := positions[:0]
	for _, pos := range positions {
		if len(out) == 0 || out[len(out)-1] != pos {
			out = append(out, pos)
		}
	}
	return out
}

func setRawBit(data []byte, bit uint) {
	data[bit/8] |= 1 << (7 - bit%8)
}

func packRawBits(data []byte, offset, bitLen uint) []byte {
	out := make([]byte, (bitLen+7)/8)
	for bit := uint(0); bit < bitLen; bit++ {
		if data[(offset+bit)/8]&(1<<(7-(offset+bit)%8)) != 0 {
			setRawBit(out, bit)
		}
	}
	return out
}

func bigIntFromRawBits(data []byte, offset, bitLen uint) *big.Int {
	value := new(big.Int)
	for bit := uint(0); bit < bitLen; bit++ {
		value.Lsh(value, 1)
		if data[(offset+bit)/8]&(1<<(7-(offset+bit)%8)) != 0 {
			value.SetBit(value, 0, 1)
		}
	}
	return value
}

func sliceIntoTestName(offset, bitLen uint) string {
	return "offset_" + strconv.FormatUint(uint64(offset), 10) + "_bits_" + strconv.FormatUint(uint64(bitLen), 10)
}

func legacyLeafDump(data []byte, bitLen uint, binary bool) string {
	data = bytes.Clone(data)
	if rem := bitLen % 8; rem != 0 && len(data) > 0 {
		data[len(data)-1] &= byte(0xFF << (8 - rem))
	}

	var value string
	if binary {
		var b strings.Builder
		for _, n := range data {
			fmt.Fprintf(&b, "%08b", n)
		}
		value = b.String()
		if rem := bitLen % 8; rem != 0 {
			value = value[:len(value)-int(8-rem)]
		}
	} else {
		encoded := make([]byte, len(data)*2)
		hex.Encode(encoded, data)
		value = strings.ToUpper(string(encoded))
		if rem := bitLen % 8; rem > 0 && rem <= 4 {
			value = value[:len(value)-1] + "_"
		}
	}
	return strconv.FormatUint(uint64(bitLen), 10) + "[" + value + "]"
}
