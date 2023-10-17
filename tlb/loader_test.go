package tlb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"reflect"
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

type testDict struct {
	Dict          *cell.Dictionary  `tlb:"dict 256"`
	DictMapBool   map[string]bool   `tlb:"dict 55 -> bool"`
	DictMapUint   map[string]uint64 `tlb:"dict 77 -> ## 43"`
	DictMapStruct map[string]any    `tlb:"dict 128 -> ^ [StructA,StructC]"`
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
	Dict        testDict         `tlb:"^"`
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

func mustParseInt(x string) *big.Int {
	ret, ok := new(big.Int).SetString(x, 10)
	if !ok {
		panic(fmt.Errorf("big int from '%s'", ret))
	}
	return ret
}

func TestLoadFromCell(t *testing.T) {
	Register(StructA{})
	Register(StructC{})

	addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")
	dKey := cell.BeginCell().MustStoreSlice(addr.Data(), 256).EndCell()
	dVal := cell.BeginCell().MustStoreAddr(addr).EndCell()

	d := cell.NewDict(256)
	err := d.Set(dKey, dVal)
	if err != nil {
		t.Fatal(err)
	}

	dMapBoolKV := map[string]bool{"43": true, "76": false, "79": true}
	dMapBool := cell.NewDict(55)
	for k, v := range dMapBoolKV {
		err := dMapBool.Set(
			cell.BeginCell().MustStoreBigInt(mustParseInt(k), 55).EndCell(),
			cell.BeginCell().MustStoreBoolBit(v).EndCell())
		if err != nil {
			t.Fatal(err)
		}
	}
	dMapIntKV := map[string]uint64{"43": 43, "76": 75, "79": 79}
	dMapInt := cell.NewDict(77)
	for k, v := range dMapIntKV {
		err := dMapInt.Set(
			cell.BeginCell().MustStoreBigInt(mustParseInt(k), 77).EndCell(),
			cell.BeginCell().MustStoreUInt(v, 43).EndCell())
		if err != nil {
			t.Fatal(err)
		}
	}
	dMapStructKV := map[string]any{"43": StructA{Val: 1}, "322": StructC{Val: true}}
	dMapStruct := cell.NewDict(128)
	for k, v := range dMapStructKV {
		cl, _ := ToCell(v)
		err := dMapStruct.Set(
			cell.BeginCell().MustStoreBigInt(mustParseInt(k), 128).EndCell(),
			cell.BeginCell().MustStoreRef(cl).EndCell())
		if err != nil {
			t.Fatal(err)
		}
	}

	dictC := cell.BeginCell().
		MustStoreDict(d).
		MustStoreDict(dMapBool).
		MustStoreDict(dMapInt).
		MustStoreDict(dMapStruct).
		EndCell()

	mRef := cell.BeginCell().MustStoreUInt('y', 8).EndCell()

	ref := cell.BeginCell().MustStoreUInt(0b1011, 4).
		MustStoreInt(-7172, 34).
		MustStoreUInt(0xCCA, 12).
		MustStoreCoins(700000).
		MustStoreUInt(5, 10).
		MustStoreUInt(7126382921832, 176).
		MustStoreBoolBit(true).
		MustStoreAddr(addr).
		MustStoreUInt('x', 8).
		MustStoreRef(dictC).
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

		if x.Inside.ValCoins.Nano().Uint64() != 700000 {
			t.Fatal("coins 700000 not eq")
		}

		if x.Inside.ValCoins.String() != "0.0007" {
			t.Fatal("coins 700000 not eq, got " + x.Inside.ValCoins.String())
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

		if !bytes.Equal(x.Part.Dict.Dict.Get(dKey).Hash(), dVal.Hash()) {
			t.Fatal("dict val not eq")
		}
		if !reflect.DeepEqual(x.Part.Dict.DictMapBool, dMapBoolKV) {
			t.Fatal("bool dict val not eq")
		}
		if !reflect.DeepEqual(x.Part.Dict.DictMapUint, dMapIntKV) {
			t.Fatal("uint dict val not eq")
		}
		if !reflect.DeepEqual(x.Part.Dict.DictMapStruct, dMapStructKV) {
			t.Fatal("struct dict val not eq")
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

func TestLoadFromCell_MappedDict(t *testing.T) {
	dict := cell.NewDict(3)

	b := cell.BeginCell()

	err := b.StoreBoolBit(true)
	if err != nil {
		t.Fatal(err)
	}

	err = dict.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreRef(b.EndCell()).EndCell())
	if err != nil {
		t.Fatal(err)
	}

	b = cell.BeginCell()
	if err := b.StoreDict(dict); err != nil {
		t.Fatal(err)
	}

	var ret struct {
		Value map[string]bool `tlb:"dict 3 -> ^ bool"`
	}

	x := b.EndCell()
	err = LoadFromCell(&ret, x.BeginParse())
	if err != nil {
		t.Fatal(err)
	}

	j, err := json.Marshal(ret)
	if err != nil {
		t.Fatal(err)
	}

	if string(j) != "{\"Value\":{\"1\":true}}" {
		t.Fatal("wrong map json")
	}

	cl, err := ToCell(ret)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(cl.Hash(), x.Hash()) {
		t.Fatal("wrong hash")
	}
}
