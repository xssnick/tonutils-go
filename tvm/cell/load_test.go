package cell

import (
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
