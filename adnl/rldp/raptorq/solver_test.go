package raptorq

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
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
	for n := 0; n < 1000; n++ {
		str := make([]byte, 4096)
		_, _ = rand.Read(str)

		buf := make([]byte, 4)
		if _, err := rand.Read(buf); err != nil {
			panic(err)
		}
		rnd := binary.LittleEndian.Uint32(buf)

		symSz := (1 + (rnd % 10)) * 10
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

// history of optimizations
// Benchmark_EncodeDecodeFuzz-12: MatrixGF2    	                 907	   1329193 ns/op	  653478 B/op	    1806 allocs/op
// Benchmark_EncodeDecodeFuzz-12: PlainMatrixGF2                1082	   1114249 ns/op	  652923 B/op	    1810 allocs/op
// Benchmark_EncodeDecodeFuzz-10    	                        4532	    264006 ns/op	  656378 B/op	    1804 allocs/op
// Benchmark_EncodeDecodeFuzz-10    	                        4906	    239055 ns/op	  675706 B/op	     959 allocs/op
// Benchmark_EncodeDecodeFuzz-10    	                        5042	    237723 ns/op	  680747 B/op	     799 allocs/op
// Benchmark_EncodeDecodeFuzz-10    	                        4368	    247088 ns/op	  683931 B/op	     687 allocs/op
// Benchmark_EncodeDecodeFuzz-10    	                        5692	    208915 ns/op	  506834 B/op	     374 allocs/op
// Benchmark_EncodeDecodeFuzz-10    	                        5490	    190236 ns/op	  365026 B/op	     225 allocs/op
// Benchmark_EncodeDecodeFuzz-10    	                        6654	    177373 ns/op	  364961 B/op	     223 allocs/op
// Benchmark_EncodeDecodeFuzz-10    	                        5732	    182353 ns/op	  364865 B/op	     203 allocs/op
func Benchmark_EncodeDecodeFuzz(b *testing.B) {
	str := make([]byte, 4096)
	rand.Read(str)

	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		var symSz uint32 = 768
		r := NewRaptorQ(symSz)
		enc, err := r.CreateEncoder(str)
		if err != nil {
			panic(err)
		}

		dec, err := r.CreateDecoder(uint32(len(str)))
		if err != nil {
			b.Fatal("create decoder err", err)
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

		_, _, err = dec.Decode()
		if err != nil {
			b.Fatal("decode err", err)
		}
	}
}
