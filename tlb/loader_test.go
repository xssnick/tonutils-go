package tlb

import (
	"bytes"
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type smallStruct struct {
	Sz uint32 `tlb:"## 8"`
}

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

type StructA struct {
	_   Magic `tlb:"$10"`
	Val int8  `tlb:"## 3"`
}

type StructB struct {
	_   Magic  `tlb:"#00AACC"`
	Val uint16 `tlb:"## 16"`
}

type StructC struct {
	_   Magic `tlb:"#00BBCC"`
	Val bool  `tlb:"bool"`
}

type testAny struct {
	StructAny any `tlb:"^ [StructA,StructB]"`
}

type testInner struct {
	_           Magic            `tlb:"$1011"`
	Val         int64            `tlb:"## 34"`
	Val2        uint64           `tlb:"## 12"`
	ValCoins    Coins            `tlb:"."`
	SmallBigVal *big.Int         `tlb:"## 10"`
	BigVal      *big.Int         `tlb:"## 176"`
	B           bool             `tlb:"bool"`
	Addr        *address.Address `tlb:"addr"`
	Manual      manualLoad       `tlb:"."`
	Dict        *cell.Dictionary `tlb:"dict 256"`
	StructMaybe *smallStruct     `tlb:"maybe ^"`
}

type testTLB struct {
	_                 Magic      `tlb:"#ffaa"`
	Val               uint16     `tlb:"## 16"`
	MaybeVal          *uint32    `tlb:"maybe ## 32"`
	Inside            testInner  `tlb:"^"`
	InsideMaybe       *testInner `tlb:"maybe ^"`
	Part              testInner  `tlb:"."`
	InsideMaybeEither *testInner `tlb:"maybe either ^ ."`
	Bits              []byte     `tlb:"bits 20"`
	Var               *big.Int   `tlb:"var uint 3"`
	EndCell           *cell.Cell `tlb:"."`
}

func TestLoadAnyRegistered(t *testing.T) {
	Register(StructA{})
	Register(StructC{})

	v := testAny{
		StructAny: StructA{
			Val: 2,
		},
	}

	c, err := ToCell(v)
	if err != nil {
		t.Fatal(err)
	}

	var v2 testAny
	err = LoadFromCell(&v2, c.BeginParse())
	if err != nil {
		t.Fatal(err)
	}
	json.NewEncoder(os.Stdout).Encode(v2)
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

	mRef := cell.BeginCell().MustStoreUInt('y', 8).EndCell()

	ref := cell.BeginCell().MustStoreUInt(0b1011, 4).
		MustStoreInt(-7172, 34).
		MustStoreUInt(0xCCA, 12).
		MustStoreCoins(700000).
		MustStoreUInt(5, 10).
		MustStoreUInt(7126382921832, 176).
		MustStoreBoolBit(true).MustStoreAddr(addr).MustStoreUInt('x', 8).MustStoreDict(d).
		MustStoreMaybeRef(mRef)

	a := cell.BeginCell().MustStoreUInt(0xFFAA, 16).
		MustStoreUInt(0xFFBF, 16).MustStoreBoolBit(true).MustStoreUInt(0xFFBFFFAA, 32).MustStoreRef(ref.EndCell()).MustStoreMaybeRef(nil).
		MustStoreBuilder(ref).MustStoreMaybeRef(ref.EndCell()).MustStoreBoolBit(false).
		MustStoreSlice([]byte{0xFF, 0xFF, 0xAA}, 20).MustStoreVarUInt(999, 3).
		MustStoreUInt(1, 1).EndCell()

	x := testTLB{}

	check := func() {
		err = LoadFromCell(&x, a.BeginParse())
		if err != nil {
			t.Fatal(err)
		}

		if x.Val != 0xFFBF {
			t.Fatal("loaded val not eq")
		}

		if x.InsideMaybe != nil {
			t.Fatal("maybe not nil")
		}

		if x.Inside.SmallBigVal.Uint64() != 5 {
			t.Fatal("uint 5 not eq")
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

		if x.Var.Uint64() != 999 {
			t.Fatal("var not eq")
		}

		if x.EndCell.BitsSize() != 1 {
			t.Fatal("cell val not eq")
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
