package cell

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
)

var data1024, _ = hex.DecodeString("0000000000000000000000000000000000000000000000000000000000000003000000000000000000000000000000000000000000000000000000000000000300000000000000000000000000000000000000000000000000000000000000030000000000000000000000000000000000000000000000000000000000000003")

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
		t.Fatal("incorrect7", val)
	}

	val = BeginCell().MustStoreUInt(0xFFFFFFFFFFFFFFFF, 60).EndCell().BeginParse().MustLoadUInt(60)
	if val != 0xFFFFFFFFFFFFFFF {
		t.Fatal("incorrect8", val)
	}
}

func TestBuilder_StoreBigInt(t *testing.T) {
	c := BeginCell()

	err := c.StoreBigInt(new(big.Int), 300)
	if err != ErrTooBigSize {
		t.Fatal("err incorrect, its:", err)
	}

	err = c.StoreBigInt(new(big.Int).Lsh(big.NewInt(1), 257), 256)
	if err != ErrTooBigValue {
		t.Fatal("err incorrect, its:", err)
	}

	c.MustStoreBigInt(new(big.Int).SetInt64(-3), 256)

	data := hex.EncodeToString(c.EndCell().BeginParse().MustLoadSlice(256))
	if data != "fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffd" {
		t.Fatal("value incorrect, its:", data)
	}
}

func TestBuilder_StoreBigUInt(t *testing.T) {
	c := BeginCell()

	err := c.StoreBigUInt(new(big.Int), 300)
	if err != ErrTooBigSize {
		t.Fatal("err incorrect, its:", err)
	}

	err = c.StoreBigUInt(new(big.Int).Lsh(big.NewInt(1), 257), 256)
	if err != ErrTooBigValue {
		t.Fatal("err incorrect, its:", err)
	}

	err = c.StoreBigUInt(big.NewInt(-1), 256)
	if err != ErrNegative {
		t.Fatal("err incorrect, its:", err)
	}

	c.MustStoreBigUInt(new(big.Int).SetInt64(3), 256)

	data := hex.EncodeToString(c.EndCell().BeginParse().MustLoadSlice(256))
	if data != "0000000000000000000000000000000000000000000000000000000000000003" {
		t.Fatal("value incorrect, its:", data)
	}
}

func TestBuilder_StoreSlice(t *testing.T) {
	c := BeginCell()

	err := c.StoreSlice([]byte{}, 1023)
	if err != ErrSmallSlice {
		t.Fatal("err incorrect, its:", err)
	}

	err = c.StoreSlice(data1024, 1024)
	if err != ErrNotFit1023 {
		t.Fatal("err incorrect, its:", err)
	}

	err = c.StoreSlice(data1024, 1023)
	if err != nil {
		t.Fatal("err incorrect, its:", err)
	}
}

func TestBuilder_StoreRef(t *testing.T) {
	c := BeginCell()

	err := c.StoreRef(nil)
	if err != ErrRefCannotBeNil {
		t.Fatal("err incorrect, its:", err)
	}

	for i := 0; i < 4; i++ {
		err = c.StoreRef(BeginCell().EndCell())
		if err != nil {
			t.Fatal("err incorrect, its:", err)
		}
	}

	err = c.StoreRef(BeginCell().EndCell())
	if err != ErrTooMuchRefs {
		t.Fatal("err incorrect, its:", err)
	}
}

func TestBuilder_StoreBuilder(t *testing.T) {
	c := BeginCell().MustStoreSlice(data1024, 1015).MustStoreRef(BeginCell().EndCell())
	b1bad := BeginCell().MustStoreSlice([]byte{0xAA, 0xBB}, 16).MustStoreRef(BeginCell().EndCell())
	b2bad := BeginCell().MustStoreSlice([]byte{0xAA}, 8).MustStoreRef(BeginCell().EndCell()).MustStoreRef(BeginCell().EndCell()).MustStoreRef(BeginCell().EndCell()).MustStoreRef(BeginCell().EndCell())
	b3 := BeginCell().MustStoreSlice([]byte{0xAA}, 8).MustStoreRef(BeginCell().EndCell()).MustStoreRef(BeginCell().EndCell()).MustStoreRef(BeginCell().EndCell())

	err := c.StoreBuilder(b1bad)
	if err != ErrNotFit1023 {
		t.Fatal("err incorrect, its:", err)
	}

	err = c.StoreBuilder(b2bad)
	if err != ErrTooMuchRefs {
		t.Fatal("err incorrect, its:", err)
	}

	err = c.StoreBuilder(b3)
	if err != nil {
		t.Fatal("err incorrect, its:", err)
	}

	if val := c.RefsLeft(); val != 0 {
		t.Fatal("refs left incorrect, its:", val)
	}

	if val := c.BitsLeft(); val != 0 {
		t.Fatal("bits left incorrect, its:", val)
	}

	if val := c.BitsUsed(); val != 1023 {
		t.Fatal("bits used incorrect, its:", val)
	}

	if val := c.RefsUsed(); val != 4 {
		t.Fatal("refs used incorrect, its:", val)
	}
}
