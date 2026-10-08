//go:build cgo && tvm_cross_emulator

package tvm

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"math/bits"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorENDXCPrunedDepthLimit(t *testing.T) {
	// Drop the resulting cell to keep non-zero-level pruned roots out of the
	// serialized result stack. A successful ENDXC must still charge creation.
	code := cell.BeginCell().MustStoreSlice([]byte{0xcf, 0x23, 0x30}, 24).EndCell()
	for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
		refCfg := differentialFuzzExplicitVersionRefConfig(t, version)
		for mask := byte(1); mask <= 7; mask++ {
			count := bits.OnesCount8(mask)
			for index := 0; index < count; index++ {
				for _, depth := range []uint16{1024, 1025, 65535} {
					t.Run(fmt.Sprintf("v%d/mask%03b/slot%d/depth%d", version, mask, index, depth), func(t *testing.T) {
						payload := make([]byte, 2+count*(32+2))
						payload[0], payload[1] = byte(cell.PrunedCellType), mask
						binary.BigEndian.PutUint16(payload[2+count*32+index*2:], depth)
						builder := cell.BeginCell().MustStoreSlice(payload, uint(len(payload)*8))
						runDifferentialFuzzCase(t, differentialFuzzCase{
							family:           "pruned_depth",
							op:               "ENDXC DROP",
							code:             code,
							stack:            []any{builder, big.NewInt(-1)},
							globalVersion:    version,
							hasGlobalVersion: true,
							refCfg:           refCfg,
						})
					})
				}
			}
		}
	}
}
