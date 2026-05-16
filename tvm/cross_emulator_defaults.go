//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"strconv"
)

const (
	referenceDefaultMaxGas            = int64(1_000_000_000)
	referenceDefaultTonopsBalance     = uint64(10_000_000)
	referenceDefaultWalletSendBalance = uint64(10_000_000_000)
)

var (
	referenceDefaultTonopsSeed     = bytes.Repeat([]byte{0x11}, 32)
	referenceDefaultWalletSendSeed = bytes.Repeat([]byte{0x37}, 32)
)

func referenceVMLogEnabled() bool {
	return os.Getenv("TVM_REFERENCE_VM_LOG") != "" || os.Getenv("TVM_TRACE_WALLET_SEND_REF") != ""
}

func referenceVMLogVerbosity() int {
	if raw := os.Getenv("TVM_REFERENCE_VM_LOG"); raw != "" {
		verbosity, err := strconv.Atoi(raw)
		if err == nil {
			return verbosity
		}
		return 1
	}
	if os.Getenv("TVM_TRACE_WALLET_SEND_REF") != "" {
		return 3
	}
	return 0
}

func referenceVMLog(raw string) string {
	if !referenceVMLogEnabled() {
		return ""
	}
	return raw
}
