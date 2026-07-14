package tvm

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	expectedTransactionVersionFuzzerCount              = 66
	expectedTransactionVersionFuzzerHash               = "358d6cff1d2a28c28d4c58e8f9db1f33bc793713607bf88d674eaf0223f0e2ff"
	expectedTransactionFullRangeVersionFuzzerCount     = 55
	expectedTransactionFullRangeVersionFuzzerHash      = "2ea4005802355c1c9679fee19a9b85c595fc8c76f88aeaaa55682dd50653cd44"
	expectedTransactionTargetedRangeVersionFuzzerCount = 11
	expectedTransactionTargetedRangeVersionFuzzerHash  = "13f956599d5192c4b06b22a60bdcdb8f35ff6e214a729da84d695b3c24833df9"
	expectedTransactionTargetedBoundaryInventoryCount  = 21
	expectedTransactionTargetedBoundaryInventoryHash   = "497d65f953e3fa164e5a43b6b5c6bce30b34ed188cec5041d29aff96027aa7e2"
)

var expectedTransactionTargetedBoundaryVersions = map[string][]int{
	"FuzzMessageEmulationBuildProofLibraryCodeCellStartupV9Boundary":     {8, 9},
	"FuzzTickTockBuildProofLibraryCodeCellStartupV9Boundary":             {8, 9},
	"FuzzTransactionEmulationBuildProofLibraryCodeCellStartupV9Boundary": {8, 9},
	"FuzzTransactionVersionedActionFineStartsAtV4":                       {3, 4},
	"FuzzTransactionVersionedAnycastAccountSerialization":                {8, 9, 10},
	"FuzzTransactionVersionedComputePhaseExitArgAndBits256":              {},
	"FuzzTransactionVersionedCustomFeeLowerBoundStopsAtV8":               {7, 8},
	"FuzzTransactionVersionedInboundExternalValidation":                  {9, 10},
	"FuzzTransactionVersionedNonCanonicalForwardFee":                     {7, 8},
	"FuzzTransactionVersionedPrecompiledGasUsageBoundaries":              {},
	"FuzzTransactionVersionedSendExtraFlagsBoundaries":                   {11, 12},
}

func TestTransactionVersionFuzzerInventory(t *testing.T) {
	fuzzers := transactionVersionFuzzers(t)
	if len(fuzzers) != expectedTransactionVersionFuzzerCount {
		t.Fatalf("transaction version fuzzer count = %d, want %d", len(fuzzers), expectedTransactionVersionFuzzerCount)
	}
	if got := transactionVersionFuzzerInventoryHash(fuzzers); got != expectedTransactionVersionFuzzerHash {
		t.Fatalf("transaction version fuzzer hash = %s, want %s", got, expectedTransactionVersionFuzzerHash)
	}
}

func TestTransactionVersionFuzzerRangeCoverageInventory(t *testing.T) {
	fuzzers := transactionVersionFuzzers(t)

	var fullRange []transactionVersionFuzzer
	var targeted []transactionVersionFuzzer
	for _, fuzz := range fuzzers {
		if transactionVersionFuzzerCoversSupportedRange(fuzz.fn) {
			fullRange = append(fullRange, fuzz)
		} else {
			targeted = append(targeted, fuzz)
		}
	}

	if len(fullRange) != expectedTransactionFullRangeVersionFuzzerCount {
		t.Fatalf("transaction full-range version fuzzer count = %d, want %d:\n%s", len(fullRange), expectedTransactionFullRangeVersionFuzzerCount, transactionVersionFuzzerInventoryList(fullRange))
	}
	if got := transactionVersionFuzzerInventoryHash(fullRange); got != expectedTransactionFullRangeVersionFuzzerHash {
		t.Fatalf("transaction full-range version fuzzer hash = %s, want %s:\n%s", got, expectedTransactionFullRangeVersionFuzzerHash, transactionVersionFuzzerInventoryList(fullRange))
	}
	if len(targeted) != expectedTransactionTargetedRangeVersionFuzzerCount {
		t.Fatalf("transaction targeted-range version fuzzer count = %d, want %d:\n%s", len(targeted), expectedTransactionTargetedRangeVersionFuzzerCount, transactionVersionFuzzerInventoryList(targeted))
	}
	if got := transactionVersionFuzzerInventoryHash(targeted); got != expectedTransactionTargetedRangeVersionFuzzerHash {
		t.Fatalf("transaction targeted-range version fuzzer hash = %s, want %s:\n%s", got, expectedTransactionTargetedRangeVersionFuzzerHash, transactionVersionFuzzerInventoryList(targeted))
	}
}

