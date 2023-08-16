package tlb

import (
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"testing"
)

func TestCoins_FromTON(t *testing.T) {
	g := MustFromTON("0").Nano().Uint64()
	if g != 0 {
		t.Fatalf("0 wrong: %d", g)
	}

	g = MustFromTON("0.0000").Nano().Uint64()
	if g != 0 {
		t.Fatalf("0 wrong: %d", g)
	}

	g = MustFromTON("7").Nano().Uint64()
	if g != 7000000000 {
		t.Fatalf("7 wrong: %d", g)
	}

	g = MustFromTON("7.518").Nano().Uint64()
	if g != 7518000000 {
		t.Fatalf("7.518 wrong: %d", g)
	}

	g = MustFromTON("17.98765432111").Nano().Uint64()
	if g != 17987654321 {
		t.Fatalf("17.98765432111 wrong: %d", g)
	}

	g = MustFromTON("0.000000001").Nano().Uint64()
	if g != 1 {
		t.Fatalf("0.000000001 wrong: %d", g)
	}

	g = MustFromTON("0.090000001").Nano().Uint64()
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

func TestCoins_TON(t *testing.T) {
	g := MustFromTON("0.090000001")
	if g.String() != "0.090000001" {
		t.Fatalf("0.090000001 wrong: %s", g.String())
	}

	g = MustFromTON("0.19")
	if g.String() != "0.19" {
		t.Fatalf("0.19 wrong: %s", g.String())
	}

	g = MustFromTON("7123.190000")
	if g.String() != "7123.19" {
		t.Fatalf("7123.19 wrong: %s", g.String())
	}

	g = MustFromTON("5")
	if g.String() != "5" {
		t.Fatalf("5 wrong: %s", g.String())
	}

	g = MustFromTON("0")
	if g.String() != "0" {
		t.Fatalf("0 wrong: %s", g.String())
	}

	g = MustFromTON("0.2")
	if g.String() != "0.2" {
		t.Fatalf("0.2 wrong: %s", g.String())
	}

	g = MustFromTON("300")
	if g.String() != "300" {
		t.Fatalf("300 wrong: %s", g.String())
	}

	g = MustFromTON("50")
	if g.String() != "50" {
		t.Fatalf("50 wrong: %s", g.String())
	}

	g = MustFromTON("350")
	if g.String() != "350" {
		t.Fatalf("350 wrong: %s", g.String())
	}
}

func TestCoins_Decimals(t *testing.T) {
	for i := 0; i < 16; i++ {
		i := i
		t.Run("decimals "+fmt.Sprint(i), func(t *testing.T) {
			for x := 0; x < 5000; x++ {
				rnd := make([]byte, 64)
				rand.Read(rnd)

				lo := new(big.Int).Mod(new(big.Int).SetBytes(rnd), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(i)), nil))
				if i > 0 && strings.HasSuffix(lo.String(), "0") {
					lo = lo.Add(lo, big.NewInt(1))
				}
				hi := big.NewInt(rand.Int63())

				amt := new(big.Int).Mul(hi, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(i)), nil))
				amt = amt.Add(amt, lo)

				var str string
				if i > 0 {
					loStr := lo.String()
					str = fmt.Sprintf("%d.%s", hi, strings.Repeat("0", i-len(loStr))+loStr)
				} else {
					str = fmt.Sprint(hi)
				}

				g, err := FromDecimal(str, i)
				if err != nil {
					t.Fatalf("%d %s err: %s", i, str, err.Error())
					return
				}

				if g.String() != str {
					t.Fatalf("%d %s wrong: %s", i, str, g.String())
					return
				}

				if g.Nano().String() != amt.String() {
					t.Fatalf("%d %s nano wrong: %s", i, amt.String(), g.Nano().String())
					return
				}
			}
		})
	}
}
