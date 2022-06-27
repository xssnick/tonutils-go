package tlb

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

type Grams big.Int

func (g Grams) TON() string {
	a := (*big.Int)(&g).String()

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
		if a[i] != '0' && a[i] != '.' {
			a = a[:i+1]
			break
		}
	}

	return a
}

func (g Grams) NanoTON() *big.Int {
	return (*big.Int)(&g)
}

func (g *Grams) FromNanoTON(val *big.Int) *Grams {
	*g = Grams(*val)
	return g
}

func (g *Grams) MustFromTON(val string) *Grams {
	v, err := g.FromTON(val)
	if err != nil {
		panic(err)
		return nil
	}
	return v
}

func MustFromTON(val string) *Grams {
	v, err := new(Grams).FromTON(val)
	if err != nil {
		panic(err)
		return nil
	}
	return v
}

func FromTON(val string) (*Grams, error) {
	return new(Grams).FromTON(val)
}

func FromNanoTON(val *big.Int) *Grams {
	return new(Grams).FromNanoTON(val)
}

func FromNanoTONU(val uint64) *Grams {
	return new(Grams).FromNanoTON(new(big.Int).SetUint64(val))
}

func (g *Grams) FromTON(val string) (*Grams, error) {
	errInvalid := errors.New("invalid string")

	s := strings.SplitN(val, ".", 2)

	if len(s) == 0 {
		return nil, errInvalid
	}

	hi, ok := new(big.Int).SetString(s[0], 10)
	if !ok {
		return nil, errInvalid
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
			return nil, errInvalid
		}

		// log10 of 1 == 0, log10 of 10 = 1, so we need offset
		digits := int(math.Ceil(math.Log10(float64(lo + 1))))
		lo *= uint64(math.Pow10((9 - leadZeroes) - digits))

		hi = hi.Add(hi, new(big.Int).SetUint64(lo))
	}

	*g = Grams(*hi)
	return g, nil
}

func (g *Grams) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%s", g.NanoTON().String())), nil
}

func (g *Grams) String() string {
	return g.TON()
}
