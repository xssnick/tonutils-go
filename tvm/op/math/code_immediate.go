package math

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// newBytePlusOneImmediate handles the 8-bit "value-1" immediate shared by the
// LSHIFT#/RSHIFT# and compound SHIFT#/MOD opcode families: the encoded byte
// stores value-1, so the representable range is 1..256.
func newBytePlusOneImmediate(value int) (get func() int, serialize func() *cell.Builder, deserialize func(*cell.Slice) error) {
	if value < 1 || value > 256 {
		panic(fmt.Sprintf("immediate value %d is out of range 1..256", value))
	}
	decoded := value

	return func() int {
			return decoded
		}, func() *cell.Builder {
			return cell.BeginCell().MustStoreUInt(uint64(decoded-1), 8)
		}, func(code *cell.Slice) error {
			val, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			decoded = int(val) + 1
			return nil
		}
}
