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

	for _, tt := range tonOpsEmulatorC7Cases(t) {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runTonOpsEmulatorC7PathVersionCase(t, tt, vm.DefaultGlobalVersion)
		})
	}
}

func TestTVMCrossEmulatorTonOpsEmulatorC7PathAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	cases := tonOpsEmulatorC7Cases(t)
	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_TONOPS_EMULATOR_C7_VERSION_AUDIT") {
		version := version
		t.Run("global_v"+big.NewInt(int64(version)).String(), func(t *testing.T) {
			for _, tt := range cases {
				tt := tt
				t.Run(tt.name, func(t *testing.T) {
					runTonOpsEmulatorC7PathVersionCase(t, tt, version)
				})
			}
		})
	}
}

func FuzzTVMCrossEmulatorTonOpsEmulatorC7PathGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint8(version), uint8(version%tonOpsEmulatorC7CaseCount))
	}
	for i := 0; i < tonOpsEmulatorC7CaseCount; i++ {
		f.Add(uint8(MaxSupportedGlobalVersion), uint8(i))
	}
	f.Add(uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, rawVersion, rawCase uint8) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		cases := tonOpsEmulatorC7Cases(t)
		if len(cases) != tonOpsEmulatorC7CaseCount {
			t.Fatalf("tonops emulator c7 case count = %d, want %d", len(cases), tonOpsEmulatorC7CaseCount)
		}
		tt := cases[int(rawCase)%len(cases)]
		runTonOpsEmulatorC7PathVersionCase(t, tt, version)
	})
}

const tonOpsEmulatorC7CaseCount = 12

type tonOpsEmulatorC7Case struct {
	name       string
	code       *cell.Cell
	prevBlocks tuple.Tuple
	gasLimit   int64
	richConfig bool
}

func tonOpsEmulatorC7Cases(t *testing.T) []tonOpsEmulatorC7Case {
	t.Helper()

	return []tonOpsEmulatorC7Case{
		{
			name: "getparam_core_prefix",
			code: prependRawMethodDrop(codeFromBuilders(t,
				funcsop.GETPARAM(0).Serialize(),
				funcsop.GETPARAM(1).Serialize(),
				funcsop.GETPARAM(2).Serialize(),
				funcsop.GETPARAM(3).Serialize(),
				funcsop.GETPARAM(4).Serialize(),
				funcsop.GETPARAM(5).Serialize(),
			)),
		},
		{
			name: "getparamlong_now",
			code: prependRawMethodDrop(codeFromBuilders(t, funcsop.GETPARAMLONG(3).Serialize())),
		},
		{
			name: "getparam_context_aliases",
			code: prependRawMethodDrop(codeFromBuilders(t,
				funcsop.RANDSEED().Serialize(),
				funcsop.BALANCE().Serialize(),
				funcsop.MYADDR().Serialize(),
				funcsop.CONFIGROOT().Serialize(),
			)),
		},
		{
			name: "code_value_fee_aliases",
			code: prependRawMethodDrop(codeFromBuilders(t,
				funcsop.MYCODE().Serialize(),
				funcsop.INCOMINGVALUE().Serialize(),
				funcsop.STORAGEFEES().Serialize(),
			)),
		},
		{
			name: "globalid_from_config",
			code: prependRawMethodDrop(codeFromBuilders(t, funcsop.GLOBALID().Serialize())),
		},
		{
			name: "unpacked_config_tuple",
			code: prependRawMethodDrop(codeFromBuilders(t, funcsop.UNPACKEDCONFIGTUPLE().Serialize())),
		},
		{
			name:       "unpacked_config_rich_tuple",
			code:       prependRawMethodDrop(codeFromBuilders(t, funcsop.UNPACKEDCONFIGTUPLE().Serialize())),
			richConfig: true,
		},
		{
			name: "prev_blocks_info",
			code: prependRawMethodDrop(codeFromBuilders(t,
				funcsop.PREVBLOCKSINFOTUPLE().Serialize(),
				funcsop.PREVMCBLOCKS().Serialize(),
				funcsop.PREVKEYBLOCK().Serialize(),
				funcsop.PREVMCBLOCKS_100().Serialize(),
			)),
			prevBlocks: tuple.NewTupleValue(big.NewInt(111), big.NewInt(222), big.NewInt(333)),
		},
		{
			name:       "getparam_prev_blocks_direct",
			code:       prependRawMethodDrop(codeFromBuilders(t, funcsop.GETPARAM(13).Serialize())),
			prevBlocks: tuple.NewTupleValue(big.NewInt(111), big.NewInt(222), big.NewInt(333)),
		},
		{
			name: "getparamlong_context_tail",
			code: prependRawMethodDrop(codeFromBuilders(t,
				funcsop.GETPARAMLONG(6).Serialize(),
				funcsop.GETPARAMLONG(9).Serialize(),
				funcsop.GETPARAMLONG(14).Serialize(),
			)),
		},
		{
			name: "inmsgparams_defaults",
			code: prependRawMethodDrop(codeFromBuilders(t, funcsop.INMSGPARAMS().Serialize())),
		},
		{
			name:     "gas_limit_setter_path",
			code:     prependRawMethodDrop(codeFromBuilders(t, funcsop.GASCONSUMED().Serialize())),
			gasLimit: 50_000,
		},
	}
}

