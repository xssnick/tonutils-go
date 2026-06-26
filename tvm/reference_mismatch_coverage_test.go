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

const knownReferenceMismatchPrefix = "bundled reference emulator " + "predates upstream "

const (
	expectedKnownReferenceMismatchLocalAnchorCount = 15
	expectedKnownReferenceMismatchLocalAnchorHash  = "9bd1562f95ba7561a219d6ecd7e977fc8b7ada7bddc9db19ff425746b929dd7b"
	expectedKnownReferenceMismatchBoundaryCount    = 20
	expectedKnownReferenceMismatchBoundaryHash     = "8a62a22d89152532ee63b948f85365476eb281e5b4da104e8b65777d2e5f5eb5"
)

var knownReferenceMismatchLocalAnchors = map[string][]string{
	"CHKSIG v14 zero/identity public-key rejection": {
		"FuzzTVMVersionedCryptoV14Edges",
	},
	"ECRECOVER v=27/28 support": {
		"FuzzTVMEcrecoverEthereumRecoveryIDsV14Boundary",
	},
	"QRSHIFT# v14 NaN preservation": {
		"FuzzTVMVersionedImmediateShiftNaNRules",
	},
	"RIST255 v14 identity support": {
		"FuzzTVMRistrettoIdentityAndZeroScalarV14Boundary",
	},
	"RIST255 v14 zero-scalar validation": {
		"FuzzTVMRistrettoIdentityAndZeroScalarV14Boundary",
	},
	"RSHIFT# v14 NaN preservation": {
		"FuzzTVMVersionedImmediateShiftNaNRules",
	},
	"SENDMSG v14 user fwd fee handling": {
		"FuzzSendMsgVersionedUserFwdFeeLowerBound",
	},
	"control-register v14 silent duplicate save-list writes": {
		"FuzzTVMVersionedControlRegisterDuplicateSaveWrites",
	},
	"transaction v14 failed-action message-balance restore": {
		"FuzzTransactionVersionedFailedActionMessageBalance",
		"FuzzTransactionVersionedStateLimitFailureMessageBalance",
	},
	"v9 direct startup library code loading": {
		"FuzzTVMLibraryCodeCellStartupV9BoundaryCosts",
		"FuzzExecuteDetailedWithAccountProofLibraryCodeCellStartupV9Boundary",
		"FuzzMessageEmulationBuildProofLibraryCodeCellStartupV9Boundary",
		"FuzzTransactionEmulationBuildProofLibraryCodeCellStartupV9Boundary",
		"FuzzTickTockBuildProofLibraryCodeCellStartupV9Boundary",
	},
}

var knownReferenceMismatchBoundaryVersions = map[string][]int{
	"CHKSIG v14 zero/identity public-key rejection": {
		13,
		14,
	},
	"ECRECOVER v=27/28 support": {
		13,
		14,
	},
	"QRSHIFT# v14 NaN preservation": {
		13,
		14,
	},
	"RIST255 v14 identity support": {
		13,
		14,
	},
	"RIST255 v14 zero-scalar validation": {
		13,
		14,
	},
	"RSHIFT# v14 NaN preservation": {
		13,
		14,
	},
	"SENDMSG v14 user fwd fee handling": {
		13,
		14,
	},
	"control-register v14 silent duplicate save-list writes": {
		13,
		14,
	},
	"transaction v14 failed-action message-balance restore": {
		13,
		14,
	},
	"v9 direct startup library code loading": {
		8,
		9,
	},
}

func TestTVMKnownReferenceMismatchReasonsHaveLocalCoverage(t *testing.T) {
	reasons := knownReferenceMismatchSuffixesFromSource(t)
	if len(reasons) != len(knownReferenceMismatchLocalAnchors) {
		t.Fatalf("known reference mismatch suffix count = %d, want %d; got %v", len(reasons), len(knownReferenceMismatchLocalAnchors), reasons)
	}

	for suffix := range reasons {
		anchors, ok := knownReferenceMismatchLocalAnchors[suffix]
		if !ok {
			t.Fatalf("known reference mismatch suffix %q has no local coverage anchor", suffix)
		}
		for _, anchor := range anchors {
			if !localCoverageAnchorExists(t, anchor) {
				t.Fatalf("known reference mismatch suffix %q lost local coverage anchor %s", suffix, anchor)
			}
		}
	}
	for suffix := range knownReferenceMismatchLocalAnchors {
		if _, ok := reasons[suffix]; !ok {
			t.Fatalf("local coverage anchor tracks obsolete known reference mismatch suffix %q", suffix)
		}
	}
}

