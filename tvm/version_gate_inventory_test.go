package tvm

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var expectedRuntimeGlobalVersionGateCounts = map[string]int{
	"external_message.go":                  3,
	"op/cellslice/advanced_deserialize.go": 1,
	"op/dict/dictops.go":                   2,
	"op/exec/controlregs.go":               1,
	"op/funcs/tonops.go":                   3,
	"op/funcs/tonops_crypto_circl.go":      2,
	"op/funcs/tonops_fee_hash.go":          8,
	"op/funcs/tonops_misc_message.go":      15,
	"op/math/arith_helpers.go":             3,
	"op/math/quiet_compound.go":            4,
	"op/math/quiet_logic.go":               3,
	"op/math/rshiftcode.go":                1,
	"op/stack/blkswx.go":                   1,
	"op/stack/internal.go":                 1,
	"transaction_account.go":               13,
	"transaction_actions.go":               26,
	"transaction_bounce.go":                3,
	"transaction_config.go":                1,
	"transaction_emulation.go":             1,
	"transaction_fees.go":                  4,
	"vm/child_vm.go":                       2,
	"vm/exec.go":                           2,
	"vm/libraries.go":                      2,
	"vm.go":                                6,
	"vm/state.go":                          3,
}

var expectedRuntimeGlobalVersionGateThresholds = []int{0, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}

var expectedTransactionGlobalVersionGateCounts = map[string]int{
	"transaction_account.go":   13,
	"transaction_actions.go":   26,
	"transaction_bounce.go":    3,
	"transaction_config.go":    1,
	"transaction_emulation.go": 1,
	"transaction_fees.go":      4,
}

var expectedTransactionGlobalVersionGateThresholds = []int{4, 5, 7, 8, 9, 10, 11, 12, 13, 14}

const (
	expectedRuntimeGlobalVersionDynamicGateCount = 2
	expectedRuntimeGlobalVersionDynamicGateHash  = "b2ae587fe7facfa1a397df658f5b744ef454fc96fe1d345398ec064486fcd95c"

	expectedTransactionGlobalVersionDynamicGateCount = 1
)

var expectedRuntimeGlobalVersionGateCoverageAnchors = map[string][]string{
	"external_message.go": {
		"FuzzMessageExecutionGlobalVersionSelection",
	},
	"op/cellslice/advanced_deserialize.go": {
		"FuzzTVMVersionedXLoadLibraryResolution",
	},
	"op/dict/dictops.go": {
		"FuzzTVMVersionedPrefixDictShortStackPrecheckMatrix",
		"FuzzTVMCrossEmulatorDictOpsVersionedPrefixUnderflow",
	},
	"op/exec/controlregs.go": {
		"FuzzTVMVersionedControlRegisterDuplicateSaveWrites",
	},
	"op/funcs/tonops.go": {
		"FuzzTVMEcrecoverEthereumRecoveryIDsV14Boundary",
	},
	"op/funcs/tonops_crypto_circl.go": {
		"FuzzTVMRistrettoIdentityAndZeroScalarV14Boundary",
	},
	"op/funcs/tonops_fee_hash.go": {
		"FuzzTVMVersionedFeeHashUnderflowPrecheck",
		"FuzzTVMVersionedFeeHashRuntimeEdges",
	},
	"op/funcs/tonops_misc_message.go": {
		"FuzzTVMVersionedDataSizeLowGasDeferral",
		"FuzzTVMCrossEmulatorTonOpsSendMsgVersionedFeeEdges",
		"FuzzTVMCrossEmulatorTonOpsSendMsgExtraFlagsRootSizeGlobalVersion",
	},
	"op/math/arith_helpers.go": {
		"FuzzTVMVersionedLogicNaNRules",
		"FuzzTVMVersionedImmediateShiftNaNRules",
	},
	"op/math/quiet_compound.go": {
		"FuzzTVMVersionedQuietDynamicShiftRanges",
	},
	"op/math/quiet_logic.go": {
		"FuzzTVMVersionedLogicNaNRules",
	},
	"op/math/rshiftcode.go": {
		"FuzzTVMVersionedImmediateShiftNaNRules",
	},
	"op/stack/blkswx.go": {
		"FuzzTVMVersionedBLKSWXLargeMoveGas",
	},
	"op/stack/internal.go": {
		"FuzzTVMVersionedDynamicStackIndexLimits",
	},
	"transaction_account.go": {
		"FuzzTransactionFinalizeAccountStatusBoundaries",
		"FuzzTransactionVersionedAccountStateLimitBoundaries",
	},
	"transaction_actions.go": {
		"FuzzTransactionVersionedReserveActionBoundaries",
		"FuzzTVMCrossEmulatorTransactionSendMsgExtraFlagsGlobalVersion",
	},
	"transaction_bounce.go": {
		"FuzzTransactionVersionedBounceMessageUsage",
		"FuzzTVMCrossEmulatorTransactionBounceFormatGlobalVersion",
	},
	"transaction_config.go": {
		"FuzzTransactionVersionedGasLimitBoundaries",
	},
	"transaction_emulation.go": {
		"FuzzTransactionVersionedGasLimitBoundaries",
	},
	"transaction_fees.go": {
		"FuzzTransactionVersionedMessageFeeWrappersUseTailUsage",
		"FuzzTransactionVersionedOutboundFeeUsageLayout",
	},
	"vm.go": {
		"FuzzExecutionConfigGlobalVersionPerRunEntrypoints",
		"FuzzTVMLibraryCodeCellStartupV9BoundaryCosts",
		"FuzzTVMVersionedInstructionGasCommitBoundary",
	},
	"vm/child_vm.go": {
		"FuzzRunChildVMVersionedGasClamp",
		"FuzzRunVMChildGlobalVersionInheritance",
	},
	"vm/exec.go": {
		"FuzzJumpToVersionedNestedGasAndAdjust",
	},
	"vm/libraries.go": {
		"FuzzLoadLibraryByHashVersionedLookupGas",
	},
	"vm/state.go": {
		"FuzzStateVersionedGasConsumptionBoundary",
	},
}

