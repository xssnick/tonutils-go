package blsmap

import (
	"bytes"
	"errors"
	"testing"

	"github.com/cloudflare/circl/ecc/bls12381/ff"
)

func mappedG1(input []byte) g1 {
	var u ff.Fp
	u.SetBytes(input[:ff.FpSize])

	var q isogG1Point
	q.sswu(&u)

	var p g1
	p.evalIsogG1(&q)
	p.clearCofactor()
	return p
}

func mappedG2(input []byte) g2 {
	var u ff.Fp2
	u[0].SetBytes(input[:ff.FpSize])
	u[1].SetBytes(input[ff.FpSize : 2*ff.FpSize])

	var q isogG2Point
	q.sswu(&u)

	var p g2
	p.evalIsogG2(&q)
	p.clearCofactor()
	return p
}

func TestHeaderEncodingAndErr(t *testing.T) {
	if got := headerEncoding(1, 1, 0); got != 0xC0 {
		t.Fatalf("unexpected compressed+infinity header: %08b", got)
	}
	if got := headerEncoding(1, 0, 1); got != 0xA0 {
		t.Fatalf("unexpected compressed+sign header: %08b", got)
	}

	err(nil)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("err should panic on non-nil errors")
		}
	}()
	err(errors.New("boom"))
}

func TestMapToG1AndIdentityBehavior(t *testing.T) {
	if _, err := MapToG1(make([]byte, ff.FpSize-1)); !errors.Is(err, errInputLength) {
		t.Fatalf("expected short input error, got %v", err)
	}

	input := bytes.Repeat([]byte{0x42}, ff.FpSize)
	out, err := MapToG1(input)
	if err != nil {
		t.Fatalf("MapToG1 failed: %v", err)
	}
	outAgain, err := MapToG1(input)
	if err != nil {
		t.Fatalf("MapToG1 failed on second run: %v", err)
	}
	if len(out) != ff.FpSize {
		t.Fatalf("unexpected G1 compressed size: %d", len(out))
	}
	if !bytes.Equal(out, outAgain) {
		t.Fatal("MapToG1 should be deterministic")
	}
	if out[0]&0x80 == 0 || out[0]&0x40 != 0 {
		t.Fatalf("unexpected G1 header bits: %08b", out[0])
	}

	p := mappedG1(input)
	if got := p.bytesCompressed(); !bytes.Equal(got, out) {
		t.Fatalf("direct map and public helper disagree:\nwant %x\ngot  %x", out, got)
	}

	var id g1
	id.setIdentity()
	identityBytes := id.bytesCompressed()
	if len(identityBytes) != ff.FpSize || identityBytes[0]&0xC0 != 0xC0 {
		t.Fatalf("unexpected identity encoding: %x", identityBytes)
	}
	for i, b := range identityBytes[1:] {
		if b != 0 {
			t.Fatalf("identity encoding should be zeroed after header, byte %d = %x", i+1, b)
		}
	}

	var sum g1
	sum.add(&id, &p)
	if got := sum.bytesCompressed(); !bytes.Equal(got, out) {
		t.Fatalf("identity + p should equal p:\nwant %x\ngot  %x", out, got)
	}

	var doubledIdentity g1
	doubledIdentity = id
	doubledIdentity.double()
	if got := doubledIdentity.bytesCompressed(); !bytes.Equal(got, identityBytes) {
		t.Fatalf("double(identity) should stay identity:\nwant %x\ngot  %x", identityBytes, got)
	}

	var zeroMul g1
	zeroMul.scalarMultShort([]byte{0x00}, &p)
	if got := zeroMul.bytesCompressed(); !bytes.Equal(got, identityBytes) {
		t.Fatalf("0 * p should be identity:\nwant %x\ngot  %x", identityBytes, got)
	}
}

func TestMapToG2AndIdentityBehavior(t *testing.T) {
	if _, err := MapToG2(make([]byte, ff.Fp2Size-1)); !errors.Is(err, errInputLength) {
		t.Fatalf("expected short input error, got %v", err)
	}

	input := bytes.Repeat([]byte{0x24}, ff.Fp2Size)
	out, err := MapToG2(input)
	if err != nil {
		t.Fatalf("MapToG2 failed: %v", err)
	}
	outAgain, err := MapToG2(input)
	if err != nil {
		t.Fatalf("MapToG2 failed on second run: %v", err)
	}
	if len(out) != ff.Fp2Size {
		t.Fatalf("unexpected G2 compressed size: %d", len(out))
	}
	if !bytes.Equal(out, outAgain) {
		t.Fatal("MapToG2 should be deterministic")
	}
	if out[0]&0x80 == 0 || out[0]&0x40 != 0 {
		t.Fatalf("unexpected G2 header bits: %08b", out[0])
	}

	p := mappedG2(input)
	if got := p.bytesCompressed(); !bytes.Equal(got, out) {
		t.Fatalf("direct map and public helper disagree:\nwant %x\ngot  %x", out, got)
	}

	var id g2
	id.setIdentity()
	identityBytes := id.bytesCompressed()
	if len(identityBytes) != ff.Fp2Size || identityBytes[0]&0xC0 != 0xC0 {
		t.Fatalf("unexpected identity encoding: %x", identityBytes)
	}
	for i, b := range identityBytes[1:] {
		if b != 0 {
			t.Fatalf("identity encoding should be zeroed after header, byte %d = %x", i+1, b)
		}
	}

	var sum g2
	sum.add(&id, &p)
	if got := sum.bytesCompressed(); !bytes.Equal(got, out) {
		t.Fatalf("identity + p should equal p:\nwant %x\ngot  %x", out, got)
	}

	negTwice := p
	negTwice.neg()
	negTwice.neg()
	if got := negTwice.bytesCompressed(); !bytes.Equal(got, out) {
		t.Fatalf("double negation should restore point:\nwant %x\ngot  %x", out, got)
	}

	var zeroMul g2
	zeroMul.scalarMultShort([]byte{0x00}, &p)
	if got := zeroMul.bytesCompressed(); !bytes.Equal(got, identityBytes) {
		t.Fatalf("0 * p should be identity:\nwant %x\ngot  %x", identityBytes, got)
	}
}
