package tvm

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	expectedPackageLocalVersionFuzzerCount          = 46
	expectedPackageLocalVersionFuzzerHash           = "17de0266cf8bef94294759a156da1ec80b5529fe6b84ce91e2b0ba6f5907d8bd"
	expectedSupportedRangeVersionFuzzerCount        = 77
	expectedSupportedRangeVersionFuzzerHash         = "b78317c241bf59e89458afa7305a7a6287c3f498c610af5b93bea7ed398404fa"
	expectedSupportedRangeFullRangeVersionFuzzCount = 77
	expectedSupportedRangeFullRangeVersionFuzzHash  = "b78317c241bf59e89458afa7305a7a6287c3f498c610af5b93bea7ed398404fa"
	expectedSupportedRangePartialVersionFuzzCount   = 0
	expectedSupportedRangePartialVersionFuzzHash    = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	expectedNonCrossVersionNamedFuzzerCount         = 87
	expectedNonCrossVersionNamedFuzzerHash          = "6c56f47e3118ca93ab2c44044683456d6373b53500749c4a24e59d35f22bd45a"
	expectedNonCrossVersionNamedTestCount           = 31
	expectedNonCrossVersionNamedTestHash            = "7bb6f87db60ae39775589458d653da6cb02ddb909e8c5631b86bf398de89574c"
	expectedPackageLocalVersionMapperShapeCount     = 7
	expectedPackageLocalVersionMapperShapeHash      = "62c61884b8d5ee64ea57c5936f1a6f673d794af551a45e52942233d417c81074"
)

type packageLocalVersionFuzzerPackageInventory struct {
	count int
	hash  string
}

var expectedPackageLocalVersionFuzzerPackages = map[string]packageLocalVersionFuzzerPackageInventory{
	"cellslice": {count: 2, hash: "1bc6e4da6f2875dc532b2506b85b3b5eaece94946c8196102281fda692002508"},
	"dict":      {count: 18, hash: "b47af0650907696abd685b6d53cb50dedac4c6372c225166af23e9a022f00575"},
	"exec":      {count: 1, hash: "fcb1bdc057000b657e3e7d72570ad6a088ce6414d0fb222ba95bb13f8fc7a20d"},
	"funcs":     {count: 17, hash: "e059578c366c8848ff5fec7fc081971ea34e2adc2a58a75de592c33425250773"},
	"math":      {count: 4, hash: "bd39197439ca90005e8ce9afbf67f4488d216b34f50d367b6bb582b155ca7ea0"},
	"stack":     {count: 2, hash: "8ebec44e5496c2f557e93ece43b881c3ea36566ab8b2f8b92c122a5c41fbb51d"},
	"tuple":     {count: 2, hash: "23d1dd347ad1df0b3b66464eb844b53e2d1627cdb036ff832483ef795f5921ee"},
}

var expectedPackageLocalVersionMapperTests = map[string]string{
	"cellslice": "TestFuzzCellSliceVersionCoversDefaultRange",
	"dict":      "TestFuzzDictVersionCoversDefaultRange",
	"exec":      "TestFuzzExecVersionCoversDefaultRange",
	"funcs":     "TestFuzzFuncsVersionCoversDefaultRange",
	"math":      "TestFuzzMathVersionCoversDefaultRange",
	"stack":     "TestFuzzStackVersionCoversDefaultRange",
	"tuple":     "TestFuzzTupleVersionCoversDefaultRange",
}

var expectedPackageLocalVersionMappers = map[string]string{
	"cellslice": "fuzzCellSliceVersion",
	"dict":      "fuzzDictVersion",
	"exec":      "fuzzExecVersion",
	"funcs":     "fuzzFuncsVersion",
	"math":      "fuzzMathVersion",
	"stack":     "fuzzStackVersion",
	"tuple":     "fuzzTupleVersion",
}

type packageLocalVersionFuzzer struct {
	path string
	name string
	fn   *ast.FuncDecl
}