var expectedRuntimeGlobalVersionGateCrossEmulatorAnchors = map[string][]string{
	"external_message.go": {
		"FuzzTVMCrossEmulatorDirectMessageGlobalVersion",
		"FuzzTVMCrossEmulatorCheckExternalMessageAcceptedGlobalVersion",
	},
	"op/cellslice/advanced_deserialize.go": {
		"FuzzTVMCrossEmulatorAdvancedCellOpsGeneratedGlobalVersion",
	},
	"op/dict/dictops.go": {
		"FuzzTVMCrossEmulatorDictOpsVersionedPrefixUnderflow",
	},
	"op/exec/controlregs.go": {
		"FuzzTVMCrossEmulatorContOpsVersionedEdges",
	},
	"op/funcs/tonops.go": {
		"FuzzTVMCrossEmulatorSupercontractCryptoPrefixesGlobalVersion",
		"FuzzTVMCrossEmulatorTonOpsEdgeGlobalVersion",
	},
	"op/funcs/tonops_crypto_circl.go": {
		"FuzzTVMCrossEmulatorTonOpsCryptoCirclVersionedRuntimeEdges",
	},
	"op/funcs/tonops_fee_hash.go": {
		"FuzzTVMCrossEmulatorTonOpsUnderflowPrecheckGlobalVersion",
		"FuzzTVMCrossEmulatorTonOpsEdgeGlobalVersion",
	},
	"op/funcs/tonops_misc_message.go": {
		"FuzzTVMCrossEmulatorTonOpsSendMsgVersionedFeeEdges",
		"FuzzTVMCrossEmulatorTonOpsSendMsgExtraFlagsRootSizeGlobalVersion",
		"FuzzTVMCrossEmulatorDataSizeLowGasGlobalVersion",
	},
	"op/math/arith_helpers.go": {
		"FuzzTVMCrossEmulatorArithOpsOpcodeCoverageGlobalVersion",
		"FuzzTVMCrossEmulatorArithOpsVersionedQuietEdges",
	},
	"op/math/quiet_compound.go": {
		"FuzzTVMCrossEmulatorArithOpsVersionedQuietEdges",
	},
	"op/math/quiet_logic.go": {
		"FuzzTVMCrossEmulatorArithOpsVersionedQuietEdges",
	},
	"op/math/rshiftcode.go": {
		"FuzzTVMCrossEmulatorArithOpsSignedRoundingEdgesGlobalVersion",
	},
	"op/stack/blkswx.go": {
		"FuzzTVMCrossEmulatorStackOpsMiscGlobalVersion",
	},
	"op/stack/internal.go": {
		"FuzzTVMCrossEmulatorStackOpsVersionedDynamicEdges",
	},
	"transaction_account.go": {
		"FuzzTVMCrossEmulatorTransactionStorageDeletionDestroyedGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionMasterchainStateLimitGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionFrozenExternalStateHashGlobalVersion",
	},
	"transaction_actions.go": {
		"FuzzTVMCrossEmulatorTransactionRawReserveActionsGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionSendMsgExtraFlagsGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionFailedActionMessageBalanceGlobalVersion",
	},
	"transaction_bounce.go": {
		"FuzzTVMCrossEmulatorTransactionBounceFormatGlobalVersion",
	},
	"transaction_config.go": {
		"FuzzTVMCrossEmulatorTransactionSpecialGasFullGlobalVersion",
	},
	"transaction_emulation.go": {
		"FuzzTVMCrossEmulatorTransactionComputeGlobalVersionOverride",
		"FuzzTVMCrossEmulatorTransactionC7OptionsGlobalVersion",
	},
	"transaction_fees.go": {
		"FuzzTVMCrossEmulatorTransactionHistoricalGasLimitOverrideGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionSpecialGasFullGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionSendExtraCurrencySizeGlobalVersion",
	},
	"vm.go": {
		"FuzzTVMCrossEmulatorExecutionConfigGlobalVersionOverride",
		"FuzzTVMCrossEmulatorLibraryCodeCellStartupGlobalVersion",
		"FuzzTVMCrossEmulatorOpcodeMinGlobalVersionBoundaries",
	},
	"vm/child_vm.go": {
		"FuzzTVMCrossEmulatorRunVMChildGlobalVersionInheritance",
		"FuzzTVMCrossEmulatorRunVMVersionedChildOpcodeMatrix",
	},
	"vm/exec.go": {
		"FuzzTVMCrossEmulatorCaughtTryGlobalVersion",
		"FuzzTVMCrossEmulatorContExecScenariosGlobalVersion",
	},
	"vm/libraries.go": {
		"FuzzTVMCrossEmulatorLibraryLookupGasGlobalVersion",
	},
	"vm/state.go": {
		"FuzzTVMCrossEmulatorGasGlobalVersion",
		"FuzzTVMCrossEmulatorExecutionConfigGlobalVersionOverride",
		"FuzzTVMCrossEmulatorAllGlobalVersionsSmoke",
	},
}

var expectedTransactionGlobalVersionGateLocalAnchors = map[string][]string{
	"transaction_account.go": {
		"FuzzTransactionFinalizeAccountStatusBoundaries",
		"FuzzTransactionVersionedAccountStateLimitBoundaries",
	},
	"transaction_actions.go": {
		"FuzzTransactionVersionedReserveActionBoundaries",
	},
	"transaction_bounce.go": {
		"FuzzTransactionVersionedBounceMessageUsage",
	},
	"transaction_config.go": {
		"FuzzTransactionVersionedGasLimitBoundaries",
	},
	"transaction_emulation.go": {
		"FuzzTransactionVersionedGasLimitBoundaries",
	},
	"transaction_fees.go": {
		"FuzzTransactionVersionedMessageFeeWrappersUseTailUsage",
		"FuzzTransactionVersionedOutboundFeeUsageLayout",
	},
}

