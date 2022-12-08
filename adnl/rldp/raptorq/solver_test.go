package raptorq

import (
	"bytes"
	"encoding/hex"
	"math/rand"
	"testing"
)

func Test_Encode(t *testing.T) {
	str := "hello world bro! keke meme 881"

	r := NewRaptorQ(20)
	enc, err := r.CreateEncoder([]byte(str))
	if err != nil {
		panic(err)
	}

	should, _ := hex.DecodeString("05e6ddeb1f820e0a0f318b23128d889623663e66")
	sx := enc.GenSymbol(68238283)

	if !bytes.Equal(sx, should) {
		t.Fatal("encoded not eq, got", hex.EncodeToString(sx))
	}
}

func Test_EncodeDecode(t *testing.T) {
	str := []byte("hello world bro! keke meme 881")

	r := NewRaptorQ(20)
	enc, err := r.CreateEncoder(str)
	if err != nil {
		panic(err)
	}

	dec, err := r.CreateDecoder(uint32(len(str)))
	if err != nil {
		panic(err)
	}

	for i := uint32(0); i < 2; i++ {
		sx := enc.GenSymbol(i + 10000)

		_, err := dec.AddSymbol(i+10000, sx)
		if err != nil {
			t.Fatal("add symbol err", err)
		}
	}

	_, data, err := dec.Decode()
	if err != nil {
		t.Fatal("decode err", err)
	}

	if !bytes.Equal(data, str) {
		t.Fatal("initial data not eq decrypted")
	}
}

func Test_EncodeDecodeFuzz(t *testing.T) {
	for n := 0; n < 100; n++ {
		str := make([]byte, 4096)
		rand.Read(str)

		symSz := (1 + (rand.Uint32() % 10)) * 10
		r := NewRaptorQ(symSz)
		enc, err := r.CreateEncoder(str)
		if err != nil {
			panic(err)
		}

		dec, err := r.CreateDecoder(uint32(len(str)))
		if err != nil {
			panic(err)
		}

		_, err = dec.AddSymbol(2, enc.GenSymbol(2))
		if err != nil {
			t.Fatal("add 2 symbol err", err)
		}

		for i := uint32(0); i < enc.params._K; i++ {
			sx := enc.GenSymbol(i + 10000)

			_, err := dec.AddSymbol(i+10000, sx)
			if err != nil {
				t.Fatal("add symbol err", err)
			}
		}

		_, data, err := dec.Decode()
		if err != nil {
			t.Fatal("decode err", err)
		}

		if !bytes.Equal(data, str) {
			t.Fatal("initial data not eq decrypted")
		}
	}
}

func Benchmark_EncodeDecodeFuzz(b *testing.B) {
	str := make([]byte, 4096)
	rand.Read(str)
	for n := 0; n < 100; n++ {
		var symSz uint32 = 768
		r := NewRaptorQ(symSz)
		enc, err := r.CreateEncoder(str)
		if err != nil {
			panic(err)
		}

		dec, err := r.CreateDecoder(uint32(len(str)))
		if err != nil {
			panic(err)
		}

		_, err = dec.AddSymbol(2, enc.GenSymbol(2))
		if err != nil {
			b.Fatal("add 2 symbol err", err)
		}

		for i := uint32(0); i < enc.params._K; i++ {
			sx := enc.GenSymbol(i + 10000)

			_, err := dec.AddSymbol(i+10000, sx)
			if err != nil {
				b.Fatal("add symbol err", err)
			}
		}

		_, data, err := dec.Decode()
		if err != nil {
			b.Fatal("decode err", err)
		}

		if !bytes.Equal(data, str) {
			b.Fatal("initial data not eq decrypted")
		}
	}
}
