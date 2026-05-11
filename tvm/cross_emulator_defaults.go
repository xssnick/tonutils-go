//go:build cgo && tvm_cross_emulator

package tvm

import "bytes"

const (
	referenceDefaultMaxGas            = int64(1_000_000_000)
	referenceDefaultTonopsBalance     = uint64(10_000_000)
	referenceDefaultWalletSendBalance = uint64(10_000_000_000)
)

var (
	referenceDefaultTonopsSeed     = bytes.Repeat([]byte{0x11}, 32)
	referenceDefaultWalletSendSeed = bytes.Repeat([]byte{0x37}, 32)
)
