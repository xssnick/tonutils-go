package discmath

import (
	"fmt"
	"strings"
)

type GF256 struct {
	data []uint8
}

func (g *GF256) Add(g2 *GF256) {
	OctVecAdd(g.data, g2.data)
}

func (g *GF256) Mul(x uint8) {
	OctVecMul(g.data, x)
}

func (g *GF256) AddMul(g2 *GF256, x uint8) {
	OctVecMulAdd(g.data, g2.data, x)
}

func (g *GF256) Bytes() []byte {
	return g.data
}

func (g *GF256) String() string {
	var list []string
	for _, d := range g.data {
		list = append(list, fmt.Sprintf("%02x", d))
	}
	return strings.Join(list, " ")
}
