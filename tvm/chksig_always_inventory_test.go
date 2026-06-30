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

type chksigAlwaysCoverageAnchor struct {
	path          string
	fullRangeFuzz bool
}

var expectedChksigAlwaysCoverageAnchors = map[string]chksigAlwaysCoverageAnchor{
	"execution_config_fuzz_test.go:FuzzExecutionConfigGlobalVersionPerRunEntrypoints":                  {path: "execution_config_fuzz_test.go", fullRangeFuzz: true},
	"execution_config_fuzz_test.go:FuzzExecutionConfigChksigAlwaysSucceedRawEntrypoints":               {path: "execution_config_fuzz_test.go", fullRangeFuzz: true},
	"execution_config_fuzz_test.go:FuzzExecutionConfigChksigAlwaysSucceedSignatureVariants":            {path: "execution_config_fuzz_test.go", fullRangeFuzz: true},
	"execution_config_fuzz_test.go:FuzzExecutionConfigChksigAlwaysSucceedMalformedOperands":            {path: "execution_config_fuzz_test.go", fullRangeFuzz: true},
	"execution_config_fuzz_test.go:FuzzExecutionConfigLibrariesAndChksigAlwaysSucceed":                 {path: "execution_config_fuzz_test.go", fullRangeFuzz: true},
	"execution_config_fuzz_test.go:FuzzExecutionConfigChksigAlwaysSucceedRunVMChild":                   {path: "execution_config_fuzz_test.go", fullRangeFuzz: true},
	"execution_proof_test.go:TestExecuteDetailedWithAccountProofChksigAlwaysSucceedPerRun":             {path: "execution_proof_test.go"},
	"internal_message_test.go:TestEmulateInternalMessageChksigAlwaysSucceedPerRun":                     {path: "internal_message_test.go"},
	"op/funcs/tonops_ops_test.go:TestChksignAlwaysSucceed":                                             {path: "op/funcs/tonops_ops_test.go"},
	"op/funcs/tonops_ops_test.go:TestChksignRejectsZeroAndIdentityPublicKeyV14":                        {path: "op/funcs/tonops_ops_test.go"},
	"op/funcs/version_fuzz_test.go:FuzzTVMVersionedCryptoV14Edges":                                     {path: "op/funcs/version_fuzz_test.go", fullRangeFuzz: true},
	"runvm_test.go:FuzzRunVMChksigAlwaysSucceedPerRun":                                                 {path: "runvm_test.go", fullRangeFuzz: true},
	"runvm_test.go:FuzzRunVMXChksigAlwaysSucceedPerRun":                                                {path: "runvm_test.go", fullRangeFuzz: true},
	"runvm_test.go:TestRunVMChksigAlwaysSucceedPerRun":                                                 {path: "runvm_test.go"},
	"ticktock_message_test.go:TestEmulateTickTockTransactionChksigAlwaysSucceedPerRun":                 {path: "ticktock_message_test.go"},
	"transaction_emulation_test.go:TestCheckExternalMessageAcceptedChksigAlwaysSucceedPerRun":          {path: "transaction_emulation_test.go"},
	"transaction_emulation_test.go:TestEmulateTransactionChksigAlwaysSucceedPerRun":                    {path: "transaction_emulation_test.go"},
	"transaction_emulation_test.go:TestTransactionConfigForExternalMessageAcceptedCopiesRuntimeFields": {path: "transaction_emulation_test.go"},
	"transaction_version_fuzz_test.go:FuzzCheckExternalMessageAcceptedChksigAlwaysSucceedPerRun":       {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzInternalMessageEmulationChksigAlwaysSucceedPerRun":           {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzMessageEmulationBuildProofChksigAlwaysSucceedPerRun":         {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzMessageEmulationChksigAlwaysSucceedPerRun":                   {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzMessageEmulationGlobalVersionPerRun":                         {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzTickTockChksigAlwaysSucceedPerRun":                           {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzTransactionEmulationBuildProofChksigAlwaysSucceedPerRun":     {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"vm_test.go:TestTVMExecuteConfigChksigAlwaysSucceedPerRun":                                         {path: "vm_test.go"},
	"vm/state_child_test.go:TestChildVMHelpersAndExecution":                                            {path: "vm/state_child_test.go"},
	"vm/state_more_test.go:TestStateRunChildAndParamHelpers":                                           {path: "vm/state_more_test.go"},
}

const (
	expectedChksigAlwaysProductionTouchCount = 14
	expectedChksigAlwaysProductionTouchHash  = "1ea8efb842506ae8f742226d392e59b8ca6d960bf86a88acfa0eb6d872b72e18"
	expectedChksigAlwaysStateWriterCount     = 2
	expectedChksigAlwaysStateWriterHash      = "362178a87042e084f7cf70c2412e488f69c7393e77c93c9ddfafcefba8e3fa8f"
)

func TestTVMChksigAlwaysSucceedCoverageInventory(t *testing.T) {
	anchors := chksigAlwaysCoverageAnchors(t)
	if len(anchors) != len(expectedChksigAlwaysCoverageAnchors) {
		t.Fatalf("chksig-always coverage anchor count = %d, want %d; got:\n%s", len(anchors), len(expectedChksigAlwaysCoverageAnchors), chksigAlwaysCoverageAnchorList(anchors))
	}

	for key, want := range expectedChksigAlwaysCoverageAnchors {
		fn, ok := anchors[key]
		if !ok {
			t.Fatalf("missing chksig-always coverage anchor %s; got:\n%s", key, chksigAlwaysCoverageAnchorList(anchors))
		}
		if gotPath := strings.Split(key, ":")[0]; gotPath != want.path {
			t.Fatalf("chksig-always coverage anchor %s path = %s, want %s", key, gotPath, want.path)
		}
		name := strings.Split(key, ":")[1]
		if !chksigAlwaysFunctionNameIsInventoryCandidate(name) && !chksigAlwaysFunctionTouchesFlag(fn) {
			t.Fatalf("chksig-always coverage anchor %s does not touch ChksigAlwaysSucceed", key)
		}
		if want.fullRangeFuzz {
			if !strings.Contains(key, ":Fuzz") {
				t.Fatalf("chksig-always coverage anchor %s is marked full-range but is not a fuzzer", key)
			}
			if !chksigAlwaysFuzzerUsesVersionMapper(fn) {
				t.Fatalf("chksig-always coverage anchor %s does not use a supported global-version mapper", key)
			}
			if !chksigAlwaysFuzzerSeedsFullRange(fn) {
				t.Fatalf("chksig-always coverage anchor %s does not seed the full supported global-version range", key)
			}
		}
	}
	for key := range anchors {
		if _, ok := expectedChksigAlwaysCoverageAnchors[key]; !ok {
			t.Fatalf("unexpected chksig-always coverage anchor %s; update the inventory intentionally", key)
		}
	}

	var missingTouches []string
	for key := range chksigAlwaysFlagTouchingTests(t) {
		if _, ok := expectedChksigAlwaysCoverageAnchors[key]; !ok {
			missingTouches = append(missingTouches, key)
		}
	}
	if len(missingTouches) > 0 {
		sort.Strings(missingTouches)
		t.Fatalf("test/fuzz functions touching ChksigAlwaysSucceed without explicit inventory:\n%s", strings.Join(missingTouches, "\n"))
	}
}

func TestTVMChksigAlwaysSucceedProductionTouchInventory(t *testing.T) {
	touches := chksigAlwaysProductionTouchScopes(t)
	if len(touches) != expectedChksigAlwaysProductionTouchCount {
		t.Fatalf("production ChksigAlwaysSucceed touch count = %d, want %d; got:\n%s", len(touches), expectedChksigAlwaysProductionTouchCount, strings.Join(touches, "\n"))
	}

	if hash := chksigAlwaysTouchScopeHash(touches); hash != expectedChksigAlwaysProductionTouchHash {
		t.Fatalf("production ChksigAlwaysSucceed touch hash = %s, want %s; got:\n%s", hash, expectedChksigAlwaysProductionTouchHash, strings.Join(touches, "\n"))
	}
}

func TestTVMChksigAlwaysSucceedStateWritersStayPerRun(t *testing.T) {
	writers := chksigAlwaysStateWriterScopes(t)
	if len(writers) != expectedChksigAlwaysStateWriterCount {
		t.Fatalf("production State.ChksigAlwaysSucceed writer count = %d, want %d; got:\n%s", len(writers), expectedChksigAlwaysStateWriterCount, strings.Join(writers, "\n"))
	}
	if hash := chksigAlwaysTouchScopeHash(writers); hash != expectedChksigAlwaysStateWriterHash {
		t.Fatalf("production State.ChksigAlwaysSucceed writer hash = %s, want %s; got:\n%s", hash, expectedChksigAlwaysStateWriterHash, strings.Join(writers, "\n"))
	}
}

func TestTVMChksigAlwaysSucceedStateWriterSourcesStayPerRun(t *testing.T) {
	sources := chksigAlwaysStateWriterSourceScopes(t)
	want := []string{
		"vm.go:func:executeWithOptions:state.ChksigAlwaysSucceed=options.chksigAlwaysSucceed",
		"vm/state.go:func:prepareChildForRun:child.ChksigAlwaysSucceed=s.ChksigAlwaysSucceed",
	}

	if got, expected := strings.Join(sources, "\n"), strings.Join(want, "\n"); got != expected {
		t.Fatalf("production State.ChksigAlwaysSucceed writer sources:\n%s\nwant:\n%s", got, expected)
	}
}

func chksigAlwaysCoverageAnchors(t *testing.T) map[string]*ast.FuncDecl {
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
		if filepath.ToSlash(path) == "chksig_always_inventory_test.go" {
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
			if _, ok = expectedChksigAlwaysCoverageAnchors[key]; ok || chksigAlwaysFunctionNameIsInventoryCandidate(fn.Name.Name) {
				anchors[key] = fn
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan chksig-always coverage anchors: %v", err)
	}
	if len(anchors) == 0 {
		t.Fatal("chksig-always coverage anchor scan found no anchors")
	}
	return anchors
}

func chksigAlwaysFunctionNameIsInventoryCandidate(name string) bool {
	return strings.Contains(name, "ChksigAlwaysSucceed") ||
		strings.Contains(name, "ChksignAlwaysSucceed")
}

func chksigAlwaysFlagTouchingTests(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()

	tests := make(map[string]*ast.FuncDecl)
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
		if !strings.HasSuffix(path, "_test.go") || filepath.ToSlash(path) == "chksig_always_inventory_test.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !chksigAlwaysTestOrFuzzName(fn.Name.Name) {
				continue
			}
			if chksigAlwaysFunctionTouchesFlag(fn) {
				tests[filepath.ToSlash(path)+":"+fn.Name.Name] = fn
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan chksig-always flag-touching tests: %v", err)
	}
	if len(tests) == 0 {
		t.Fatal("chksig-always flag-touching test scan found no tests")
	}
	return tests
}

func chksigAlwaysProductionTouchScopes(t *testing.T) []string {
	t.Helper()

	var touches []string
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
			switch decl := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					touchCount := chksigAlwaysNodeTouchCount(spec)
					if touchCount == 0 {
						continue
					}
					name := chksigAlwaysSpecName(spec)
					if name == "" {
						continue
					}
					touches = append(touches, fmt.Sprintf("%s:decl:%s:%d", filepath.ToSlash(path), name, touchCount))
				}
			case *ast.FuncDecl:
				if decl.Body == nil {
					continue
				}
				touchCount := chksigAlwaysNodeTouchCount(decl.Body)
				if touchCount > 0 {
					touches = append(touches, fmt.Sprintf("%s:func:%s:%d", filepath.ToSlash(path), decl.Name.Name, touchCount))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production ChksigAlwaysSucceed touch scopes: %v", err)
	}
	if len(touches) == 0 {
		t.Fatal("production ChksigAlwaysSucceed touch scan found no scopes")
	}

	sort.Strings(touches)
	return touches
}

func chksigAlwaysStateWriterScopes(t *testing.T) []string {
	t.Helper()

	var writers []string
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
			writers = append(writers, chksigAlwaysStateWriterScopesInFunc(filepath.ToSlash(path), fn)...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production State.ChksigAlwaysSucceed writers: %v", err)
	}
	if len(writers) == 0 {
		t.Fatal("production State.ChksigAlwaysSucceed writer scan found no writers")
	}
	sort.Strings(writers)
	return writers
}

func chksigAlwaysStateWriterScopesInFunc(path string, fn *ast.FuncDecl) []string {
	var writers []string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			if chksigAlwaysStateFieldSelector(lhs) {
				writers = append(writers, path+":func:"+fn.Name.Name)
			}
		}
		return true
	})
	return writers
}

