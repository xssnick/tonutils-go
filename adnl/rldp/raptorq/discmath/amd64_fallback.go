//go:build !amd64

package discmath

import "unsafe"

func OctVecAdd(x, y []byte) {
	n := len(x)
	xUint64 := *(*[]uint64)(unsafe.Pointer(&x))
	yUint64 := *(*[]uint64)(unsafe.Pointer(&y))

	for i := 0; i < n/8; i++ {
		xUint64[i] ^= yUint64[i]
	}

	for i := n - n%8; i < n; i++ {
		x[i] ^= y[i]
	}
}