var expectedTransactionGlobalVersionGateCrossEmulatorAnchors = map[string][]string{
	"transaction_account.go": {
		"FuzzTVMCrossEmulatorTransactionStorageDeletionDestroyedGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionMasterchainStateLimitGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionFrozenExternalStateHashGlobalVersion",
	},
	"transaction_actions.go": {
		"FuzzTVMCrossEmulatorTransactionRawReserveActionsGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionSendMsgExtraFlagsGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionFailedActionMessageBalanceGlobalVersion",
	},
	"transaction_bounce.go": {
		"FuzzTVMCrossEmulatorTransactionBounceFormatGlobalVersion",
	},
	"transaction_config.go": {
		"FuzzTVMCrossEmulatorTransactionSpecialGasFullGlobalVersion",
	},
	"transaction_emulation.go": {
		"FuzzTVMCrossEmulatorTransactionComputeGlobalVersionOverride",
		"FuzzTVMCrossEmulatorTransactionC7OptionsGlobalVersion",
	},
	"transaction_fees.go": {
		"FuzzTVMCrossEmulatorTransactionHistoricalGasLimitOverrideGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionSpecialGasFullGlobalVersion",
		"FuzzTVMCrossEmulatorTransactionSendExtraCurrencySizeGlobalVersion",
	},
}

var expectedTVMGlobalVersionFieldWriters = map[string]int{
	"vm.go:SetGlobalVersion":  1,
	"vm.go:WithGlobalVersion": 1,
}

var expectedTVMTestGlobalVersionFieldWriters = map[string]int{
	"parity_fuzz_cross_emulator_test.go:differentialFuzzWithGlobalVersion": 1,
}

type runtimeGlobalVersionGate struct {
	file      string
	line      int
	column    int
	threshold int
}

func TestTVMRuntimeGlobalVersionGateInventory(t *testing.T) {
	gates := runtimeGlobalVersionGatesFromSource(t)

	counts := make(map[string]int)
	thresholds := make(map[int]struct{})
	for _, gate := range gates {
		counts[gate.file]++
		if gate.threshold >= 0 {
			thresholds[gate.threshold] = struct{}{}
		}
	}

	if len(counts) != len(expectedRuntimeGlobalVersionGateCounts) {
		t.Fatalf("runtime global-version gate file count = %d, want %d; got %s", len(counts), len(expectedRuntimeGlobalVersionGateCounts), runtimeGlobalVersionGateCountsString(counts))
	}
	for file, want := range expectedRuntimeGlobalVersionGateCounts {
		if got := counts[file]; got != want {
			t.Fatalf("%s runtime global-version gate count = %d, want %d; all counts %s", file, got, want, runtimeGlobalVersionGateCountsString(counts))
		}
	}
	for file, got := range counts {
		if _, ok := expectedRuntimeGlobalVersionGateCounts[file]; !ok {
			t.Fatalf("unexpected runtime global-version gate file %s with %d gates", file, got)
		}
	}

	if len(thresholds) != len(expectedRuntimeGlobalVersionGateThresholds) {
		t.Fatalf("runtime global-version gate thresholds = %v, want %v", runtimeGlobalVersionGateThresholdList(thresholds), expectedRuntimeGlobalVersionGateThresholds)
	}
	for _, version := range expectedRuntimeGlobalVersionGateThresholds {
		if _, ok := thresholds[version]; !ok {
			t.Fatalf("runtime global-version gates do not include threshold %d; got %v", version, runtimeGlobalVersionGateThresholdList(thresholds))
		}
	}
}

func TestTVMRuntimeGlobalVersionGateScannerCoversVersionComparisons(t *testing.T) {
	gates := runtimeGlobalVersionGatesFromSource(t)
	candidates := runtimeGlobalVersionGateCandidatesFromSource(t)

	gateSet := make(map[runtimeGlobalVersionGateKey]runtimeGlobalVersionGate, len(gates))
	for _, gate := range gates {
		gateSet[runtimeGlobalVersionGateKeyFromGate(gate)] = gate
	}
	candidateSet := make(map[runtimeGlobalVersionGateKey]runtimeGlobalVersionGate, len(candidates))
	for _, gate := range candidates {
		candidateSet[runtimeGlobalVersionGateKeyFromGate(gate)] = gate
	}

	var missing []string
	for key, candidate := range candidateSet {
		if _, ok := gateSet[key]; !ok {
			missing = append(missing, runtimeGlobalVersionGateString(candidate))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("runtime global-version gate scanner missed comparisons:\n%s", strings.Join(missing, "\n"))
	}

	var unexpected []string
	for key, gate := range gateSet {
		if _, ok := candidateSet[key]; !ok {
			unexpected = append(unexpected, runtimeGlobalVersionGateString(gate))
		}
	}
	if len(unexpected) > 0 {
		sort.Strings(unexpected)
		t.Fatalf("runtime global-version gate scanner found comparisons not recognized by broad audit:\n%s", strings.Join(unexpected, "\n"))
	}
}

func TestTVMRuntimeGlobalVersionGateThresholdsStaySupported(t *testing.T) {
	for _, gate := range runtimeGlobalVersionGatesFromSource(t) {
		if gate.threshold < 0 {
			continue
		}
		if gate.threshold < MinSupportedGlobalVersion || gate.threshold > MaxSupportedGlobalVersion {
			t.Fatalf("runtime global-version gate %s uses threshold outside supported range [%d, %d]", runtimeGlobalVersionGateString(gate), MinSupportedGlobalVersion, MaxSupportedGlobalVersion)
		}
	}
}

func TestTVMRuntimeGlobalVersionDynamicGateInventory(t *testing.T) {
	gates := runtimeGlobalVersionDynamicGateInventory(t)
	if len(gates) != expectedRuntimeGlobalVersionDynamicGateCount {
		t.Fatalf("runtime dynamic global-version gate count = %d, want %d:\n%s", len(gates), expectedRuntimeGlobalVersionDynamicGateCount, strings.Join(gates, "\n"))
	}
	if got := runtimeGlobalVersionGateInventoryHash(gates); got != expectedRuntimeGlobalVersionDynamicGateHash {
		t.Fatalf("runtime dynamic global-version gate hash = %s, want %s:\n%s", got, expectedRuntimeGlobalVersionDynamicGateHash, strings.Join(gates, "\n"))
	}
}

func TestTVMRuntimeGlobalVersionGateLiteralScannerCoversSupportedConstants(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "global_version_gate_literals.go", `
package tvm

func globalVersionGateLiteralFixture(version int) {
	_ = version < MinSupportedGlobalVersion
	_ = version > MaxSupportedGlobalVersion
}
`, 0)
	if err != nil {
		t.Fatalf("parse global-version gate literal fixture: %v", err)
	}

	var thresholds []int
	ast.Inspect(file, func(node ast.Node) bool {
		expr, ok := node.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		threshold, ok := runtimeGlobalVersionGateThreshold(expr.X, expr.Y)
		if ok {
			thresholds = append(thresholds, threshold)
		}
		return true
	})
	if len(thresholds) != 2 || thresholds[0] != MinSupportedGlobalVersion || thresholds[1] != MaxSupportedGlobalVersion {
		t.Fatalf("global-version gate constant thresholds = %v, want [%d %d]", thresholds, MinSupportedGlobalVersion, MaxSupportedGlobalVersion)
	}
}

