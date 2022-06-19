package cell

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestCell(t *testing.T) {
	c := BeginCell()

	bs := []byte{11, 22, 33}

	err := c.StoreUInt(1, 1)
	if err != nil {
		t.Fatal(err)
		return
	}

	err = c.StoreSlice(bs, 24)
	if err != nil {
		t.Fatal(err)
		return
	}

	amount := uint64(777)
	c2 := BeginCell().MustStoreCoins(amount).EndCell()

	err = c.StoreRef(c2)
	if err != nil {
		t.Fatal(err)
		return
	}

	u38val := uint64(0xAABBCCF)

	err = c.StoreUInt(u38val, 40)
	if err != nil {
		t.Fatal(err)
		return
	}

	boc := c.EndCell().ToBOC()

	cl, err := FromBOC(boc)
	if err != nil {
		t.Fatal(err)
		return
	}

	lc := cl.BeginParse()

	i, err := lc.LoadUInt(1)
	if err != nil {
		t.Fatal(err)
		return
	}

	if i != 1 {
		t.Fatal("1 bit not eq 1")
		return
	}

	bl, err := lc.LoadSlice(24)
	if err != nil {
		t.Fatal(err)
		return
	}

	if !bytes.Equal(bs, bl) {
		t.Fatal("slices not eq:\n" + hex.EncodeToString(bs) + "\n" + hex.EncodeToString(bl))
		return
	}

	u38, err := lc.LoadUInt(40)
	if err != nil {
		t.Fatal(err)
		return
	}

	if u38 != u38val {
		t.Fatal("uint38 not eq")
		return
	}

	ref, err := lc.LoadRef()
	if err != nil {
		t.Fatal(err)
		return
	}

	amt, err := ref.LoadCoins()
	if err != nil {
		t.Fatal(err)
		return
	}

	if amt != amount {
		t.Fatal("coins ref not eq")
		return
	}
}

func TestCell24(t *testing.T) {
	c := BeginCell()

	bs := []byte{11, 22, 33}

	err := c.StoreSlice(bs, 24)
	if err != nil {
		t.Fatal(err)
		return
	}

	lc := c.EndCell().BeginParse()

	res, err := lc.LoadSlice(24)
	if err != nil {
		t.Fatal(err)
		return
	}

	if !bytes.Equal(bs, res) {
		t.Fatal("slices not eq:\n" + hex.EncodeToString(bs) + "\n" + hex.EncodeToString(res))
		return
	}
}

func TestCell25(t *testing.T) {
	c := BeginCell()

	bs := []byte{11, 22, 33, 0x80}

	err := c.StoreSlice(bs, 25)
	if err != nil {
		t.Fatal(err)
		return
	}

	lc := c.EndCell().BeginParse()

	res, err := lc.LoadSlice(25)
	if err != nil {
		t.Fatal(err)
		return
	}

	if !bytes.Equal(bs, res) {
		t.Fatal("slices not eq:\n" + hex.EncodeToString(bs) + "\n" + hex.EncodeToString(res))
		return
	}
}

func TestCellReadSmall(t *testing.T) {
	c := BeginCell()

	bs := []byte{0b10101010, 0x00, 0x00}

	err := c.StoreSlice(bs, 24)
	if err != nil {
		t.Fatal(err)
		return
	}

	lc := c.EndCell().BeginParse()

	for i := 0; i < 8; i++ {
		res, err := lc.LoadUInt(1)
		if err != nil {
			t.Fatal(err)
			return
		}

		if (res != 1 && i%2 == 0) || (res != 0 && i%2 == 1) {
			t.Fatal("not eq " + fmt.Sprint(i*2))
			return
		}
	}

	res, err := lc.LoadUInt(1)
	if err != nil {
		t.Fatal(err)
		return
	}

	if res != 0 {
		t.Fatal("not 0")
		return
	}
}

func TestCellReadEmpty(t *testing.T) {
	c := BeginCell().EndCell().BeginParse()
	sz, _, err := c.RestBits()
	if err != nil {
		t.Fatal(err)
		return
	}

	if sz != 0 {
		t.Fatal("not 0")
		return
	}
}

func TestBuilder_MustStoreUInt(t *testing.T) {
	val := BeginCell().MustStoreUInt(516783, 23).EndCell().BeginParse().MustLoadUInt(23)
	if val != 516783 {
		t.Fatal("incorrect", val)
	}

	val = BeginCell().MustStoreUInt(2, 64).EndCell().BeginParse().MustLoadUInt(64)
	if val != 2 {
		t.Fatal("incorrect2", val)
	}

	val = BeginCell().MustStoreUInt(0xFFFFFF, 24).EndCell().BeginParse().MustLoadUInt(24)
	if val != 0xFFFFFF {
		t.Fatal("incorrect3", val)
	}

	val = BeginCell().MustStoreUInt(0xFFFFFF, 24).EndCell().BeginParse().MustLoadUInt(20)
	if val != 0xFFFFF {
		t.Fatal("incorrect4", val)
	}

	val = BeginCell().MustStoreUInt(2, 2).EndCell().BeginParse().MustLoadUInt(2)
	if val != 2 {
		t.Fatal("incorrect5", val)
	}

	val = BeginCell().MustStoreUInt(1, 1).EndCell().BeginParse().MustLoadUInt(1)
	if val != 1 {
		t.Fatal("incorrect6", val)
	}

	val = BeginCell().MustStoreUInt(123456789, 70).EndCell().BeginParse().MustLoadUInt(70)
	if val != 123456789 {
		t.Fatal("incorrect6", val)
	}
}
