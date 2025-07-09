//go:build tinygo

package tlb

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"reflect"
)

func prepareMap(settings []string, structField reflect.StructField, dict *cell.Dictionary, sz uint64, skipProofBranches bool) (reflect.Value, error) {
	panic("dict->map serialization not supported in wasm js")
}

func prepareDict(fieldVal reflect.Value, settings []string, structField reflect.StructField) (*cell.Dictionary, error) {
	panic("dict->map serialization not supported in wasm js")
}
