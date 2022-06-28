package tlb

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type manualLoad struct {
	Val string
}

func (m *manualLoad) LoadFromCell(loader *cell.LoadCell) error {
	m.Val = string(uint8(loader.MustLoadUInt(8)))
	return nil
}

type testInner struct {
	_      Magic    `tlb:"$1011"`
	Val    int64    `tlb:"## 34"`
	BigVal *big.Int `tlb:"## 176"`
	// Dict   *cell.Dictionary `tlb:"dict 32"`
	C      *cell.Cell       `tlb:"^"`
	B      bool             `tlb:"bool"`
	Addr   *address.Address `tlb:"addr"`
	Manual manualLoad       `tlb:"."`
}

type testTLB struct {
	_           Magic      `tlb:"#ffaa"`
	Val         uint32     `tlb:"## 32"`
	Inside      testInner  `tlb:"^"`
	Inside2     *testInner `tlb:"^"`
	InsideMaybe *testInner `tlb:"maybe ^"`
	Part        testInner  `tlb:"."`
	Bits        []byte     `tlb:"bits 20"`
}

func TestLoadFromCell(t *testing.T) {
	addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")

	ref := cell.BeginCell().MustStoreUInt(0b1011, 4).
		MustStoreUInt(0xFFAA, 34).
		MustStoreUInt(7126382921832, 176).MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreBoolBit(true).MustStoreAddr(addr).MustStoreUInt('x', 8)

	a := cell.BeginCell().MustStoreUInt(0xFFAA, 16).
		MustStoreUInt(0xFFBFFFAA, 32).MustStoreRef(ref.EndCell()).MustStoreRef(ref.EndCell()).MustStoreMaybeRef(nil).MustStoreBuilder(ref).MustStoreSlice([]byte{0xFF, 0xFF, 0xAA}, 20).EndCell().BeginParse()

	x := testTLB{}
	err := LoadFromCell(&x, a)
	if err != nil {
		t.Fatal(err)
	}

	if x.Val != 0xFFBFFFAA {
		t.Fatal("loaded val not eq")
	}

	if x.InsideMaybe != nil {
		t.Fatal("maybe not nil")
	}

	if x.Inside2.BigVal.Uint64() != 7126382921832 {
		t.Fatal("uint 7126382921832 not eq")
	}

	if x.Part.BigVal.Uint64() != 7126382921832 {
		t.Fatal("uint part 7126382921832 not eq")
	}

	if !bytes.Equal(x.Bits, []byte{0xFF, 0xFF, 0xA0}) {
		t.Fatal("bits not eq")
	}

	if !x.Part.B {
		t.Fatal("bool not eq")
	}

	if x.Part.Addr.String() != addr.String() {
		t.Fatal("addr not eq")
	}

	if x.Part.Manual.Val != "x" {
		t.Fatal("manual not eq")
	}
}
