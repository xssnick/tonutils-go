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

type signatureCheckAlwaysCoverageAnchor struct {
	path          string
	fullRangeFuzz bool
}

var expectedSignatureCheckAlwaysCoverageAnchors = map[string]signatureCheckAlwaysCoverageAnchor{
	"execution_config_fuzz_test.go:FuzzExecutionConfigSignatureCheckAlwaysSucceedRawEntrypoints":           {path: "execution_config_fuzz_test.go", fullRangeFuzz: true},
	"execution_config_fuzz_test.go:FuzzExecutionConfigSignatureCheckAlwaysSucceedSignatureVariants":        {path: "execution_config_fuzz_test.go", fullRangeFuzz: true},
	"execution_config_fuzz_test.go:FuzzExecutionConfigSignatureCheckAlwaysSucceedMalformedOperands":        {path: "execution_config_fuzz_test.go", fullRangeFuzz: true},
	"execution_config_fuzz_test.go:FuzzExecutionConfigLibrariesAndSignatureCheckAlwaysSucceed":             {path: "execution_config_fuzz_test.go", fullRangeFuzz: true},
	"execution_config_fuzz_test.go:FuzzExecutionConfigSignatureCheckAlwaysSucceedRunVMChild":               {path: "execution_config_fuzz_test.go", fullRangeFuzz: true},
	"execution_proof_test.go:TestExecuteDetailedWithAccountProofSignatureCheckAlwaysSucceedPerRun":         {path: "execution_proof_test.go"},
	"internal_message_test.go:TestEmulateInternalMessageSignatureCheckAlwaysSucceedPerRun":                 {path: "internal_message_test.go"},
	"op/funcs/tonops_ops_test.go:TestSignatureCheckAlwaysSucceed":                                          {path: "op/funcs/tonops_ops_test.go"},
	"op/funcs/tonops_ops_test.go:TestSignatureCheckRejectsZeroAndIdentityPublicKeyV14":                     {path: "op/funcs/tonops_ops_test.go"},
	"op/funcs/version_fuzz_test.go:FuzzTVMVersionedCryptoV14Edges":                                         {path: "op/funcs/version_fuzz_test.go", fullRangeFuzz: true},
	"runvm_test.go:FuzzRunVMSignatureCheckAlwaysSucceedPerRun":                                             {path: "runvm_test.go", fullRangeFuzz: true},
	"runvm_test.go:FuzzRunVMXSignatureCheckAlwaysSucceedPerRun":                                            {path: "runvm_test.go", fullRangeFuzz: true},
	"runvm_test.go:TestRunVMSignatureCheckAlwaysSucceedPerRun":                                             {path: "runvm_test.go"},
	"ticktock_message_test.go:TestEmulateTickTockTransactionSignatureCheckAlwaysSucceedPerRun":             {path: "ticktock_message_test.go"},
	"transaction_emulation_test.go:TestCheckExternalMessageAcceptedSignatureCheckAlwaysSucceedPerRun":      {path: "transaction_emulation_test.go"},
	"transaction_emulation_test.go:TestEmulateTransactionSignatureCheckAlwaysSucceedPerRun":                {path: "transaction_emulation_test.go"},
	"transaction_version_fuzz_test.go:FuzzCheckExternalMessageAcceptedSignatureCheckAlwaysSucceedPerRun":   {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzInternalMessageEmulationSignatureCheckAlwaysSucceedPerRun":       {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzMessageEmulationBuildProofSignatureCheckAlwaysSucceedPerRun":     {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzMessageEmulationSignatureCheckAlwaysSucceedPerRun":               {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzMessageEmulationGlobalVersionPerRun":                             {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzTickTockSignatureCheckAlwaysSucceedPerRun":                       {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"transaction_version_fuzz_test.go:FuzzTransactionEmulationBuildProofSignatureCheckAlwaysSucceedPerRun": {path: "transaction_version_fuzz_test.go", fullRangeFuzz: true},
	"vm_test.go:TestTVMExecuteConfigSignatureCheckAlwaysSucceedPerRun":                                     {path: "vm_test.go"},
	"vm/state_child_test.go:TestChildVMHelpersAndExecution":                                                {path: "vm/state_child_test.go"},
	"vm/state_more_test.go:TestStateRunChildAndParamHelpers":                                               {path: "vm/state_more_test.go"},
}

const (
	expectedSignatureCheckAlwaysProductionTouchCount = 13
	expectedSignatureCheckAlwaysProductionTouchHash  = "c19450bc4afadea545c8cd70241820cbe57d1c7462cc2135e22e0547d4f176ae"
	expectedSignatureCheckAlwaysStateWriterCount     = 2
	expectedSignatureCheckAlwaysStateWriterHash      = "362178a87042e084f7cf70c2412e488f69c7393e77c93c9ddfafcefba8e3fa8f"
)

func TestTVMSignatureCheckAlwaysSucceedCoverageInventory(t *testing.T) {
	anchors := signatureCheckAlwaysCoverageAnchors(t)
	if len(anchors) != len(expectedSignatureCheckAlwaysCoverageAnchors) {
		t.Fatalf("signature-check-always coverage anchor count = %d, want %d; got:\n%s", len(anchors), len(expectedSignatureCheckAlwaysCoverageAnchors), signatureCheckAlwaysCoverageAnchorList(anchors))
	}

	for key, want := range expectedSignatureCheckAlwaysCoverageAnchors {
		fn, ok := anchors[key]
		if !ok {
			t.Fatalf("missing signature-check-always coverage anchor %s; got:\n%s", key, signatureCheckAlwaysCoverageAnchorList(anchors))
		}
		if gotPath := strings.Split(key, ":")[0]; gotPath != want.path {
			t.Fatalf("signature-check-always coverage anchor %s path = %s, want %s", key, gotPath, want.path)
		}
		name := strings.Split(key, ":")[1]
		if !signatureCheckAlwaysFunctionNameIsInventoryCandidate(name) && !signatureCheckAlwaysFunctionTouchesFlag(fn) {
			t.Fatalf("signature-check-always coverage anchor %s does not touch SignatureCheckAlwaysSucceed", key)
		}
		if want.fullRangeFuzz {
			if !strings.Contains(key, ":Fuzz") {
				t.Fatalf("signature-check-always coverage anchor %s is marked full-range but is not a fuzzer", key)
			}
			if !signatureCheckAlwaysFuzzerUsesVersionMapper(fn) {
				t.Fatalf("signature-check-always coverage anchor %s does not use a supported global-version mapper", key)
			}
			if !signatureCheckAlwaysFuzzerSeedsFullRange(fn) {
				t.Fatalf("signature-check-always coverage anchor %s does not seed the full supported global-version range", key)
			}
		}
	}
	for key := range anchors {
		if _, ok := expectedSignatureCheckAlwaysCoverageAnchors[key]; !ok {
			t.Fatalf("unexpected signature-check-always coverage anchor %s; update the inventory intentionally", key)
		}
	}

	var missingTouches []string
	for key := range signatureCheckAlwaysFlagTouchingTests(t) {
		if _, ok := expectedSignatureCheckAlwaysCoverageAnchors[key]; !ok {
			missingTouches = append(missingTouches, key)
		}
	}
	if len(missingTouches) > 0 {
		sort.Strings(missingTouches)
		t.Fatalf("test/fuzz functions touching SignatureCheckAlwaysSucceed without explicit inventory:\n%s", strings.Join(missingTouches, "\n"))
	}
}

func TestTVMSignatureCheckAlwaysSucceedProductionTouchInventory(t *testing.T) {
	touches := signatureCheckAlwaysProductionTouchScopes(t)
	if len(touches) != expectedSignatureCheckAlwaysProductionTouchCount {
		t.Fatalf("production SignatureCheckAlwaysSucceed touch count = %d, want %d; got:\n%s", len(touches), expectedSignatureCheckAlwaysProductionTouchCount, strings.Join(touches, "\n"))
	}

	if hash := signatureCheckAlwaysTouchScopeHash(touches); hash != expectedSignatureCheckAlwaysProductionTouchHash {
		t.Fatalf("production SignatureCheckAlwaysSucceed touch hash = %s, want %s; got:\n%s", hash, expectedSignatureCheckAlwaysProductionTouchHash, strings.Join(touches, "\n"))
	}
}

func TestTVMSignatureCheckAlwaysSucceedStateWritersStayPerRun(t *testing.T) {
	writers := signatureCheckAlwaysStateWriterScopes(t)
	if len(writers) != expectedSignatureCheckAlwaysStateWriterCount {
		t.Fatalf("production State.SignatureCheckAlwaysSucceed writer count = %d, want %d; got:\n%s", len(writers), expectedSignatureCheckAlwaysStateWriterCount, strings.Join(writers, "\n"))
	}
	if hash := signatureCheckAlwaysTouchScopeHash(writers); hash != expectedSignatureCheckAlwaysStateWriterHash {
		t.Fatalf("production State.SignatureCheckAlwaysSucceed writer hash = %s, want %s; got:\n%s", hash, expectedSignatureCheckAlwaysStateWriterHash, strings.Join(writers, "\n"))
	}
}

func TestTVMSignatureCheckAlwaysSucceedStateWriterSourcesStayPerRun(t *testing.T) {
	sources := signatureCheckAlwaysStateWriterSourceScopes(t)
	want := []string{
		"vm.go:func:executeWithOptions:state.SignatureCheckAlwaysSucceed=options.signatureCheckAlwaysSucceed",
		"vm/state.go:func:prepareChildForRun:child.SignatureCheckAlwaysSucceed=s.SignatureCheckAlwaysSucceed",
	}

	if got, expected := strings.Join(sources, "\n"), strings.Join(want, "\n"); got != expected {
		t.Fatalf("production State.SignatureCheckAlwaysSucceed writer sources:\n%s\nwant:\n%s", got, expected)
	}
}

func signatureCheckAlwaysCoverageAnchors(t *testing.T) map[string]*ast.FuncDecl {
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
		if filepath.ToSlash(path) == "signature_check_always_inventory_test.go" {
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
			if _, ok = expectedSignatureCheckAlwaysCoverageAnchors[key]; ok || signatureCheckAlwaysFunctionNameIsInventoryCandidate(fn.Name.Name) {
				anchors[key] = fn
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan signature-check-always coverage anchors: %v", err)
	}
	if len(anchors) == 0 {
		t.Fatal("signature-check-always coverage anchor scan found no anchors")
	}
	return anchors
}

func signatureCheckAlwaysFunctionNameIsInventoryCandidate(name string) bool {
	return strings.Contains(name, "SignatureCheckAlwaysSucceed")
}

func signatureCheckAlwaysFlagTouchingTests(t *testing.T) map[string]*ast.FuncDecl {
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
		if !strings.HasSuffix(path, "_test.go") || filepath.ToSlash(path) == "signature_check_always_inventory_test.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !signatureCheckAlwaysTestOrFuzzName(fn.Name.Name) {
				continue
			}
			if signatureCheckAlwaysFunctionTouchesFlag(fn) {
				tests[filepath.ToSlash(path)+":"+fn.Name.Name] = fn
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan signature-check-always flag-touching tests: %v", err)
	}
	if len(tests) == 0 {
		t.Fatal("signature-check-always flag-touching test scan found no tests")
	}
	return tests
}

func signatureCheckAlwaysProductionTouchScopes(t *testing.T) []string {
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
					touchCount := signatureCheckAlwaysNodeTouchCount(spec)
					if touchCount == 0 {
						continue
					}
					name := signatureCheckAlwaysSpecName(spec)
					if name == "" {
						continue
					}
					touches = append(touches, fmt.Sprintf("%s:decl:%s:%d", filepath.ToSlash(path), name, touchCount))
				}
			case *ast.FuncDecl:
				if decl.Body == nil {
					continue
				}
				touchCount := signatureCheckAlwaysNodeTouchCount(decl.Body)
				if touchCount > 0 {
					touches = append(touches, fmt.Sprintf("%s:func:%s:%d", filepath.ToSlash(path), decl.Name.Name, touchCount))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production SignatureCheckAlwaysSucceed touch scopes: %v", err)
	}
	if len(touches) == 0 {
		t.Fatal("production SignatureCheckAlwaysSucceed touch scan found no scopes")
	}

	sort.Strings(touches)
	return touches
}

func signatureCheckAlwaysStateWriterScopes(t *testing.T) []string {
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
			writers = append(writers, signatureCheckAlwaysStateWriterScopesInFunc(filepath.ToSlash(path), fn)...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production State.SignatureCheckAlwaysSucceed writers: %v", err)
	}
	if len(writers) == 0 {
		t.Fatal("production State.SignatureCheckAlwaysSucceed writer scan found no writers")
	}
	sort.Strings(writers)
	return writers
}

func signatureCheckAlwaysStateWriterScopesInFunc(path string, fn *ast.FuncDecl) []string {
	var writers []string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			if signatureCheckAlwaysStateFieldSelector(lhs) {
				writers = append(writers, path+":func:"+fn.Name.Name)
			}
		}
		return true
	})
	return writers
}

