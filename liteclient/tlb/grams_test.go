package tlb

import (
	"testing"
)

func TestGrams_FromTON(t *testing.T) {
	g := MustFromTON("0").NanoTON().Uint64()
	if g != 0 {
		t.Fatalf("0 wrong: %d", g)
	}

	g = MustFromTON("0.0000").NanoTON().Uint64()
	if g != 0 {
		t.Fatalf("0 wrong: %d", g)
	}

	g = MustFromTON("7").NanoTON().Uint64()
	if g != 7000000000 {
		t.Fatalf("7 wrong: %d", g)
	}

	g = MustFromTON("7.518").NanoTON().Uint64()
	if g != 7518000000 {
		t.Fatalf("7.518 wrong: %d", g)
	}

	g = MustFromTON("17.98765432111").NanoTON().Uint64()
	if g != 17987654321 {
		t.Fatalf("17.98765432111 wrong: %d", g)
	}

	g = MustFromTON("0.000000001").NanoTON().Uint64()
	if g != 1 {
		t.Fatalf("0.000000001 wrong: %d", g)
	}

	g = MustFromTON("0.090000001").NanoTON().Uint64()
	if g != 90000001 {
		t.Fatalf("0.090000001 wrong: %d", g)
	}

	_, err := FromTON("17.987654.32111")
	if err == nil {
		t.Fatalf("17.987654.32111 should be error: %d", g)
	}

	_, err = FromTON(".17")
	if err == nil {
		t.Fatalf(".17 should be error: %d", g)
	}

	_, err = FromTON("0..17")
	if err == nil {
		t.Fatalf("0..17 should be error: %d", g)
	}
}
