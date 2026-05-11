package tvm

import (
	"bytes"
	"math/big"
	"testing"

	ristretto "github.com/bwesterb/go-ristretto"
	circlbls "github.com/cloudflare/circl/ecc/bls12381"
	circlgroup "github.com/cloudflare/circl/group"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

const tonBLSTestDST = "BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_"

func testSliceFromBytes(data []byte) *cell.Slice {
	return cell.BeginCell().MustStoreSlice(data, uint(len(data))*8).ToSlice()
}

func testSliceToBytes(t *testing.T, sl *cell.Slice) []byte {
	t.Helper()

	if sl.BitsLeft()%8 != 0 {
		t.Fatalf("slice is not byte aligned: %d bits", sl.BitsLeft())
	}

	data, err := sl.LoadSlice(sl.BitsLeft())
	if err != nil {
		t.Fatalf("failed to load slice bytes: %v", err)
	}
	return data
}

func testBLSScalar(n int64) *circlbls.Scalar {
	var s circlbls.Scalar
	s.SetBytes(big.NewInt(n).FillBytes(make([]byte, circlbls.ScalarSize)))
	return &s
}

func testBLSPubBytes(n int64) []byte {
	var out circlbls.G1
	out.ScalarMult(testBLSScalar(n), circlbls.G1Generator())
	return out.BytesCompressed()
}

func testBLSSigBytes(n int64, msg []byte) []byte {
	var hash circlbls.G2
	hash.Hash(msg, []byte(tonBLSTestDST))

	var sig circlbls.G2
	sig.ScalarMult(testBLSScalar(n), &hash)
	return sig.BytesCompressed()
}

func testBLSAggregateSigBytes(t *testing.T, sigs ...[]byte) []byte {
	t.Helper()

	var agg circlbls.G2
	for i, raw := range sigs {
		var p circlbls.G2
		if err := p.SetBytes(raw); err != nil {
			t.Fatalf("failed to parse signature %d: %v", i, err)
		}
		if i == 0 {
			agg = p
		} else {
			agg.Add(&agg, &p)
		}
	}
	return agg.BytesCompressed()
}

func testBLSG1BytesForScalar(n int64) []byte {
	return testBLSPubBytes(n)
}

func testBLSG2BytesForScalar(n int64) []byte {
	var out circlbls.G2
	out.ScalarMult(testBLSScalar(n), circlbls.G2Generator())
	return out.BytesCompressed()
}

func testBLSG1ZeroBytes() []byte {
	var zero circlbls.G1
	zero.SetIdentity()
	return zero.BytesCompressed()
}

func testBLSG2ZeroBytes() []byte {
	var zero circlbls.G2
	zero.SetIdentity()
	return zero.BytesCompressed()
}

func testInvalidBLSG1Bytes(t *testing.T) []byte {
	t.Helper()

	candidates := [][]byte{
		bytes.Repeat([]byte{0x00}, 48),
		bytes.Repeat([]byte{0xFF}, 48),
		bytes.Repeat([]byte{0x01}, 48),
		append([]byte{0xC0}, bytes.Repeat([]byte{0x00}, 47)...),
	}

	for _, candidate := range candidates {
		var p circlbls.G1
		if err := p.SetBytes(candidate); err != nil {
			return candidate
		}
	}

	valid := testBLSG1BytesForScalar(1)
	for i := range valid {
		for bit := 0; bit < 8; bit++ {
			candidate := append([]byte(nil), valid...)
			candidate[i] ^= 1 << bit

			var p circlbls.G1
			if err := p.SetBytes(candidate); err != nil {
				return candidate
			}
		}
	}

	t.Fatal("failed to find invalid BLS G1 encoding")
	return nil
}

func testInvalidBLSG2Bytes(t *testing.T) []byte {
	t.Helper()

	candidates := [][]byte{
		bytes.Repeat([]byte{0x00}, 96),
		bytes.Repeat([]byte{0xFF}, 96),
		bytes.Repeat([]byte{0x01}, 96),
		append([]byte{0xC0}, bytes.Repeat([]byte{0x00}, 95)...),
	}

	for _, candidate := range candidates {
		var p circlbls.G2
		if err := p.SetBytes(candidate); err != nil {
			return candidate
		}
	}

	valid := testBLSG2BytesForScalar(1)
	for i := range valid {
		for bit := 0; bit < 8; bit++ {
			candidate := append([]byte(nil), valid...)
			candidate[i] ^= 1 << bit

			var p circlbls.G2
			if err := p.SetBytes(candidate); err != nil {
				return candidate
			}
		}
	}

	t.Fatal("failed to find invalid BLS G2 encoding")
	return nil
}

func testRistrettoElementInt(t *testing.T, el circlgroup.Element) *big.Int {
	t.Helper()

	raw, err := el.MarshalBinary()
	if err != nil {
		t.Fatalf("failed to marshal ristretto element: %v", err)
	}
	return new(big.Int).SetBytes(raw)
}

func testRistrettoMulBaseInt(t *testing.T, n int64) *big.Int {
	t.Helper()

	scalar := circlgroup.Ristretto255.NewScalar().SetBigInt(big.NewInt(n))
	el := circlgroup.Ristretto255.NewElement()
	el.MulGen(scalar)
	return testRistrettoElementInt(t, el)
}

func testInvalidRistrettoInt(t *testing.T) *big.Int {
	t.Helper()

	candidates := [][]byte{
		bytes.Repeat([]byte{0xFF}, 32),
		bytes.Repeat([]byte{0x01}, 32),
		append([]byte{0x80}, bytes.Repeat([]byte{0x00}, 31)...),
	}

	for _, candidate := range candidates {
		el := circlgroup.Ristretto255.NewElement()
		if err := el.UnmarshalBinary(candidate); err != nil {
			return new(big.Int).SetBytes(candidate)
		}
	}

	for i := 1; i < 256; i++ {
		candidate := make([]byte, 32)
		candidate[31] = byte(i)
		el := circlgroup.Ristretto255.NewElement()
		if err := el.UnmarshalBinary(candidate); err != nil {
			return new(big.Int).SetBytes(candidate)
		}
	}

	t.Fatal("failed to find invalid ristretto encoding")
	return nil
}

func testRistrettoFromHashInt(t *testing.T, x1, x2 *big.Int) *big.Int {
	t.Helper()

	x1Bytes := x1.FillBytes(make([]byte, 32))
	x2Bytes := x2.FillBytes(make([]byte, 32))

	var u1, u2 [32]byte
	copy(u1[:], x1Bytes)
	copy(u2[:], x2Bytes)

	var p1, p2, out ristretto.Point
	p1.SetElligator(&u1)
	p2.SetElligator(&u2)
	out.Add(&p1, &p2)
	return new(big.Int).SetBytes(out.Bytes())
}

func testBLSFPBytes(seed byte) []byte {
	data := make([]byte, 48)
	for i := range data {
		data[i] = seed + byte(i)
	}
	return data
}

func testBLSFP2Bytes(seed byte) []byte {
	data := make([]byte, 96)
	for i := range data {
		data[i] = seed + byte(i*3+1)
	}
	return data
}
