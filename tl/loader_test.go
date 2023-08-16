package tl

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"net"
	"testing"
)

type TestInner struct {
	Double int64             `tl:"long"`
	Key    ed25519.PublicKey `tl:"int256"`
}

type TestTL struct {
	Simple          int64        `tl:"int"`
	Flags           uint32       `tl:"flags"`
	SimpleOptional  int64        `tl:"?0 long"`
	SimpleOptional2 int64        `tl:"?1 long"`
	SimpleUint      uint         `tl:"int"`
	SimpleUintBig   uint64       `tl:"long"`
	In              *TestInner   `tl:"struct boxed"`
	InX             any          `tl:"struct boxed [in]"`
	In2             []any        `tl:"vector struct boxed [in]"`
	KeyEmpty        []byte       `tl:"int256"`
	Data            [][]byte     `tl:"vector bytes"`
	CellArr         []*cell.Cell `tl:"cell 1"`
	Cell            *cell.Cell   `tl:"cell"`
	CellOptional    *cell.Cell   `tl:"cell optional"`
	InBytes         TestInner    `tl:"bytes struct boxed"`
	IP              net.IP       `tl:"int"`
	Str             string       `tl:"string"`
	BoolTrue        bool         `tl:"bool"`
	BoolFalse       bool         `tl:"bool"`
}

func TestParse(t *testing.T) {
	Register(TestInner{}, "in 123")
	Register(TestTL{}, "root 222")

	data, _ := hex.DecodeString(
		"391523a1" + "01000000" + "05000000" + "0900000000000000" + "01000000" + "0A00000000000000" +
			"e323006f" + "0200000000000000" + "7777777777777777777777777777777777777777777777777777777777777777" +
			"e323006f" + "0800000000000000" + "7177777777777777777777777777777777777777777777777777777777777777" +
			"02000000" + "e323006f" + "0700000000000000" + "7777777777777777777777777777777777777777777777777777777777777777" + "e323006f" + "0800000000000000" + "7777777777777777777777777777777777777777777777777777777777777777" +
			"0000000000000000000000000000000000000000000000000000000000000000" + "03000000" + "00000000" + "03112233" + "0411223344" + "000000" +
			"4d" + "b5ee9c72010104010042000114ff00f4a413f4bcf2c80b010201620203000ed05f04840ff2f00049a1c56105e1fab9f6e0bf71cfc409201390cedfb9e3496fe2f806bd6762483cab4506717e09" + "0000" +
			"4d" + "b5ee9c72010104010042000114ff00f4a413f4bcf2c80b010201620203000ed05f04840ff2f00049a1c56105e1fab9f6e0bf71cfc409201390cedfb9e3496fe2f806bd6762483cab4506717e09" + "0000" +
			"00000000" +
			"2c" + "e323006f" + "0200000000000000" + "7777777777777777777777777777777777777777777777777777777777777777" + "000000" +
			"04030201" + "073A3A3A3A3A3A3A" + "b5757299" + "379779bc")
	var tst TestTL
	_, err := Parse(&tst, data, true)
	if err != nil {
		panic(err)
	}
	tst.KeyEmpty = nil

	println(hex.EncodeToString(tst.IP))

	data2, err := Serialize(tst, true)
	if err != nil {
		panic(err)
	}

	if !bytes.Equal(data, data2) {
		println(hex.EncodeToString(data))
		println(hex.EncodeToString(data2))

		t.Fatal("data not eq after serialize")
	}
}

func TestHash(t *testing.T) {
	Register(TestInner{}, "root 777")

	hash, err := Hash(TestInner{Double: 777})
	if err != nil {
		t.Fatal(err.Error())
	}

	if hex.EncodeToString(hash) != "dc6da5a30847889eaa194ef0851c99eb721a3750450a6b353949172ea40456c2" {
		t.Fatal("incorrect hash " + hex.EncodeToString(hash))
	}
}
