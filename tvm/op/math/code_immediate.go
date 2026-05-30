package math

import "github.com/xssnick/tonutils-go/tvm/cell"

func newBytePlusOneImmediate(value int8) (get func() int, serialize func() *cell.Builder, deserialize func(*cell.Slice) error) {
	decoded := int(value)
	if decoded < 1 {
		// The byte stores shift-1; keep zero-value registration placeholders
		// consistent when they are interpreted directly.
		decoded = 1
	}

	return func() int {
			return decoded
		}, func() *cell.Builder {
			encoded := uint64(0)
			if decoded > 0 {
				encoded = uint64(decoded - 1)
			}
			return cell.BeginCell().MustStoreUInt(encoded, 8)
		}, func(code *cell.Slice) error {
			val, err := code.LoadUInt(8)
			if err != nil {
				return err
			}
			decoded = int(val) + 1
			return nil
		}
}
