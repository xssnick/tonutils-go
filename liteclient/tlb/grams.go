package tlb

import (
	"errors"
	"fmt"
	"math/big"
)

type Grams big.Int

func (g Grams) TON() string {
	f := new(big.Float).SetInt((*big.Int)(&g))
	t := new(big.Float).Quo(f, new(big.Float).SetUint64(1000000000))

	return t.String()
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

func (g *Grams) FromTON(val string) (*Grams, error) {
	f, ok := new(big.Float).SetString(val)
	if !ok {
		return nil, errors.New("invalid string")
	}

	f = f.Mul(f, new(big.Float).SetUint64(1000000000))
	i, _ := f.Int(new(big.Int))

	*g = Grams(*i)
	return g, nil
}

func (g *Grams) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", g.TON())), nil
}
