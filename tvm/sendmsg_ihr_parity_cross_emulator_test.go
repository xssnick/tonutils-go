//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
)

// The IHR component of the SENDMSG estimate is floored by the reference
// (uint128 shr 16), unlike the forward fee which rounds up; factors that leave
// a fractional remainder pin the difference.
func TestTVMCrossEmulatorSENDMSGIHRFloorParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	const version = 10
	for _, tc := range []struct {
		name      string
		ihrFactor uint32
	}{
		{name: "tiny factor floors to zero", ihrFactor: 1},
		{name: "fractional factor", ihrFactor: 3 << 15},
		{name: "near full factor", ihrFactor: (1 << 16) - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prices := tlb.ConfigMsgForwardPrices{
				LumpPrice: 1,
				BitPrice:  1 << 16,
				CellPrice: 3 << 16,
				IHRFactor: tc.ihrFactor,
				FirstFrac: 1 << 15,
			}
			configRoot := tonopsCrossSendMsgConfig(t, version, prices)
			unpacked := tonopsCrossSendMsgUnpackedConfig(t, prices)

			msg, err := tlb.ToCell(&tlb.InternalMessage{
				IHRDisabled: false,
				SrcAddr:     tonopsTestAddr,
				DstAddr:     tonopsTestAddr,
				Amount:      tlb.FromNanoTONU(100),
			})
			if err != nil {
				t.Fatalf("failed to build SENDMSG fixture: %v", err)
			}

			c7 := makeTonopsTestC7(t, tonopsTestC7Config{
				ConfigRoot:     configRoot,
				UnpackedConfig: unpacked,
				ExtraParams: map[int]any{
					8: cell.BeginCell().MustStoreAddr(tonopsTestAddr).ToSlice(),
				},
			})
			code := prependRawMethodDrop(codeFromBuilders(t, funcsop.SENDMSG().Serialize()))

			goStack, err := buildCrossStack(msg, int64(1024))
			if err != nil {
				t.Fatalf("failed to build go stack: %v", err)
			}
			refStack, err := buildCrossStack(msg, int64(1024))
			if err != nil {
				t.Fatalf("failed to build reference stack: %v", err)
			}

			goRes, err := runGoCrossCodeWithVersionGasAndLibs(code, cell.BeginCell().EndCell(), c7, nil, goStack, version, referenceDefaultMaxGas)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}
			refCfg := *tonopsCrossRefConfig(configRoot)
			refCfg.GasLimit = referenceDefaultMaxGas
			refRes, err := runReferenceCrossCodeViaEmulator(code, cell.BeginCell().EndCell(), refStack, refCfg)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != 0 || refRes.exitCode != 0 {
				t.Fatalf("unexpected exit code: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
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
