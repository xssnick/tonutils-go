package tlb

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"reflect"
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
				_, _ = rand.Read(rnd)

				lo := new(big.Int).Mod(new(big.Int).SetBytes(rnd), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(i)), nil))
				if i > 0 && strings.HasSuffix(lo.String(), "0") {
					lo = lo.Add(lo, big.NewInt(1))
				}

				buf := make([]byte, 8)
				if _, err := rand.Read(buf); err != nil {
					panic(err)
				}

				hi := new(big.Int).SetBytes(buf)

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

func TestCoins_MarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		coins   Coins
		want    string
		wantErr bool
	}{
		{
			name: "0.123456789 TON",
			coins: Coins{
				decimals: 9,
				val:      big.NewInt(123_456_789),
			},
			want:    "\"123456789\"",
			wantErr: false,
		},
		{
			name: "1 TON",
			coins: Coins{
				decimals: 9,
				val:      big.NewInt(1_000_000_000),
			},
			want:    "\"1000000000\"",
			wantErr: false,
		},
		{
			name: "123 TON",
			coins: Coins{
				decimals: 9,
				val:      big.NewInt(123_000_000_000),
			},
			want:    "\"123000000000\"",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.coins.MarshalJSON()
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			wantBytes := []byte(tt.want)
			if !reflect.DeepEqual(got, wantBytes) {
				t.Errorf("MarshalJSON() got = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func TestCoins_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    Coins
		wantErr bool
	}{
		{
			name:    "empty invalid",
			data:    "",
			wantErr: true,
		},
		{
			name:    "empty",
			data:    "\"\"",
			wantErr: true,
		},
		{
			name:    "invalid",
			data:    "\"123a\"",
			wantErr: true,
		},
		{
			name: "0.123456789 TON",
			data: "\"123456789\"",
			want: Coins{
				decimals: 9,
				val:      big.NewInt(123_456_789),
			},
		},
		{
			name: "1 TON",
			data: "\"1000000000\"",
			want: Coins{
				decimals: 9,
				val:      big.NewInt(1_000_000_000),
			},
		},
		{
			name: "123 TON",
			data: "\"123000000000\"",
			want: Coins{
				decimals: 9,
				val:      big.NewInt(123_000_000_000),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var coins Coins

			err := coins.UnmarshalJSON([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !reflect.DeepEqual(coins, tt.want) {
				t.Errorf("UnmarshalJSON() got = %v, want %v", coins, tt.want)
			}
		})
	}
}
