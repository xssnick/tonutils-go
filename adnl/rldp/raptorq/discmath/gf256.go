package discmath

import (
	"fmt"
	"strings"
)

type GF256 struct {
	data []uint8
}

func (g *GF256) Add(g2 *GF256) {
	for i := 0; i < len(g.data); i++ {
		g.data[i] ^= g2.data[i]
	}
}

func (g *GF256) Mul(x uint8) {
	for i := 0; i < len(g.data); i++ {
		g.data[i] = octMul(g.data[i], x)
	}
}

func (g *GF256) AddMul(g2 *GF256, x uint8) {
	for i := 0; i < len(g.data); i++ {
		g.data[i] = octAdd(g.data[i], octMul(x, g2.data[i]))
	}
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
