//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorTonOpsEmulatorC7Path(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	globalIDCell, err := tlb.ToCell(&tlb.GlobalIDConfig{GlobalID: tonopsTestGlobalID})
	if err != nil {
		t.Fatalf("failed to build global id config: %v", err)
	}
	globalVersionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: vm.DefaultGlobalVersion})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	configRoot := mustConfigDictCell(t, map[uint32]*cell.Cell{
		uint32(tlb.ConfigParamGlobalID):      globalIDCell,
		uint32(tlb.ConfigParamGlobalVersion): globalVersionCell,
	})

	tests := []struct {
		name       string
		code       *cell.Cell
		prevBlocks tuple.Tuple
		gasLimit   int64
	}{
		{
			name: "getparamlong_now",
			code: prependRawMethodDrop(codeFromBuilders(t, funcsop.GETPARAMLONG(3).Serialize())),
		},
		{
			name: "globalid_from_config",
			code: prependRawMethodDrop(codeFromBuilders(t, funcsop.GLOBALID().Serialize())),
		},
		{
			name: "prev_blocks_info",
			code: prependRawMethodDrop(codeFromBuilders(t,
				funcsop.PREVBLOCKSINFOTUPLE().Serialize(),
				funcsop.PREVMCBLOCKS().Serialize(),
				funcsop.PREVKEYBLOCK().Serialize(),
				funcsop.PREVMCBLOCKS_100().Serialize(),
			)),
			prevBlocks: tuple.NewTupleValue(int64(111), int64(222), int64(333)),
		},
		{
			name:     "gas_limit_setter_path",
			code:     prependRawMethodDrop(codeFromBuilders(t, funcsop.GASCONSUMED().Serialize())),
			gasLimit: 50_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goStack, err := buildCrossStack()
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack()
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			c7, err := buildMessageEmulationC7(tonopsTestAddr, tt.code, MessageEmulationConfig{
				Now:        uint32(tonopsTestTime.Unix()),
				Balance:    new(big.Int).Set(tonopsTestBalance),
				RandSeed:   append([]byte(nil), tonopsTestSeed...),
				ConfigRoot: configRoot,
				GlobalID:   tonopsTestGlobalID,
				PrevBlocks: tt.prevBlocks,
			}, new(big.Int).Set(tonopsTestBalance))
			if err != nil {
				t.Fatalf("failed to build go c7: %v", err)
			}

			var goRes *crossRunResult
			if tt.gasLimit != 0 {
				goRes, err = runGoCrossCodeWithGas(tt.code, cell.BeginCell().EndCell(), c7, goStack, tt.gasLimit)
			} else {
				goRes, err = runGoCrossCode(tt.code, cell.BeginCell().EndCell(), c7, goStack)
			}
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refRes, err := runReferenceCrossCodeViaEmulator(tt.code, cell.BeginCell().EndCell(), refStack, referenceGetMethodConfig{
				Address:    tonopsTestAddr,
				Now:        uint32(tonopsTestTime.Unix()),
				Balance:    uint64(tonopsTestBalance.Int64()),
				RandSeed:   tonopsTestSeed,
				ConfigRoot: configRoot,
				PrevBlocks: tt.prevBlocks,
				GasLimit:   tt.gasLimit,
			})
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != refRes.exitCode {
				t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
			}
			if goRes.gasUsed != refRes.gasUsed {
				t.Fatalf("gas mismatch: go=%d reference=%d", goRes.gasUsed, refRes.gasUsed)
			}

			goStackCell, err := normalizeStackCell(goRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize go stack: %v", err)
			}
			refStackCell, err := normalizeStackCell(refRes.stack)
			if err != nil {
				t.Fatalf("failed to normalize reference stack: %v", err)
			}
			if !bytes.Equal(goStackCell.Hash(), refStackCell.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goStackCell.Dump(), refStackCell.Dump())
			}
		})
	}
}
