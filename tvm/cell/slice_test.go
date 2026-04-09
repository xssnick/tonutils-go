package cell

import (
	"bytes"
	"math"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
)

func TestLoadCell_LoadAddr(t *testing.T) {
	addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")

	c := BeginCell().MustStoreUInt(1, 3).MustStoreAddr(addr).EndCell().BeginParse()
	c.MustLoadUInt(3)

	lAddr, err := c.LoadAddr()
	if err != nil {
		t.Fatal(err)
		return
	}

	if addr.String() != lAddr.String() {
		t.Fatal(err)
		return
	}
}

func TestLoadCell_Loaders(t *testing.T) {
	empty := BeginCell().EndCell()
	builder := BeginCell().MustStoreRef(empty).MustStoreSlice([]byte{0xFF, 0xFF, 0xFF}, 20).MustStoreAddr(nil)
	ref := BeginCell().MustStoreBoolBit(true).MustStoreBuilder(builder).EndCell()

	a := BeginCell().
		MustStoreUInt(54310, 17).
		MustStoreCoins(41282931).
		MustStoreMaybeRef(ref).MustStoreMaybeRef(nil).EndCell()

	b := a.BeginParse().MustToCell()

	if !bytes.Equal(b.Hash(), a.Hash()) {
		t.Fatal("hashes diff after serialize")
		return
	}

	c := b.BeginParse()

	if c.RefsNum() != 1 || a.RefsNum() != 1 {
		t.Fatal("refs num diff")
		return
	}

	if b.BitsSize() != a.BitsSize() {
		t.Fatal("bits num diff")
		return
	}

	if b.Dump() != a.Dump() {
		t.Fatal("dump diff")
		return
	}

	if c.MustLoadUInt(17) != 54310 {
		t.Fatal("uint diff")
		return
	}

	if c.MustLoadCoins() != 41282931 {
		t.Fatal("coins diff")
		return
	}

	lr := c.MustLoadMaybeRef()
	if lr == nil {
		t.Fatal("first maybe ref nil")
		return
	}

	if lr.MustLoadBoolBit() != true {
		t.Fatal("first bit of loaded ref not true")
		return
	}

	if lr.MustLoadRef().bitsSz != 0 {
		t.Fatal("first ref of loaded ref not empty")
		return
	}

	if bytes.Equal(lr.MustLoadSlice(20), []byte{0xFF, 0xFF, 0x0F}) {
		t.Fatal("slice loaded ref not eq")
		return
	}

	if !lr.MustLoadAddr().IsAddrNone() {
		t.Fatal("addr in loaded ref not nil")
		return
	}

	if c.MustLoadMaybeRef() != nil {
		t.Fatal("second maybe ref not nil")
		return
	}
}

func TestSlice_LoadBigInt(t *testing.T) {
	v := BeginCell().MustStoreInt(-5, 5).EndCell().BeginParse().MustLoadInt(5)
	if v != -5 {
		t.Fatal("i not -5", v)
		return
	}

	v = BeginCell().MustStoreInt(-53276879, 256).EndCell().BeginParse().MustLoadInt(256)
	if v != -53276879 {
		t.Fatal("i not -53276879", v)
		return
	}
}

func TestSlice_Snake(t *testing.T) {
	str := "big brown cherry-pick going to hunt your pussy 😃😃😄😇🤪🤪🙁😤😨🖕💅👏☝️👍👃👃👨‍👩‍👩🧑👨‍"
	v := BeginCell().MustStoreStringSnake(str).EndCell().BeginParse()

	ldStr := v.MustLoadStringSnake()
	if str != ldStr {
		t.Fatal("str not eq", str, ldStr)
	}
}

func TestSlice_PreloadUInt_UnalignedDoesNotAdvance(t *testing.T) {
	s := BeginCell().
		MustStoreUInt(0b101, 3).
		MustStoreUInt(0b11010, 5).
		EndCell().
		BeginParse()

	if got := s.MustLoadUInt(3); got != 0b101 {
		t.Fatalf("unexpected prefix bits: %b", got)
	}

	before := s.BitsLeft()
	got, err := s.PreloadUInt(5)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0b11010 {
		t.Fatalf("unexpected preloaded bits: %b", got)
	}
	if s.BitsLeft() != before {
		t.Fatalf("preload advanced slice: before=%d after=%d", before, s.BitsLeft())
	}
	if got := s.MustPreloadUInt(5); got != 0b11010 {
		t.Fatalf("unexpected must-preloaded bits: %b", got)
	}
	if got := s.MustLoadUInt(5); got != 0b11010 {
		t.Fatalf("unexpected loaded bits after preload: %b", got)
	}
}

