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

func TestGrams_TON(t *testing.T) {
	g := MustFromTON("0.090000001")
	if g.TON() != "0.090000001" {
		t.Fatalf("0.090000001 wrong: %s", g.TON())
	}

	g = MustFromTON("0.19")
	if g.TON() != "0.19" {
		t.Fatalf("0.19 wrong: %s", g.TON())
	}

	g = MustFromTON("7123.190000")
	if g.TON() != "7123.19" {
		t.Fatalf("7123.19 wrong: %s", g.TON())
	}

	g = MustFromTON("5")
	if g.TON() != "5" {
		t.Fatalf("5 wrong: %s", g.TON())
	}

	g = MustFromTON("0")
	if g.TON() != "0" {
		t.Fatalf("0 wrong: %s", g.TON())
	}

	g = MustFromTON("0.2")
	if g.TON() != "0.2" {
		t.Fatalf("0.2 wrong: %s", g.TON())
	}
}