func signatureCheckAlwaysStateWriterSourceScopes(t *testing.T) []string {
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
			sources = append(sources, signatureCheckAlwaysStateWriterSourceScopesInFunc(filepath.ToSlash(path), fn)...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production State.SignatureCheckAlwaysSucceed writer sources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("production State.SignatureCheckAlwaysSucceed writer source scan found no writers")
	}
	sort.Strings(sources)
	return sources
}

func signatureCheckAlwaysStateWriterSourceScopesInFunc(path string, fn *ast.FuncDecl) []string {
	var sources []string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			if !signatureCheckAlwaysStateFieldSelector(lhs) {
				continue
			}
			rhs := "<missing>"
			if i < len(assign.Rhs) {
				rhs = signatureCheckAlwaysExprName(assign.Rhs[i])
			}
			sources = append(sources, path+":func:"+fn.Name.Name+":"+signatureCheckAlwaysExprName(lhs)+"="+rhs)
		}
		return true
	})
	return sources
}

func signatureCheckAlwaysExprName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		return signatureCheckAlwaysExprName(expr.X) + "." + expr.Sel.Name
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func signatureCheckAlwaysStateFieldSelector(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "SignatureCheckAlwaysSucceed"
}

func signatureCheckAlwaysSpecName(spec ast.Spec) string {
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

func signatureCheckAlwaysNodeTouchesFlag(node ast.Node) bool {
	return signatureCheckAlwaysNodeTouchCount(node) > 0
}

func signatureCheckAlwaysNodeTouchCount(node ast.Node) int {
	if node == nil {
		return 0
	}

	count := 0
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.Ident:
			if node.Name == "SignatureCheckAlwaysSucceed" {
				count++
			}
		case *ast.SelectorExpr:
			if node.Sel.Name == "SignatureCheckAlwaysSucceed" {
				count++
				return false
			}
		}
		return true
	})
	return count
}

func signatureCheckAlwaysTouchScopeHash(touches []string) string {
	sum := sha256.Sum256([]byte(strings.Join(touches, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func signatureCheckAlwaysTestOrFuzzName(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz")
}

func signatureCheckAlwaysFunctionTouchesFlag(fn *ast.FuncDecl) bool {
	return signatureCheckAlwaysNodeTouchesFlag(fn.Body)
}

func signatureCheckAlwaysFuzzerUsesVersionMapper(fn *ast.FuncDecl) bool {
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
		if !ok || !signatureCheckAlwaysPackageLocalVersionMapper(ident.Name) {
			return true
		}
		found = true
		return false
	})
	return found
}

func signatureCheckAlwaysPackageLocalVersionMapper(name string) bool {
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

func signatureCheckAlwaysFuzzerSeedsFullRange(fn *ast.FuncDecl) bool {
	if supportedRangeVersionFuzzerSeedCoverage(fn).fullRange {
		return true
	}
	return packageLocalVersionFuzzerSeedsDefaultRange(fn)
}

func signatureCheckAlwaysCoverageAnchorList(anchors map[string]*ast.FuncDecl) string {
	items := make([]string, 0, len(anchors))
	for key := range anchors {
		items = append(items, key)
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}