func TestTVMRuntimeGlobalVersionGateScannerCoversSupportedShapes(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "global_version_gate_shapes.go", `
package tvm

func globalVersionGateShapeFixture(globalVersion int, version int, state *State, s *State, cfg TransactionEmulationConfig, action TransactionAction) {
	_ = globalVersion >= 4
	_ = version < 5
	_ = state.GlobalVersion <= 6
	_ = s.GlobalVersion > 7
	_ = cfg.globalVersion() >= 8
	_ = state.effectiveGlobalVersion() >= 10
	_ = version.Version == 11
}
	`, 0)
	if err != nil {
		t.Fatalf("parse global-version gate shape fixture: %v", err)
	}

	var thresholds []int
	ast.Inspect(file, func(node ast.Node) bool {
		expr, ok := node.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		threshold, ok := runtimeGlobalVersionGateThreshold(expr.X, expr.Y)
		if ok {
			thresholds = append(thresholds, threshold)
		}
		return true
	})

	want := []int{4, 5, 6, 7, 8, 10, 11}
	if len(thresholds) != len(want) {
		t.Fatalf("global-version gate shape thresholds = %v, want %v", thresholds, want)
	}
	for i, want := range want {
		if thresholds[i] != want {
			t.Fatalf("global-version gate shape thresholds = %v, want %v", thresholds, []int{4, 5, 6, 7, 8, 10, 11})
		}
	}
}

func TestTVMGlobalVersionFieldWritesStayValidated(t *testing.T) {
	writers := tvmGlobalVersionFieldWriters(t)
	if len(writers) != len(expectedTVMGlobalVersionFieldWriters) {
		t.Fatalf("TVM.globalVersion writer count = %d, want %d; got:\n%s", len(writers), len(expectedTVMGlobalVersionFieldWriters), tvmGlobalVersionFieldWriterList(writers))
	}
	for key, want := range expectedTVMGlobalVersionFieldWriters {
		if got, ok := writers[key]; !ok {
			t.Fatalf("missing TVM.globalVersion writer %s; got:\n%s", key, tvmGlobalVersionFieldWriterList(writers))
		} else if got != want {
			t.Fatalf("TVM.globalVersion writer %s count = %d, want %d", key, got, want)
		}
	}
	for key := range writers {
		if _, ok := expectedTVMGlobalVersionFieldWriters[key]; !ok {
			t.Fatalf("unexpected direct TVM.globalVersion writer %s; use SetGlobalVersion or WithGlobalVersion", key)
		}
	}
}

func TestTVMTestsUseGlobalVersionSetterForMachines(t *testing.T) {
	writers := tvmTestGlobalVersionFieldWriters(t)
	if len(writers) != len(expectedTVMTestGlobalVersionFieldWriters) {
		t.Fatalf("test globalVersion writer count = %d, want %d; got:\n%s", len(writers), len(expectedTVMTestGlobalVersionFieldWriters), tvmGlobalVersionFieldWriterList(writers))
	}
	for key, want := range expectedTVMTestGlobalVersionFieldWriters {
		if got, ok := writers[key]; !ok {
			t.Fatalf("missing expected test globalVersion metadata writer %s; got:\n%s", key, tvmGlobalVersionFieldWriterList(writers))
		} else if got != want {
			t.Fatalf("test globalVersion writer %s count = %d, want %d", key, got, want)
		}
	}
	for key := range writers {
		if _, ok := expectedTVMTestGlobalVersionFieldWriters[key]; !ok {
			t.Fatalf("unexpected test globalVersion writer %s; use SetGlobalVersion/WithGlobalVersion for TVM machines or add explicit metadata inventory", key)
		}
	}
}

func TestTVMGlobalVersionFieldWriteScannerCoversArbitrarySelectors(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "global_version_writers_test.go", `
package tvm

func writesMachine(machine *TVM, tv *TVM, tc *caseMeta) {
	machine.globalVersion = 1
	tv.globalVersion = 2
	tc.globalVersion = 3
}
`, 0)
	if err != nil {
		t.Fatalf("parse global version writer fixture: %v", err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if got := tvmGlobalVersionFieldWriteCount(fn); got != 3 {
			t.Fatalf("globalVersion field write scanner count = %d, want 3", got)
		}
		return
	}
	t.Fatal("global version writer fixture has no function")
}

func TestTVMProductionExecutionStateConstructorsUseExplicitGlobalVersion(t *testing.T) {
	var calls []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !runtimeGlobalVersionLegacyStateConstructor(call.Fun) {
				return true
			}
			pos := fset.Position(call.Pos())
			calls = append(calls, fmt.Sprintf("%s:%d:%d", filepath.ToSlash(path), pos.Line, pos.Column))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan execution state constructors: %v", err)
	}
	if len(calls) > 0 {
		sort.Strings(calls)
		t.Fatalf("production code uses legacy NewExecutionState; use NewExecutionStateWithGlobalVersion on runtime paths so explicit v0 is preserved:\n%s", strings.Join(calls, "\n"))
	}
}

