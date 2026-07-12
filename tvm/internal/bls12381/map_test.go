package bls12381

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/cloudflare/circl/ecc/bls12381/ff"
)

func TestMapToCurveReferenceVectors(t *testing.T) {
	// These inputs are also executed against cppnode's libemulator by
	// TestTVMCrossEmulatorTonOpsCryptoCircl. The expected encodings pin the legacy
	// blsmap result while this package reuses CIRCL's native map internals.
	g1Input := make([]byte, ff.FpSize)
	for i := range g1Input {
		g1Input[i] = 7 + byte(i)
	}
	g2Input := make([]byte, ff.Fp2Size)
	for i := range g2Input {
		g2Input[i] = 11 + byte(i*3+1)
	}

	tests := []struct {
		name string
		mapf func([]byte) ([]byte, error)
		in   []byte
		want string
	}{
		{
			name: "g1",
			mapf: MapToG1,
			in:   g1Input,
			want: "84f893a0958b579101e6daeb088cfbf867517c9bb13f998b6d6d4dc396487a90954982a0b4b42a547eeccea4c3548288",
		},
		{
			name: "g2",
			mapf: MapToG2,
			in:   g2Input,
			want: "a0af86b27c19ff378ea4abd57ee7c58db37b548e89137755f1d6598e62d4734bd4b52e3a562cb6ad8b957850412080dd0c7098aaca6b17faaacb1b5fe4205e33804da7be0aa52fe4e52b7deb238c7eb59b64ba1580150a51cdc984929612699a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, err := hex.DecodeString(tt.want)
			if err != nil {
				t.Fatal(err)
			}
			got, err := tt.mapf(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("unexpected mapped point:\nwant %x\ngot  %x", want, got)
			}
		})
	}
}

func TestMapToCurveInputAndSubgroupProperties(t *testing.T) {
	if _, err := MapToG1(make([]byte, ff.FpSize-1)); !errors.Is(err, errInputLength) {
		t.Fatalf("MapToG1 short input error = %v", err)
	}
	if _, err := MapToG2(make([]byte, ff.Fp2Size-1)); !errors.Is(err, errInputLength) {
		t.Fatalf("MapToG2 short input error = %v", err)
	}

	for i := byte(0); i < 64; i++ {
		seed := sha512.Sum512([]byte{i})
		g1Input := append([]byte(nil), seed[:ff.FpSize]...)
		second := sha512.Sum512(seed[:])
		g2Input := append(append([]byte(nil), seed[:]...), second[:ff.Fp2Size-len(seed)]...)

		g1Bytes, err := MapToG1(g1Input)
		if err != nil {
			t.Fatalf("case %d: MapToG1: %v", i, err)
		}
		var g1 G1
		if err = g1.SetBytes(g1Bytes); err != nil {
			t.Fatalf("case %d: mapped G1 is not canonically encoded in-subgroup: %v", i, err)
		}
		if !g1.InSubgroup() {
			t.Fatalf("case %d: mapped G1 is outside the subgroup", i)
		}

		g2Bytes, err := MapToG2(g2Input)
		if err != nil {
			t.Fatalf("case %d: MapToG2: %v", i, err)
		}
		var g2 G2
		if err = g2.SetBytes(g2Bytes); err != nil {
			t.Fatalf("case %d: mapped G2 is not canonically encoded in-subgroup: %v", i, err)
		}
		if !g2.InSubgroup() {
			t.Fatalf("case %d: mapped G2 is outside the subgroup", i)
		}
	}
}

func TestMapToCurveIgnoresTrailingBytes(t *testing.T) {
	g1Input := bytes.Repeat([]byte{0x42}, ff.FpSize)
	g2Input := bytes.Repeat([]byte{0x24}, ff.Fp2Size)

	g1, err := MapToG1(g1Input)
	if err != nil {
		t.Fatal(err)
	}
	g1Trailing, err := MapToG1(append(append([]byte(nil), g1Input...), 1, 2, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g1, g1Trailing) {
		t.Fatal("MapToG1 used bytes past the raw field element")
	}

	g2, err := MapToG2(g2Input)
	if err != nil {
		t.Fatal(err)
	}
	g2Trailing, err := MapToG2(append(append([]byte(nil), g2Input...), 1, 2, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g2, g2Trailing) {
		t.Fatal("MapToG2 used bytes past the raw field element")
	}
}
