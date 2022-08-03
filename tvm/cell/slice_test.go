package cell

import (
	"bytes"
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

	ldStr, err := v.LoadStringSnake()
	if err != nil {
		t.Fatal(err)
	}

	if str != ldStr {
		t.Fatal("str not eq", str, ldStr)
	}
}