func chksigAlwaysStateWriterSourceScopes(t *testing.T) []string {
	t.Helper()

	var sources []string
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
			sources = append(sources, chksigAlwaysStateWriterSourceScopesInFunc(filepath.ToSlash(path), fn)...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production State.ChksigAlwaysSucceed writer sources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("production State.ChksigAlwaysSucceed writer source scan found no writers")
	}
	sort.Strings(sources)
	return sources
}

func chksigAlwaysStateWriterSourceScopesInFunc(path string, fn *ast.FuncDecl) []string {
	var sources []string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			if !chksigAlwaysStateFieldSelector(lhs) {
				continue
			}
			rhs := "<missing>"
			if i < len(assign.Rhs) {
				rhs = chksigAlwaysExprName(assign.Rhs[i])
			}
			sources = append(sources, path+":func:"+fn.Name.Name+":"+chksigAlwaysExprName(lhs)+"="+rhs)
		}
		return true
	})
	return sources
}

func chksigAlwaysExprName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		return chksigAlwaysExprName(expr.X) + "." + expr.Sel.Name
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func chksigAlwaysStateFieldSelector(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "ChksigAlwaysSucceed"
}

func chksigAlwaysSpecName(spec ast.Spec) string {
	switch spec := spec.(type) {
	case *ast.TypeSpec:
		return spec.Name.Name
	case *ast.ValueSpec:
		names := make([]string, 0, len(spec.Names))
		for _, name := range spec.Names {
			names = append(names, name.Name)
		}
		return strings.Join(names, ",")
	default:
		return ""
	}
}