func tonOpsEmulatorC7ConfigRoot(t *testing.T, version uint32, rich bool) *cell.Cell {
	t.Helper()

	globalIDCell, err := tlb.ToCell(&tlb.GlobalIDConfig{GlobalID: tonopsTestGlobalID})
	if err != nil {
		t.Fatalf("failed to build global id config: %v", err)
	}
	globalVersionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: version})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}

	entries := map[uint32]*cell.Cell{
		uint32(tlb.ConfigParamGlobalID):      globalIDCell,
		uint32(tlb.ConfigParamGlobalVersion): globalVersionCell,
	}
	if rich {
		storageDict := cell.NewDict(32)
		if err = storageDict.SetIntKey(big.NewInt(100), makeStoragePricesSlice(100, 3, 5, 7, 11).MustToCell()); err != nil {
			t.Fatalf("failed to seed storage prices 100: %v", err)
		}
		if err = storageDict.SetIntKey(big.NewInt(200), makeStoragePricesSlice(200, 13, 17, 19, 23).MustToCell()); err != nil {
			t.Fatalf("failed to seed storage prices 200: %v", err)
		}
		entries[uint32(tlb.ConfigParamStoragePrices)] = storageDict.AsCell()
		entries[uint32(tlb.ConfigParamGasPricesMasterchain)] = makeGasPricesSlice(100, 77, 200, 1000, 1200, 50, 2000, 3000, 4000, true).MustToCell()
		entries[uint32(tlb.ConfigParamGasPricesBasechain)] = makeGasPricesSlice(100, 55, 150, 900, 900, 40, 1800, 2800, 3800, true).MustToCell()
		entries[uint32(tlb.ConfigParamMsgForwardPricesMasterchain)] = makeMsgPricesSlice(1000, 200, 300, 500, 1000, 2000).MustToCell()
		entries[uint32(tlb.ConfigParamMsgForwardPricesBasechain)] = makeMsgPricesSlice(900, 120, 220, 400, 800, 1200).MustToCell()
		entries[uint32(tlb.ConfigParamSizeLimits)] = makeSizeLimitsSlice(1<<20, 128).MustToCell()
	}

	return mustConfigDictCell(t, entries)
}

func runTonOpsEmulatorC7PathVersionCase(t *testing.T, tt tonOpsEmulatorC7Case, version int) {
	t.Helper()

	goStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build go stack: %v", err)
	}
	refStack, err := buildCrossStack()
	if err != nil {
		t.Fatalf("failed to build reference stack: %v", err)
	}
	configRoot := tonOpsEmulatorC7ConfigRoot(t, uint32(version), tt.richConfig)

	c7, err := buildMessageEmulationC7(tonopsTestAddr, tt.code, MessageEmulationConfig{
		Now:        uint32(tonopsTestTime.Unix()),
		Balance:    new(big.Int).Set(tonopsTestBalance),
		RandSeed:   append([]byte(nil), tonopsTestSeed...),
		Config:     MustPrepareConfig(configRoot),
		GlobalID:   tonopsTestGlobalID,
		PrevBlocks: tt.prevBlocks,
	}, new(big.Int).Set(tonopsTestBalance), uint32(version))
	if err != nil {
		t.Fatalf("failed to build go c7: %v", err)
	}

	gasLimit := referenceDefaultMaxGas
	if tt.gasLimit != 0 {
		gasLimit = tt.gasLimit
	}
	goRes, err := runGoCrossCodeWithVersionGasAndLibs(tt.code, cell.BeginCell().EndCell(), c7, nil, goStack, version, gasLimit)
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
}
