//go:build amd64

package discmath

import "unsafe"

func asmSSE2XORBlocks(x, y unsafe.Pointer, blocks int)

func OctVecAdd(x, y []byte) {
	n := len(x)

	// split data to 16 byte blocks
	blocks := n / 16

	// TODO: try avx256 if supported
	// xor blocks using sse2 asm
	if blocks > 0 {
		asmSSE2XORBlocks(
			unsafe.Pointer(&x[0]),
			unsafe.Pointer(&y[0]),
			blocks,
		)
	}

	// xor rest bytes
	for i := blocks * 16; i < n; i++ {
		x[i] ^= y[i]
	}
}
