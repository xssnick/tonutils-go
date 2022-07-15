package wallet

import (
	"encoding/hex"
	"testing"
)

func TestAddressFromPubKey(t *testing.T) {
	pkey, _ := hex.DecodeString("dcc39550bb494f4b493e7efe1aa18ea31470f33a2553c568cb74a17ed56790c1")

	a, err := AddressFromPubKey(pkey, V3, DefaultSubwallet)
	if err != nil {
		t.Fatal(err)
	}

	if a.String() != "EQCvoBT5Keb46oUhI_DpX0WXFDdX9ZyxXBfX3FC9cZa90nQP" {
		t.Fatal("v3 not match")
	}
}