func chksigAlwaysNodeTouchesFlag(node ast.Node) bool {
	return chksigAlwaysNodeTouchCount(node) > 0
}

func chksigAlwaysNodeTouchCount(node ast.Node) int {
	if node == nil {
		return 0
	}

	count := 0
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.Ident:
			if node.Name == "ChksigAlwaysSucceed" {
				count++
			}
		case *ast.SelectorExpr:
			if node.Sel.Name == "ChksigAlwaysSucceed" {
				count++
				return false
			}
		}
		return true
	})
	return count
}

func chksigAlwaysTouchScopeHash(touches []string) string {
	sum := sha256.Sum256([]byte(strings.Join(touches, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func chksigAlwaysTestOrFuzzName(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz")
}

func chksigAlwaysFunctionTouchesFlag(fn *ast.FuncDecl) bool {
	return chksigAlwaysNodeTouchesFlag(fn.Body)
}

func chksigAlwaysFuzzerUsesVersionMapper(fn *ast.FuncDecl) bool {
	if versionFuzzFunctionUsesSupportedRangeMapper(fn) {
		return true
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || !chksigAlwaysPackageLocalVersionMapper(ident.Name) {
			return true
		}
		found = true
		return false
	})
	return found
}

func chksigAlwaysPackageLocalVersionMapper(name string) bool {
	switch name {
	case "fuzzCellSliceVersion",
		"fuzzDictVersion",
		"fuzzExecVersion",
		"fuzzFuncsVersion",
		"fuzzMathVersion",
		"fuzzStackVersion",
		"fuzzTupleVersion":
		return true
	default:
		return false
	}
}

func chksigAlwaysFuzzerSeedsFullRange(fn *ast.FuncDecl) bool {
	if supportedRangeVersionFuzzerSeedCoverage(fn).fullRange {
		return true
	}
	return packageLocalVersionFuzzerSeedsDefaultRange(fn)
}

func chksigAlwaysCoverageAnchorList(anchors map[string]*ast.FuncDecl) string {
	items := make([]string, 0, len(anchors))
	for key := range anchors {
		items = append(items, key)
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}
