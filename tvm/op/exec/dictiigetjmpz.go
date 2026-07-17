package exec

import (
	dictop "github.com/xssnick/tonutils-go/tvm/op/dict"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
)

// Deprecated: use dict.DICTIGETJMPZ. This constructor returns the canonical
// dictionary implementation registered for F4BC.
func DICTIGETJMPZ() *helpers.SimpleOP {
	return dictop.DICTIGETJMPZ()
}
