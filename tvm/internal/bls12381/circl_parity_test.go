package bls12381

import (
	"bytes"
	"testing"

	circl "github.com/cloudflare/circl/ecc/bls12381"
)

func TestCIRCLConstantsParity(t *testing.T) {
	if ScalarSize != circl.ScalarSize || G1Size != circl.G1Size || G1SizeCompressed != circl.G1SizeCompressed ||
		G2Size != circl.G2Size || G2SizeCompressed != circl.G2SizeCompressed || GtSize != circl.GtSize {
		t.Fatal("vendored group size constants differ from CIRCL v1.6.4")
	}
	if !bytes.Equal(Order(), circl.Order()) {
		t.Fatalf("group order differs:\nlocal %x\ncircl %x", Order(), circl.Order())
	}
}

func TestCIRCLGroupAndSerializationParity(t *testing.T) {
	for _, k := range []uint64{0, 1, 2, 3, 17, 255, 65537} {
		var localScalar Scalar
		localScalar.SetUint64(k)
		var circlScalar circl.Scalar
		circlScalar.SetUint64(k)

		var localG1 G1
		localG1.ScalarMult(&localScalar, G1Generator())
		var circlG1 circl.G1
		circlG1.ScalarMult(&circlScalar, circl.G1Generator())
		assertBytesEqual(t, "G1 compressed", k, localG1.BytesCompressed(), circlG1.BytesCompressed())
		assertBytesEqual(t, "G1 uncompressed", k, localG1.Bytes(), circlG1.Bytes())

		var decodedG1 G1
		if err := decodedG1.SetBytes(circlG1.BytesCompressed()); err != nil {
			t.Fatalf("k=%d: local G1 rejected CIRCL encoding: %v", k, err)
		}

		var localG2 G2
		localG2.ScalarMult(&localScalar, G2Generator())
		var circlG2 circl.G2
		circlG2.ScalarMult(&circlScalar, circl.G2Generator())
		assertBytesEqual(t, "G2 compressed", k, localG2.BytesCompressed(), circlG2.BytesCompressed())
		assertBytesEqual(t, "G2 uncompressed", k, localG2.Bytes(), circlG2.Bytes())

		var decodedG2 G2
		if err := decodedG2.SetBytes(circlG2.BytesCompressed()); err != nil {
			t.Fatalf("k=%d: local G2 rejected CIRCL encoding: %v", k, err)
		}
	}

	var localA, localB Scalar
	localA.SetUint64(19)
	localB.SetUint64(23)
	var circlA, circlB circl.Scalar
	circlA.SetUint64(19)
	circlB.SetUint64(23)

	var localG1A, localG1B, localG1Sum G1
	localG1A.ScalarMult(&localA, G1Generator())
	localG1B.ScalarMult(&localB, G1Generator())
	localG1Sum.Add(&localG1A, &localG1B)
	var circlG1A, circlG1B, circlG1Sum circl.G1
	circlG1A.ScalarMult(&circlA, circl.G1Generator())
	circlG1B.ScalarMult(&circlB, circl.G1Generator())
	circlG1Sum.Add(&circlG1A, &circlG1B)
	assertBytesEqual(t, "G1 add", 0, localG1Sum.BytesCompressed(), circlG1Sum.BytesCompressed())
	localG1Sum = localG1A
	localG1Sum.Double()
	circlG1Sum = circlG1A
	circlG1Sum.Double()
	assertBytesEqual(t, "G1 double", 0, localG1Sum.BytesCompressed(), circlG1Sum.BytesCompressed())
	localG1Sum.Neg()
	circlG1Sum.Neg()
	assertBytesEqual(t, "G1 neg", 0, localG1Sum.BytesCompressed(), circlG1Sum.BytesCompressed())

	var localG2A, localG2B, localG2Sum G2
	localG2A.ScalarMult(&localA, G2Generator())
	localG2B.ScalarMult(&localB, G2Generator())
	localG2Sum.Add(&localG2A, &localG2B)
	var circlG2A, circlG2B, circlG2Sum circl.G2
	circlG2A.ScalarMult(&circlA, circl.G2Generator())
	circlG2B.ScalarMult(&circlB, circl.G2Generator())
	circlG2Sum.Add(&circlG2A, &circlG2B)
	assertBytesEqual(t, "G2 add", 0, localG2Sum.BytesCompressed(), circlG2Sum.BytesCompressed())
	localG2Sum = localG2A
	localG2Sum.Double()
	circlG2Sum = circlG2A
	circlG2Sum.Double()
	assertBytesEqual(t, "G2 double", 0, localG2Sum.BytesCompressed(), circlG2Sum.BytesCompressed())
	localG2Sum.Neg()
	circlG2Sum.Neg()
	assertBytesEqual(t, "G2 neg", 0, localG2Sum.BytesCompressed(), circlG2Sum.BytesCompressed())
}

func TestCIRCLHashEncodeAndPairingParity(t *testing.T) {
	messages := [][]byte{nil, []byte("abc"), bytes.Repeat([]byte{0xA5}, 257)}
	dsts := [][]byte{[]byte("TON_BLS_PARITY_G1"), []byte("TON_BLS_PARITY_G2")}
	for i, msg := range messages {
		var localG1 G1
		var circlG1 circl.G1
		localG1.Encode(msg, dsts[0])
		circlG1.Encode(msg, dsts[0])
		assertBytesEqual(t, "G1 encode", uint64(i), localG1.BytesCompressed(), circlG1.BytesCompressed())
		localG1.Hash(msg, dsts[0])
		circlG1.Hash(msg, dsts[0])
		assertBytesEqual(t, "G1 hash", uint64(i), localG1.BytesCompressed(), circlG1.BytesCompressed())

		var localG2 G2
		var circlG2 circl.G2
		localG2.Encode(msg, dsts[1])
		circlG2.Encode(msg, dsts[1])
		assertBytesEqual(t, "G2 encode", uint64(i), localG2.BytesCompressed(), circlG2.BytesCompressed())
		localG2.Hash(msg, dsts[1])
		circlG2.Hash(msg, dsts[1])
		assertBytesEqual(t, "G2 hash", uint64(i), localG2.BytesCompressed(), circlG2.BytesCompressed())
	}

	localPair, err := Pair(G1Generator(), G2Generator()).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	circlPair, err := circl.Pair(circl.G1Generator(), circl.G2Generator()).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	assertBytesEqual(t, "pairing", 0, localPair, circlPair)

	var localTwo, localThree Scalar
	localTwo.SetUint64(2)
	localThree.SetUint64(3)
	var circlTwo, circlThree circl.Scalar
	circlTwo.SetUint64(2)
	circlThree.SetUint64(3)
	localProd, err := ProdPair(
		[]*G1{G1Generator(), G1Generator()},
		[]*G2{G2Generator(), G2Generator()},
		[]*Scalar{&localTwo, &localThree},
	).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	circlProd, err := circl.ProdPair(
		[]*circl.G1{circl.G1Generator(), circl.G1Generator()},
		[]*circl.G2{circl.G2Generator(), circl.G2Generator()},
		[]*circl.Scalar{&circlTwo, &circlThree},
	).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	assertBytesEqual(t, "pairing product", 0, localProd, circlProd)
}

func assertBytesEqual(t *testing.T, name string, k uint64, got, want []byte) {
	t.Helper()
	if !bytes.Equal(got, want) {
		t.Fatalf("%s k=%d differs:\nlocal %x\ncircl %x", name, k, got, want)
	}
}
