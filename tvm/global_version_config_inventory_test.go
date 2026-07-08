package tvm

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type globalVersionConfigCoverageAnchor struct {
	fullRangeFuzz bool
}

type globalVersionConfigCapabilityGateCoverageAnchor struct {
	localFullRangeFuzz string
	crossFullRangeFuzz string
}

var expectedGlobalVersionConfigCoverageAnchors = map[string]globalVersionConfigCoverageAnchor{
	"execution_config_fuzz_test.go:FuzzExecutionConfigGlobalVersionPerRunEntrypoints":                         {fullRangeFuzz: true},
	"execution_config_fuzz_test.go:TestExecutionConfigGlobalVersionValidatesRange":                            {},
	"execution_proof_test.go:FuzzExecuteDetailedWithAccountProofConfigGlobalVersionPerRun":                    {fullRangeFuzz: true},
	"external_message_helpers_test.go:TestMessageExecutionGlobalVersionRequiresConfigRootAndValidates":        {},
	"external_message_helpers_test.go:FuzzMessageExecutionGlobalVersionSelection":                             {fullRangeFuzz: true},
	"transaction_emulation_test.go:TestEmulateTransactionConfigGlobalVersionControlsOpcodeDispatch":           {},
	"transaction_fees_test.go:TestTransactionGlobalVersionFallbackContracts":                                  {},
	"transaction_version_fuzz_test.go:FuzzCheckExternalMessageAcceptedGlobalVersionFallbackAndConfigOverride": {fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzCheckExternalMessageAcceptedGlobalVersionPerRun":                    {fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzMessageEmulationBuildProofGlobalVersionFallbackAndConfigOverride":   {fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzMessageEmulationGlobalVersionFallbackAndConfigOverride":             {fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzMessageEmulationGlobalVersionPerRun":                                {fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzTickTockGlobalVersionFallbackAndConfigOverride":                     {fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzTickTockGlobalVersionPerRun":                                        {fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzTransactionEmulationGlobalVersionFallbackAndConfigOverride":         {fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzTransactionEmulationGlobalVersionPerRun":                            {fullRangeFuzz: true},

	"execution_proof_cross_emulator_test.go:FuzzExecutionProofCrossEmulatorExecutionConfigGlobalVersionOverride":                    {fullRangeFuzz: true},
	"execution_proof_cross_emulator_test.go:FuzzExecutionProofCrossEmulatorExecutionConfigLibrariesGlobalVersionOverride":           {fullRangeFuzz: true},
	"message_version_cross_emulator_test.go:FuzzTVMCrossEmulatorCheckExternalMessageAcceptedGlobalVersionFallbackAndConfigOverride": {fullRangeFuzz: true},
	"message_version_cross_emulator_test.go:FuzzTVMCrossEmulatorCheckExternalMessageAcceptedGlobalVersionOverride":                  {fullRangeFuzz: true},
	"message_version_cross_emulator_test.go:FuzzTVMCrossEmulatorDirectMessageBuildProofGlobalVersionFallbackAndConfigOverride":      {fullRangeFuzz: true},
	"message_version_cross_emulator_test.go:FuzzTVMCrossEmulatorDirectMessageBuildProofGlobalVersionOverride":                       {fullRangeFuzz: true},
	"message_version_cross_emulator_test.go:FuzzTVMCrossEmulatorDirectMessageBuildProofLibrariesGlobalVersionOverride":              {fullRangeFuzz: true},
	"message_version_cross_emulator_test.go:FuzzTVMCrossEmulatorDirectMessageGlobalVersionFallbackAndConfigOverride":                {fullRangeFuzz: true},
	"message_version_cross_emulator_test.go:FuzzTVMCrossEmulatorDirectMessageGlobalVersionOverride":                                 {fullRangeFuzz: true},
	"ticktock_message_cross_emulator_test.go:FuzzTVMCrossEmulatorTickTockBuildProofGlobalVersionOverride":                           {fullRangeFuzz: true},
	"ticktock_message_cross_emulator_test.go:FuzzTVMCrossEmulatorTickTockBuildProofLibrariesGlobalVersionOverride":                  {fullRangeFuzz: true},
	"ticktock_message_cross_emulator_test.go:FuzzTVMCrossEmulatorTickTockGlobalVersionFallbackAndConfigOverride":                    {fullRangeFuzz: true},
	"ticktock_message_cross_emulator_test.go:FuzzTVMCrossEmulatorTickTockGlobalVersionOverride":                                     {fullRangeFuzz: true},
	"transaction_version_cross_emulator_test.go:FuzzTVMCrossEmulatorTransactionBuildProofComputeGlobalVersionOverride":              {fullRangeFuzz: true},
	"transaction_version_cross_emulator_test.go:FuzzTVMCrossEmulatorTransactionBuildProofLibrariesGlobalVersionOverride":            {fullRangeFuzz: true},
	"transaction_version_cross_emulator_test.go:FuzzTVMCrossEmulatorTransactionComputeGlobalVersionFallbackAndConfigOverride":       {fullRangeFuzz: true},
	"transaction_version_cross_emulator_test.go:FuzzTVMCrossEmulatorTransactionComputeGlobalVersionOverride":                        {fullRangeFuzz: true},
	"version_cross_emulator_test.go:FuzzTVMCrossEmulatorExecutionConfigGlobalVersionOverride":                                       {fullRangeFuzz: true},
	"version_cross_emulator_test.go:FuzzTVMCrossEmulatorExecutionConfigLibrariesGlobalVersionOverride":                              {fullRangeFuzz: true},
	"version_cross_emulator_test.go:FuzzTVMCrossEmulatorGetMethodExecutionConfigGlobalVersionOverride":                              {fullRangeFuzz: true},
	"version_cross_emulator_test.go:FuzzTVMCrossEmulatorGetMethodExecutionConfigLibrariesGlobalVersionOverride":                     {fullRangeFuzz: true},
}

var expectedGlobalVersionConfigProductionReaders = map[string]int{
	"cross_emulator_transaction_reference.go:referenceTransactionConfigRootWithGlobalVersion": 1,
	"transaction_config.go:PrepareConfig":                                                     1,
}

var expectedGlobalVersionConfigCapabilityGates = map[string]int{
	"transaction_bounce.go:transactionBuildBounceBody:4": 1,
}

var expectedGlobalVersionConfigCapabilityGateCoverageAnchors = map[string]globalVersionConfigCapabilityGateCoverageAnchor{
	"transaction_bounce.go:transactionBuildBounceBody:4": {
		localFullRangeFuzz: "transaction_version_fuzz_test.go:FuzzTransactionVersionedDetailedBounceBodyPhaseExit",
		crossFullRangeFuzz: "transaction_version_cross_emulator_test.go:FuzzTVMCrossEmulatorTransactionBounceFormatGlobalVersion",
	},
}

func TestTVMGlobalVersionConfigCoverageInventory(t *testing.T) {
	anchors := globalVersionConfigCoverageAnchors(t)
	if len(anchors) != len(expectedGlobalVersionConfigCoverageAnchors) {
		t.Fatalf("global-version config coverage anchor count = %d, want %d; got:\n%s", len(anchors), len(expectedGlobalVersionConfigCoverageAnchors), globalVersionConfigCoverageAnchorList(anchors))
	}

	for key, want := range expectedGlobalVersionConfigCoverageAnchors {
		fn, ok := anchors[key]
		if !ok {
			t.Fatalf("missing global-version config coverage anchor %s; got:\n%s", key, globalVersionConfigCoverageAnchorList(anchors))
		}
		if !strings.Contains(fn.Name.Name, "GlobalVersion") {
			t.Fatalf("global-version config coverage anchor %s should be explicit in its name", key)
		}
		if want.fullRangeFuzz {
			if !strings.HasPrefix(fn.Name.Name, "Fuzz") {
				t.Fatalf("global-version config coverage anchor %s is marked full-range but is not a fuzzer", key)
			}
			if !versionFuzzFunctionUsesSupportedRangeMapper(fn) {
				t.Fatalf("global-version config coverage anchor %s does not use a supported global-version mapper", key)
			}
			if seeds := supportedRangeVersionFuzzerSeedCoverage(fn); !seeds.fullRange {
				t.Fatalf("global-version config coverage anchor %s does not seed the full supported global-version range", key)
			}
		}
	}

	var unexpectedExplicitSetUsers []string
	for key := range globalVersionConfigExplicitSetUsers(t) {
		if _, ok := expectedGlobalVersionConfigCoverageAnchors[key]; !ok {
			unexpectedExplicitSetUsers = append(unexpectedExplicitSetUsers, key)
		}
	}
	if len(unexpectedExplicitSetUsers) > 0 {
		sort.Strings(unexpectedExplicitSetUsers)
		t.Fatalf("global-version config tests using GlobalVersionSet without explicit inventory:\n%s", strings.Join(unexpectedExplicitSetUsers, "\n"))
	}

	var missingExplicitSet []string
	for key := range globalVersionConfigRuntimeOverrideUsersWithoutSet(t) {
		missingExplicitSet = append(missingExplicitSet, key)
	}
	if len(missingExplicitSet) > 0 {
		sort.Strings(missingExplicitSet)
		t.Fatalf("global-version runtime config overrides without GlobalVersionSet:\n%s", strings.Join(missingExplicitSet, "\n"))
	}

	var missingExplicitVersion []string
	for key := range globalVersionConfigRuntimeSetUsersWithoutOverride(t) {
		missingExplicitVersion = append(missingExplicitVersion, key)
	}
	if len(missingExplicitVersion) > 0 {
		sort.Strings(missingExplicitVersion)
		t.Fatalf("global-version runtime config GlobalVersionSet=true without explicit GlobalVersion:\n%s", strings.Join(missingExplicitVersion, "\n"))
	}
}

func TestTVMGlobalVersionConfigProductionReaderInventory(t *testing.T) {
	readers := globalVersionConfigProductionReaders(t)
	if len(readers) != len(expectedGlobalVersionConfigProductionReaders) {
		t.Fatalf("global-version production reader count = %d, want %d; got:\n%s", len(readers), len(expectedGlobalVersionConfigProductionReaders), globalVersionConfigProductionReaderList(readers))
	}
	for key, want := range expectedGlobalVersionConfigProductionReaders {
		if got, ok := readers[key]; !ok {
			t.Fatalf("missing global-version production reader %s; got:\n%s", key, globalVersionConfigProductionReaderList(readers))
		} else if got != want {
			t.Fatalf("global-version production reader %s call count = %d, want %d", key, got, want)
		}
	}
	for key := range readers {
		if _, ok := expectedGlobalVersionConfigProductionReaders[key]; !ok {
			t.Fatalf("unexpected global-version production reader %s; got:\n%s", key, globalVersionConfigProductionReaderList(readers))
		}
	}

	fn := globalVersionConfigProductionReaderFunction(t, "transaction_config.go:PrepareConfig")
	if !globalVersionConfigFunctionCalls(fn, "validateGlobalVersion") {
		t.Fatal("PrepareConfig must validate the config global version eagerly")
	}
}

func TestTVMGlobalVersionConfigCapabilityGateInventory(t *testing.T) {
	gates := globalVersionConfigCapabilityGates(t)
	if len(gates) != len(expectedGlobalVersionConfigCapabilityGates) {
		t.Fatalf("global-version capability gate count = %d, want %d; got:\n%s", len(gates), len(expectedGlobalVersionConfigCapabilityGates), globalVersionConfigCapabilityGateList(gates))
	}
	for key, want := range expectedGlobalVersionConfigCapabilityGates {
		if got, ok := gates[key]; !ok {
			t.Fatalf("missing global-version capability gate %s; got:\n%s", key, globalVersionConfigCapabilityGateList(gates))
		} else if got != want {
			t.Fatalf("global-version capability gate %s call count = %d, want %d", key, got, want)
		}
	}
	for key := range gates {
		if _, ok := expectedGlobalVersionConfigCapabilityGates[key]; !ok {
			t.Fatalf("unexpected global-version capability gate %s; got:\n%s", key, globalVersionConfigCapabilityGateList(gates))
		}
	}
}

func TestTVMGlobalVersionConfigCapabilityGatesHaveCoverageAnchors(t *testing.T) {
	gates := globalVersionConfigCapabilityGates(t)
	if len(expectedGlobalVersionConfigCapabilityGateCoverageAnchors) != len(gates) {
		t.Fatalf("capability gate coverage anchor count = %d, want %d", len(expectedGlobalVersionConfigCapabilityGateCoverageAnchors), len(gates))
	}
	for gate := range gates {
		anchors, ok := expectedGlobalVersionConfigCapabilityGateCoverageAnchors[gate]
		if !ok {
			t.Fatalf("capability gate %s has no coverage anchors", gate)
		}
		local := globalVersionConfigTestFunction(t, anchors.localFullRangeFuzz)
		if !supportedRangeVersionFuzzerSeedCoverage(local).fullRange {
			t.Fatalf("capability gate %s local anchor %s does not seed full supported global-version range", gate, anchors.localFullRangeFuzz)
		}
		if !globalVersionConfigFunctionCalls(local, "transactionTestConfigWithGlobalVersionAndCapabilities") ||
			!globalVersionConfigFunctionCalls(local, "transactionFuzzCheckLegacyBounceBody") {
			t.Fatalf("capability gate %s local anchor %s does not exercise legacy capability body path", gate, anchors.localFullRangeFuzz)
		}

		cross := globalVersionConfigTestFunction(t, anchors.crossFullRangeFuzz)
		if !supportedRangeVersionFuzzerSeedCoverage(cross).fullRange {
			t.Fatalf("capability gate %s cross anchor %s does not seed full supported global-version range", gate, anchors.crossFullRangeFuzz)
		}
		if !globalVersionConfigFunctionCalls(cross, "assertTransactionBounceFormatVersionParity") {
			t.Fatalf("capability gate %s cross anchor %s does not call bounce parity assertion helper", gate, anchors.crossFullRangeFuzz)
		}
		crossAssert := globalVersionConfigTestFunction(t, "transaction_version_cross_emulator_test.go:assertTransactionBounceFormatVersionParity")
		if !globalVersionConfigFunctionCalls(crossAssert, "referenceTransactionConfigRootWithGlobalVersionAndCapabilities") {
			t.Fatalf("capability gate %s cross assertion helper does not build explicit capability config root", gate)
		}
	}
	for gate := range expectedGlobalVersionConfigCapabilityGateCoverageAnchors {
		if _, ok := gates[gate]; !ok {
			t.Fatalf("capability gate coverage anchor tracks missing gate %s", gate)
		}
	}
}

func TestTVMDifferentialFuzzCaseGlobalVersionLiteralsAreExplicit(t *testing.T) {
	missing := differentialFuzzCaseGlobalVersionLiteralsWithoutSet(t)
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("differential fuzz cases with GlobalVersion but without explicit GlobalVersionSet:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMGlobalVersionConfigScannerCoversHelperFunctions(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "helpers_test.go", `
package tvm

func runtimeConfigHelperMissingSet() {
	_ = ExecutionConfig{GlobalVersion: 0}
}

func runtimeConfigHelperComplete() {
	_ = ExecutionConfig{GlobalVersion: 0, GlobalVersionSet: true}
}

func runtimeConfigHelperSetOnly() {
	_ = ExecutionConfig{GlobalVersionSet: true}
}
`, 0)
	if err != nil {
		t.Fatalf("parse helper scanner fixture: %v", err)
	}

	funcs := make(map[string]*ast.FuncDecl)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok {
			funcs[fn.Name.Name] = fn
		}
	}

	if fn := funcs["runtimeConfigHelperMissingSet"]; !globalVersionConfigFunctionUsesRuntimeOverride(fn) || globalVersionConfigFunctionUsesExplicitSet(fn) {
		t.Fatalf("missing-set helper not classified as runtime override without explicit set")
	}
	if fn := funcs["runtimeConfigHelperComplete"]; !globalVersionConfigFunctionUsesRuntimeOverride(fn) || !globalVersionConfigFunctionUsesExplicitSet(fn) {
		t.Fatalf("complete helper not classified as runtime override with explicit set")
	}
	if fn := funcs["runtimeConfigHelperSetOnly"]; !globalVersionConfigFunctionSetsRuntimeOverrideFlag(fn) || globalVersionConfigFunctionUsesRuntimeOverride(fn) {
		t.Fatalf("set-only helper not classified as explicit set without runtime override")
	}

	file, err = parser.ParseFile(token.NewFileSet(), "helpers_test.go", `
package tvm

func runtimeConfigHelperMismatchedVars() {
	cfg := ExecutionConfig{}
	cfg.GlobalVersion = 7

	other := ExecutionConfig{}
	other.GlobalVersionSet = true
}
`, 0)
	if err != nil {
		t.Fatalf("parse helper mismatch scanner fixture: %v", err)
	}
	funcs = make(map[string]*ast.FuncDecl)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok {
			funcs[fn.Name.Name] = fn
		}
	}

	fn := funcs["runtimeConfigHelperMismatchedVars"]
	if !globalVersionConfigFunctionHasRuntimeOverrideWithoutSet(fn) {
		t.Fatalf("mismatched helper did not report GlobalVersion without paired GlobalVersionSet")
	}
	if !globalVersionConfigFunctionHasRuntimeSetWithoutOverride(fn) {
		t.Fatalf("mismatched helper did not report GlobalVersionSet without paired GlobalVersion")
	}
}

func TestTVMGlobalVersionConfigCapabilityGateScannerCoversLiteralAndDynamicArgs(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "capability_fixture.go", `
package tvm

func capabilityGateFixture(cfg tlb.BlockchainConfig, capability uint64) {
	_ = cfg.hasCapability(4)
	_ = cfg.hasCapability(capability)
}
`, 0)
	if err != nil {
		t.Fatalf("parse capability scanner fixture: %v", err)
	}

	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if parsed, ok := decl.(*ast.FuncDecl); ok {
			fn = parsed
			break
		}
	}
	if fn == nil {
		t.Fatal("capability scanner fixture has no function")
	}

	gates := globalVersionConfigCapabilityGatesInFunc(fn)
	if gates["4"] != 1 || gates["<dynamic>"] != 1 || len(gates) != 2 {
		t.Fatalf("capability scanner fixture gates = %s, want literal 4 and dynamic", globalVersionConfigCapabilityGateList(gates))
	}
}

func globalVersionConfigCoverageAnchors(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()

	anchors := make(map[string]*ast.FuncDecl)
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata", "vm/cross-emulate-test":
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
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			key := filepath.ToSlash(path) + ":" + fn.Name.Name
			if _, ok = expectedGlobalVersionConfigCoverageAnchors[key]; ok {
				anchors[key] = fn
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan global-version config coverage anchors: %v", err)
	}
	return anchors
}

func differentialFuzzCaseGlobalVersionLiteralsWithoutSet(t *testing.T) []string {
	t.Helper()

	var missing []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata", "vm/cross-emulate-test":
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
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.CompositeLit)
			if !ok || !globalVersionConfigDifferentialFuzzCaseType(lit.Type) {
				return true
			}
			if !globalVersionConfigCompositeLitHasKey(lit, "globalVersion") {
				return true
			}
			if globalVersionConfigCompositeLitHasKey(lit, "globalVersionSet") && !globalVersionConfigCompositeLitHasFalseKey(lit, "globalVersionSet") {
				return true
			}

			pos := fset.Position(lit.Pos())
			missing = append(missing, fmt.Sprintf("%s:%d", filepath.ToSlash(path), pos.Line))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan differential fuzz global-version literals: %v", err)
	}
	return missing
}

func globalVersionConfigCoverageAnchorList(anchors map[string]*ast.FuncDecl) string {
	items := make([]string, 0, len(anchors))
	for key := range anchors {
		items = append(items, key)
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}

func globalVersionConfigProductionReaders(t *testing.T) map[string]int {
	t.Helper()

	readers := make(map[string]int)
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata", "vm/cross-emulate-test":
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
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			calls := globalVersionConfigFunctionCallCount(fn, "GetGlobalVersion")
			if calls == 0 {
				continue
			}
			readers[filepath.ToSlash(path)+":"+fn.Name.Name] = calls
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production global-version readers: %v", err)
	}
	if len(readers) == 0 {
		t.Fatal("production global-version reader inventory is empty")
	}
	return readers
}

func globalVersionConfigProductionReaderFunction(t *testing.T, key string) *ast.FuncDecl {
	t.Helper()

	fset := token.NewFileSet()
	var found *ast.FuncDecl
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata", "vm/cross-emulate-test":
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
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if filepath.ToSlash(path)+":"+fn.Name.Name == key {
				found = fn
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production global-version reader %s: %v", key, err)
	}
	if found == nil {
		t.Fatalf("missing production global-version reader function %s", key)
	}
	return found
}

func globalVersionConfigTestFunction(t *testing.T, key string) *ast.FuncDecl {
	t.Helper()

	fset := token.NewFileSet()
	var found *ast.FuncDecl
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata", "vm/cross-emulate-test":
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
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if filepath.ToSlash(path)+":"+fn.Name.Name == key {
				found = fn
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan test function %s: %v", key, err)
	}
	if found == nil {
		t.Fatalf("missing test function %s", key)
	}
	return found
}

func globalVersionConfigProductionReaderList(readers map[string]int) string {
	items := make([]string, 0, len(readers))
	for key, calls := range readers {
		items = append(items, fmt.Sprintf("%s:%d", key, calls))
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}

func globalVersionConfigCapabilityGates(t *testing.T) map[string]int {
	t.Helper()

	gates := make(map[string]int)
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata", "vm/cross-emulate-test":
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
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			for capability, count := range globalVersionConfigCapabilityGatesInFunc(fn) {
				gates[filepath.ToSlash(path)+":"+fn.Name.Name+":"+capability] += count
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production global-version capability gates: %v", err)
	}
	if len(gates) == 0 {
		t.Fatal("production global-version capability gate inventory is empty")
	}
	return gates
}

func globalVersionConfigCapabilityGatesInFunc(fn *ast.FuncDecl) map[string]int {
	gates := make(map[string]int)
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := globalVersionConfigCallName(call.Fun)
		if name != "transactionHasCapability" && name != "hasCapability" {
			return true
		}
		gates[globalVersionConfigCapabilityGateArg(call, name == "hasCapability")]++
		return true
	})
	return gates
}

func globalVersionConfigCapabilityGateArg(call *ast.CallExpr, methodCall bool) string {
	argIdx := 1
	if methodCall {
		argIdx = 0
	}
	if len(call.Args) <= argIdx {
		return "<dynamic>"
	}
	lit, ok := call.Args[argIdx].(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return "<dynamic>"
	}
	capability, err := strconv.ParseUint(lit.Value, 0, 64)
	if err != nil {
		return "<dynamic>"
	}
	return strconv.FormatUint(capability, 10)
}

func globalVersionConfigCapabilityGateList(gates map[string]int) string {
	items := make([]string, 0, len(gates))
	for key, calls := range gates {
		items = append(items, fmt.Sprintf("%s:%d", key, calls))
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}

func globalVersionConfigFunctionCallCount(fn *ast.FuncDecl, name string) int {
	count := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && globalVersionConfigCallName(call.Fun) == name {
			count++
		}
		return true
	})
	return count
}

func globalVersionConfigFunctionCalls(fn *ast.FuncDecl, name string) bool {
	return globalVersionConfigFunctionCallCount(fn, name) > 0
}

func globalVersionConfigCallName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		return expr.Sel.Name
	default:
		return ""
	}
}

func globalVersionConfigExplicitSetUsers(t *testing.T) map[string]struct{} {
	t.Helper()

	users := make(map[string]struct{})
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata", "vm/cross-emulate-test":
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
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || (!strings.HasPrefix(fn.Name.Name, "Test") && !strings.HasPrefix(fn.Name.Name, "Fuzz")) {
				continue
			}
			if globalVersionConfigFunctionUsesExplicitSet(fn) {
				users[filepath.ToSlash(path)+":"+fn.Name.Name] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan GlobalVersionSet users: %v", err)
	}
	return users
}

func globalVersionConfigFunctionUsesExplicitSet(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.Ident:
			if node.Name == "GlobalVersionSet" {
				found = true
				return false
			}
		case *ast.SelectorExpr:
			if node.Sel.Name == "GlobalVersionSet" {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func globalVersionConfigRuntimeOverrideUsersWithoutSet(t *testing.T) map[string]struct{} {
	t.Helper()

	users := make(map[string]struct{})
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata", "vm/cross-emulate-test":
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
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if globalVersionConfigFunctionHasRuntimeOverrideWithoutSet(fn) {
				users[filepath.ToSlash(path)+":"+fn.Name.Name] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan runtime GlobalVersion override users: %v", err)
	}
	return users
}

func globalVersionConfigRuntimeSetUsersWithoutOverride(t *testing.T) map[string]struct{} {
	t.Helper()

	users := make(map[string]struct{})
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata", "vm/cross-emulate-test":
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
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if globalVersionConfigFunctionHasRuntimeSetWithoutOverride(fn) {
				users[filepath.ToSlash(path)+":"+fn.Name.Name] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan runtime GlobalVersionSet users: %v", err)
	}
	return users
}

func globalVersionConfigFunctionUsesRuntimeOverride(fn *ast.FuncDecl) bool {
	runtimeConfigVars := make(map[string]struct{})
	found := false

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.ValueSpec:
			for i, name := range node.Names {
				if i < len(node.Values) && globalVersionConfigRuntimeConfigExpr(node.Values[i], runtimeConfigVars) {
					runtimeConfigVars[name.Name] = struct{}{}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				if globalVersionConfigRuntimeConfigGlobalVersionSelector(lhs, runtimeConfigVars) {
					found = true
					continue
				}
				if i >= len(node.Rhs) {
					continue
				}
				ident, ok := lhs.(*ast.Ident)
				if ok && globalVersionConfigRuntimeConfigExpr(node.Rhs[i], runtimeConfigVars) {
					runtimeConfigVars[ident.Name] = struct{}{}
				}
			}
		case *ast.CompositeLit:
			if globalVersionConfigRuntimeConfigType(node.Type) && globalVersionConfigCompositeLitHasKey(node, "GlobalVersion") {
				found = true
			}
		}
		return true
	})
	return found
}

func globalVersionConfigFunctionSetsRuntimeOverrideFlag(fn *ast.FuncDecl) bool {
	runtimeConfigVars := make(map[string]struct{})
	found := false

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.ValueSpec:
			for i, name := range node.Names {
				if i < len(node.Values) && globalVersionConfigRuntimeConfigExpr(node.Values[i], runtimeConfigVars) {
					runtimeConfigVars[name.Name] = struct{}{}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				if globalVersionConfigRuntimeConfigGlobalVersionSetSelector(lhs, runtimeConfigVars) && globalVersionConfigRHSIsTrue(node.Rhs, i) {
					found = true
					continue
				}
				if i >= len(node.Rhs) {
					continue
				}
				ident, ok := lhs.(*ast.Ident)
				if ok && globalVersionConfigRuntimeConfigExpr(node.Rhs[i], runtimeConfigVars) {
					runtimeConfigVars[ident.Name] = struct{}{}
				}
			}
		case *ast.CompositeLit:
			if globalVersionConfigRuntimeConfigType(node.Type) && globalVersionConfigCompositeLitHasTrueKey(node, "GlobalVersionSet") {
				found = true
			}
		}
		return true
	})
	return found
}

type globalVersionConfigRuntimePairState struct {
	override bool
	set      bool
}

func globalVersionConfigFunctionHasRuntimeOverrideWithoutSet(fn *ast.FuncDecl) bool {
	withoutSet, _ := globalVersionConfigRuntimePairIssues(fn)
	return withoutSet
}

func globalVersionConfigFunctionHasRuntimeSetWithoutOverride(fn *ast.FuncDecl) bool {
	_, withoutOverride := globalVersionConfigRuntimePairIssues(fn)
	return withoutOverride
}

func globalVersionConfigRuntimePairIssues(fn *ast.FuncDecl) (withoutSet bool, withoutOverride bool) {
	if fn == nil || fn.Body == nil {
		return false, false
	}

	runtimeConfigVars := make(map[string]globalVersionConfigRuntimePairState)
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.ValueSpec:
			for i, name := range node.Names {
				state, ok := globalVersionConfigRuntimePairStateFromValueSpec(node, i, runtimeConfigVars)
				if ok {
					runtimeConfigVars[name.Name] = state
				}
			}
		case *ast.AssignStmt:
			globalVersionConfigTrackRuntimePairAssignment(node, runtimeConfigVars)
		case *ast.CompositeLit:
			if !globalVersionConfigRuntimeConfigType(node.Type) {
				return true
			}
			state := globalVersionConfigRuntimePairStateFromComposite(node)
			if state.override && !state.set {
				withoutSet = true
			}
			if state.set && !state.override {
				withoutOverride = true
			}
		}
		return true
	})

	for _, state := range runtimeConfigVars {
		if state.override && !state.set {
			withoutSet = true
		}
		if state.set && !state.override {
			withoutOverride = true
		}
	}
	return withoutSet, withoutOverride
}

func globalVersionConfigRuntimePairStateFromValueSpec(spec *ast.ValueSpec, idx int, known map[string]globalVersionConfigRuntimePairState) (globalVersionConfigRuntimePairState, bool) {
	if idx < len(spec.Values) {
		if state, ok := globalVersionConfigRuntimePairStateFromExpr(spec.Values[idx], known); ok {
			return state, true
		}
	}
	if globalVersionConfigRuntimeConfigType(spec.Type) {
		return globalVersionConfigRuntimePairState{}, true
	}
	return globalVersionConfigRuntimePairState{}, false
}

func globalVersionConfigTrackRuntimePairAssignment(assign *ast.AssignStmt, known map[string]globalVersionConfigRuntimePairState) {
	for i, lhs := range assign.Lhs {
		if ident, ok := lhs.(*ast.Ident); ok && i < len(assign.Rhs) {
			if state, ok := globalVersionConfigRuntimePairStateFromExpr(assign.Rhs[i], known); ok {
				known[ident.Name] = state
				continue
			}
		}

		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			continue
		}

		state, ok := known[ident.Name]
		if !ok {
			continue
		}
		switch sel.Sel.Name {
		case "GlobalVersion":
			state.override = true
			known[ident.Name] = state
		case "GlobalVersionSet":
			if globalVersionConfigRHSIsTrue(assign.Rhs, i) {
				state.set = true
				known[ident.Name] = state
			}
		}
	}
}

func globalVersionConfigRuntimePairStateFromExpr(expr ast.Expr, known map[string]globalVersionConfigRuntimePairState) (globalVersionConfigRuntimePairState, bool) {
	switch expr := expr.(type) {
	case *ast.CompositeLit:
		if !globalVersionConfigRuntimeConfigType(expr.Type) {
			return globalVersionConfigRuntimePairState{}, false
		}
		return globalVersionConfigRuntimePairStateFromComposite(expr), true
	case *ast.Ident:
		state, ok := known[expr.Name]
		return state, ok
	case *ast.UnaryExpr:
		return globalVersionConfigRuntimePairStateFromExpr(expr.X, known)
	default:
		return globalVersionConfigRuntimePairState{}, false
	}
}

func globalVersionConfigRuntimePairStateFromComposite(lit *ast.CompositeLit) globalVersionConfigRuntimePairState {
	return globalVersionConfigRuntimePairState{
		override: globalVersionConfigCompositeLitHasKey(lit, "GlobalVersion"),
		set:      globalVersionConfigCompositeLitHasTrueKey(lit, "GlobalVersionSet"),
	}
}

func globalVersionConfigRuntimeConfigExpr(expr ast.Expr, known map[string]struct{}) bool {
	switch expr := expr.(type) {
	case *ast.CompositeLit:
		return globalVersionConfigRuntimeConfigType(expr.Type)
	case *ast.Ident:
		_, ok := known[expr.Name]
		return ok
	case *ast.UnaryExpr:
		return globalVersionConfigRuntimeConfigExpr(expr.X, known)
	default:
		return false
	}
}

func globalVersionConfigRuntimeConfigType(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.Ident:
		return globalVersionConfigRuntimeConfigTypeName(expr.Name)
	case *ast.SelectorExpr:
		return globalVersionConfigRuntimeConfigTypeName(expr.Sel.Name)
	default:
		return false
	}
}

func globalVersionConfigRuntimeConfigTypeName(name string) bool {
	switch name {
	case "ExecutionConfig",
		"MessageEmulationConfig",
		"EmulateExternalMessageConfig",
		"EmulateInternalMessageConfig",
		"TransactionEmulationConfig",
		"CheckExternalMessageAcceptedConfig":
		return true
	default:
		return false
	}
}

func globalVersionConfigRuntimeConfigGlobalVersionSelector(expr ast.Expr, known map[string]struct{}) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "GlobalVersion" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = known[ident.Name]
	return ok
}

func globalVersionConfigRuntimeConfigGlobalVersionSetSelector(expr ast.Expr, known map[string]struct{}) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "GlobalVersionSet" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = known[ident.Name]
	return ok
}

func globalVersionConfigCompositeLitHasKey(lit *ast.CompositeLit, key string) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := kv.Key.(*ast.Ident)
		if ok && ident.Name == key {
			return true
		}
	}
	return false
}

func globalVersionConfigCompositeLitHasTrueKey(lit *ast.CompositeLit, key string) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := kv.Key.(*ast.Ident)
		if ok && ident.Name == key && globalVersionConfigExprIsTrue(kv.Value) {
			return true
		}
	}
	return false
}

func globalVersionConfigCompositeLitHasFalseKey(lit *ast.CompositeLit, key string) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := kv.Key.(*ast.Ident)
		if ok && ident.Name == key && globalVersionConfigExprIsFalse(kv.Value) {
			return true
		}
	}
	return false
}

func globalVersionConfigDifferentialFuzzCaseType(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name == "differentialFuzzCase"
	case *ast.SelectorExpr:
		return expr.Sel.Name == "differentialFuzzCase"
	default:
		return false
	}
}

func globalVersionConfigRHSIsTrue(exprs []ast.Expr, idx int) bool {
	if idx >= len(exprs) {
		return false
	}
	return globalVersionConfigExprIsTrue(exprs[idx])
}

func globalVersionConfigExprIsTrue(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "true"
}

func globalVersionConfigExprIsFalse(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "false"
}
