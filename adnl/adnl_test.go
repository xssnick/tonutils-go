package adnl

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"github.com/xssnick/tonutils-go/tl"
	"testing"
	"time"
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
		t.Fatal(err)
	}

	var res any
	forRow, err := hex.DecodeString("e191b161")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = adnl.Query(ctx, tl.Raw(forRow), &res)
	if err != nil {
		if !errors.Is(err, ctx.Err()) {
			t.Fatal(err.Error())
		}
	}
	err = adnl.Close()
	if err != nil {
		t.Fatal(err)
	}
}