func TestSlice_LoadBoolBit_Unaligned(t *testing.T) {
	s := BeginCell().
		MustStoreUInt(0b101, 3).
		MustStoreBoolBit(true).
		MustStoreBoolBit(false).
		EndCell().
		BeginParse()

	if got := s.MustLoadUInt(3); got != 0b101 {
		t.Fatalf("unexpected prefix bits: %b", got)
	}
	if !s.MustLoadBoolBit() {
		t.Fatal("expected first bool bit to be true")
	}
	if s.MustLoadBoolBit() {
		t.Fatal("expected second bool bit to be false")
	}
}

func TestSlice_LoadUIntZeroBits(t *testing.T) {
	s := BeginCell().MustStoreUInt(0xAB, 8).EndCell().BeginParse()

	before := s.BitsLeft()
	got, err := s.LoadUInt(0)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("expected zero-bit load to return 0, got %d", got)
	}
	if s.BitsLeft() != before {
		t.Fatalf("zero-bit load advanced slice: before=%d after=%d", before, s.BitsLeft())
	}
	if got := s.MustLoadUInt(8); got != 0xAB {
		t.Fatalf("unexpected byte after zero-bit load: %x", got)
	}
}

func TestSlice_LoadIntSmallWidths(t *testing.T) {
	tests := []struct {
		name  string
		value int64
		bits  uint
	}{
		{name: "OneBitMinusOne", value: -1, bits: 1},
		{name: "FiveBitMinusFive", value: -5, bits: 5},
		{name: "SixtyThreeBitNegative", value: -(1 << 40), bits: 63},
		{name: "SixtyFourBitMinInt64", value: math.MinInt64, bits: 64},
		{name: "SixtyFourBitMaxInt64", value: math.MaxInt64, bits: 64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BeginCell().MustStoreInt(tc.value, tc.bits).EndCell().BeginParse().MustLoadInt(tc.bits)
			if got != tc.value {
				t.Fatalf("unexpected roundtrip: got=%d want=%d", got, tc.value)
			}
		})
	}
}

func TestCellBeginParse_SnapshotsUnsafeData(t *testing.T) {
	c := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	s := c.BeginParse()

	raw := c.ToRawUnsafe()
	raw.Data[0] = 0xCD

	if got := s.MustLoadUInt(8); got != 0xAB {
		t.Fatalf("snapshot changed after unsafe mutation, got %x", got)
	}
}

func TestSliceCopy_SnapshotsUnsafeData(t *testing.T) {
	c := BeginCell().MustStoreUInt(0b10110110, 8).EndCell()
	s := c.BeginParse()
	s.MustLoadUInt(3)
	cp := s.Copy()

	raw := c.ToRawUnsafe()
	raw.Data[0] = 0x00

	if got := cp.MustLoadUInt(5); got != 0b10110 {
		t.Fatalf("copied slice changed after unsafe mutation, got %b", got)
	}
}

func TestCellBeginParseTrusted_IndependentCursors(t *testing.T) {
	ref := BeginCell().MustStoreUInt(0xCD, 8).EndCell()
	c := BeginCell().
		MustStoreUInt(0xAB, 8).
		MustStoreRef(ref).
		EndCell()

	left := c.BeginParseNoCopy()
	right := c.BeginParseNoCopy()

	if got := left.MustLoadUInt(4); got != 0xA {
		t.Fatalf("unexpected left prefix: %x", got)
	}
	if got := right.MustLoadUInt(8); got != 0xAB {
		t.Fatalf("unexpected right value: %x", got)
	}
	if left.RefsNum() != 1 || right.RefsNum() != 1 {
		t.Fatalf("unexpected refs before load: left=%d right=%d", left.RefsNum(), right.RefsNum())
	}

	leftRef := left.MustLoadRef()
	if got := leftRef.MustLoadUInt(8); got != 0xCD {
		t.Fatalf("unexpected left ref value: %x", got)
	}
	if left.RefsNum() != 0 {
		t.Fatalf("left ref cursor was not consumed")
	}
	if right.RefsNum() != 1 {
		t.Fatalf("right cursor was unexpectedly affected")
	}
}

func TestCellHashKey_MatchesHash(t *testing.T) {
	c := BeginCell().
		MustStoreUInt(0xABCD, 16).
		MustStoreRef(BeginCell().MustStoreUInt(1, 1).EndCell()).
		EndCell()

	key := c.HashKey()
	if !bytes.Equal(key[:], c.Hash()) {
		t.Fatalf("hash key does not match regular hash")
	}
}

