package vm

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
	expectedVMVersionFuzzerCount           = 5
	expectedVMVersionFuzzerHash            = "1f72b7048c86f34ac60a06229f218ddee96b4ea2e20da7a6b3f14fb544f14ed9"
	expectedVMFullRangeVersionFuzzerCount  = 5
	expectedVMFullRangeVersionFuzzerHash   = "1f72b7048c86f34ac60a06229f218ddee96b4ea2e20da7a6b3f14fb544f14ed9"
	expectedVMPartialVersionFuzzerCount    = 0
	expectedVMPartialVersionFuzzerHash     = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	expectedVMStateGlobalVersionWriters    = 2
	expectedVMStateGlobalVersionWriterHash = "100e34d1ef68518f532d9b4c02afb80de2942f2e08aaca298d34593b4d3f9f10"
	expectedVMFreeGasCounterWriters        = 11
	expectedVMFreeGasCounterWriterHash     = "e0df6d34fab43f52f528ff588a02814a4ba05be7acf7b52925616192b003e564"
)

type vmVersionFuzzer struct {
	path string
	name string
	fn   *ast.FuncDecl
}

func TestVMVersionFuzzersSeedDefaultRange(t *testing.T) {
	fuzzers := vmVersionFuzzers(t)
	if len(fuzzers) != expectedVMVersionFuzzerCount {
		t.Fatalf("vm version fuzzer count = %d, want %d", len(fuzzers), expectedVMVersionFuzzerCount)
	}
	if got := vmVersionFuzzerInventoryHash(fuzzers); got != expectedVMVersionFuzzerHash {
		t.Fatalf("vm version fuzzer hash = %s, want %s", got, expectedVMVersionFuzzerHash)
	}

	var missing []string
	for _, fuzz := range fuzzers {
		if !vmVersionFuzzerSeedsDefaultRange(fuzz.fn) {
			missing = append(missing, fuzz.path+":"+fuzz.name)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("vm version fuzzers without full default-version f.Add seed loops:\n%s", strings.Join(missing, "\n"))
	}
}

func TestVMVersionFuzzersSeedFullDefaultRange(t *testing.T) {
	fuzzers := vmVersionFuzzers(t)

	var fullRange []vmVersionFuzzer
	var partial []vmVersionFuzzer
	for _, fuzz := range fuzzers {
		if vmVersionFuzzerSeedsDefaultRange(fuzz.fn) {
			fullRange = append(fullRange, fuzz)
		} else {
			partial = append(partial, fuzz)
		}
	}

	if len(fullRange) != expectedVMFullRangeVersionFuzzerCount {
		t.Fatalf("vm full-range version fuzzer count = %d, want %d:\n%s", len(fullRange), expectedVMFullRangeVersionFuzzerCount, vmVersionFuzzerInventoryList(fullRange))
	}
	if got := vmVersionFuzzerInventoryHash(fullRange); got != expectedVMFullRangeVersionFuzzerHash {
		t.Fatalf("vm full-range version fuzzer hash = %s, want %s:\n%s", got, expectedVMFullRangeVersionFuzzerHash, vmVersionFuzzerInventoryList(fullRange))
	}
	if len(partial) != expectedVMPartialVersionFuzzerCount {
		t.Fatalf("vm partial version fuzzer count = %d, want %d:\n%s", len(partial), expectedVMPartialVersionFuzzerCount, vmVersionFuzzerInventoryList(partial))
	}
	if got := vmVersionFuzzerInventoryHash(partial); got != expectedVMPartialVersionFuzzerHash {
		t.Fatalf("vm partial version fuzzer hash = %s, want %s:\n%s", got, expectedVMPartialVersionFuzzerHash, vmVersionFuzzerInventoryList(partial))
	}
}

func TestVMStateGlobalVersionWritersStayCentralized(t *testing.T) {
	writers := vmStateGlobalVersionWriters(t)
	if len(writers) != expectedVMStateGlobalVersionWriters {
		t.Fatalf("vm State global-version writer count = %d, want %d:\n%s", len(writers), expectedVMStateGlobalVersionWriters, strings.Join(writers, "\n"))
	}
	if got := vmStateGlobalVersionWriterHash(writers); got != expectedVMStateGlobalVersionWriterHash {
		t.Fatalf("vm State global-version writer hash = %s, want %s:\n%s", got, expectedVMStateGlobalVersionWriterHash, strings.Join(writers, "\n"))
	}
}

func TestVMFreeGasCounterWritersStayCentralized(t *testing.T) {
	writers := vmFreeGasCounterWriters(t)
	if len(writers) != expectedVMFreeGasCounterWriters {
		t.Fatalf("vm free-gas counter writer count = %d, want %d:\n%s", len(writers), expectedVMFreeGasCounterWriters, strings.Join(writers, "\n"))
	}
	if got := vmFreeGasCounterWriterHash(writers); got != expectedVMFreeGasCounterWriterHash {
		t.Fatalf("vm free-gas counter writer hash = %s, want %s:\n%s", got, expectedVMFreeGasCounterWriterHash, strings.Join(writers, "\n"))
	}
}

func vmVersionFuzzers(t *testing.T) []vmVersionFuzzer {
	t.Helper()

	var fuzzers []vmVersionFuzzer
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "version_fuzz_inventory_test.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !vmVersionFuzzerName(fn.Name.Name) {
				continue
			}
			fuzzers = append(fuzzers, vmVersionFuzzer{
				path: filepath.ToSlash(path),
				name: fn.Name.Name,
				fn:   fn,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan vm version fuzzers: %v", err)
	}
	if len(fuzzers) == 0 {
		t.Fatal("vm version fuzzer scan found no fuzzers")
	}
	return fuzzers
}

func vmStateGlobalVersionWriters(t *testing.T) []string {
	t.Helper()

	var writers []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." {
				return filepath.SkipDir
			}
			return nil
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
			writers = append(writers, vmStateGlobalVersionWritersInFunc(filepath.ToSlash(path), fn)...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan vm State global-version writers: %v", err)
	}
	if len(writers) == 0 {
		t.Fatal("vm State global-version writer scan found no writers")
	}
	sort.Strings(writers)
	return writers
}

