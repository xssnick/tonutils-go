package helpers

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func Builder(b []byte) *cell.Builder {
	return cell.BeginCell().MustStoreSlice(b, uint(len(b)*8))
}