func TestTVMKnownReferenceMismatchLocalCoverageTouchesBoundaryVersions(t *testing.T) {
	if len(knownReferenceMismatchBoundaryVersions) != len(knownReferenceMismatchLocalAnchors) {
		t.Fatalf("known reference mismatch boundary version suffix count = %d, want %d", len(knownReferenceMismatchBoundaryVersions), len(knownReferenceMismatchLocalAnchors))
	}

	boundaries := knownReferenceMismatchBoundaryInventory()
	if len(boundaries) != expectedKnownReferenceMismatchBoundaryCount {
		t.Fatalf("known reference mismatch boundary count = %d, want %d:\n%s", len(boundaries), expectedKnownReferenceMismatchBoundaryCount, strings.Join(boundaries, "\n"))
	}
	if got := knownReferenceMismatchLocalAnchorInventoryHash(boundaries); got != expectedKnownReferenceMismatchBoundaryHash {
		t.Fatalf("known reference mismatch boundary hash = %s, want %s:\n%s", got, expectedKnownReferenceMismatchBoundaryHash, strings.Join(boundaries, "\n"))
	}

	anchors := localCoverageAnchorFunctions(t)
	for suffix, versions := range knownReferenceMismatchBoundaryVersions {
		names, ok := knownReferenceMismatchLocalAnchors[suffix]
		if !ok {
			t.Fatalf("known reference mismatch boundary suffix %q has no local anchors", suffix)
		}
		for _, version := range versions {
			if version < MinSupportedGlobalVersion || version > MaxSupportedGlobalVersion {
				t.Fatalf("known reference mismatch suffix %q boundary version %d is outside supported range [%d, %d]", suffix, version, MinSupportedGlobalVersion, MaxSupportedGlobalVersion)
			}

			covered := false
			for _, name := range names {
				anchor, ok := anchors[name]
				if !ok {
					t.Fatalf("known reference mismatch suffix %q lost local coverage anchor %s", suffix, name)
				}
				if knownReferenceMismatchAnchorCoversVersion(anchor.fn, version) {
					covered = true
					break
				}
			}
			if !covered {
				t.Fatalf("known reference mismatch suffix %q has no local fuzz anchor touching global version %d", suffix, version)
			}
		}
	}
}

func TestTVMKnownReferenceMismatchLocalCoverageInventory(t *testing.T) {
	anchors := knownReferenceMismatchLocalAnchorInventory()
	if len(anchors) != expectedKnownReferenceMismatchLocalAnchorCount {
		t.Fatalf("known reference mismatch local coverage anchor count = %d, want %d:\n%s", len(anchors), expectedKnownReferenceMismatchLocalAnchorCount, strings.Join(anchors, "\n"))
	}
	if got := knownReferenceMismatchLocalAnchorInventoryHash(anchors); got != expectedKnownReferenceMismatchLocalAnchorHash {
		t.Fatalf("known reference mismatch local coverage anchor hash = %s, want %s:\n%s", got, expectedKnownReferenceMismatchLocalAnchorHash, strings.Join(anchors, "\n"))
	}
}

func TestTVMKnownReferenceMismatchAnchorVersionScannerCoversSeedShapes(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "fixture_test.go", `
package tvm

func FuzzKnownReferenceScannerFixture(f *testing.F) {
	f.Add(13, uint8(0))
	f.Add(int64(14), uint8(1))
	for _, version := range []int{8, 9} {
		f.Add(version, uint8(2))
	}

	f.Fuzz(func(t *testing.T, rawVersion int, rawCase uint8) {
		helper(12)
		runFixture(t, 8, uint8(3))
		assertFixture(t, int64(9))
		for _, version := range []int{10, 11} {
			helper(version)
		}
	})
}
`, 0)
	if err != nil {
		t.Fatalf("parse known reference mismatch anchor scanner fixture: %v", err)
	}

	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		parsed, ok := decl.(*ast.FuncDecl)
		if ok {
			fn = parsed
			break
		}
	}
	if fn == nil {
		t.Fatal("known reference mismatch scanner fixture has no function")
	}

	versions := knownReferenceMismatchAnchorLiteralVersions(fn)
	if got := knownReferenceMismatchVersionSetList(versions); got != "8,9,13,14" {
		t.Fatalf("known reference mismatch scanner versions = %s, want 8,9,13,14", got)
	}
}

func knownReferenceMismatchLocalAnchorInventory() []string {
	anchors := make([]string, 0)
	for suffix, names := range knownReferenceMismatchLocalAnchors {
		for _, name := range names {
			anchors = append(anchors, suffix+":"+name)
		}
	}
	sort.Strings(anchors)
	return anchors
}

func knownReferenceMismatchBoundaryInventory() []string {
	boundaries := make([]string, 0)
	for suffix, versions := range knownReferenceMismatchBoundaryVersions {
		for _, version := range versions {
			boundaries = append(boundaries, fmt.Sprintf("%s:v%d", suffix, version))
		}
	}
	sort.Strings(boundaries)
	return boundaries
}

