package http

import (
	"encoding/hex"
	"testing"
)

func Test_parseADNLAddress(t *testing.T) {
	res, err := parseADNLAddress("ui52b4urpcoigi26kfwp7vt2cgs2b5ljudwigvra35nhvymdqvqlfsa")
	if err != nil {
		t.Fatal(err)
	}

	if hex.EncodeToString(res) != "2d11dd07948bc4e4191af28b67feb3d08d2d07ab4d07641ab106fad3d70c1c2b" {
		t.Fatal("incorrect result")
	}
}