func TestTransactionTargetedVersionFuzzerBoundaryInventory(t *testing.T) {
	fuzzers := transactionVersionFuzzers(t)
	targeted := transactionTargetedVersionFuzzers(fuzzers)
	if len(targeted) != len(expectedTransactionTargetedBoundaryVersions) {
		t.Fatalf("transaction targeted boundary fuzzer count = %d, want %d:\n%s", len(targeted), len(expectedTransactionTargetedBoundaryVersions), transactionVersionFuzzerInventoryList(targeted))
	}

	inventory := transactionTargetedBoundaryInventory()
	if len(inventory) != expectedTransactionTargetedBoundaryInventoryCount {
		t.Fatalf("transaction targeted boundary inventory count = %d, want %d:\n%s", len(inventory), expectedTransactionTargetedBoundaryInventoryCount, strings.Join(inventory, "\n"))
	}
	if got := transactionTargetedBoundaryInventoryHash(inventory); got != expectedTransactionTargetedBoundaryInventoryHash {
		t.Fatalf("transaction targeted boundary inventory hash = %s, want %s:\n%s", got, expectedTransactionTargetedBoundaryInventoryHash, strings.Join(inventory, "\n"))
	}

	for _, fuzz := range targeted {
		wantVersions, ok := expectedTransactionTargetedBoundaryVersions[fuzz.name]
		if !ok {
			t.Fatalf("transaction targeted fuzzer %s has no expected boundary inventory entry", fuzz.name)
		}
		gotVersions := transactionTargetedBoundaryVersions(fuzz.fn)
		for _, version := range wantVersions {
			if _, ok = gotVersions[version]; !ok {
				t.Fatalf("transaction targeted fuzzer %s does not mention boundary global version %d", fuzz.name, version)
			}
		}
	}
	for name := range expectedTransactionTargetedBoundaryVersions {
		if !transactionVersionFuzzerNamed(targeted, name) {
			t.Fatalf("transaction targeted boundary inventory tracks missing or full-range fuzzer %s", name)
		}
	}
}

func TestTransactionBounceBodyFuzzerCoversLegacyCapabilityPath(t *testing.T) {
	fuzzers := transactionVersionFuzzers(t)
	var fn *ast.FuncDecl
	for _, fuzz := range fuzzers {
		if fuzz.name == "FuzzTransactionVersionedDetailedBounceBodyPhaseExit" {
			fn = fuzz.fn
			break
		}
	}
	if fn == nil {
		t.Fatal("FuzzTransactionVersionedDetailedBounceBodyPhaseExit is missing")
	}

	requiredCalls := map[string]bool{
		"transactionFuzzCheckDetailedBounceBody":                false,
		"transactionFuzzCheckLegacyBounceBody":                  false,
		"transactionTestConfigWithGlobalVersionAndCapabilities": false,
	}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, ok = requiredCalls[transactionVersionCallName(call.Fun)]; ok {
			requiredCalls[transactionVersionCallName(call.Fun)] = true
		}
		return true
	})

	var missing []string
	for name, found := range requiredCalls {
		if !found {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("detailed bounce body fuzzer is missing capability/detailed coverage calls:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTransactionVersionFuzzerScannerCoversAllVersionsRangeLoop(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "transaction_version_fuzz_test.go", `
package tvm

func FuzzTransactionScannerAllVersionsFixture(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0))
		_ = transactionTestConfigWithGlobalVersion(t, version)
	}
}

