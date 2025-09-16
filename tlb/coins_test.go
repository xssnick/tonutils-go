package tlb

import (
	"crypto/rand"
	"errors"
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

func TestCoins_GreaterThan(t *testing.T) {
	tests := []struct {
		name   string
		coins1 string
		coins2 string
		want   bool
	}{
		{"zero comparison", "0.0", "0.0", false},
		{"zero comparison negative", "-0.0", "-0.0", false},
		{"greater", "2.0", "1.0", true},
		{"equal", "1.0", "1.0", false},
		{"less", "1.0", "2.0", false},
		{"small difference", "1.000000001", "1.0", true},
		{"greater negative comparison", "-1.0", "-2.0", true},
		{"equal negative comparison", "-1.0", "-1.0", false},
		{"less negative comparison", "-2.0", "-1.0", false},
		{"small difference negative", "-1.0", "-1.000000001", true},
		{"too many decimals", "1.00000000001", "1.0", false},
		{"too many decimals ", "-1.0", "-1.0000000001", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c1 := MustFromTON(tt.coins1)
			c2 := MustFromTON(tt.coins2)
			if got := c1.GreaterThan(c2); got != tt.want {
				t.Logf("c1: %s, c2: %s", c1, c2)
				t.Errorf("GreaterThan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoins_GreaterOrEqual(t *testing.T) {
	tests := []struct {
		name   string
		coins1 string
		coins2 string
		want   bool
	}{
		{"zero comparison", "0.0", "0.0", true},
		{"zero comparison negative", "-0.0", "-0.0", true},
		{"greater", "2.0", "1.0", true},
		{"equal", "1.0", "1.0", true},
		{"less", "1.0", "2.0", false},
		{"zero comparison", "0.0", "0.0", true},
		{"small difference", "1.000000001", "1.0", true},
		{"greater negative comparison", "-1.0", "-2.0", true},
		{"equal negative comparison", "-1.0", "-1.0", true},
		{"less negative comparison", "-2.0", "-1.0", false},
		{"small difference negative", "-1.0", "-1.000000001", true},
		{"too many decimals", "1.00000000001", "1.0", true},
		{"too many decimals ", "-1.0", "-1.0000000001", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c1 := MustFromTON(tt.coins1)
			c2 := MustFromTON(tt.coins2)
			if got := c1.GreaterOrEqual(c2); got != tt.want {
				t.Errorf("GreaterOrEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoins_LessThan(t *testing.T) {
	tests := []struct {
		name   string
		coins1 string
		coins2 string
		want   bool
	}{
		{"zero comparison", "0.0", "0.0", false},
		{"zero comparison negative", "-0.0", "-0.0", false},
		{"greater", "2.0", "1.0", false},
		{"equal", "1.0", "1.0", false},
		{"less", "1.0", "2.0", true},
		{"small difference", "1.0", "1.000000001", true},
		{"greater negative comparison", "-1.0", "-2.0", false},
		{"equal negative comparison", "-1.0", "-1.0", false},
		{"less negative comparison", "-2.0", "-1.0", true},
		{"small difference negative", "-1.000000001", "-1.0", true},
		{"too many decimals", "1.00000000001", "1.0", false},
		{"too many decimals ", "-1.0", "-1.0000000001", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c1 := MustFromTON(tt.coins1)
			c2 := MustFromTON(tt.coins2)
			if got := c1.LessThan(c2); got != tt.want {
				t.Errorf("LessThan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoins_LessOrEqual(t *testing.T) {
	tests := []struct {
		name   string
		coins1 string
		coins2 string
		want   bool
	}{
		{"zero comparison", "0.0", "0.0", true},
		{"zero comparison negative", "-0.0", "-0.0", true},
		{"greater", "2.0", "1.0", false},
		{"equal", "1.0", "1.0", true},
		{"less", "1.0", "2.0", true},
		{"small difference", "1.0", "1.000000001", true},
		{"greater negative comparison", "-1.0", "-2.0", false},
		{"equal negative comparison", "-1.0", "-1.0", true},
		{"less negative comparison", "-2.0", "-1.0", true},
		{"small difference negative", "-1.000000001", "-1.0", true},
		{"too many decimals", "1.00000000001", "1.0", true},
		{"too many decimals ", "-1.0", "-1.0000000001", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c1 := MustFromTON(tt.coins1)
			c2 := MustFromTON(tt.coins2)
			if got := c1.LessOrEqual(c2); got != tt.want {
				t.Errorf("LessOrEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoins_Equals(t *testing.T) {
	tests := []struct {
		name   string
		coins1 string
		coins2 string
		want   bool
	}{
		{"equal", "1.0", "1.0", true},
		{"not equal", "1.0", "2.0", false},
		{"zero comparison", "0.0", "0.0", true},
		{"small difference", "1.0", "1.000000001", false},
		{"too many decimals", "1.0", "1.000000000001", true},
		{"negative equal", "-1.0", "-1.0", true},
		{"negative not equal", "-1.0", "-2.0", false},
		{"different decimal representation", "1.0", "1.000000000", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c1 := MustFromTON(tt.coins1)
			c2 := MustFromTON(tt.coins2)
			if got := c1.Equals(c2); got != tt.want {
				t.Errorf("Equals() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoins_IsZero(t *testing.T) {
	tests := []struct {
		name  string
		coins string
		want  bool
	}{
		{"zero", "0.0", true},
		{"positive", "1.0", false},
		{"negative", "-1.0", false},
		{"small positive", "0.000000001", false},
		{"small negative", "-0.000000001", false},
		{"too many decimals", "0.000000000001", true},
		{"too many decimals, negative", "-0.000000000001", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := MustFromTON(tt.coins)
			if got := c.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoins_IsPositive(t *testing.T) {
	tests := []struct {
		name  string
		coins string
		want  bool
	}{
		{"zero", "0.0", false},
		{"positive", "1.0", true},
		{"negative", "-1.0", false},
		{"small positive", "0.000000001", true},
		{"small negative", "-0.1", false},
		{"too many decimals", "0.000000000001", false},
		{"too many decimals, negative", "-0.000000000001", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := MustFromTON(tt.coins)
			if got := c.IsPositive(); got != tt.want {
				t.Logf("n: %s", c)
				t.Errorf("IsPositive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoins_IsNegative(t *testing.T) {
	tests := []struct {
		name  string
		coins string
		want  bool
	}{
		{"zero", "0.0", false},
		{"positive", "1.0", false},
		{"negative", "-1.0", true},
		{"small positive", "0.000000001", false},
		{"small negative", "-0.000000001", true},
		{"too many decimals", "0.000000000001", false},
		{"too many decimals, negative", "-0.000000000001", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := MustFromTON(tt.coins)
			if got := c.IsNegative(); got != tt.want {
				t.Errorf("IsNegative() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoins_Add(t *testing.T) {
	// Define a very large number close to the 16-byte limit
	largeValStr := "1329227995784915872903807060280344575" // 2^120 - 1
	largeVal := new(big.Int)
	largeVal.SetString(largeValStr, 10)
	maxCoins, err := FromNano(largeVal, 9)
	if err != nil {
		t.Fatalf("got err: %s", err)
	}
	oneNano, err := FromNano(big.NewInt(1), 9)
	if err != nil {
		t.Fatalf("got err: %s", err)
	}

	tests := []struct {
		name    string
		a       Coins
		b       Coins
		want    Coins
		wantErr error // Expected error for non-Must version
	}{
		{
			name: "add zero to positive",
			a:    MustFromTON("1.234567890"),
			b:    MustFromTON("0"),
			want: MustFromTON("1.234567890"),
		},
		{
			name: "add positive to zero",
			a:    MustFromTON("0"),
			b:    MustFromTON("4.567890123"),
			want: MustFromTON("4.567890123"),
		},
		{
			name: "add two positives",
			a:    MustFromTON("1.111111111"),
			b:    MustFromTON("2.222222222"),
			want: MustFromTON("3.333333333"),
		},
		{
			name: "add two zeros",
			a:    MustFromTON("0"),
			b:    MustFromTON("0"),
			want: MustFromTON("0"),
		},
		{
			name: "add positive and negative (positive result)",
			a:    FromNanoTON(big.NewInt(5_123_456_789)),  // 5.123456789 TON
			b:    FromNanoTON(big.NewInt(-2_012_345_678)), // -2.012345678 TON
			want: FromNanoTON(big.NewInt(3_111_111_111)),  // 3.111111111 TON
		},
		{
			name: "add positive and negative (negative result)",
			a:    FromNanoTON(big.NewInt(1_987_654_321)),  // 1.987654321 TON
			b:    FromNanoTON(big.NewInt(-4_123_456_789)), // -4.123456789 TON
			want: FromNanoTON(big.NewInt(-2_135_802_468)), // -2.135802468 TON
		},
		{
			name: "add positive and negative (zero result)",
			a:    FromNanoTON(big.NewInt(7_111_222_333)),  // 7.111222333 TON
			b:    FromNanoTON(big.NewInt(-7_111_222_333)), // -7.111222333 TON
			want: FromNanoTON(big.NewInt(0)),              // 0 TON
		},
		{
			name: "add two negatives",
			a:    FromNanoTON(big.NewInt(-1_000_000_001)), // -1.000000001 TON
			b:    FromNanoTON(big.NewInt(-2_543_210_987)), // -2.543210987 TON
			want: FromNanoTON(big.NewInt(-3_543_210_988)), // -3.543210988 TON
		},
		{
			name: "too many decimals should get truncated.",
			a:    MustFromDecimal("1.234567891234", 9),
			b:    MustFromDecimal("1.987654321987", 9),
			want: MustFromDecimal("3.222222212", 9),
		},
		{
			name: "non-standard decimals works",
			a:    MustFromDecimal("1", 6),
			b:    MustFromDecimal("1", 6),
			want: MustFromDecimal("2", 6),
		},
		{
			name:    "different decimals error",
			a:       MustFromDecimal("1", 9),
			b:       MustFromDecimal("1", 6),
			wantErr: errDecimalMismatch,
		},
		{
			name:    "result too big error",
			a:       maxCoins,
			b:       oneNano, // Adding 1 nano makes it 2^120
			wantErr: errTooBigForVarUint16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.Add(tt.b)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Add() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr == nil {
				if !got.Equals(tt.want) {
					t.Errorf("Add() got = %v, want %v", got, tt.want)
				}
				if got.Decimals() != tt.want.Decimals() {
					t.Errorf("Add() got decimals = %d, want %d", got.Decimals(), tt.want.Decimals())
				}
			}
		})
	}
}

func TestCoins_MustAdd(t *testing.T) {
	// Define a very large number close to the 16-byte limit
	largeValStr := "1329227995784915872903807060280344575" // 2^120 - 1
	largeVal := new(big.Int)
	largeVal.SetString(largeValStr, 10)
	maxCoins, err := FromNano(largeVal, 9)
	if err != nil {
		t.Fatal(err)
	}
	oneNano, err := FromNano(big.NewInt(1), 9)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		a         Coins
		b         Coins
		wantPanic bool // Expected panic for Must version
	}{
		{
			name:      "different decimals panic",
			a:         MustFromDecimal("1", 9),
			b:         MustFromDecimal("1", 6),
			wantPanic: true,
		},
		{
			name:      "result too big panic",
			a:         maxCoins,
			b:         oneNano,
			wantPanic: true,
		},
		{
			name:      "no panic",
			a:         MustFromTON("1"),
			b:         MustFromTON("2"),
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if (r != nil) != tt.wantPanic {
					t.Errorf("MustAdd() panic = %v, wantPanic %v", r, tt.wantPanic)
				}
			}()

			// We only call MustAdd to check for panics, result checked in TestCoins_Add
			_ = tt.a.MustAdd(tt.b)
		})
	}
}

func TestCoins_Sub(t *testing.T) {
	// Define a very large negative number close to the 16-byte limit.
	largeNegValStr := "-1329227995784915872903807060280344575" // 2^120 - 1
	largeNegVal := new(big.Int)
	largeNegVal.SetString(largeNegValStr, 10)
	minCoins, err := FromNano(largeNegVal, 9)
	if err != nil {
		t.Fatal(err)
	}
	oneNano, err := FromNano(big.NewInt(1), 9)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		a       Coins
		b       Coins
		want    Coins
		wantErr error
	}{
		{
			name: "subtract zero from positive",
			a:    MustFromTON("1.234567890"),
			b:    MustFromTON("0"),
			want: MustFromTON("1.234567890"),
		},
		{
			name: "subtract positive from zero",
			a:    MustFromTON("0"),
			b:    MustFromTON("4.567890123"),
			want: MustFromTON("-4.567890123"),
		},
		{
			name: "subtract two positives (positive result)",
			a:    MustFromTON("3.333333333"),
			b:    MustFromTON("1.111111111"),
			want: MustFromTON("2.222222222"),
		},
		{
			name: "subtract two positives (negative result)",
			a:    MustFromTON("1.111111111"),
			b:    MustFromTON("2.222222222"),
			want: MustFromTON("-1.111111111"),
		},
		{
			name: "subtract two positives (zero result)",
			a:    MustFromTON("1.111111111"),
			b:    MustFromTON("1.111111111"),
			want: MustFromTON("0"),
		},
		{
			name: "subtract two zeros",
			a:    MustFromTON("0"),
			b:    MustFromTON("0"),
			want: MustFromTON("0"),
		},
		{
			name: "subtract negative from positive (positive result)",
			a:    FromNanoTON(big.NewInt(5_123_456_789)),  // 5.123456789 TON
			b:    FromNanoTON(big.NewInt(-2_012_345_678)), // -2.012345678 TON
			want: FromNanoTON(big.NewInt(7_135_802_467)),  // 7.135802467 TON
		},
		{
			name: "subtract positive from negative (negative result)",
			a:    FromNanoTON(big.NewInt(-1_987_654_321)), // -1.987654321 TON
			b:    FromNanoTON(big.NewInt(4_123_456_789)),  // 4.123456789 TON
			want: FromNanoTON(big.NewInt(-6_111_111_110)), // -6.111111110 TON
		},
		{
			name: "subtract negative from negative (zero result)",
			a:    FromNanoTON(big.NewInt(-7_111_222_333)), // -7.111222333 TON
			b:    FromNanoTON(big.NewInt(-7_111_222_333)), // -7.111222333 TON
			want: FromNanoTON(big.NewInt(0)),              // 0 TON
		},
		{
			name: "subtract negative from negative (positive result)",
			a:    FromNanoTON(big.NewInt(-1_000_000_001)), // -1.000000001 TON
			b:    FromNanoTON(big.NewInt(-2_543_210_987)), // -2.543210987 TON
			want: FromNanoTON(big.NewInt(1_543_210_986)),  // 1.543210986 TON
		},
		{
			name: "subtract negative from negative (negative result)",
			a:    FromNanoTON(big.NewInt(-3_543_210_988)), // -3.543210988 TON
			b:    FromNanoTON(big.NewInt(-1_000_000_001)), // -1.000000001 TON
			want: FromNanoTON(big.NewInt(-2_543_210_987)), // -2.543210987 TON
		},
		{
			name: "too many decimals should get truncated",
			a:    MustFromDecimal("1.987654321987", 9),
			b:    MustFromDecimal("1.234567891234", 9),
			want: MustFromDecimal("0.753086430", 9),
		},
		{
			name: "non-standard decimals works",
			a:    MustFromDecimal("2", 6),
			b:    MustFromDecimal("1", 6),
			want: MustFromDecimal("1", 6),
		},
		{
			name:    "different decimals error",
			a:       MustFromDecimal("1", 9),
			b:       MustFromDecimal("1", 6),
			wantErr: errDecimalMismatch,
		},
		{
			name:    "result too small error",
			a:       minCoins,
			b:       oneNano,               // Subtracting 1 nano makes it less than -2^120
			wantErr: errTooBigForVarUint16, // The check is based on bit length, large negative numbers also fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.Sub(tt.b)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Sub() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr == nil {
				// Use Nano().Cmp because direct Equals might fail due to precision diffs in test setup vs calculation
				if got.Nano().Cmp(tt.want.Nano()) != 0 {
					t.Errorf("Sub() got = %v (%s), want %v (%s)", got, got.Nano().String(), tt.want, tt.want.Nano().String())
				}
				if got.Decimals() != tt.want.Decimals() {
					t.Errorf("Sub() got decimals = %d, want %d", got.Decimals(), tt.want.Decimals())
				}
			}
		})
	}
}

func TestCoins_MustSub(t *testing.T) {
	// Define a very large negative number close to the 16-byte limit
	largeNegValStr := "-1329227995784915872903807060280344575" // 2^120 - 1
	largeNegVal := new(big.Int)
	largeNegVal.SetString(largeNegValStr, 10)
	minCoins, err := FromNano(largeNegVal, 9)
	if err != nil {
		t.Fatal(err)
	}
	oneNano, err := FromNano(big.NewInt(1), 9)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		a         Coins
		b         Coins
		wantPanic bool
	}{
		{
			name:      "different decimals panic",
			a:         MustFromDecimal("1", 9),
			b:         MustFromDecimal("1", 6),
			wantPanic: true,
		},
		{
			name:      "result too small panic",
			a:         minCoins,
			b:         oneNano,
			wantPanic: true,
		},
		{
			name:      "no panic",
			a:         MustFromTON("3"),
			b:         MustFromTON("1"),
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if (r != nil) != tt.wantPanic {
					t.Errorf("MustSub() panic = %v, wantPanic %v", r, tt.wantPanic)
				}
			}()

			_ = tt.a.MustSub(tt.b)
		})
	}
}

func TestCoins_Mul(t *testing.T) {
	largeValStr := "1329227995784915872903807060280344575" // 2^120 - 1
	largeVal := new(big.Int)
	largeVal.SetString(largeValStr, 10)
	halfMaxCoins, err := FromNano(largeVal, 9)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		a       Coins
		x       *big.Int
		want    Coins
		wantErr error
	}{
		{
			name: "multiply positive by positive",
			a:    MustFromTON("1.111111111"),
			x:    big.NewInt(3),
			want: MustFromTON("3.333333333"),
		},
		{
			name: "multiply positive by zero",
			a:    MustFromTON("1.234567890"),
			x:    big.NewInt(0),
			want: MustFromTON("0"),
		},
		{
			name: "multiply positive by negative",
			a:    MustFromTON("2.222222222"),
			x:    big.NewInt(-2),
			want: MustFromTON("-4.444444444"),
		},
		{
			name: "multiply zero by positive",
			a:    MustFromTON("0"),
			x:    big.NewInt(100),
			want: MustFromTON("0"),
		},
		{
			name: "multiply zero by negative",
			a:    MustFromTON("0"),
			x:    big.NewInt(-5),
			want: MustFromTON("0"),
		},
		{
			name: "multiply negative by positive",
			a:    MustFromTON("-1.5"),
			x:    big.NewInt(2),
			want: MustFromTON("-3.0"),
		},
		{
			name: "multiply negative by negative",
			a:    MustFromTON("-1.1"),
			x:    big.NewInt(-3),
			want: MustFromTON("3.3"),
		},
		{
			name: "too many decimals gets truncated",
			a:    MustFromDecimal("1.1234567891234", 9),
			x:    big.NewInt(2),
			want: MustFromDecimal("2.246913578", 9),
		},
		{
			name: "non-standard decimals works",
			a:    MustFromDecimal("1.2345", 4),
			x:    big.NewInt(2),
			want: MustFromDecimal("2.4690", 4),
		},
		{
			name:    "result too big error",
			a:       halfMaxCoins,
			x:       big.NewInt(2), // 2^119 * 2 = 2^120
			wantErr: errTooBigForVarUint16,
		},
		{
			name:    "result too small error",
			a:       halfMaxCoins,
			x:       big.NewInt(-2), // 2^119 * -2 = -2^120
			wantErr: errTooBigForVarUint16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.Mul(tt.x)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Mul() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr == nil {
				if !got.Equals(tt.want) {
					t.Errorf("Mul() got = %v, want %v", got, tt.want)
				}
				if got.Decimals() != tt.want.Decimals() {
					t.Errorf("Mul() got decimals = %d, want %d", got.Decimals(), tt.want.Decimals())
				}
			}
		})
	}
}

func TestCoins_MustMul(t *testing.T) {
	largeValStr := "1329227995784915872903807060280344575" // 2^120 - 1
	largeVal := new(big.Int)
	largeVal.SetString(largeValStr, 10)
	halfMaxCoins, err := FromNano(largeVal, 9)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		a         Coins
		x         *big.Int
		wantPanic bool
	}{
		{
			name:      "result too big panic",
			a:         halfMaxCoins,
			x:         big.NewInt(2),
			wantPanic: true,
		},
		{
			name:      "result too small panic",
			a:         halfMaxCoins,
			x:         big.NewInt(-2),
			wantPanic: true,
		},
		{
			name:      "no panic",
			a:         MustFromTON("1.5"),
			x:         big.NewInt(2),
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if (r != nil) != tt.wantPanic {
					t.Errorf("MustMul() panic = %v, wantPanic %v", r, tt.wantPanic)
				}
			}()

			_ = tt.a.MustMul(tt.x)
		})
	}
}

func TestCoins_Div(t *testing.T) {
	tests := []struct {
		name    string
		a       Coins
		x       *big.Int
		want    Coins
		wantErr error
	}{
		{
			name: "divide positive by positive",
			a:    MustFromTON("3.333333333"),
			x:    big.NewInt(3),
			want: MustFromTON("1.111111111"),
		},
		{
			name: "divide positive by positive (truncation)",
			a:    MustFromTON("1.000000000"),
			x:    big.NewInt(3),
			want: MustFromTON("0.333333333"), // 1_000_000_000 / 3 = 333_333_333
		},
		{
			name:    "divide by zero error",
			a:       MustFromTON("1.234567890"),
			x:       big.NewInt(0),
			wantErr: errDivisionByZero,
		},
		{
			name: "divide positive by negative",
			a:    MustFromTON("4.444444444"),
			x:    big.NewInt(-2),
			want: MustFromTON("-2.222222222"),
		},
		{
			name: "divide zero by positive",
			a:    MustFromTON("0"),
			x:    big.NewInt(100),
			want: MustFromTON("0"),
		},
		{
			name: "divide zero by negative",
			a:    MustFromTON("0"),
			x:    big.NewInt(-5),
			want: MustFromTON("0"),
		},
		{
			name: "divide negative by positive",
			a:    MustFromTON("-3.0"),
			x:    big.NewInt(2),
			want: MustFromTON("-1.5"),
		},
		{
			name: "divide negative by negative",
			a:    MustFromTON("-3.3"),
			x:    big.NewInt(-3),
			want: MustFromTON("1.1"),
		},
		{
			name: "too many decimals gets truncated", // Input truncation
			a:    MustFromDecimal("1.123456789123", 9),
			x:    big.NewInt(2),
			want: MustFromDecimal("0.561728394", 9), // 1123456789 / 2 = 561728394
		},
		{
			name: "non-standard decimals works",
			a:    MustFromDecimal("2.4690", 4),
			x:    big.NewInt(2),
			want: MustFromDecimal("1.2345", 4),
		},
		// errTooBigForVarUint16 is less likely with Div but possible if dividing a large number by < 1 (not possible with integer Div)
		// or if the input 'a' itself was already invalid (handled by FromNano). The check is on the result.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.Div(tt.x)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Div() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr == nil {
				if !got.Equals(tt.want) {
					t.Errorf("Div() got = %v, want %v", got, tt.want)
				}
				if got.Decimals() != tt.want.Decimals() {
					t.Errorf("Div() got decimals = %d, want %d", got.Decimals(), tt.want.Decimals())
				}
			}
		})
	}
}

func TestCoins_MustDiv(t *testing.T) {
	tests := []struct {
		name      string
		a         Coins
		x         *big.Int
		wantPanic bool
	}{
		{
			name:      "divide by zero panic",
			a:         MustFromTON("1.234567890"),
			x:         big.NewInt(0),
			wantPanic: true,
		},
		{
			name:      "no panic",
			a:         MustFromTON("4"),
			x:         big.NewInt(2),
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if (r != nil) != tt.wantPanic {
					t.Errorf("MustDiv() panic = %v, wantPanic %v", r, tt.wantPanic)
				}
			}()

			_ = tt.a.MustDiv(tt.x)
		})
	}
}

func TestCoins_Neg(t *testing.T) {
	tests := []struct {
		name string
		a    Coins
		want Coins
	}{
		{
			name: "negate positive",
			a:    MustFromTON("1.234567890"),
			want: MustFromTON("-1.234567890"),
		},
		{
			name: "negate zero",
			a:    MustFromTON("0"),
			want: MustFromTON("0"),
		},
		{
			name: "negate zero 2",
			a:    MustFromTON("0"),
			want: MustFromTON("-0"),
		},
		{
			name: "negate zero 3",
			a:    MustFromTON("-0"),
			want: MustFromTON("-0"),
		},
		{
			name: "negate negative",
			a:    MustFromTON("-9.876543210"),
			want: MustFromTON("9.876543210"),
		},
		{
			name: "non-standard decimals works",
			a:    MustFromDecimal("-1.23", 2),
			want: MustFromDecimal("1.23", 2),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Neg()

			if !got.Equals(tt.want) {
				t.Errorf("Neg() got = %v, want %v", got, tt.want)
			}
			if got.Decimals() != tt.want.Decimals() {
				t.Errorf("Neg() got decimals = %d, want %d", got.Decimals(), tt.want.Decimals())
			}
		})
	}
}

func TestCoins_Abs(t *testing.T) {
	tests := []struct {
		name string
		a    Coins
		want Coins
	}{
		{
			name: "abs positive",
			a:    MustFromTON("1.234567890"),
			want: MustFromTON("1.234567890"),
		},
		{
			name: "abs zero",
			a:    MustFromTON("0"),
			want: MustFromTON("0"),
		},
		{
			name: "abs negative",
			a:    MustFromTON("-9.876543210"),
			want: MustFromTON("9.876543210"),
		},
		{
			name: "too many decimals gets truncated",
			a:    MustFromTON("1.123456789123"),
			want: MustFromTON("1.123456789"),
		},
		{
			name: "non-standard decimals works",
			a:    MustFromDecimal("-1.23", 2),
			want: MustFromDecimal("1.23", 2),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.Abs()

			if !got.Equals(tt.want) {
				t.Errorf("Abs() got = %v, want %v", got, tt.want)
			}
			if got.Decimals() != tt.want.Decimals() {
				t.Errorf("Abs() got decimals = %d, want %d", got.Decimals(), tt.want.Decimals())
			}
		})
	}
}

func TestCoins_MulRat(t *testing.T) {
	largeValStr := "1329227995784915872903807060280344575" // 2^120 - 1
	largeVal := new(big.Int)
	largeVal.SetString(largeValStr, 10)
	halfMaxCoins, err := FromNano(largeVal, 9)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		a       Coins
		r       *big.Rat
		want    Coins
		wantErr error
	}{
		{
			name: "multiply by 1/2",
			a:    MustFromTON("1.5"),
			r:    big.NewRat(1, 2),
			want: MustFromTON("0.75"),
		},
		{
			name: "multiply by 2/3 (truncation)",
			a:    MustFromTON("1.0"),
			r:    big.NewRat(2, 3),
			want: MustFromTON("0.666666666"), // 1e9 * 2 / 3 = 666,666,666
		},
		{
			name: "multiply by 2 (integer as rational)",
			a:    MustFromTON("1.1"),
			r:    big.NewRat(2, 1),
			want: MustFromTON("2.2"),
		},
		{
			name: "multiply by -1/2",
			a:    MustFromTON("5.0"),
			r:    big.NewRat(-1, 2),
			want: MustFromTON("-2.5"),
		},
		{
			name: "multiply negative by 1/3",
			a:    MustFromTON("-3.3"),
			r:    big.NewRat(1, 3),
			want: MustFromTON("-1.1"),
		},
		{
			name: "multiply zero by 5/7",
			a:    MustFromTON("0"),
			r:    big.NewRat(5, 7),
			want: MustFromTON("0"),
		},
		{
			name: "non-standard decimals",
			a:    MustFromDecimal("10.00", 2),
			r:    big.NewRat(1, 4),
			want: MustFromDecimal("2.50", 2),
		},
		{
			name: "multiply by zero rational",
			a:    MustFromTON("123.456"),
			r:    big.NewRat(0, 1), // 0/1 is valid
			want: MustFromTON("0"),
		},
		{
			name: "multiply negative by negative rational",
			a:    MustFromTON("-10.0"),
			r:    big.NewRat(-1, 2), // -0.5
			want: MustFromTON("5.0"),
		},
		{
			name: "multiply by rational equal to 1",
			a:    MustFromTON("7.89"),
			r:    big.NewRat(3, 3),
			want: MustFromTON("7.89"),
		},
		{
			name: "multiply negative by negative rational (-5 * -1/2)",
			a:    MustFromTON("-5.0"),
			r:    big.NewRat(-1, 2),
			want: MustFromTON("2.5"),
		},
		{
			name:    "result too big error",
			a:       halfMaxCoins,
			r:       big.NewRat(2, 1), // * 2
			wantErr: errTooBigForVarUint16,
		},
		{
			name:    "result too small error",
			a:       halfMaxCoins,
			r:       big.NewRat(-2, 1), // * -2
			wantErr: errTooBigForVarUint16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.MulRat(tt.r)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("MulRat() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr == nil {
				if !got.Equals(tt.want) {
					t.Errorf("MulRat() got = %v (%s), want %v (%s)", got, got.Nano().String(), tt.want, tt.want.Nano().String())
				}
				if got.Decimals() != tt.want.Decimals() {
					t.Errorf("MulRat() got decimals = %d, want %d", got.Decimals(), tt.want.Decimals())
				}
			}
		})
	}
}

func TestCoins_MustMulRat(t *testing.T) {
	largeValStr := "1329227995784915872903807060280344575" // 2^120 - 1
	largeVal := new(big.Int)
	largeVal.SetString(largeValStr, 10)
	halfMaxCoins, err := FromNano(largeVal, 9)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		a         Coins
		r         *big.Rat
		wantPanic bool
	}{
		{
			name:      "result too big panic",
			a:         halfMaxCoins,
			r:         big.NewRat(2, 1),
			wantPanic: true,
		},
		{
			name:      "result too small panic",
			a:         halfMaxCoins,
			r:         big.NewRat(-2, 1),
			wantPanic: true,
		},
		{
			name:      "no panic",
			a:         MustFromTON("1.5"),
			r:         big.NewRat(1, 2),
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if (r != nil) != tt.wantPanic {
					t.Errorf("MustMulRat() panic = %v, wantPanic %v", r, tt.wantPanic)
				}
			}()

			_ = tt.a.MustMulRat(tt.r)
		})
	}
}

func TestCoins_DivRat(t *testing.T) {
	largeValStr := "1329227995784915872903807060280344575" // 2^120 - 1
	largeVal := new(big.Int)
	largeVal.SetString(largeValStr, 10)
	halfMaxCoins, err := FromNano(largeVal, 9)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		a       Coins
		r       *big.Rat
		want    Coins
		wantErr error
	}{
		{
			name: "divide by 1/2",
			a:    MustFromTON("1.5"),
			r:    big.NewRat(1, 2),
			want: MustFromTON("3.0"),
		},
		{
			name: "divide by 2/3",
			a:    MustFromTON("1.0"),
			r:    big.NewRat(2, 3),
			want: MustFromTON("1.5"), // 1e9 * 3 / 2 = 1_500_000_000
		},
		{
			name: "divide by 3/2 (truncation)",
			a:    MustFromTON("1.0"),
			r:    big.NewRat(3, 2),
			want: MustFromTON("0.666666666"), // 1e9 * 2 / 3 = 666,666,666
		},
		{
			name: "divide by 2 (integer as rational)",
			a:    MustFromTON("2.2"),
			r:    big.NewRat(2, 1),
			want: MustFromTON("1.1"),
		},
		{
			name: "divide by -1/2",
			a:    MustFromTON("5.0"),
			r:    big.NewRat(-1, 2),
			want: MustFromTON("-10.0"),
		},
		{
			name: "divide negative by 1/3",
			a:    MustFromTON("-1.1"),
			r:    big.NewRat(1, 3),
			want: MustFromTON("-3.3"),
		},
		{
			name: "divide zero by 5/7",
			a:    MustFromTON("0"),
			r:    big.NewRat(5, 7),
			want: MustFromTON("0"),
		},
		{
			name:    "divide by rational with zero numerator", // Division by zero
			a:       MustFromTON("1.0"),
			r:       big.NewRat(0, 1),
			wantErr: errDivisionByZero,
		},
		{
			name: "non-standard decimals",
			a:    MustFromDecimal("2.50", 2),
			r:    big.NewRat(1, 4),
			want: MustFromDecimal("10.00", 2),
		},
		{
			name: "divide negative by negative rational",
			a:    MustFromTON("-5.0"),
			r:    big.NewRat(-1, 2),   // -0.5
			want: MustFromTON("10.0"), // -5.0 / -0.5 = 10.0
		},
		{
			name: "divide by rational equal to 1",
			a:    MustFromTON("7.89"),
			r:    big.NewRat(3, 3),
			want: MustFromTON("7.89"),
		},
		{
			name: "divide by rational equal to -1",
			a:    MustFromTON("7.89"),
			r:    big.NewRat(-3, 3),
			want: MustFromTON("-7.89"),
		},
		{
			name:    "result too big error",
			a:       halfMaxCoins,
			r:       big.NewRat(1, 2), // * 2
			wantErr: errTooBigForVarUint16,
		},
		{
			name:    "result too small error",
			a:       halfMaxCoins,
			r:       big.NewRat(-1, 2), // * -2
			wantErr: errTooBigForVarUint16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.DivRat(tt.r)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("DivRat() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr == nil {
				if !got.Equals(tt.want) {
					t.Errorf("DivRat() got = %v (%s), want %v (%s)", got, got.Nano().String(), tt.want, tt.want.Nano().String())
				}
				if got.Decimals() != tt.want.Decimals() {
					t.Errorf("DivRat() got decimals = %d, want %d", got.Decimals(), tt.want.Decimals())
				}
			}
		})
	}
}

func TestCoins_MustDivRat(t *testing.T) {
	largeValStr := "1329227995784915872903807060280344575" // 2^120 - 1
	largeVal := new(big.Int)
	largeVal.SetString(largeValStr, 10)
	halfMaxCoins, err := FromNano(largeVal, 9)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		a         Coins
		r         *big.Rat
		wantPanic bool
	}{
		{
			name:      "divide by rational with zero numerator panic",
			a:         MustFromTON("1.0"),
			r:         big.NewRat(0, 1),
			wantPanic: true,
		},
		{
			name:      "result too big panic",
			a:         halfMaxCoins,
			r:         big.NewRat(1, 2),
			wantPanic: true,
		},
		{
			name:      "result too small panic",
			a:         halfMaxCoins,
			r:         big.NewRat(-1, 2),
			wantPanic: true,
		},
		{
			name:      "no panic",
			a:         MustFromTON("3.0"),
			r:         big.NewRat(1, 2),
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				// Check if panic occurred as expected
				if (r != nil) != tt.wantPanic {
					t.Errorf("MustDivRat() panic = %v, wantPanic %v", r, tt.wantPanic)
				}
				// Optionally check the panic value matches expected error type if needed
				// if tt.wantPanic && r != nil {
				//	 Check if r matches errDivisionByZero or similar if Must wraps errors
				// }
			}()

			_ = tt.a.MustDivRat(tt.r)
		})
	}
}

func TestCoins_NegStr(t *testing.T) {
	c, err := FromDecimal("-1.22", 9)
	if err != nil {
		t.Fatal(err)
	}

	if c.String() != "-1.22" {
		t.Fatalf("NegStr() got = %s, want %s", c.String(), "-1.22")
	}

	c, err = FromDecimal("-0.0011", 9)
	if err != nil {
		t.Fatal(err)
	}

	if c.String() != "-0.0011" {
		t.Fatalf("NegStr() got = %s, want %s", c.String(), "-0.0011")
	}

	c, err = FromDecimal("-0.01111", 2)
	if err != nil {
		t.Fatal(err)
	}

	if c.String() != "-0.01" {
		t.Fatalf("NegStr() got = %s, want %s", c.String(), "-0.01")
	}
}
