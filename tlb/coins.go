package tlb

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type Coins struct {
	val *big.Int
}

func (g Coins) TON() string {
	if g.val == nil {
		return "0"
	}

	a := g.val.String()
	if a == "0" {
		// process 0 faster and simpler
		return a
	}

	splitter := len(a) - 9
	if splitter <= 0 {
		a = "0." + strings.Repeat("0", 9-len(a)) + a
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

func (g Coins) NanoTON() *big.Int {
	if g.val == nil {
		return big.NewInt(0)
	}
	return g.val
}

func MustFromTON(val string) Coins {
	v, err := FromTON(val)
	if err != nil {
		panic(err)
	}
	return v
}

func FromNanoTON(val *big.Int) Coins {
	return Coins{
		val: new(big.Int).Set(val),
	}
}

func FromNanoTONU(val uint64) Coins {
	return Coins{
		val: new(big.Int).SetUint64(val),
	}
}

func FromTON(val string) (Coins, error) {
	errInvalid := errors.New("invalid string")

	s := strings.SplitN(val, ".", 2)

	if len(s) == 0 {
		return Coins{}, errInvalid
	}

	hi, ok := new(big.Int).SetString(s[0], 10)
	if !ok {
		return Coins{}, errInvalid
	}

	hi = hi.Mul(hi, new(big.Int).SetUint64(1000000000))

	if len(s) == 2 {
		loStr := s[1]
		// lo can have max 9 digits for ton
		if len(loStr) > 9 {
			loStr = loStr[:9]
		}

		leadZeroes := 0
		for _, sym := range loStr {
			if sym != '0' {
				break
			}
			leadZeroes++
		}

		lo, err := strconv.ParseUint(loStr, 10, 64)
		if err != nil {
			return Coins{}, errInvalid
		}

		// log10 of 1 == 0, log10 of 10 = 1, so we need offset
		digits := int(math.Ceil(math.Log10(float64(lo + 1))))
		lo *= uint64(math.Pow10((9 - leadZeroes) - digits))

		hi = hi.Add(hi, new(big.Int).SetUint64(lo))
	}

	return Coins{
		val: hi,
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
	return cell.BeginCell().MustStoreBigCoins(g.NanoTON()).EndCell(), nil
}

func (g Coins) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", g.NanoTON().String())), nil
}

func (g Coins) String() string {
	return g.TON()
}
