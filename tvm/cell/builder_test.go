package cell

import (
	"bytes"
	"encoding/hex"
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