func vmFreeGasCounterWriters(t *testing.T) []string {
	t.Helper()

	var writers []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." {
				return filepath.SkipDir
			}
			return nil
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
			writers = append(writers, vmFreeGasCounterWritersInFunc(fset, filepath.ToSlash(path), fn)...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan vm free-gas counter writers: %v", err)
	}
	if len(writers) == 0 {
		t.Fatal("vm free-gas counter writer scan found no writers")
	}
	sort.Strings(writers)
	return writers
}

func vmFreeGasCounterWritersInFunc(fset *token.FileSet, path string, fn *ast.FuncDecl) []string {
	var writers []string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				if field := vmFreeGasCounterSelector(lhs); field != "" {
					pos := fset.Position(lhs.Pos())
					writers = append(writers, fmt.Sprintf("%s:%d:%s:assign:%s", path, pos.Line, fn.Name.Name, field))
				}
			}
		case *ast.IncDecStmt:
			if field := vmFreeGasCounterSelector(node.X); field != "" {
				pos := fset.Position(node.Pos())
				writers = append(writers, fmt.Sprintf("%s:%d:%s:incdec:%s", path, pos.Line, fn.Name.Name, field))
			}
		}
		return true
	})
	return writers
}

func vmFreeGasCounterSelector(expr ast.Expr) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || !vmFreeGasCounterField(sel.Sel.Name) {
		return ""
	}
	return sel.Sel.Name
}

func vmFreeGasCounterField(name string) bool {
	return name == "SignatureCheckCounter" || name == "GetExtraBalanceCounter"
}

func vmFreeGasCounterWriterHash(writers []string) string {
	sum := sha256.Sum256([]byte(strings.Join(writers, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func vmStateGlobalVersionWritersInFunc(path string, fn *ast.FuncDecl) []string {
	var writers []string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				if field := vmStateGlobalVersionSelector(lhs); field != "" {
					writers = append(writers, path+":"+fn.Name.Name+":assign:"+field)
				}
			}
		case *ast.CompositeLit:
			if !vmStateCompositeLit(node) {
				return true
			}
			for _, elt := range node.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if ok && vmStateGlobalVersionField(key.Name) {
					writers = append(writers, path+":"+fn.Name.Name+":literal:"+key.Name)
				}
			}
		}
		return true
	})
	return writers
}

