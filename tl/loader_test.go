package tl

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"net"
	"reflect"
	"testing"
)

var testData = func() []byte {
	data, _ := hex.DecodeString(
		"391523a1" + "01000000" + "05000000" + "0900000000000000" + "01000000" + "0A00000000000000" + "0A00000000000000" + "0A00000000000000" + "0A00000000000000" + "0A00000000000000" + "0A00000000000000" + "0A00000000000000" +
			"e323006f" + "0200000000000000" + "7777777777777777777777777777777777777777777777777777777777777777" +
			"e323006f" + "0800000000000000" + "7177777777777777777777777777777777777777777777777777777777777777" +
			"02000000" + "e323006f" + "0700000000000000" + "7777777777777777777777777777777777777777777777777777777777777777" + "e323006f" + "0800000000000000" + "7777777777777777777777777777777777777777777777777777777777777777" +
			"0000000000000000000000000000000000000000000000000000000000000000" + "03000000" + "00000000" + "03112233" + "0411223344" + "000000" +
			"4d" + "b5ee9c72010104010042000114ff00f4a413f4bcf2c80b010201620203000ed05f04840ff2f00049a1c56105e1fab9f6e0bf71cfc409201390cedfb9e3496fe2f806bd6762483cab4506717e09" + "0000" +
			"4d" + "b5ee9c72010104010042000114ff00f4a413f4bcf2c80b010201620203000ed05f04840ff2f00049a1c56105e1fab9f6e0bf71cfc409201390cedfb9e3496fe2f806bd6762483cab4506717e09" + "0000" +
			"00000000" +
			"2c" + "e323006f" + "0200000000000000" + "7777777777777777777777777777777777777777777777777777777777777777" + "000000" +
			"04030201" + "073A3A3A3A3A3A3A" + "b5757299" + "379779bc" + "00000001" +
			"08" + "1659c7bd" + "11223344" + "000000" +
			"10" + "1659c7bd" + "AAAAAAAA" + "1659c7bd" + "BBBBBBBB" + "000000" + "FFA0000000000000" + "FFB00000" + "FFC0000000000000000000000000000F" +
			"00" + "000000",
		// "55000000000000000000000000000055",
	)
	return data
}()

type Small struct {
	Value uint32 `tl:"int"`
}

type TestManual struct {
	Value [4]byte
}

func (t *TestManual) Serialize(buf *bytes.Buffer) error {
	buf.Write(t.Value[:])
	return nil
}

func (t *TestManual) Parse(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, errors.New("invalid data")
	}
	copy(t.Value[:], data[:4])
	return data[4:], nil
}

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
	SimpleUintBig2  uint32       `tl:"long"`
	SimpleUintBig3  uint16       `tl:"long"`
	SimpleUintBig4  int32        `tl:"long"`
	SimpleUintBig5  int8         `tl:"long"`
	SimpleUintBig6  int          `tl:"long"`
	SimpleUintBig7  uint         `tl:"long"`
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
	Manual          TestManual   `tl:"struct"`
	AnyBytes        Serializable `tl:"bytes struct boxed"`
	AnyBytesMany    Serializable `tl:"bytes struct boxed"`
	Key64           []byte       `tl:"long"`
	Key32           []byte       `tl:"int"`
	Key128          []byte       `tl:"int128"`
	CellArrOpt      []*cell.Cell `tl:"cell optional 500000"`
}

func init() {
	Register(TestInner{}, "in 123") // root 777
	Register(TestTL{}, "root 222")
	Register(TestManual{}, "manual val")

	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, RegisterWithFabric(Small{}, "small 123", func() reflect.Value {
		return reflect.ValueOf(&Small{})
	}))
	println(hex.EncodeToString(buf))
}

func parse() (TestTL, []byte) {
	var tst TestTL
	_, err := Parse(&tst, testData, true)
	if err != nil {
		panic(err)
	}
	tst.KeyEmpty = nil
	return tst, testData
}

func TestSerialize(t *testing.T) {
	tst, data := parse()

	data2, err := Serialize(&tst, true)
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
	// Register(TestInner{}, "root 777")

	hash, err := Hash(TestInner{Double: 777})
	if err != nil {
		t.Fatal(err.Error())
	}

	if hex.EncodeToString(hash) != "7e1e3df09869a6f2e7d8488a9433409190eef332c0efd3e8d1147f3e5608b5ed" {
		t.Fatal("incorrect hash " + hex.EncodeToString(hash))
	}
}

func BenchmarkSerializePrecompiled(b *testing.B) {
	tst, _ := parse()

	b.ResetTimer()
	b.ReportAllocs()
	var dt []byte
	for i := 0; i < b.N; i++ {
		var err error
		dt, err = Serialize(&tst, true)
		if err != nil {
			panic(err)
		}
	}
	_ = dt
}

func BenchmarkParse(b *testing.B) {
	data := testData

	b.ResetTimer()
	b.ReportAllocs()
	var tst TestTL
	for i := 0; i < b.N; i++ {
		_, err := Parse(&tst, data, true)
		if err != nil {
			panic(err)
		}
	}
	_ = tst
}
