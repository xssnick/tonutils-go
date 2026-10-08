package keys

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"strconv"
	"testing"
)

func sharedStreamInputs(t testing.TB) (key, checksum []byte) {
	t.Helper()

	key = make([]byte, 32)
	checksum = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(checksum); err != nil {
		t.Fatal(err)
	}
	return key, checksum
}

// TestXORSharedStreamMatchesCipherStream pins the new packet path to the
// keystream the old BuildSharedCipher produced. Both the direct counter loop
// and the crypto/cipher one are covered because the sizes cross
// ctrDirectMaxSize, and a peer must not be able to tell them apart.
func TestXORSharedStreamMatchesCipherStream(t *testing.T) {
	key, checksum := sharedStreamInputs(t)

	sizes := []int{0, 1, 15, 16, 17, 31, 32, 33, 63, 64, 127, 128,
		ctrDirectMaxSize - 1, ctrDirectMaxSize, ctrDirectMaxSize + 1,
		512, 1000, 1452, 4096}

	for _, size := range sizes {
		src := make([]byte, size)
		if _, err := rand.Read(src); err != nil {
			t.Fatal(err)
		}

		want := make([]byte, size)
		stream, err := BuildSharedCipher(key, checksum)
		if err != nil {
			t.Fatalf("size %d: BuildSharedCipher: %v", size, err)
		}
		stream.XORKeyStream(want, src)

		got := make([]byte, size)
		if err = XORSharedStream(got, src, key, checksum); err != nil {
			t.Fatalf("size %d: XORSharedStream: %v", size, err)
		}

		if !bytes.Equal(got, want) {
			t.Fatalf("size %d: keystream mismatch\n got %x\nwant %x", size, got, want)
		}

		// in-place is how every ADNL packet uses it
		inPlace := append([]byte(nil), src...)
		if err = XORSharedStream(inPlace, inPlace, key, checksum); err != nil {
			t.Fatalf("size %d: in-place XORSharedStream: %v", size, err)
		}
		if !bytes.Equal(inPlace, want) {
			t.Fatalf("size %d: in-place keystream mismatch", size)
		}
	}
}

// TestXORSharedStreamRoundTrip proves the stream is still its own inverse,
// which is what decodePacket relies on.
func TestXORSharedStreamRoundTrip(t *testing.T) {
	key, checksum := sharedStreamInputs(t)

	for _, size := range []int{7, ctrDirectMaxSize, ctrDirectMaxSize + 1, 1452} {
		src := make([]byte, size)
		if _, err := rand.Read(src); err != nil {
			t.Fatal(err)
		}

		data := append([]byte(nil), src...)
		if err := XORSharedStream(data, data, key, checksum); err != nil {
			t.Fatal(err)
		}
		if size > 0 && bytes.Equal(data, src) {
			t.Fatalf("size %d: data was not encrypted", size)
		}
		if err := XORSharedStream(data, data, key, checksum); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, src) {
			t.Fatalf("size %d: round trip mismatch", size)
		}
	}
}

func TestXORSharedStreamShortInput(t *testing.T) {
	key, checksum := sharedStreamInputs(t)
	data := make([]byte, 64)

	if err := XORSharedStream(data, data, key[:31], checksum); err != ErrShortCipherInput {
		t.Fatalf("short key: got %v, want %v", err, ErrShortCipherInput)
	}
	if err := XORSharedStream(data, data, key, checksum[:31]); err != ErrShortCipherInput {
		t.Fatalf("short checksum: got %v, want %v", err, ErrShortCipherInput)
	}
	if _, err := BuildSharedCipher(key[:31], checksum); err != ErrShortCipherInput {
		t.Fatalf("short key cipher: got %v, want %v", err, ErrShortCipherInput)
	}
}

// packet sizes: a bare nop/ack, a plumtree IHAVE envelope, half an MTU and a
// full ADNL datagram.
var sharedStreamBenchSizes = []int{64, 240, 512, 1024, 1452}

// BenchmarkSharedStreamBuild is the pre-change path: one cipher.Stream built
// per packet and thrown away.
func BenchmarkSharedStreamBuild(b *testing.B) {
	key, checksum := sharedStreamInputs(b)

	for _, size := range sharedStreamBenchSizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			data := make([]byte, size)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				stream, err := BuildSharedCipher(key, checksum)
				if err != nil {
					b.Fatal(err)
				}
				stream.XORKeyStream(data, data)
			}
		})
	}
}

func BenchmarkSharedStreamXOR(b *testing.B) {
	key, checksum := sharedStreamInputs(b)

	for _, size := range sharedStreamBenchSizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			data := make([]byte, size)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if err := XORSharedStream(data, data, key, checksum); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSharedStreamPathDirect and BenchmarkSharedStreamPathPipelined pin
// ctrDirectMaxSize: they run the two implementations over the same sizes so
// the crossover can be re-measured on a new platform or Go release.
func BenchmarkSharedStreamPathDirect(b *testing.B) {
	benchSharedStreamPath(b, func(block cipher.Block, s *ctrScratch, dst, src []byte) {
		xorCTRDirect(block, s, dst, src)
	})
}

func BenchmarkSharedStreamPathPipelined(b *testing.B) {
	benchSharedStreamPath(b, func(block cipher.Block, s *ctrScratch, dst, src []byte) {
		cipher.NewCTR(block, s.kiv[32:]).XORKeyStream(dst, src)
	})
}

func benchSharedStreamPath(b *testing.B, run func(cipher.Block, *ctrScratch, []byte, []byte)) {
	key, checksum := sharedStreamInputs(b)

	for _, size := range []int{128, 192, 256, 320, 384, 512, 768, 1024} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			data := make([]byte, size)
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				s := ctrScratchPool.Get().(*ctrScratch)
				if err := deriveSharedKIV(&s.kiv, key, checksum); err != nil {
					b.Fatal(err)
				}
				block, err := aes.NewCipher(s.kiv[:32])
				if err != nil {
					b.Fatal(err)
				}
				run(block, s, data, data)
				ctrScratchPool.Put(s)
			}
		})
	}
}

// TestXorCTRDirectAcrossChunks covers the direct loop beyond one keystream
// chunk. XORSharedStream never sends it inputs that big, but the path
// benchmark does, and a counter that did not carry across chunks would only
// show up here.
func TestXorCTRDirectAcrossChunks(t *testing.T) {
	key, checksum := sharedStreamInputs(t)

	for _, size := range []int{ctrDirectMaxSize + 1, 2*ctrDirectMaxSize - 1,
		2 * ctrDirectMaxSize, 5*ctrDirectMaxSize + 7} {
		src := make([]byte, size)
		if _, err := rand.Read(src); err != nil {
			t.Fatal(err)
		}

		want := make([]byte, size)
		stream, err := BuildSharedCipher(key, checksum)
		if err != nil {
			t.Fatal(err)
		}
		stream.XORKeyStream(want, src)

		s := ctrScratchPool.Get().(*ctrScratch)
		if err = deriveSharedKIV(&s.kiv, key, checksum); err != nil {
			t.Fatal(err)
		}
		block, err := aes.NewCipher(s.kiv[:32])
		if err != nil {
			t.Fatal(err)
		}
		got := make([]byte, size)
		xorCTRDirect(block, s, got, src)
		ctrScratchPool.Put(s)

		if !bytes.Equal(got, want) {
			t.Fatalf("size %d: direct loop diverges from cipher.Stream", size)
		}
	}
}