func tvmGlobalVersionFieldWriters(t *testing.T) map[string]int {
	t.Helper()

	writers := make(map[string]int)
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch path {
			case ".git":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			count := tvmGlobalVersionFieldWriteCount(fn)
			if count == 0 {
				continue
			}
			writers[filepath.ToSlash(path)+":"+fn.Name.Name] = count
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan TVM.globalVersion writers: %v", err)
	}
	if len(writers) == 0 {
		t.Fatal("TVM.globalVersion writer inventory is empty")
	}
	return writers
}

func tvmTestGlobalVersionFieldWriters(t *testing.T) map[string]int {
	t.Helper()

	writers := make(map[string]int)
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch path {
			case ".git":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			count := tvmGlobalVersionFieldWriteCount(fn)
			if count == 0 {
				continue
			}
			writers[filepath.ToSlash(path)+":"+fn.Name.Name] = count
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan test TVM.globalVersion writers: %v", err)
	}
	return writers
}

func tvmGlobalVersionFieldWriteCount(fn *ast.FuncDecl) int {
	count := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			if tvmGlobalVersionFieldSelector(lhs) {
				count++
			}
		}
		return true
	})
	return count
}

func tvmGlobalVersionFieldSelector(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "globalVersion"
}

func tvmGlobalVersionFieldWriterList(writers map[string]int) string {
	items := make([]string, 0, len(writers))
	for key, count := range writers {
		items = append(items, fmt.Sprintf("%s:%d", key, count))
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}

func TestTVMTransactionGlobalVersionGateInventory(t *testing.T) {
	gates := runtimeGlobalVersionGatesFromSource(t)

	counts := make(map[string]int)
	thresholds := make(map[int]struct{})
	dynamicGates := 0
	for _, gate := range gates {
		if !strings.HasPrefix(gate.file, "transaction_") {
			continue
		}
		counts[gate.file]++
		if gate.threshold >= 0 {
			thresholds[gate.threshold] = struct{}{}
		} else {
			dynamicGates++
		}
	}

	if len(counts) != len(expectedTransactionGlobalVersionGateCounts) {
		t.Fatalf("transaction global-version gate file count = %d, want %d; got %s", len(counts), len(expectedTransactionGlobalVersionGateCounts), runtimeGlobalVersionGateCountsString(counts))
	}
	for file, want := range expectedTransactionGlobalVersionGateCounts {
		if got := counts[file]; got != want {
			t.Fatalf("%s transaction global-version gate count = %d, want %d; all counts %s", file, got, want, runtimeGlobalVersionGateCountsString(counts))
		}
	}
	for file, got := range counts {
		if _, ok := expectedTransactionGlobalVersionGateCounts[file]; !ok {
			t.Fatalf("unexpected transaction global-version gate file %s with %d gates", file, got)
		}
	}

	if got := dynamicGates; got != expectedTransactionGlobalVersionDynamicGateCount {
		t.Fatalf("transaction dynamic global-version gate count = %d, want %d", got, expectedTransactionGlobalVersionDynamicGateCount)
	}
	if len(thresholds) != len(expectedTransactionGlobalVersionGateThresholds) {
		t.Fatalf("transaction global-version gate thresholds = %v, want %v", runtimeGlobalVersionGateThresholdList(thresholds), expectedTransactionGlobalVersionGateThresholds)
	}
	for _, version := range expectedTransactionGlobalVersionGateThresholds {
		if _, ok := thresholds[version]; !ok {
			t.Fatalf("transaction global-version gates do not include threshold %d; got %v", version, runtimeGlobalVersionGateThresholdList(thresholds))
		}
	}
}

func TestTVMRuntimeGlobalVersionGateFilesHaveCoverageAnchors(t *testing.T) {
	if len(expectedRuntimeGlobalVersionGateCoverageAnchors) != len(expectedRuntimeGlobalVersionGateCounts) {
		t.Fatalf("runtime global-version coverage anchor file count = %d, want %d", len(expectedRuntimeGlobalVersionGateCoverageAnchors), len(expectedRuntimeGlobalVersionGateCounts))
	}

	knownAnchors := runtimeGlobalVersionCoverageAnchorNames(t)
	for file := range expectedRuntimeGlobalVersionGateCounts {
		anchors, ok := expectedRuntimeGlobalVersionGateCoverageAnchors[file]
		if !ok {
			t.Fatalf("runtime global-version gate file %s has no coverage anchors", file)
		}
		if len(anchors) == 0 {
			t.Fatalf("runtime global-version gate file %s has an empty coverage anchor list", file)
		}
		for _, anchor := range anchors {
			if _, ok = knownAnchors[anchor]; !ok {
				t.Fatalf("runtime global-version gate file %s lost coverage anchor %s", file, anchor)
			}
		}
	}
	for file := range expectedRuntimeGlobalVersionGateCoverageAnchors {
		if _, ok := expectedRuntimeGlobalVersionGateCounts[file]; !ok {
			t.Fatalf("coverage anchor file %s does not match a runtime global-version gate file", file)
		}
	}
}

func TestTVMRuntimeGlobalVersionGateFilesHaveVersionSeededLocalAnchors(t *testing.T) {
	functions := runtimeGlobalVersionCoverageAnchorFunctions(t)
	for file := range expectedRuntimeGlobalVersionGateCounts {
		anchors := expectedRuntimeGlobalVersionGateCoverageAnchors[file]

		var local []string
		var seeded []string
		for _, anchor := range anchors {
			if strings.Contains(anchor, "CrossEmulator") {
				continue
			}
			local = append(local, anchor)

			fn, ok := functions[anchor]
			if !ok {
				t.Fatalf("runtime global-version gate file %s lost local coverage anchor %s", file, anchor)
			}
			if runtimeGlobalVersionCoverageAnchorSeedsVersions(fn.fn) {
				seeded = append(seeded, anchor)
			}
		}

		if len(local) == 0 {
			t.Fatalf("runtime global-version gate file %s has no local coverage anchors", file)
		}
		if len(seeded) == 0 {
			t.Fatalf("runtime global-version gate file %s local anchors do not seed or iterate global versions: %s", file, strings.Join(local, ", "))
		}
	}
}

func TestTVMRuntimeGlobalVersionGateBoundaryLocalAnchorsUseAdjacentVersions(t *testing.T) {
	functions := runtimeGlobalVersionCoverageAnchorFunctions(t)

	var missing []string
	for file, anchors := range expectedRuntimeGlobalVersionGateCoverageAnchors {
		for _, anchor := range anchors {
			if strings.Contains(anchor, "CrossEmulator") {
				continue
			}

			fn, ok := functions[anchor]
			if !ok {
				t.Fatalf("runtime global-version gate file %s lost local coverage anchor %s", file, anchor)
			}
			if !runtimeGlobalVersionCoverageAnchorSeedsVersions(fn.fn) || runtimeGlobalVersionCoverageAnchorSeedsFullRange(fn.fn) {
				continue
			}

			versions := runtimeGlobalVersionCoverageAnchorTargetedVersions(fn.fn)
			if !runtimeGlobalVersionCoverageVersionsHaveAdjacentPair(versions) {
				missing = append(missing, fmt.Sprintf("%s:%s seeds %s", file, anchor, runtimeGlobalVersionCoverageVersionSetString(versions)))
			}
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("runtime global-version boundary local anchors without adjacent supported-version seeds:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMRuntimeGlobalVersionGateFilesHaveCrossEmulatorAnchors(t *testing.T) {
	if len(expectedRuntimeGlobalVersionGateCrossEmulatorAnchors) != len(expectedRuntimeGlobalVersionGateCounts) {
		t.Fatalf("runtime global-version cross-emulator anchor file count = %d, want %d", len(expectedRuntimeGlobalVersionGateCrossEmulatorAnchors), len(expectedRuntimeGlobalVersionGateCounts))
	}

	knownAnchors := runtimeGlobalVersionCoverageAnchorNames(t)
	for file := range expectedRuntimeGlobalVersionGateCounts {
		anchors, ok := expectedRuntimeGlobalVersionGateCrossEmulatorAnchors[file]
		if !ok {
			t.Fatalf("runtime global-version gate file %s has no cross-emulator anchors", file)
		}
		if len(anchors) == 0 {
			t.Fatalf("runtime global-version gate file %s has an empty cross-emulator anchor list", file)
		}
		for _, anchor := range anchors {
			if !strings.HasPrefix(anchor, "Fuzz") {
				t.Fatalf("runtime global-version gate file %s anchor %s is not a fuzz anchor", file, anchor)
			}
			if !strings.Contains(anchor, "CrossEmulator") {
				t.Fatalf("runtime global-version gate file %s anchor %s is not a cross-emulator anchor", file, anchor)
			}
			if _, ok = knownAnchors[anchor]; !ok {
				t.Fatalf("runtime global-version gate file %s lost cross-emulator anchor %s", file, anchor)
			}
		}
	}
	for file := range expectedRuntimeGlobalVersionGateCrossEmulatorAnchors {
		if _, ok := expectedRuntimeGlobalVersionGateCounts[file]; !ok {
			t.Fatalf("cross-emulator anchor file %s does not match a runtime global-version gate file", file)
		}
	}
}

func TestTVMTransactionGateFilesHaveLocalAnchors(t *testing.T) {
	transactionGlobalVersionGateFilesHaveAnchors(t, "local", expectedTransactionGlobalVersionGateLocalAnchors, func(file, anchor string) {
		if !strings.HasPrefix(anchor, "FuzzTransaction") {
			t.Fatalf("transaction global-version gate file %s local anchor %s is not transaction-local", file, anchor)
		}
	})
}

func TestTVMTransactionGateFilesHaveCrossEmulatorAnchors(t *testing.T) {
	transactionGlobalVersionGateFilesHaveAnchors(t, "cross-emulator", expectedTransactionGlobalVersionGateCrossEmulatorAnchors, func(file, anchor string) {
		if !strings.HasPrefix(anchor, "FuzzTVMCrossEmulatorTransaction") {
			t.Fatalf("transaction global-version gate file %s cross-emulator anchor %s is not transaction-specific", file, anchor)
		}
	})
}

func TestTVMTransactionGateCrossEmulatorAnchorsMatchRuntimeAnchors(t *testing.T) {
	for file := range expectedTransactionGlobalVersionGateCounts {
		got := append([]string(nil), expectedTransactionGlobalVersionGateCrossEmulatorAnchors[file]...)
		want := append([]string(nil), expectedRuntimeGlobalVersionGateCrossEmulatorAnchors[file]...)
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("transaction global-version gate file %s cross-emulator anchors drifted from runtime anchors:\ngot:\n%s\nwant:\n%s", file, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

func transactionGlobalVersionGateFilesHaveAnchors(t *testing.T, kind string, anchorsByFile map[string][]string, checkAnchor func(file, anchor string)) {
	t.Helper()

	if len(anchorsByFile) != len(expectedTransactionGlobalVersionGateCounts) {
		t.Fatalf("transaction global-version %s anchor file count = %d, want %d", kind, len(anchorsByFile), len(expectedTransactionGlobalVersionGateCounts))
	}

	knownAnchors := runtimeGlobalVersionCoverageAnchorNames(t)
	for file := range expectedTransactionGlobalVersionGateCounts {
		anchors, ok := anchorsByFile[file]
		if !ok {
			t.Fatalf("transaction global-version gate file %s has no %s anchors", file, kind)
		}
		if len(anchors) == 0 {
			t.Fatalf("transaction global-version gate file %s has an empty %s anchor list", file, kind)
		}
		for _, anchor := range anchors {
			if !strings.HasPrefix(anchor, "Fuzz") {
				t.Fatalf("transaction global-version gate file %s %s anchor %s is not a fuzz anchor", file, kind, anchor)
			}
			checkAnchor(file, anchor)
			if _, ok = knownAnchors[anchor]; !ok {
				t.Fatalf("transaction global-version gate file %s lost %s anchor %s", file, kind, anchor)
			}
		}
	}
	for file := range anchorsByFile {
		if _, ok := expectedTransactionGlobalVersionGateCounts[file]; !ok {
			t.Fatalf("transaction global-version %s anchor file %s does not match a transaction gate file", kind, file)
		}
	}
}

func runtimeGlobalVersionGatesFromSource(t *testing.T) []runtimeGlobalVersionGate {
	t.Helper()

	var gates []runtimeGlobalVersionGate
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch path {
			case ".git":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			expr, ok := node.(*ast.BinaryExpr)
			if !ok || !runtimeGlobalVersionGateOperator(expr.Op) {
				return true
			}

			threshold, ok := runtimeGlobalVersionGateThreshold(expr.X, expr.Y)
			if !ok {
				return true
			}
			pos := fset.Position(expr.Pos())
			gates = append(gates, runtimeGlobalVersionGate{
				file:      filepath.ToSlash(path),
				line:      pos.Line,
				column:    pos.Column,
				threshold: threshold,
			})
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("failed to scan runtime global-version gates: %v", err)
	}
	if len(gates) == 0 {
		t.Fatal("runtime global-version gate inventory is empty")
	}

	return gates
}

func runtimeGlobalVersionCoverageAnchorNames(t *testing.T) map[string]struct{} {
	t.Helper()

	out := make(map[string]struct{})
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "Test") || strings.HasPrefix(fn.Name.Name, "Fuzz") {
				out[fn.Name.Name] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan runtime global-version coverage anchors: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("runtime global-version coverage anchor scan found no tests")
	}
	return out
}

type runtimeGlobalVersionCoverageAnchorFunction struct {
	path string
	fn   *ast.FuncDecl
}

func runtimeGlobalVersionCoverageAnchorFunctions(t *testing.T) map[string]runtimeGlobalVersionCoverageAnchorFunction {
	t.Helper()

	out := make(map[string]runtimeGlobalVersionCoverageAnchorFunction)
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || (!strings.HasPrefix(fn.Name.Name, "Test") && !strings.HasPrefix(fn.Name.Name, "Fuzz")) {
				continue
			}
			if prev, ok := out[fn.Name.Name]; ok {
				t.Fatalf("duplicate coverage anchor function %s in %s and %s", fn.Name.Name, prev.path, filepath.ToSlash(path))
			}
			out[fn.Name.Name] = runtimeGlobalVersionCoverageAnchorFunction{
				path: filepath.ToSlash(path),
				fn:   fn,
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan runtime global-version coverage anchor functions: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("runtime global-version coverage anchor function scan found no tests")
	}
	return out
}

func runtimeGlobalVersionCoverageAnchorSeedsVersions(fn *ast.FuncDecl) bool {
	if runtimeGlobalVersionCoverageAnchorSeedsFullRange(fn) {
		return true
	}
	if len(transactionTargetedBoundaryVersions(fn)) > 0 {
		return true
	}
	if seeds := supportedRangeVersionFuzzerSeedCoverage(fn); seeds.min && seeds.max {
		return true
	}
	return runtimeGlobalVersionCoverageAnchorSeedsDefaultRange(fn)
}

func runtimeGlobalVersionCoverageAnchorSeedsFullRange(fn *ast.FuncDecl) bool {
	if packageLocalVersionFuzzerSeedsDefaultRange(fn) {
		return true
	}
	if transactionVersionFuzzerCoversSupportedRange(fn) {
		return true
	}
	if supportedRangeVersionFuzzerSeedCoverage(fn).fullRange {
		return true
	}
	return runtimeGlobalVersionCoverageAnchorSeedsDefaultRange(fn)
}

func runtimeGlobalVersionCoverageAnchorTargetedVersions(fn *ast.FuncDecl) map[int]struct{} {
	versions := transactionTargetedBoundaryVersions(fn)
	if versions == nil {
		versions = make(map[int]struct{})
	}

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !packageLocalFuzzAddCall(call.Fun) || len(call.Args) == 0 {
			return true
		}
		if version, ok := knownReferenceMismatchVersionLiteral(call.Args[0]); ok {
			versions[version] = struct{}{}
		}
		return true
	})
	return versions
}

func runtimeGlobalVersionCoverageVersionsHaveAdjacentPair(versions map[int]struct{}) bool {
	for version := range versions {
		if _, ok := versions[version-1]; ok {
			return true
		}
		if _, ok := versions[version+1]; ok {
			return true
		}
	}
	return false
}

func runtimeGlobalVersionCoverageVersionSetString(versions map[int]struct{}) string {
	if len(versions) == 0 {
		return "<none>"
	}

	items := make([]int, 0, len(versions))
	for version := range versions {
		items = append(items, version)
	}
	sort.Ints(items)

	out := make([]string, 0, len(items))
	for _, version := range items {
		out = append(out, fmt.Sprintf("v%d", version))
	}
	return strings.Join(out, ",")
}

func runtimeGlobalVersionCoverageAnchorSeedsDefaultRange(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		stmt, ok := node.(*ast.ForStmt)
		if !ok {
			return true
		}
		versionVar := runtimeGlobalVersionDefaultRangeForLoopVar(stmt)
		if versionVar == "" || !packageLocalForBodyAddsFuzzVersionSeed(stmt.Body, versionVar) {
			return true
		}
		found = true
		return false
	})
	return found
}

func runtimeGlobalVersionDefaultRangeForLoopVar(stmt *ast.ForStmt) string {
	init, ok := stmt.Init.(*ast.AssignStmt)
	if !ok || len(init.Lhs) != 1 || len(init.Rhs) != 1 || init.Tok != token.DEFINE {
		return ""
	}
	ident, ok := init.Lhs[0].(*ast.Ident)
	if !ok || !versionFuzzExprContainsSupportedMin(init.Rhs[0]) {
		return ""
	}

	cond, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.LEQ || !packageLocalExprContainsIdent(cond.X, ident.Name) || !runtimeGlobalVersionDefaultRangeMaxExpr(cond.Y) {
		return ""
	}

	post, ok := stmt.Post.(*ast.IncDecStmt)
	if !ok || post.Tok != token.INC || !packageLocalExprContainsIdent(post.X, ident.Name) {
		return ""
	}
	return ident.Name
}

func runtimeGlobalVersionDefaultRangeMaxExpr(expr ast.Expr) bool {
	return versionFuzzExprContainsSupportedMax(expr) ||
		packageLocalExprContainsIdent(expr, "DefaultGlobalVersion")
}

func runtimeGlobalVersionLegacyStateConstructor(expr ast.Expr) bool {
	switch fn := expr.(type) {
	case *ast.Ident:
		return fn.Name == "NewExecutionState"
	case *ast.SelectorExpr:
		return fn.Sel.Name == "NewExecutionState"
	default:
		return false
	}
}

func runtimeGlobalVersionGateCandidatesFromSource(t *testing.T) []runtimeGlobalVersionGate {
	t.Helper()

	var gates []runtimeGlobalVersionGate
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch path {
			case ".git":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			expr, ok := node.(*ast.BinaryExpr)
			if !ok || !runtimeGlobalVersionGateOperator(expr.Op) {
				return true
			}
			if !runtimeGlobalVersionGateCandidateExpr(expr.X) && !runtimeGlobalVersionGateCandidateExpr(expr.Y) {
				return true
			}

			threshold, _ := runtimeGlobalVersionGateThreshold(expr.X, expr.Y)
			pos := fset.Position(expr.Pos())
			gates = append(gates, runtimeGlobalVersionGate{
				file:      filepath.ToSlash(path),
				line:      pos.Line,
				column:    pos.Column,
				threshold: threshold,
			})
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("failed to scan runtime global-version gate candidates: %v", err)
	}
	if len(gates) == 0 {
		t.Fatal("runtime global-version gate candidate inventory is empty")
	}

	return gates
}

func runtimeGlobalVersionGateCandidateExpr(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.Ident:
		switch expr.Name {
		case "globalVersion", "version", "transactionGlobalVersion":
			return true
		default:
			return false
		}
	case *ast.SelectorExpr:
		if expr.Sel.Name == "effectiveGlobalVersion" {
			return true
		}
		if expr.Sel.Name == "GlobalVersion" || expr.Sel.Name == "globalVersion" {
			return true
		}
		if expr.Sel.Name == "Version" {
			if x, ok := expr.X.(*ast.Ident); ok && x.Name == "version" {
				return true
			}
		}
		return runtimeGlobalVersionGateCandidateExpr(expr.X)
	case *ast.CallExpr:
		return runtimeGlobalVersionGateCandidateExpr(expr.Fun)
	case *ast.ParenExpr:
		return runtimeGlobalVersionGateCandidateExpr(expr.X)
	case *ast.UnaryExpr:
		return runtimeGlobalVersionGateCandidateExpr(expr.X)
	default:
		return false
	}
}

func runtimeGlobalVersionGateOperator(op token.Token) bool {
	return op == token.LSS || op == token.GTR || op == token.LEQ || op == token.GEQ || op == token.EQL || op == token.NEQ
}

func runtimeGlobalVersionGateThreshold(left, right ast.Expr) (int, bool) {
	if runtimeGlobalVersionGateExpr(left) {
		return runtimeGlobalVersionGateLiteral(right), true
	}
	if runtimeGlobalVersionGateExpr(right) {
		return runtimeGlobalVersionGateLiteral(left), true
	}
	return -1, false
}

func runtimeGlobalVersionGateExpr(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name == "globalVersion" || expr.Name == "version"
	case *ast.SelectorExpr:
		x, ok := expr.X.(*ast.Ident)
		if !ok {
			return false
		}
		switch expr.Sel.Name {
		case "GlobalVersion":
			return x.Name == "state" || x.Name == "s"
		case "Version":
			return x.Name == "version"
		default:
			return false
		}
	case *ast.CallExpr:
		switch name := expr.Fun.(type) {
		case *ast.Ident:
			return name.Name == "transactionGlobalVersion"
		case *ast.SelectorExpr:
			return name.Sel.Name == "effectiveGlobalVersion" || name.Sel.Name == "globalVersion"
		default:
			return false
		}
	default:
		return false
	}
}

