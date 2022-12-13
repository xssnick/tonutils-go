package adnl

import (
	"context"
	"crypto/ed25519"
	"testing"
)

func TestADNL_Connect(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	adnl, err := NewADNL(pub)
	if err != nil {
		t.Fatal(err)
	}

	err = adnl.Connect(context.Background(), "178.18.243.125:15888")
	if err != nil {
		t.Fatal()
	}
	//var res any
	//err = adnl.Query(context.Background(), &res)
	//if err != nil {
	//	t.Fatal()
	//}
}