func TestTVMPackageLocalVersionFuzzersSeedDefaultRange(t *testing.T) {
	fuzzers := packageLocalVersionFuzzers(t)
	if len(fuzzers) != expectedPackageLocalVersionFuzzerCount {
		t.Fatalf("package-local version fuzzer count = %d, want %d", len(fuzzers), expectedPackageLocalVersionFuzzerCount)
	}
	if got := versionFuzzerInventoryHash(fuzzers); got != expectedPackageLocalVersionFuzzerHash {
		t.Fatalf("package-local version fuzzer hash = %s, want %s", got, expectedPackageLocalVersionFuzzerHash)
	}

	var missing []string
	for _, fuzz := range fuzzers {
		if !packageLocalVersionFuzzerSeedsDefaultRange(fuzz.fn) {
			missing = append(missing, fuzz.path+":"+fuzz.name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("package-local version fuzzers without full default-version f.Add seed loops:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMPackageLocalVersionFuzzersUseDefaultRangeMapper(t *testing.T) {
	fuzzers := packageLocalVersionFuzzers(t)

	var missing []string
	for _, fuzz := range fuzzers {
		if !packageLocalVersionFuzzerUsesDefaultRangeMapper(fuzz) {
			missing = append(missing, fuzz.path+":"+fuzz.name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("package-local version fuzzers without package default-version mapper:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMPackageLocalVersionFuzzerPackageInventory(t *testing.T) {
	byPackage := packageLocalVersionFuzzersByPackage(packageLocalVersionFuzzers(t))
	if len(byPackage) != len(expectedPackageLocalVersionFuzzerPackages) {
		t.Fatalf("package-local version fuzzer package count = %d, want %d; got %s", len(byPackage), len(expectedPackageLocalVersionFuzzerPackages), packageLocalVersionFuzzerPackageList(byPackage))
	}

	for pkg, want := range expectedPackageLocalVersionFuzzerPackages {
		fuzzers, ok := byPackage[pkg]
		if !ok {
			t.Fatalf("package-local version fuzzer package %s is missing; got %s", pkg, packageLocalVersionFuzzerPackageList(byPackage))
		}
		if len(fuzzers) != want.count {
			t.Fatalf("package-local version fuzzer package %s count = %d, want %d:\n%s", pkg, len(fuzzers), want.count, versionFuzzerInventoryList(fuzzers))
		}
		if got := versionFuzzerInventoryHash(fuzzers); got != want.hash {
			t.Fatalf("package-local version fuzzer package %s hash = %s, want %s:\n%s", pkg, got, want.hash, versionFuzzerInventoryList(fuzzers))
		}
	}
	for pkg := range byPackage {
		if _, ok := expectedPackageLocalVersionFuzzerPackages[pkg]; !ok {
			t.Fatalf("unexpected package-local version fuzzer package %s:\n%s", pkg, versionFuzzerInventoryList(byPackage[pkg]))
		}
	}
}

func TestTVMPackageLocalVersionMapperCoverageInventory(t *testing.T) {
	tests := packageLocalVersionMapperTests(t)
	if len(tests) != len(expectedPackageLocalVersionMapperTests) {
		t.Fatalf("package-local version mapper coverage test count = %d, want %d; got %s", len(tests), len(expectedPackageLocalVersionMapperTests), packageLocalVersionMapperTestList(tests))
	}

	for pkg, want := range expectedPackageLocalVersionMapperTests {
		got, ok := tests[pkg]
		if !ok {
			t.Fatalf("package-local version mapper coverage test for package %s is missing; got %s", pkg, packageLocalVersionMapperTestList(tests))
		}
		if got != want {
			t.Fatalf("package-local version mapper coverage test for package %s = %s, want %s", pkg, got, want)
		}
		if _, ok = expectedPackageLocalVersionFuzzerPackages[pkg]; !ok {
			t.Fatalf("package-local version mapper coverage test tracks package %s without version fuzzer inventory", pkg)
		}
		if _, ok = expectedPackageLocalVersionMappers[pkg]; !ok {
			t.Fatalf("package-local version mapper coverage test tracks package %s without mapper function inventory", pkg)
		}
	}
	for pkg := range tests {
		if _, ok := expectedPackageLocalVersionMapperTests[pkg]; !ok {
			t.Fatalf("unexpected package-local version mapper coverage test for package %s: %s", pkg, tests[pkg])
		}
	}
}

func TestTVMPackageLocalVersionFuzzerScannerIncludesBoundaryNames(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "op/funcs/version_fuzz_test.go", `
		package funcs

		func FuzzTVMRistrettoIdentityAndZeroScalarV14Boundary(f *testing.F) {}
		func FuzzTVMEcrecoverEthereumRecoveryIDsV14Boundary(f *testing.F) {}
		func FuzzTVMUnrelatedCryptoCase(f *testing.F) {}
	`, 0)
	if err != nil {
		t.Fatalf("parse package-local boundary fuzzer fixture: %v", err)
	}

	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !packageLocalVersionFuzzerName(fn.Name.Name) {
			continue
		}
		names = append(names, fn.Name.Name)
	}
	sort.Strings(names)
	if got := strings.Join(names, ","); got != "FuzzTVMEcrecoverEthereumRecoveryIDsV14Boundary,FuzzTVMRistrettoIdentityAndZeroScalarV14Boundary" {
		t.Fatalf("package-local version fuzzer scanner fixture = %s", got)
	}
}

func TestTVMPackageLocalVersionMapperShapeInventory(t *testing.T) {
	inventory := packageLocalVersionMapperShapeInventory(t)
	if len(inventory) != expectedPackageLocalVersionMapperShapeCount {
		t.Fatalf("package-local version mapper shape count = %d, want %d:\n%s", len(inventory), expectedPackageLocalVersionMapperShapeCount, strings.Join(inventory, "\n"))
	}
	if got := packageLocalVersionMapperShapeInventoryHash(inventory); got != expectedPackageLocalVersionMapperShapeHash {
		t.Fatalf("package-local version mapper shape hash = %s, want %s:\n%s", got, expectedPackageLocalVersionMapperShapeHash, strings.Join(inventory, "\n"))
	}
}

func TestTVMOpVersionStateLiteralScannerCoversExplicitZero(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "op/stack/fixture_test.go", `
package stack

func stateLiteralScannerFixture(version int) {
	_ = &vm.State{GlobalVersion: 0}
	_ = &vm.State{GlobalVersion: 0}
	_ = &vm.State{GlobalVersion: version}
}
`, 0)
	if err != nil {
		t.Fatalf("parse state literal scanner fixture: %v", err)
	}

	var explicit int
	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok || !opVersionStateLiteral(lit) || !opVersionStateLiteralUsesExplicitVersion(lit) {
			return true
		}
		explicit++
		return true
	})
	if explicit != 3 {
		t.Fatalf("state literal scanner explicit = %d, want 3", explicit)
	}
}

func TestTVMPackageLocalVersionStateAssignmentScannerCoversExplicitZero(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "op/stack/fixture_test.go", `
package stack

func stateAssignmentScannerFixture(st *vm.State, version int) {
	st.GlobalVersion = 0
	st.GlobalVersion = version
}
`, 0)
	if err != nil {
		t.Fatalf("parse state assignment scanner fixture: %v", err)
	}

	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if parsed, ok := decl.(*ast.FuncDecl); ok {
			fn = parsed
			break
		}
	}
	if fn == nil {
		t.Fatal("state assignment scanner fixture has no function")
	}
	if !versionFuzzFunctionWritesExplicitGlobalVersion(fn) {
		t.Fatal("state assignment scanner did not detect explicit global version writes")
	}
}

func TestTVMSupportedRangeVersionFuzzersSeedBoundaries(t *testing.T) {
	fuzzers := supportedRangeVersionFuzzers(t)
	if len(fuzzers) != expectedSupportedRangeVersionFuzzerCount {
		t.Fatalf("supported-range version fuzzer count = %d, want %d", len(fuzzers), expectedSupportedRangeVersionFuzzerCount)
	}
	if got := versionFuzzerInventoryHash(fuzzers); got != expectedSupportedRangeVersionFuzzerHash {
		t.Fatalf("supported-range version fuzzer hash = %s, want %s", got, expectedSupportedRangeVersionFuzzerHash)
	}

	var missing []string
	for _, fuzz := range fuzzers {
		seeds := supportedRangeVersionFuzzerSeedCoverage(fuzz.fn)
		if !seeds.min || !seeds.max {
			missing = append(missing, fuzz.path+":"+fuzz.name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("supported-range version fuzzers without min/max f.Add seeds:\n%s", strings.Join(missing, "\n"))
	}
}

func TestTVMSupportedRangeVersionFuzzersSeedFullSupportedRange(t *testing.T) {
	fuzzers := supportedRangeVersionFuzzers(t)

	var fullRange []packageLocalVersionFuzzer
	var partial []packageLocalVersionFuzzer
	for _, fuzz := range fuzzers {
		seeds := supportedRangeVersionFuzzerSeedCoverage(fuzz.fn)
		if seeds.fullRange {
			fullRange = append(fullRange, fuzz)
		} else {
			partial = append(partial, fuzz)
		}
	}

	if len(fullRange) != expectedSupportedRangeFullRangeVersionFuzzCount {
		t.Fatalf("supported-range full-range fuzzer count = %d, want %d\n%s", len(fullRange), expectedSupportedRangeFullRangeVersionFuzzCount, versionFuzzerInventoryList(fullRange))
	}
	if got := versionFuzzerInventoryHash(fullRange); got != expectedSupportedRangeFullRangeVersionFuzzHash {
		t.Fatalf("supported-range full-range fuzzer hash = %s, want %s\n%s", got, expectedSupportedRangeFullRangeVersionFuzzHash, versionFuzzerInventoryList(fullRange))
	}
	if len(partial) != expectedSupportedRangePartialVersionFuzzCount {
		t.Fatalf("supported-range partial fuzzer count = %d, want %d\n%s", len(partial), expectedSupportedRangePartialVersionFuzzCount, versionFuzzerInventoryList(partial))
	}
	if got := versionFuzzerInventoryHash(partial); got != expectedSupportedRangePartialVersionFuzzHash {
		t.Fatalf("supported-range partial fuzzer hash = %s, want %s\n%s", got, expectedSupportedRangePartialVersionFuzzHash, versionFuzzerInventoryList(partial))
	}
}

func TestTVMSupportedRangeVersionFuzzerScannerCoversHelperSeedSources(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "opcode_version_fuzz_test.go", `
	package tvm

	func FuzzSupportedRangeRegisteredHelperSeedFixture(f *testing.F) {
		cases := opcodeMinGlobalVersionBoundaryCases()
		for _, seed := range registeredOpcodeAvailabilityFuzzSeeds(cases) {
			f.Add(uint16(seed.caseIdx), int64(seed.version))
		}
	}

	func FuzzSupportedRangeRepresentativeHelperSeedFixture(f *testing.F) {
		cases := opcodeMinGlobalVersionBoundaryCases()
		for _, seed := range opcodeMinGlobalVersionRepresentativeFuzzSeeds(cases) {
			f.Add(uint16(seed.caseIdx), int64(seed.version))
		}
	}

	func FuzzSupportedRangeMappedArgFixture(f *testing.F) {
		f.Add(uint8(0), uint8(99))
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(99))
		f.Add(uint8(99), uint8(0))
		f.Add(uint8(99), uint8(vm.MaxSupportedGlobalVersion))

		f.Fuzz(func(t *testing.T, rawVersion uint8, payload uint8) {
			_ = tvmFuzzGlobalVersionByte(rawVersion)
		})
	}

	func FuzzSupportedRangeMappedArgNegativeFixture(f *testing.F) {
		f.Add(uint8(0), uint8(99))
		f.Add(uint8(vm.MaxSupportedGlobalVersion), uint8(99))

		f.Fuzz(func(t *testing.T, payload uint8, rawVersion uint8) {
			_ = tvmFuzzGlobalVersionByte(rawVersion)
		})
	}

	func FuzzSupportedRangeMappedLoopFixture(f *testing.F) {
		for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
			f.Add(uint8(version), uint8(99))
		}

		f.Fuzz(func(t *testing.T, rawVersion uint8, payload uint8) {
			_ = tvmFuzzGlobalVersionByte(rawVersion)
		})
	}

	func FuzzSupportedRangeMappedLoopNegativeFixture(f *testing.F) {
		for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
			f.Add(uint8(version), uint8(99))
		}

		f.Fuzz(func(t *testing.T, payload uint8, rawVersion uint8) {
			_ = tvmFuzzGlobalVersionByte(rawVersion)
		})
	}
	`, 0)
	if err != nil {
		t.Fatalf("parse supported-range helper seed fixture: %v", err)
	}

	checked := 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		seeds := supportedRangeVersionFuzzerSeedCoverage(fn)
		if fn.Name.Name == "FuzzSupportedRangeMappedArgNegativeFixture" ||
			fn.Name.Name == "FuzzSupportedRangeMappedLoopNegativeFixture" {
			if seeds.min || seeds.max || seeds.fullRange {
				t.Fatalf("%s supported-range scanner coverage = min:%t max:%t full:%t, want all false", fn.Name.Name, seeds.min, seeds.max, seeds.fullRange)
			}
			checked++
			continue
		}
		if fn.Name.Name == "FuzzSupportedRangeMappedArgFixture" {
			if !seeds.min || !seeds.max || seeds.fullRange {
				t.Fatalf("%s supported-range scanner coverage = min:%t max:%t full:%t, want min/max only", fn.Name.Name, seeds.min, seeds.max, seeds.fullRange)
			}
			checked++
			continue
		}
		if !seeds.min || !seeds.max || !seeds.fullRange {
			t.Fatalf("%s supported-range helper seed scanner coverage = min:%t max:%t full:%t, want all true", fn.Name.Name, seeds.min, seeds.max, seeds.fullRange)
		}
		checked++
	}
	if checked != 6 {
		t.Fatalf("supported-range helper seed scanner fixture checked %d functions, want 6", checked)
	}
}

func TestTVMNonCrossVersionNamedFuzzerInventory(t *testing.T) {
	fuzzers := nonCrossVersionNamedFuzzers(t)
	if len(fuzzers) != expectedNonCrossVersionNamedFuzzerCount {
		t.Fatalf("non-cross version-named fuzzer count = %d, want %d:\n%s", len(fuzzers), expectedNonCrossVersionNamedFuzzerCount, versionFuzzerInventoryList(fuzzers))
	}
	if got := versionFuzzerInventoryHash(fuzzers); got != expectedNonCrossVersionNamedFuzzerHash {
		t.Fatalf("non-cross version-named fuzzer hash = %s, want %s:\n%s", got, expectedNonCrossVersionNamedFuzzerHash, versionFuzzerInventoryList(fuzzers))
	}
}

func TestTVMNonCrossVersionNamedTestInventory(t *testing.T) {
	tests := nonCrossVersionNamedTests(t)
	if len(tests) != expectedNonCrossVersionNamedTestCount {
		t.Fatalf("non-cross version-named test count = %d, want %d:\n%s", len(tests), expectedNonCrossVersionNamedTestCount, versionFuzzerInventoryList(tests))
	}
	if got := versionFuzzerInventoryHash(tests); got != expectedNonCrossVersionNamedTestHash {
		t.Fatalf("non-cross version-named test hash = %s, want %s:\n%s", got, expectedNonCrossVersionNamedTestHash, versionFuzzerInventoryList(tests))
	}
}

func TestTVMNonCrossVersionNamedFuzzersUseSupportedRange(t *testing.T) {
	fuzzers := nonCrossVersionNamedFuzzers(t)

	var missing []string
	for _, fuzz := range fuzzers {
		if nonCrossVersionNamedFuzzerHasSupportedRangeCoverage(fuzz) ||
			nonCrossVersionNamedFuzzerHasTargetedBoundaryInventory(fuzz) {
			continue
		}
		missing = append(missing, fuzz.path+":"+fuzz.name)
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("non-cross version-named fuzzers missing supported-range or targeted-boundary coverage:\n%s", strings.Join(missing, "\n"))
	}
}

func packageLocalVersionFuzzers(t *testing.T) []packageLocalVersionFuzzer {
	t.Helper()

	var fuzzers []packageLocalVersionFuzzer
	fset := token.NewFileSet()
	err := filepath.WalkDir("op", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
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
			if !ok || fn.Body == nil || !packageLocalVersionFuzzerName(fn.Name.Name) {
				continue
			}
			fuzzers = append(fuzzers, packageLocalVersionFuzzer{
				path: filepath.ToSlash(path),
				name: fn.Name.Name,
				fn:   fn,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan package-local version fuzzers: %v", err)
	}
	if len(fuzzers) == 0 {
		t.Fatal("package-local version fuzzer scan found no fuzzers")
	}
	return fuzzers
}

func packageLocalVersionFuzzerName(name string) bool {
	return strings.HasPrefix(name, "FuzzTVMVersioned") ||
		strings.Contains(name, "V14Boundary")
}

func packageLocalVersionFuzzersByPackage(fuzzers []packageLocalVersionFuzzer) map[string][]packageLocalVersionFuzzer {
	byPackage := make(map[string][]packageLocalVersionFuzzer)
	for _, fuzz := range fuzzers {
		pkg := packageLocalVersionFuzzerPackage(fuzz.path)
		if pkg == "" {
			continue
		}
		byPackage[pkg] = append(byPackage[pkg], fuzz)
	}
	return byPackage
}

func packageLocalVersionFuzzerPackageList(byPackage map[string][]packageLocalVersionFuzzer) string {
	items := make([]string, 0, len(byPackage))
	for pkg, fuzzers := range byPackage {
		items = append(items, fmt.Sprintf("%s:%d", pkg, len(fuzzers)))
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}

func packageLocalVersionMapperTests(t *testing.T) map[string]string {
	t.Helper()

	tests := make(map[string]string)
	fset := token.NewFileSet()
	err := filepath.WalkDir("op", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
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
			if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "TestFuzz") || !strings.HasSuffix(fn.Name.Name, "VersionCoversDefaultRange") {
				continue
			}
			pkg := packageLocalVersionFuzzerPackage(path)
			if pkg == "" {
				continue
			}
			if prev, ok := tests[pkg]; ok {
				t.Fatalf("package-local version mapper coverage package %s has duplicate tests %s and %s", pkg, prev, fn.Name.Name)
			}
			tests[pkg] = fn.Name.Name
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan package-local version mapper coverage tests: %v", err)
	}
	if len(tests) == 0 {
		t.Fatal("package-local version mapper coverage test scan found no tests")
	}
	return tests
}

func packageLocalVersionFuzzerPackage(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 3 || parts[0] != "op" {
		return ""
	}
	return parts[1]
}

func packageLocalVersionMapperTestList(tests map[string]string) string {
	items := make([]string, 0, len(tests))
	for pkg, name := range tests {
		items = append(items, pkg+":"+name)
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}

func packageLocalVersionMapperShapeInventory(t *testing.T) []string {
	t.Helper()

	mapperPackages := make(map[string]string, len(expectedPackageLocalVersionMappers))
	for pkg, mapper := range expectedPackageLocalVersionMappers {
		mapperPackages[mapper] = pkg
	}

	found := make(map[string]string, len(expectedPackageLocalVersionMappers))
	fset := token.NewFileSet()
	err := filepath.WalkDir("op", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
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
			pkg, ok := mapperPackages[fn.Name.Name]
			if !ok {
				continue
			}
			path = filepath.ToSlash(path)
			if wantPkg := packageLocalVersionFuzzerPackage(path); wantPkg != pkg {
				t.Fatalf("package-local version mapper %s is in package %s, want %s", fn.Name.Name, wantPkg, pkg)
			}
			if prev, ok := found[fn.Name.Name]; ok {
				t.Fatalf("duplicate package-local version mapper %s in %s and %s", fn.Name.Name, prev, path)
			}
			if !packageLocalVersionMapperHasDefaultRangeShape(fn) {
				t.Fatalf("package-local version mapper %s:%s has unsupported body shape", path, fn.Name.Name)
			}
			found[fn.Name.Name] = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan package-local version mapper shapes: %v", err)
	}

	inventory := make([]string, 0, len(expectedPackageLocalVersionMappers))
	for pkg, mapper := range expectedPackageLocalVersionMappers {
		path, ok := found[mapper]
		if !ok {
			t.Fatalf("package-local version mapper %s for package %s is missing", mapper, pkg)
		}
		inventory = append(inventory, pkg+":"+path+":"+mapper+":default-range-modulo")
	}
	sort.Strings(inventory)
	return inventory
}

func packageLocalVersionMapperShapeInventoryHash(inventory []string) string {
	sum := sha256.Sum256([]byte(strings.Join(inventory, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func opVersionStateLiteral(lit *ast.CompositeLit) bool {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "State" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "vm"
}

func opVersionStateLiteralUsesExplicitVersion(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok || versionFuzzFieldName(kv.Key) != "GlobalVersion" {
			continue
		}
		return versionFuzzExprIsExplicitGlobalVersion(kv.Value)
	}
	return false
}

func versionFuzzFunctionWritesExplicitGlobalVersion(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			if versionFuzzAssignmentWritesFieldFromVersion(node, "GlobalVersion") {
				found = true
				return false
			}
		case *ast.KeyValueExpr:
			if versionFuzzFieldName(node.Key) == "GlobalVersion" && versionFuzzExprIsExplicitGlobalVersion(node.Value) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func versionFuzzAssignmentWritesFieldFromVersion(stmt *ast.AssignStmt, field string) bool {
	for i, lhs := range stmt.Lhs {
		if versionFuzzFieldName(lhs) != field {
			continue
		}
		rhs := versionFuzzAssignmentRHS(stmt, i)
		if rhs != nil && versionFuzzExprIsExplicitGlobalVersion(rhs) {
			return true
		}
	}
	return false
}

func versionFuzzAssignmentWritesTrueField(stmt *ast.AssignStmt, field string) bool {
	for i, lhs := range stmt.Lhs {
		if versionFuzzFieldName(lhs) != field {
			continue
		}
		rhs := versionFuzzAssignmentRHS(stmt, i)
		if rhs != nil && versionFuzzExprIsTrue(rhs) {
			return true
		}
	}
	return false
}

func versionFuzzAssignmentRHS(stmt *ast.AssignStmt, idx int) ast.Expr {
	if len(stmt.Rhs) == 1 {
		return stmt.Rhs[0]
	}
	if idx < len(stmt.Rhs) {
		return stmt.Rhs[idx]
	}
	return nil
}

func versionFuzzFieldName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		return expr.Sel.Name
	default:
		return ""
	}
}

func versionFuzzExprIsTrue(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "true"
}

func versionFuzzExprContainsExplicitVersionVar(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.Ident:
			if node.Name == "version" || node.Name == "globalVersion" {
				found = true
				return false
			}
		case *ast.SelectorExpr:
			if node.Sel.Name == "version" || node.Sel.Name == "globalVersion" {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func versionFuzzExprIsExplicitGlobalVersion(expr ast.Expr) bool {
	return versionFuzzExprContainsExplicitVersionVar(expr) || versionFuzzExprIsZeroLiteral(expr)
}

func packageLocalVersionMapperHasDefaultRangeShape(fn *ast.FuncDecl) bool {
	rawParam, ok := packageLocalVersionMapperParamName(fn)
	if !ok || len(fn.Body.List) != 3 {
		return false
	}

	versionVar, rawVar, ok := packageLocalVersionMapperAssign(fn.Body.List[0])
	if !ok || rawVar != rawParam {
		return false
	}
	if !packageLocalVersionMapperNegativeClamp(fn.Body.List[1], versionVar) {
		return false
	}
	return packageLocalVersionMapperReturn(fn.Body.List[2], versionVar) && rawVar != ""
}

func packageLocalVersionMapperParamName(fn *ast.FuncDecl) (string, bool) {
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return "", false
	}
	if len(fn.Type.Params.List[0].Names) != 1 || fn.Type.Params.List[0].Names[0].Name == "" {
		return "", false
	}
	param, ok := fn.Type.Params.List[0].Type.(*ast.Ident)
	if !ok || param.Name != "int64" {
		return "", false
	}
	result, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
	if !ok || result.Name != "int" {
		return "", false
	}
	return fn.Type.Params.List[0].Names[0].Name, true
}

func packageLocalVersionMapperAssign(stmt ast.Stmt) (versionVar, rawVar string, ok bool) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return "", "", false
	}
	version, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || version.Name == "" {
		return "", "", false
	}

	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || packageLocalVersionMapperCallName(call.Fun) != "int" || len(call.Args) != 1 {
		return "", "", false
	}
	mod, ok := call.Args[0].(*ast.BinaryExpr)
	if !ok || mod.Op != token.REM {
		return "", "", false
	}
	raw, ok := mod.X.(*ast.Ident)
	if !ok || raw.Name == "" {
		return "", "", false
	}
	divisor, ok := mod.Y.(*ast.CallExpr)
	if !ok || packageLocalVersionMapperCallName(divisor.Fun) != "int64" || len(divisor.Args) != 1 {
		return "", "", false
	}
	if !packageLocalVersionMapperMaxSupportedGlobalVersionPlusOne(divisor.Args[0]) {
		return "", "", false
	}
	return version.Name, raw.Name, true
}

func packageLocalVersionMapperNegativeClamp(stmt ast.Stmt, versionVar string) bool {
	ifs, ok := stmt.(*ast.IfStmt)
	if !ok || ifs.Init != nil || ifs.Else != nil || len(ifs.Body.List) != 1 {
		return false
	}
	cond, ok := ifs.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.LSS || !packageLocalVersionMapperIdent(cond.X, versionVar) || !packageLocalVersionMapperZero(cond.Y) {
		return false
	}

	assign, ok := ifs.Body.List[0].(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || !packageLocalVersionMapperIdent(assign.Lhs[0], versionVar) {
		return false
	}
	neg, ok := assign.Rhs[0].(*ast.UnaryExpr)
	return ok && neg.Op == token.SUB && packageLocalVersionMapperIdent(neg.X, versionVar)
}

func packageLocalVersionMapperReturn(stmt ast.Stmt, versionVar string) bool {
	ret, ok := stmt.(*ast.ReturnStmt)
	return ok && len(ret.Results) == 1 && packageLocalVersionMapperIdent(ret.Results[0], versionVar)
}

func packageLocalVersionMapperMaxSupportedGlobalVersionPlusOne(expr ast.Expr) bool {
	add, ok := expr.(*ast.BinaryExpr)
	if !ok || add.Op != token.ADD || !packageLocalVersionMapperOne(add.Y) {
		return false
	}
	return packageLocalVersionMapperMaxSupportedGlobalVersion(add.X)
}

func packageLocalVersionMapperMaxSupportedGlobalVersion(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "MaxSupportedGlobalVersion" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "vm"
}

func packageLocalVersionMapperIdent(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func packageLocalVersionMapperCallName(expr ast.Expr) string {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

func packageLocalVersionMapperZero(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == "0"
}

func packageLocalVersionMapperOne(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == "1"
}

func supportedRangeVersionFuzzers(t *testing.T) []packageLocalVersionFuzzer {
	t.Helper()

	var fuzzers []packageLocalVersionFuzzer
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch path {
			case ".git", "op", "testdata", "vm/cross-emulate-test":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, "_test.go") || strings.Contains(filepath.Base(path), "cross_emulator") {
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
			if !versionFuzzFunctionUsesSupportedRangeMapper(fn) {
				continue
			}
			fuzzers = append(fuzzers, packageLocalVersionFuzzer{
				path: filepath.ToSlash(path),
				name: fn.Name.Name,
				fn:   fn,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan supported-range version fuzzers: %v", err)
	}
	if len(fuzzers) == 0 {
		t.Fatal("supported-range version fuzzer scan found no fuzzers")
	}
	return fuzzers
}

func nonCrossVersionNamedFuzzers(t *testing.T) []packageLocalVersionFuzzer {
	t.Helper()

	var fuzzers []packageLocalVersionFuzzer
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch path {
			case ".git", "op", "testdata", "vm/cross-emulate-test":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, "_test.go") || strings.Contains(filepath.Base(path), "cross_emulator") {
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
			if !nonCrossVersionNamedFuzzerName(fn.Name.Name) {
				continue
			}
			fuzzers = append(fuzzers, packageLocalVersionFuzzer{
				path: filepath.ToSlash(path),
				name: fn.Name.Name,
				fn:   fn,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan non-cross version-named fuzzers: %v", err)
	}
	if len(fuzzers) == 0 {
		t.Fatal("non-cross version-named fuzzer scan found no fuzzers")
	}
	return fuzzers
}

func nonCrossVersionNamedTests(t *testing.T) []packageLocalVersionFuzzer {
	t.Helper()

	var tests []packageLocalVersionFuzzer
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch path {
			case ".git", "op", "testdata", "vm/cross-emulate-test":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, "_test.go") ||
			strings.Contains(filepath.Base(path), "cross_emulator") ||
			strings.Contains(filepath.Base(path), "inventory_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			if !nonCrossVersionNamedFuzzerName(fn.Name.Name) {
				continue
			}
			tests = append(tests, packageLocalVersionFuzzer{
				path: filepath.ToSlash(path),
				name: fn.Name.Name,
				fn:   fn,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan non-cross version-named tests: %v", err)
	}
	if len(tests) == 0 {
		t.Fatal("non-cross version-named test scan found no tests")
	}
	return tests
}

func nonCrossVersionNamedFuzzerName(name string) bool {
	return strings.Contains(name, "GlobalVersion") ||
		strings.Contains(name, "Versioned") ||
		strings.Contains(name, "AcrossGlobalVersions") ||
		strings.Contains(name, "PerRun") ||
		strings.Contains(name, "V9Boundary") ||
		strings.Contains(name, "V14Boundary")
}

func nonCrossVersionNamedFuzzerHasSupportedRangeCoverage(fuzz packageLocalVersionFuzzer) bool {
	if versionFuzzFunctionUsesSupportedRangeMapper(fuzz.fn) && supportedRangeVersionFuzzerSeedCoverage(fuzz.fn).fullRange {
		return true
	}
	if nonCrossVersionNamedFuzzerUsesMapperHelper(fuzz) && supportedRangeVersionFuzzerSeedCoverage(fuzz.fn).fullRange {
		return true
	}
	return transactionVersionFuzzerRuntimeLoopsAllVersions(fuzz.fn)
}

func nonCrossVersionNamedFuzzerUsesMapperHelper(fuzz packageLocalVersionFuzzer) bool {
	switch fuzz.path + ":" + fuzz.name {
	case "runvm_test.go:FuzzRunVMVersionedChildOpcodeMatrix",
		"runvm_test.go:FuzzRunVMXVersionedChildOpcodeMatrix":
		return true
	default:
		return false
	}
}

func nonCrossVersionNamedFuzzerHasTargetedBoundaryInventory(fuzz packageLocalVersionFuzzer) bool {
	if fuzz.path == "transaction_version_fuzz_test.go" {
		_, ok := expectedTransactionTargetedBoundaryVersions[fuzz.name]
		return ok
	}
	switch fuzz.path + ":" + fuzz.name {
	case "execution_proof_test.go:FuzzExecuteDetailedWithAccountProofLibraryCodeCellStartupV9Boundary",
		"vm_test.go:FuzzTVMLibraryCodeCellStartupV9BoundaryCosts":
		return len(transactionTargetedBoundaryVersions(fuzz.fn)) > 0
	default:
		return false
	}
}

func versionFuzzerInventoryHash(fuzzers []packageLocalVersionFuzzer) string {
	items := make([]string, 0, len(fuzzers))
	for _, fuzz := range fuzzers {
		items = append(items, fuzz.path+":"+fuzz.name)
	}
	sort.Strings(items)
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func versionFuzzerInventoryList(fuzzers []packageLocalVersionFuzzer) string {
	items := make([]string, 0, len(fuzzers))
	for _, fuzz := range fuzzers {
		items = append(items, fuzz.path+":"+fuzz.name)
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}

type supportedRangeVersionSeeds struct {
	min       bool
	max       bool
	fullRange bool
}

func supportedRangeVersionFuzzerSeedCoverage(fn *ast.FuncDecl) supportedRangeVersionSeeds {
	var seeds supportedRangeVersionSeeds
	versionSeedArgIndexes := supportedRangeVersionMappedSeedArgIndexes(fn)
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			if versionFuzzFullRangeSeedHelperCall(node.Fun) {
				seeds.min = true
				seeds.max = true
				seeds.fullRange = true
				return true
			}
			if !packageLocalFuzzAddCall(node.Fun) {
				return true
			}
			for idx, arg := range node.Args {
				if len(versionSeedArgIndexes) > 0 {
					if _, ok := versionSeedArgIndexes[idx]; !ok {
						continue
					}
				}
				if versionFuzzExprContainsSupportedMin(arg) {
					seeds.min = true
				}
				if versionFuzzExprContainsSupportedMax(arg) {
					seeds.max = true
				}
			}
		case *ast.ForStmt:
			versionVar := supportedRangeForLoopVar(node)
			if versionVar != "" && versionFuzzForBodyAddsFuzzVersionSeed(node.Body, versionVar, versionSeedArgIndexes) {
				seeds.min = true
				seeds.max = true
				seeds.fullRange = true
			}
		case *ast.RangeStmt:
			versionVar := transactionFuzzAllVersionsRangeLoopVar(node)
			if versionVar != "" && versionFuzzForBodyAddsFuzzVersionSeed(node.Body, versionVar, versionSeedArgIndexes) {
				seeds.min = true
				seeds.max = true
				seeds.fullRange = true
			}
		}
		return true
	})
	return seeds
}

func supportedRangeVersionMappedSeedArgIndexes(fn *ast.FuncDecl) map[int]struct{} {
	paramIndexes := make(map[string]int)
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !versionFuzzFuzzFuncCall(call) || len(call.Args) != 1 {
			return true
		}
		fnLit, ok := call.Args[0].(*ast.FuncLit)
		if !ok || fnLit.Type.Params == nil {
			return true
		}
		idx := 0
		for _, field := range fnLit.Type.Params.List {
			for _, name := range field.Names {
				if idx > 0 {
					paramIndexes[name.Name] = idx - 1
				}
				idx++
			}
			if len(field.Names) == 0 {
				idx++
			}
		}
		return false
	})

	indexes := make(map[int]struct{})
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !versionFuzzSupportedRangeMapperCall(call.Fun) || len(call.Args) == 0 {
			return true
		}
		for _, name := range versionFuzzIdentNames(call.Args[0]) {
			if idx, ok := paramIndexes[name]; ok {
				indexes[idx] = struct{}{}
			}
		}
		return true
	})
	return indexes
}

func versionFuzzFuzzFuncCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Fuzz" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "f"
}

func versionFuzzIdentNames(expr ast.Expr) []string {
	seen := make(map[string]struct{})
	ast.Inspect(expr, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok && ident.Name != "" {
			seen[ident.Name] = struct{}{}
		}
		return true
	})

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names
}

func versionFuzzFullRangeSeedHelperCall(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	switch ident.Name {
	case "registeredOpcodeAvailabilityFuzzSeeds",
		"opcodeMinGlobalVersionRepresentativeFuzzSeeds":
		return true
	default:
		return false
	}
}

func packageLocalVersionFuzzerSeedsDefaultRange(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		stmt, ok := node.(*ast.ForStmt)
		if !ok {
			return true
		}
		versionVar := packageLocalDefaultVersionForLoopVar(stmt)
		if versionVar == "" || !packageLocalForBodyAddsFuzzVersionSeed(stmt.Body, versionVar) {
			return true
		}
		found = true
		return false
	})
	return found
}

func packageLocalVersionFuzzerUsesDefaultRangeMapper(fuzz packageLocalVersionFuzzer) bool {
	mapper := expectedPackageLocalVersionMappers[packageLocalVersionFuzzerPackage(fuzz.path)]
	if mapper == "" {
		return false
	}

	found := false
	ast.Inspect(fuzz.fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if ok && ident.Name == mapper {
			found = true
			return false
		}
		return true
	})
	return found
}

func versionFuzzFunctionUsesSupportedRangeMapper(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && versionFuzzSupportedRangeMapperCall(call.Fun) {
			found = true
			return false
		}
		expr, ok := node.(ast.Expr)
		if !ok || !versionFuzzExprUsesMaxSupportedGlobalVersionModulo(expr) {
			return true
		}
		found = true
		return false
	})
	return found
}

func versionFuzzSupportedRangeMapperCall(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	switch ident.Name {
	case "tvmFuzzGlobalVersion",
		"tvmFuzzGlobalVersionByte",
		"tvmFuzzGlobalVersionSeed",
		"tvmFuzzGlobalVersionUint32",
		"transactionFuzzGlobalVersion",
		"fuzzExecutionConfigVersion",
		"fuzzOpcodeVersion":
		return true
	default:
		return false
	}
}

func versionFuzzExprUsesMaxSupportedGlobalVersionModulo(expr ast.Expr) bool {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Op != token.REM {
		return false
	}
	return versionFuzzExprContainsSupportedMax(bin.Y)
}

func supportedRangeForLoopVar(stmt *ast.ForStmt) string {
	init, ok := stmt.Init.(*ast.AssignStmt)
	if !ok || len(init.Lhs) != 1 || len(init.Rhs) != 1 || init.Tok != token.DEFINE {
		return ""
	}
	ident, ok := init.Lhs[0].(*ast.Ident)
	if !ok || !versionFuzzExprContainsSupportedMin(init.Rhs[0]) {
		return ""
	}

	cond, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.LEQ || !packageLocalExprContainsIdent(cond.X, ident.Name) || !versionFuzzExprContainsSupportedMax(cond.Y) {
		return ""
	}

	post, ok := stmt.Post.(*ast.IncDecStmt)
	if !ok || post.Tok != token.INC || !packageLocalExprContainsIdent(post.X, ident.Name) {
		return ""
	}
	return ident.Name
}

func transactionFuzzAllVersionsRangeLoopVar(stmt *ast.RangeStmt) string {
	if !packageLocalExprContainsIdent(stmt.X, "transactionFuzzAllVersions") {
		return ""
	}
	ident, ok := stmt.Value.(*ast.Ident)
	if !ok || ident.Name == "_" {
		return ""
	}
	return ident.Name
}

func versionFuzzExprContainsSupportedMin(expr ast.Expr) bool {
	return versionFuzzExprIsZeroLiteral(expr)
}

func versionFuzzExprContainsSupportedMax(expr ast.Expr) bool {
	if packageLocalExprContainsIdent(expr, "MaxSupportedGlobalVersion") {
		return true
	}

	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "MaxSupportedGlobalVersion" {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if ok && (ident.Name == "vm" || ident.Name == "vmcore") {
			found = true
			return false
		}
		return true
	})
	return found
}