func vmStateGlobalVersionSelector(expr ast.Expr) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || !vmStateGlobalVersionField(sel.Sel.Name) {
		return ""
	}
	return sel.Sel.Name
}

func vmStateGlobalVersionField(name string) bool {
	return name == "GlobalVersion"
}

func vmStateCompositeLit(lit *ast.CompositeLit) bool {
	switch expr := lit.Type.(type) {
	case *ast.Ident:
		return expr.Name == "State"
	case *ast.SelectorExpr:
		return expr.Sel.Name == "State"
	default:
		return false
	}
}

func vmStateGlobalVersionWriterHash(writers []string) string {
	sum := sha256.Sum256([]byte(strings.Join(writers, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func vmVersionFuzzerName(name string) bool {
	return strings.HasPrefix(name, "Fuzz") &&
		(strings.Contains(name, "GlobalVersion") ||
			strings.Contains(name, "Versioned") ||
			strings.Contains(name, "VersionBoundary"))
}

func vmVersionFuzzerInventoryHash(fuzzers []vmVersionFuzzer) string {
	items := vmVersionFuzzerInventoryItems(fuzzers)
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func vmVersionFuzzerInventoryList(fuzzers []vmVersionFuzzer) string {
	return strings.Join(vmVersionFuzzerInventoryItems(fuzzers), "\n")
}

func vmVersionFuzzerInventoryItems(fuzzers []vmVersionFuzzer) []string {
	items := make([]string, 0, len(fuzzers))
	for _, fuzz := range fuzzers {
		items = append(items, fuzz.path+":"+fuzz.name)
	}
	sort.Strings(items)
	return items
}

func vmVersionFuzzerSeedsDefaultRange(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		stmt, ok := node.(*ast.ForStmt)
		if !ok {
			return true
		}
		versionVar := vmDefaultVersionForLoopVar(stmt)
		if versionVar == "" || !vmForBodyAddsFuzzVersionSeed(stmt.Body, versionVar) {
			return true
		}
		found = true
		return false
	})
	return found
}

func vmDefaultVersionForLoopVar(stmt *ast.ForStmt) string {
	init, ok := stmt.Init.(*ast.AssignStmt)
	if !ok || len(init.Lhs) != 1 || len(init.Rhs) != 1 || init.Tok != token.DEFINE {
		return ""
	}
	ident, ok := init.Lhs[0].(*ast.Ident)
	if !ok || !vmExprIsZero(init.Rhs[0]) {
		return ""
	}

	cond, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.LEQ || !vmExprContainsIdent(cond.X, ident.Name) || !vmExprContainsMaxSupportedGlobalVersion(cond.Y) {
		return ""
	}

	post, ok := stmt.Post.(*ast.IncDecStmt)
	if !ok || post.Tok != token.INC || !vmExprContainsIdent(post.X, ident.Name) {
		return ""
	}
	return ident.Name
}

func vmForBodyAddsFuzzVersionSeed(body *ast.BlockStmt, versionVar string) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !vmFuzzAddCall(call.Fun) {
			return true
		}
		for _, arg := range call.Args {
			if vmExprContainsIdent(arg, versionVar) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func vmFuzzAddCall(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Add" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "f"
}

func vmExprIsZero(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		return expr.Kind == token.INT && expr.Value == "0"
	case *ast.CallExpr:
		return len(expr.Args) == 1 && vmExprIsZero(expr.Args[0])
	default:
		return false
	}
}

func vmExprContainsMaxSupportedGlobalVersion(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok && ident.Name == "MaxSupportedGlobalVersion" {
			found = true
			return false
		}
		return true
	})
	return found
}

func vmExprContainsIdent(expr ast.Expr, name string) bool {
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