func knownReferenceMismatchLocalAnchorInventoryHash(anchors []string) string {
	sum := sha256.Sum256([]byte(strings.Join(anchors, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func knownReferenceMismatchSuffixesFromSource(t *testing.T) map[string]int {
	t.Helper()

	got := make(map[string]int)
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
		if !strings.HasSuffix(path, ".go") || knownReferenceMismatchCoverageAuditFile(path) {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil || !strings.HasPrefix(value, knownReferenceMismatchPrefix) {
				return true
			}
			got[strings.TrimPrefix(value, knownReferenceMismatchPrefix)]++
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan known reference mismatch suffixes: %v", err)
	}
	return got
}

func localCoverageAnchorExists(t *testing.T, anchor string) bool {
	t.Helper()

	if !strings.HasPrefix(anchor, "Test") && !strings.HasPrefix(anchor, "Fuzz") {
		t.Fatalf("local coverage anchor %q must name a Test or Fuzz function", anchor)
	}

	found := false
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
		if !strings.HasSuffix(path, "_test.go") || knownReferenceMismatchCoverageAuditFile(path) || strings.Contains(filepath.ToSlash(path), "cross_emulator") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Name.Name == anchor {
				found = true
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan local coverage anchors: %v", err)
	}
	return found
}

type localCoverageAnchorFunction struct {
	path string
	fn   *ast.FuncDecl
}

func localCoverageAnchorFunctions(t *testing.T) map[string]localCoverageAnchorFunction {
	t.Helper()

	out := make(map[string]localCoverageAnchorFunction)
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
		if !strings.HasSuffix(path, "_test.go") || knownReferenceMismatchCoverageAuditFile(path) || strings.Contains(filepath.ToSlash(path), "cross_emulator") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Fuzz") {
				continue
			}
			out[fn.Name.Name] = localCoverageAnchorFunction{
				path: filepath.ToSlash(path),
				fn:   fn,
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan local coverage fuzz anchors: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("local coverage fuzz anchor scan found no fuzzers")
	}
	return out
}

func knownReferenceMismatchAnchorCoversVersion(fn *ast.FuncDecl, version int) bool {
	if knownReferenceMismatchAnchorCoversAllSupportedVersions(fn) {
		return true
	}
	_, ok := knownReferenceMismatchAnchorLiteralVersions(fn)[version]
	return ok
}

func knownReferenceMismatchAnchorCoversAllSupportedVersions(fn *ast.FuncDecl) bool {
	if supportedRangeVersionFuzzerSeedCoverage(fn).fullRange {
		return true
	}
	if packageLocalVersionFuzzerSeedsDefaultRange(fn) {
		return true
	}
	return transactionVersionFuzzerRuntimeLoopsAllVersions(fn)
}

func knownReferenceMismatchAnchorLiteralVersions(fn *ast.FuncDecl) map[int]struct{} {
	versions := make(map[int]struct{})

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			for _, arg := range knownReferenceMismatchCallVersionArgs(node) {
				if version, ok := knownReferenceMismatchVersionLiteral(arg); ok {
					versions[version] = struct{}{}
				}
			}
		case *ast.RangeStmt:
			value, ok := node.Value.(*ast.Ident)
			if !ok || value.Name == "_" || !knownReferenceMismatchRangeBodyAddsFuzzVersion(node.Body, value.Name) {
				return true
			}
			for _, version := range knownReferenceMismatchCompositeVersionLiterals(node.X) {
				versions[version] = struct{}{}
			}
		}
		return true
	})

	return versions
}

func knownReferenceMismatchCallVersionArgs(call *ast.CallExpr) []ast.Expr {
	if packageLocalFuzzAddCall(call.Fun) {
		if len(call.Args) == 0 {
			return nil
		}
		return call.Args[:1]
	}
	if len(call.Args) < 2 || !knownReferenceMismatchTestingArg(call.Args[0]) {
		return nil
	}
	return call.Args[1:2]
}

func knownReferenceMismatchTestingArg(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "t"
}

func knownReferenceMismatchRangeBodyAddsFuzzVersion(body *ast.BlockStmt, versionVar string) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !packageLocalFuzzAddCall(call.Fun) {
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

func knownReferenceMismatchCompositeVersionLiterals(expr ast.Expr) []int {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}

	versions := make([]int, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		if version, ok := knownReferenceMismatchVersionLiteral(elt); ok {
			versions = append(versions, version)
		}
	}
	return versions
}

func knownReferenceMismatchVersionLiteral(expr ast.Expr) (int, bool) {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		if expr.Kind != token.INT {
			return 0, false
		}
		version, err := strconv.ParseInt(expr.Value, 0, 64)
		if err != nil || version < int64(MinSupportedGlobalVersion) || version > int64(MaxSupportedGlobalVersion) {
			return 0, false
		}
		return int(version), true
	case *ast.CallExpr:
		if len(expr.Args) != 1 {
			return 0, false
		}
		return knownReferenceMismatchVersionLiteral(expr.Args[0])
	case *ast.ParenExpr:
		return knownReferenceMismatchVersionLiteral(expr.X)
	default:
		return 0, false
	}
}

func knownReferenceMismatchCoverageAuditFile(path string) bool {
	switch filepath.Base(path) {
	case "cross_emulator_version_audit_test.go", "reference_mismatch_coverage_test.go":
		return true
	default:
		return false
	}
}

func knownReferenceMismatchVersionSetList(versions map[int]struct{}) string {
	items := make([]int, 0, len(versions))
	for version := range versions {
		items = append(items, version)
	}
	sort.Ints(items)

	out := make([]string, 0, len(items))
	for _, version := range items {
		out = append(out, strconv.Itoa(version))
	}
	return strings.Join(out, ",")
}