func versionFuzzExprIsZeroLiteral(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		return expr.Kind == token.INT && expr.Value == "0"
	case *ast.CallExpr:
		return len(expr.Args) == 1 && versionFuzzExprIsZeroLiteral(expr.Args[0])
	default:
		return false
	}
}

func packageLocalDefaultVersionForLoopVar(stmt *ast.ForStmt) string {
	init, ok := stmt.Init.(*ast.AssignStmt)
	if !ok || len(init.Lhs) != 1 || len(init.Rhs) != 1 || init.Tok != token.DEFINE {
		return ""
	}
	ident, ok := init.Lhs[0].(*ast.Ident)
	if !ok || !packageLocalExprIsZero(init.Rhs[0]) {
		return ""
	}

	cond, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.LEQ || !packageLocalExprContainsIdent(cond.X, ident.Name) || !packageLocalExprContainsMaxSupportedGlobalVersion(cond.Y) {
		return ""
	}

	post, ok := stmt.Post.(*ast.IncDecStmt)
	if !ok || post.Tok != token.INC || !packageLocalExprContainsIdent(post.X, ident.Name) {
		return ""
	}
	return ident.Name
}

func packageLocalForBodyAddsFuzzVersionSeed(body *ast.BlockStmt, versionVar string) bool {
	return versionFuzzForBodyAddsFuzzVersionSeed(body, versionVar, nil)
}

func versionFuzzForBodyAddsFuzzVersionSeed(body *ast.BlockStmt, versionVar string, versionSeedArgIndexes map[int]struct{}) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !packageLocalFuzzAddCall(call.Fun) {
			return true
		}
		for idx, arg := range call.Args {
			if len(versionSeedArgIndexes) > 0 {
				if _, ok := versionSeedArgIndexes[idx]; !ok {
					continue
				}
			}
			if packageLocalExprContainsIdent(arg, versionVar) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func packageLocalFuzzAddCall(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Add" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "f"
}

func packageLocalExprIsZero(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		return expr.Kind == token.INT && expr.Value == "0"
	case *ast.CallExpr:
		return len(expr.Args) == 1 && packageLocalExprIsZero(expr.Args[0])
	default:
		return false
	}
}

func packageLocalExprContainsMaxSupportedGlobalVersion(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "MaxSupportedGlobalVersion" {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if ok && ident.Name == "vm" {
			found = true
			return false
		}
		return true
	})
	return found
}

func packageLocalExprContainsIdent(expr ast.Expr, name string) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok && ident.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}
