package tvm

import (
	"testing"

	circlbls "github.com/xssnick/tonutils-go/tvm/internal/bls12381"
)

func TestBLSOffSubgroupHelpersSanity(t *testing.T) {
	g1 := testBLSG1OffSubgroupBytes(t)
	if len(g1) != 48 {
		t.Fatalf("g1 off-subgroup len=%d", len(g1))
	}
	var a circlbls.G1
	if a.SetBytesOnCurve(g1) != nil {
		t.Fatal("g1 off-subgroup not accepted on-curve")
	}
	if a.SetBytes(g1) == nil {
		t.Fatal("g1 off-subgroup unexpectedly passed full subgroup check")
	}

	g2 := testBLSG2OffSubgroupBytes(t)
	if len(g2) != 96 {
		t.Fatalf("g2 off-subgroup len=%d", len(g2))
	}
	var b circlbls.G2
	if b.SetBytesOnCurve(g2) != nil {
		t.Fatal("g2 off-subgroup not accepted on-curve")
	}
	if b.SetBytes(g2) == nil {
		t.Fatal("g2 off-subgroup unexpectedly passed full subgroup check")
	}
}