func TestSlice_PreloadRefAndBigUIntWrappers(t *testing.T) {
	refCell := BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	value := new(big.Int).Lsh(big.NewInt(1), 72)
	value.Add(value, big.NewInt(0x12345))

	s := BeginCell().
		MustStoreRef(refCell).
		MustStoreBigUInt(value, 80).
		EndCell().
		BeginParse()

	preCell, err := s.PreloadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if !equalCellContents(preCell, refCell) {
		t.Fatal("preloaded ref cell does not match")
	}

	preSlice, err := s.PreloadRef()
	if err != nil {
		t.Fatal(err)
	}
	if got := preSlice.MustLoadUInt(8); got != 0xAB {
		t.Fatalf("unexpected preloaded ref slice value: %x", got)
	}
	if s.RefsNum() != 1 {
		t.Fatalf("preload should not consume refs, got %d left", s.RefsNum())
	}

	loadedRef, err := s.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if !equalCellContents(loadedRef, refCell) {
		t.Fatal("loaded ref cell does not match")
	}

	before := s.BitsLeft()
	preBig := s.MustPreloadBigUInt(80)
	if preBig.Cmp(value) != 0 {
		t.Fatalf("unexpected preloaded big uint: got %s want %s", preBig, value)
	}
	if s.BitsLeft() != before {
		t.Fatalf("preload advanced slice: before=%d after=%d", before, s.BitsLeft())
	}

	got := s.MustLoadBigUInt(80)
	if got.Cmp(value) != 0 {
		t.Fatalf("unexpected loaded big uint: got %s want %s", got, value)
	}
}

func TestSlice_LoadVarUIntZeroAndBinarySnake(t *testing.T) {
	s := BeginCell().MustStoreVarUInt(0, 16).EndCell().BeginParse()
	if got := s.MustLoadVarUInt(16); got.Sign() != 0 {
		t.Fatalf("expected zero varuint, got %s", got)
	}

	data := []byte("binary snake payload")
	if got := BeginCell().MustStoreBinarySnake(data).EndCell().BeginParse().MustLoadBinarySnake(); !bytes.Equal(got, data) {
		t.Fatalf("unexpected snake payload: %q", got)
	}

	invalid := BeginCell().
		MustStoreRef(BeginCell().EndCell()).
		MustStoreRef(BeginCell().EndCell()).
		EndCell().
		BeginParse()

	if _, err := invalid.LoadBinarySnake(); err == nil {
		t.Fatal("expected invalid snake with more than one ref")
	}
}

func TestSlice_LoadAddrVariants(t *testing.T) {
	ext := address.NewAddressExt(0, 20, []byte{0xAA, 0xBB, 0xC0})
	gotExt := BeginCell().MustStoreAddr(ext).EndCell().BeginParse().MustLoadAddr()
	if gotExt.String() != ext.String() {
		t.Fatalf("unexpected ext address: got=%s want=%s", gotExt.String(), ext.String())
	}

	varAddr := address.NewAddressVar(0, -1, 20, []byte{0xDE, 0xAD, 0xB0})
	gotVar := BeginCell().MustStoreAddr(varAddr).EndCell().BeginParse().MustLoadAddr()
	if gotVar.String() != varAddr.String() {
		t.Fatalf("unexpected var address: got=%s want=%s", gotVar.String(), varAddr.String())
	}

	stdAnycastData := bytes.Repeat([]byte{0xAB}, 32)
	stdAnycast := BeginCell().
		MustStoreUInt(0b10, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(3, 5).
		MustStoreUInt(0b101, 3).
		MustStoreUInt(0xFF, 8).
		MustStoreSlice(stdAnycastData, 256).
		EndCell().
		BeginParse().
		MustLoadAddr()

	wantStdAnycast := address.NewAddress(0, 0xFF, stdAnycastData)
	if stdAnycast.String() != wantStdAnycast.String() {
		t.Fatalf("unexpected std anycast address: got=%s want=%s", stdAnycast.String(), wantStdAnycast.String())
	}

	varAnycast := BeginCell().
		MustStoreUInt(0b11, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(3, 5).
		MustStoreUInt(0b101, 3).
		MustStoreUInt(20, 9).
		MustStoreInt(-1, 32).
		MustStoreSlice([]byte{0xF0, 0x0D, 0x50}, 20).
		EndCell().
		BeginParse().
		MustLoadAddr()

	wantVarAnycast := address.NewAddressVar(0, -1, 20, []byte{0xF0, 0x0D, 0x50})
	if varAnycast.String() != wantVarAnycast.String() {
		t.Fatalf("unexpected var anycast address: got=%s want=%s", varAnycast.String(), wantVarAnycast.String())
	}
}
