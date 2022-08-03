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

func (m *manualLoad) LoadFromCell(loader *cell.Slice) error {
	m.Val = string(uint8(loader.MustLoadUInt(8)))
	return nil
}

func (m manualLoad) ToCell() (*cell.Cell, error) {
	return cell.BeginCell().MustStoreUInt(uint64(m.Val[0]), 8).EndCell(), nil
}

type testTransform struct {
	DictTransform []*manualLoad `tlb:"dict 32 -> array"`
}

type testInner struct {
	_        Magic            `tlb:"$1011"`
	Val      int64            `tlb:"## 34"`
	Val2     uint64           `tlb:"## 12"`
	ValCoins Coins            `tlb:"."`
	BigVal   *big.Int         `tlb:"## 176"`
	B        bool             `tlb:"bool"`
	Addr     *address.Address `tlb:"addr"`
	Manual   manualLoad       `tlb:"."`
	Dict     *cell.Dictionary `tlb:"dict 256"`
}

type testTLB struct {
	_                 Magic      `tlb:"#ffaa"`
	Val               uint32     `tlb:"## 32"`
	Inside            testInner  `tlb:"^"`
	InsideMaybe       *testInner `tlb:"maybe ^"`
	Part              testInner  `tlb:"."`
	InsideMaybeEither *testInner `tlb:"maybe either ^ ."`
	Bits              []byte     `tlb:"bits 20"`
}

func TestLoadFromCell(t *testing.T) {
	addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")
	dKey := cell.BeginCell().MustStoreSlice(addr.Data(), 256).EndCell()
	dVal := cell.BeginCell().MustStoreAddr(addr).EndCell()

	d := cell.NewDict(256)
	err := d.Set(dKey, dVal)
	if err != nil {
		t.Fatal(err)
	}

	ref := cell.BeginCell().MustStoreUInt(0b1011, 4).
		MustStoreInt(-7172, 34).
		MustStoreUInt(0xCCA, 12).
		MustStoreCoins(700000).
		MustStoreUInt(7126382921832, 176).
		MustStoreBoolBit(true).MustStoreAddr(addr).MustStoreUInt('x', 8).MustStoreDict(d)

	a := cell.BeginCell().MustStoreUInt(0xFFAA, 16).
		MustStoreUInt(0xFFBFFFAA, 32).MustStoreRef(ref.EndCell()).MustStoreMaybeRef(nil).
		MustStoreBuilder(ref).MustStoreMaybeRef(ref.EndCell()).MustStoreBoolBit(false).MustStoreSlice([]byte{0xFF, 0xFF, 0xAA}, 20).EndCell()

	x := testTLB{}

	check := func() {
		err = LoadFromCell(&x, a.BeginParse())
		if err != nil {
			t.Fatal(err)
		}

		if x.Val != 0xFFBFFFAA {
			t.Fatal("loaded val not eq")
		}

		if x.InsideMaybe != nil {
			t.Fatal("maybe not nil")
		}

		if x.Inside.BigVal.Uint64() != 7126382921832 {
			t.Fatal("uint 7126382921832 not eq")
		}

		if x.Inside.ValCoins.NanoTON().Uint64() != 700000 {
			t.Fatal("coins 700000 not eq")
		}

		if x.Part.BigVal.Uint64() != 7126382921832 {
			t.Fatal("uint part 7126382921832 not eq")
		}

		if x.InsideMaybeEither.BigVal.Uint64() != 7126382921832 {
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

		if !bytes.Equal(x.Part.Dict.Get(dKey).Hash(), dVal.Hash()) {
			t.Fatal("dict val not eq")
		}
	}
	hashA := a.Hash()
	check()

	a, err = ToCell(x)
	if err != nil {
		t.Fatal(err)
	}
	hashB := a.Hash()
	check()

	if !bytes.Equal(hashA, hashB) {
		t.Fatal("cell hashes not same after From to")
	}
}

func TestLoadFromCellTransform(t *testing.T) {
	ldVal := cell.BeginCell().MustStoreUInt(uint64('M'), 8).EndCell()

	d2 := cell.NewDict(32)
	for i := 0; i < 200; i++ {
		err := d2.Set(cell.BeginCell().MustStoreInt(int64(i), 32).EndCell(), ldVal)
		if err != nil {
			t.Fatal(err)
		}
	}

	a := cell.BeginCell().MustStoreDict(d2).EndCell().BeginParse()

	x := testTransform{}
	err := LoadFromCell(&x, a)
	if err != nil {
		t.Fatal(err)
	}

	if len(x.DictTransform) != 200 {
		t.Fatal("dict transform len not 200")
	}

	for _, m := range x.DictTransform {
		if m.Val != "M" {
			t.Fatal("dict transform values corrupted")
		}
	}
}
