package tlb

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type Coins struct {
	decimals int
	val      *big.Int
}

var ZeroCoins = MustFromTON("0")

// Deprecated: use String
func (g Coins) TON() string {
	return g.String()
}

func (g Coins) String() string {
	if g.val == nil {
		return "0"
	}

	a := g.val.String()
	if a == "0" {
		// process 0 faster and simpler
		return a
	}

	splitter := len(a) - g.decimals
	if splitter <= 0 {
		a = "0." + strings.Repeat("0", g.decimals-len(a)) + a
	} else {
		// set . between lo and hi
		a = a[:splitter] + "." + a[splitter:]
	}

	// cut last zeroes
	for i := len(a) - 1; i >= 0; i-- {
		if a[i] == '.' {
			a = a[:i]
			break
		}
		if a[i] != '0' {
			a = a[:i+1]
			break
		}
	}

	return a
}

// Deprecated: use Nano
func (g Coins) NanoTON() *big.Int {
	return g.Nano()
}

func (g Coins) Nano() *big.Int {
	if g.val == nil {
		return big.NewInt(0)
	}
	return g.val
}

func MustFromDecimal(val string, decimals int) Coins {
	v, err := FromDecimal(val, decimals)
	if err != nil {
		panic(err)
	}
	return v
}

func MustFromTON(val string) Coins {
	v, err := FromTON(val)
	if err != nil {
		panic(err)
	}
	return v
}

func MustFromNano(val *big.Int, decimals int) Coins {
	v, err := FromNano(val, decimals)
	if err != nil {
		panic(err)
	}
	return v
}

func FromNano(val *big.Int, decimals int) (Coins, error) {
	if uint((val.BitLen()+7)>>3) >= 16 {
		return Coins{}, fmt.Errorf("too big number for coins")
	}

	return Coins{
		decimals: decimals,
		val:      new(big.Int).Set(val),
	}, nil
}

func FromNanoTON(val *big.Int) Coins {
	return Coins{
		decimals: 9,
		val:      new(big.Int).Set(val),
	}
}

func FromNanoTONU(val uint64) Coins {
	return Coins{
		decimals: 9,
		val:      new(big.Int).SetUint64(val),
	}
}

func FromTON(val string) (Coins, error) {
	return FromDecimal(val, 9)
}

func FromDecimal(val string, decimals int) (Coins, error) {
	if decimals < 0 || decimals >= 128 {
		return Coins{}, fmt.Errorf("invalid decmals")
	}
	errInvalid := errors.New("invalid string")

	s := strings.SplitN(val, ".", 2)

	if len(s) == 0 {
		return Coins{}, errInvalid
	}

	hi, ok := new(big.Int).SetString(s[0], 10)
	if !ok {
		return Coins{}, errInvalid
	}

	hi = hi.Mul(hi, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))

	if len(s) == 2 {
		loStr := s[1]
		// lo can have max {decimals} digits
		if len(loStr) > decimals {
			loStr = loStr[:decimals]
		}

		leadZeroes := 0
		for _, sym := range loStr {
			if sym != '0' {
				break
			}
			leadZeroes++
		}

		lo, ok := new(big.Int).SetString(loStr, 10)
		if !ok {
			return Coins{}, errInvalid
		}

		digits := len(lo.String()) // =_=
		lo = lo.Mul(lo, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64((decimals-leadZeroes)-digits)), nil))

		hi = hi.Add(hi, lo)
	}

	if uint((hi.BitLen()+7)>>3) >= 16 {
		return Coins{}, fmt.Errorf("too big number for coins")
	}

	return Coins{
		decimals: decimals,
		val:      hi,
	}, nil
}

func (g *Coins) LoadFromCell(loader *cell.Slice) error {
	coins, err := loader.LoadBigCoins()
	if err != nil {
		return err
	}
	g.val = coins
	return nil
}

func (g Coins) ToCell() (*cell.Cell, error) {
	return cell.BeginCell().MustStoreBigCoins(g.Nano()).EndCell(), nil
}

func (g Coins) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", g.Nano().String())), nil
}