func runtimeGlobalVersionGateLiteral(expr ast.Expr) int {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		if expr.Kind != token.INT {
			return -1
		}
		val, err := strconv.Atoi(expr.Value)
		if err != nil {
			return -1
		}
		return val
	case *ast.Ident:
		switch expr.Name {
		case "MinSupportedGlobalVersion":
			return MinSupportedGlobalVersion
		case "MaxSupportedGlobalVersion":
			return MaxSupportedGlobalVersion
		}
	}
	return -1
}

func runtimeGlobalVersionGateCountsString(counts map[string]int) string {
	files := make([]string, 0, len(counts))
	for file := range counts {
		files = append(files, file)
	}
	sort.Strings(files)

	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, fmt.Sprintf("%s=%d", file, counts[file]))
	}
	return strings.Join(out, ", ")
}

func runtimeGlobalVersionGateThresholdList(thresholds map[int]struct{}) []int {
	out := make([]int, 0, len(thresholds))
	for version := range thresholds {
		out = append(out, version)
	}
	sort.Ints(out)
	return out
}

func runtimeGlobalVersionDynamicGateInventory(t *testing.T) []string {
	t.Helper()

	gates := make([]string, 0)
	for _, gate := range runtimeGlobalVersionGatesFromSource(t) {
		if gate.threshold >= 0 {
			continue
		}
		gates = append(gates, runtimeGlobalVersionGateString(gate))
	}
	if len(gates) == 0 {
		t.Fatal("runtime dynamic global-version gate inventory is empty")
	}
	sort.Strings(gates)
	return gates
}

func runtimeGlobalVersionGateInventoryHash(items []string) string {
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

type runtimeGlobalVersionGateKey struct {
	file   string
	line   int
	column int
}

func runtimeGlobalVersionGateKeyFromGate(gate runtimeGlobalVersionGate) runtimeGlobalVersionGateKey {
	return runtimeGlobalVersionGateKey{
		file:   gate.file,
		line:   gate.line,
		column: gate.column,
	}
}

func runtimeGlobalVersionGateString(gate runtimeGlobalVersionGate) string {
	return fmt.Sprintf("%s:%d:%d threshold=%d", gate.file, gate.line, gate.column, gate.threshold)
}
