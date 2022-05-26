package tlb

import (
	"fmt"
	"math/big"
)

type Grams struct {
	val *big.Int
}

func (g Grams) TON() string {
	f := new(big.Float).SetInt(g.val)
	t := new(big.Float).Quo(f, new(big.Float).SetUint64(1000000000))

	return t.String()
}

func (g Grams) NanoTON() *big.Int {
	return g.val
}

func (g Grams) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%d", g.val)), nil
}
