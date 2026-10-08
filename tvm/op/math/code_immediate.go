package math

import (
	"fmt"
)

// bytePlusOneArg encodes the 8-bit "value-1" immediate shared by the
// LSHIFT#/RSHIFT# and compound SHIFT#/MOD opcode families: the encoded byte
// stores value-1, so the representable range is 1..256.
func bytePlusOneArg(value int) uint64 {
	if value < 1 || value > 256 {
		panic(fmt.Sprintf("immediate value %d is out of range 1..256", value))
	}
	return uint64(value - 1)
}

// bytePlusOneValue is the 1..256 immediate carried by an encoded byte.
func bytePlusOneValue(args uint64) int {
	return int(uint8(args)) + 1
}