func FuzzTransactionScannerOtherRangeFixture(f *testing.F) {
	for _, version := range otherVersions {
		f.Add(byte(version), byte(0))
	}
}

func FuzzTransactionScannerAllVersionsNoSeedFixture(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		_ = version
	}
}

func FuzzTransactionScannerAllVersionsWrongSeedArgFixture(f *testing.F) {
	for _, version := range transactionFuzzAllVersions {
		f.Add(byte(version), byte(0))
	}

	f.Fuzz(func(t *testing.T, payload byte, rawVersion byte) {
		_ = transactionFuzzGlobalVersion(rawVersion)
	})
}
`, 0)
	if err != nil {
		t.Fatalf("parse transaction version fuzzer scanner fixture: %v", err)
	}

	funcs := make(map[string]*ast.FuncDecl)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok {
			funcs[fn.Name.Name] = fn
		}
	}
	if !transactionVersionFuzzerRuntimeLoopsAllVersions(funcs["FuzzTransactionScannerAllVersionsFixture"]) {
		t.Fatal("transaction fuzzer scanner did not detect transactionFuzzAllVersions range loop")
	}
	if transactionVersionFuzzerRuntimeLoopsAllVersions(funcs["FuzzTransactionScannerOtherRangeFixture"]) {
		t.Fatal("transaction fuzzer scanner accepted non-transactionFuzzAllVersions range loop")
	}
	if transactionVersionFuzzerRuntimeLoopsAllVersions(funcs["FuzzTransactionScannerAllVersionsNoSeedFixture"]) {
		t.Fatal("transaction fuzzer scanner accepted transactionFuzzAllVersions range loop without f.Add version seed")
	}
	if transactionVersionFuzzerRuntimeLoopsAllVersions(funcs["FuzzTransactionScannerAllVersionsWrongSeedArgFixture"]) {
		t.Fatal("transaction fuzzer scanner accepted transactionFuzzAllVersions seed in non-version fuzz arg")
	}
}

type transactionVersionFuzzer struct {
	path string
	name string
	fn   *ast.FuncDecl
}

func transactionVersionFuzzers(t *testing.T) []transactionVersionFuzzer {
	t.Helper()

	const path = "transaction_version_fuzz_test.go"

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var fuzzers []transactionVersionFuzzer
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Fuzz") {
			continue
		}
		fuzzers = append(fuzzers, transactionVersionFuzzer{
			path: path,
			name: fn.Name.Name,
			fn:   fn,
		})
	}
	if len(fuzzers) == 0 {
		t.Fatal("transaction version fuzzer scan found no fuzzers")
	}
	return fuzzers
}

func transactionVersionFuzzerCoversSupportedRange(fn *ast.FuncDecl) bool {
	if supportedRangeVersionFuzzerSeedCoverage(fn).fullRange {
		return true
	}
	return transactionVersionFuzzerRuntimeLoopsAllVersions(fn)
}

func transactionTargetedVersionFuzzers(fuzzers []transactionVersionFuzzer) []transactionVersionFuzzer {
	var targeted []transactionVersionFuzzer
	for _, fuzz := range fuzzers {
		if !transactionVersionFuzzerCoversSupportedRange(fuzz.fn) {
			targeted = append(targeted, fuzz)
		}
	}
	return targeted
}

func transactionVersionFuzzerRuntimeLoopsAllVersions(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		stmt, ok := node.(*ast.RangeStmt)
		if !ok {
			return true
		}
		versionVar := transactionFuzzAllVersionsRangeLoopVar(stmt)
		if versionVar == "" || !transactionVersionLoopBodyUsesGlobalVersion(stmt.Body, versionVar) {
			return true
		}
		found = true
		return false
	})
	return found
}

func transactionVersionLoopBodyUsesGlobalVersion(body *ast.BlockStmt, versionVar string) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !transactionGlobalVersionConfigCall(call.Fun) {
			return true
		}
		for _, arg := range call.Args {
			if packageLocalExprContainsIdent(arg, versionVar) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func transactionGlobalVersionConfigCall(expr ast.Expr) bool {
	switch transactionVersionCallName(expr) {
	case "transactionFuzzInboundExternalConfig",
		"transactionFuzzGasConfig",
		"transactionFuzzSendExtraFlagsConfig",
		"transactionLoadActions",
		"transactionProcessReserveAction",
		"transactionTestConfigWithGlobalVersion",
		"transactionTestConfigWithGlobalVersionAndCapabilities",
		"transactionTestGlobalVersionCell":
		return true
	default:
		return false
	}
}

func transactionTargetedBoundaryVersions(fn *ast.FuncDecl) map[int]struct{} {
	versions := make(map[int]struct{})
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			if !transactionVersionBoundaryCall(node.Fun) {
				return true
			}
			for _, arg := range node.Args {
				transactionCollectBoundaryVersionLiterals(arg, versions)
			}
		case *ast.KeyValueExpr:
			if key, ok := node.Key.(*ast.Ident); !ok || key.Name != "version" {
				return true
			}
			transactionCollectBoundaryVersionLiterals(node.Value, versions)
		}
		return true
	})
	return versions
}

func transactionVersionBoundaryCall(expr ast.Expr) bool {
	name := transactionVersionCallName(expr)
	switch name {
	case "checkAccountSerializationVersion",
		"executeExecutionProofLibraryStartupTarget",
		"executeLibraryStartupTarget",
		"runMessageBuildProofLibraryStartup",
		"runTickTockBuildProofLibraryStartup",
		"runTransactionBuildProofLibraryStartup",
		"transactionFuzzInboundExternalConfig",
		"transactionFuzzSendExtraFlagsConfig",
		"transactionTestGlobalVersionCell":
		return true
	default:
		return false
	}
}

func transactionVersionCallName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		return expr.Sel.Name
	default:
		return ""
	}
}

func transactionCollectBoundaryVersionLiterals(expr ast.Expr, versions map[int]struct{}) {
	ast.Inspect(expr, func(node ast.Node) bool {
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.INT {
			return true
		}
		version, err := strconv.ParseInt(lit.Value, 0, 64)
		if err == nil {
			versions[int(version)] = struct{}{}
		}
		return true
	})
}

func transactionTargetedBoundaryInventory() []string {
	items := make([]string, 0)
	for name, versions := range expectedTransactionTargetedBoundaryVersions {
		if len(versions) == 0 {
			items = append(items, name+":<no-global-version-boundary>")
			continue
		}
		for _, version := range versions {
			items = append(items, fmt.Sprintf("%s:v%d", name, version))
		}
	}
	sort.Strings(items)
	return items
}

func transactionTargetedBoundaryInventoryHash(items []string) string {
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func transactionVersionFuzzerNamed(fuzzers []transactionVersionFuzzer, name string) bool {
	for _, fuzz := range fuzzers {
		if fuzz.name == name {
			return true
		}
	}
	return false
}

func transactionVersionFuzzerInventoryHash(fuzzers []transactionVersionFuzzer) string {
	items := transactionVersionFuzzerInventoryItems(fuzzers)
	sort.Strings(items)
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func transactionVersionFuzzerInventoryList(fuzzers []transactionVersionFuzzer) string {
	items := transactionVersionFuzzerInventoryItems(fuzzers)
	sort.Strings(items)
	return strings.Join(items, "\n")
}

func transactionVersionFuzzerInventoryItems(fuzzers []transactionVersionFuzzer) []string {
	items := make([]string, 0, len(fuzzers))
	for _, fuzz := range fuzzers {
		items = append(items, fuzz.path+":"+fuzz.name)
	}
	return items
}
