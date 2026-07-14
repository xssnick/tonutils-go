package tvm

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var expectedGlobalVersionConfigProductionReaders = map[string]int{
	"cross_emulator_transaction_reference.go:referenceTransactionConfigRootWithGlobalVersion": 1,
	"transaction_config.go:PrepareBlockchainConfig":                                           1,
}

func TestTVMGlobalVersionPreparedBlockchainConfigAPIInventory(t *testing.T) {
	oldNames := []string{
		"Prepared" + "Config",
		"Prepare" + "Config",
		"MustPrepare" + "Config",
		"GlobalVersion" + "Set",
		"GlobalVersion" + "Configured",
		"NewExecutionState" + "With" + "GlobalVersion",
	}

	var hits []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		for _, name := range oldNames {
			if strings.Contains(text, name) {
				hits = append(hits, fmt.Sprintf("%s:%s", filepath.ToSlash(path), name))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan old global-version API names: %v", err)
	}
	if len(hits) > 0 {
		sort.Strings(hits)
		t.Fatalf("old global-version API names remain:\n%s", strings.Join(hits, "\n"))
	}

	oldExactIdentifiers := map[string]struct{}{
		"With" + "GlobalVersion": {},
		"Set" + "GlobalVersion":  {},
	}
	exactHits := globalVersionConfigExactIdentifierUsers(t, oldExactIdentifiers)
	if len(exactHits) > 0 {
		sort.Strings(exactHits)
		t.Fatalf("old exact global-version API identifiers remain:\n%s", strings.Join(exactHits, "\n"))
	}
}

func TestTVMExecutionConfigsCarryPreparedBlockchainConfig(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "vm.go", nil, 0)
	if err != nil {
		t.Fatalf("parse vm.go: %v", err)
	}

	executionConfig := globalVersionConfigStruct(file, "ExecutionConfig")
	if executionConfig == nil {
		t.Fatal("ExecutionConfig struct not found")
	}
	if !globalVersionConfigStructHasPreparedBlockchainConfigField(executionConfig, "Config") {
		t.Fatal("ExecutionConfig must expose Config *PreparedBlockchainConfig")
	}
	if fields := globalVersionConfigStructFieldsWithPrefix(executionConfig, "GlobalVersion"); len(fields) > 0 {
		t.Fatalf("ExecutionConfig still has global-version fields: %s", strings.Join(fields, ", "))
	}

	file, err = parser.ParseFile(fset, "external_message.go", nil, 0)
	if err != nil {
		t.Fatalf("parse external_message.go: %v", err)
	}
	messageConfig := globalVersionConfigStruct(file, "MessageEmulationConfig")
	if messageConfig == nil {
		t.Fatal("MessageEmulationConfig struct not found")
	}
	if !globalVersionConfigStructHasPreparedBlockchainConfigField(messageConfig, "Config") {
		t.Fatal("MessageEmulationConfig must expose Config *PreparedBlockchainConfig")
	}
	if fields := globalVersionConfigStructFieldsWithPrefix(messageConfig, "GlobalVersion"); len(fields) > 0 {
		t.Fatalf("MessageEmulationConfig still has global-version fields: %s", strings.Join(fields, ", "))
	}
}

func TestTVMExecutionConfigLiteralsSetPreparedBlockchainConfig(t *testing.T) {
	missing := globalVersionConfigExecutionLiteralsWithoutConfig(t)
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("ExecutionConfig literals without Config:\n%s", strings.Join(missing, "\n"))
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

	fn := globalVersionConfigProductionReaderFunction(t, "transaction_config.go:PrepareBlockchainConfig")
	if !globalVersionConfigFunctionCalls(fn, "validateGlobalVersion") {
		t.Fatal("PrepareBlockchainConfig must validate the config global version eagerly")
	}
}

func globalVersionConfigExecutionLiteralsWithoutConfig(t *testing.T) []string {
	t.Helper()

	var missing []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.CompositeLit)
			if !ok || !globalVersionConfigExecutionConfigLiteral(lit) {
				return true
			}
			if globalVersionConfigCompositeLiteralHasKey(lit, "Config") {
				return true
			}
			pos := fset.Position(lit.Pos())
			missing = append(missing, fmt.Sprintf("%s:%d", filepath.ToSlash(path), pos.Line))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan ExecutionConfig literals: %v", err)
	}
	return missing
}

func globalVersionConfigExecutionConfigLiteral(lit *ast.CompositeLit) bool {
	switch typ := lit.Type.(type) {
	case *ast.Ident:
		return typ.Name == "ExecutionConfig"
	case *ast.SelectorExpr:
		return typ.Sel.Name == "ExecutionConfig"
	default:
		return false
	}
}

func globalVersionConfigCompositeLiteralHasKey(lit *ast.CompositeLit, name string) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := kv.Key.(*ast.Ident)
		if ok && ident.Name == name {
			return true
		}
	}
	return false
}

func globalVersionConfigStruct(file *ast.File, name string) *ast.StructType {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != name {
				continue
			}
			st, ok := typeSpec.Type.(*ast.StructType)
			if ok {
				return st
			}
		}
	}
	return nil
}

func globalVersionConfigStructHasPreparedBlockchainConfigField(st *ast.StructType, fieldName string) bool {
	for _, field := range st.Fields.List {
		if len(field.Names) != 1 || field.Names[0].Name != fieldName {
			continue
		}
		ptr, ok := field.Type.(*ast.StarExpr)
		if !ok {
			return false
		}
		ident, ok := ptr.X.(*ast.Ident)
		return ok && ident.Name == "PreparedBlockchainConfig"
	}
	return false
}

func globalVersionConfigStructFieldsWithPrefix(st *ast.StructType, prefix string) []string {
	var out []string
	for _, field := range st.Fields.List {
		for _, name := range field.Names {
			if strings.HasPrefix(name.Name, prefix) {
				out = append(out, name.Name)
			}
		}
	}
	sort.Strings(out)
	return out
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
	return readers
}

func globalVersionConfigExactIdentifierUsers(t *testing.T, names map[string]struct{}) []string {
	t.Helper()

	var hits []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", "testdata":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.Ident:
				if _, ok := names[node.Name]; ok {
					pos := fset.Position(node.Pos())
					hits = append(hits, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(path), pos.Line, node.Name))
				}
			case *ast.SelectorExpr:
				if _, ok := names[node.Sel.Name]; ok {
					pos := fset.Position(node.Sel.Pos())
					hits = append(hits, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(path), pos.Line, node.Sel.Name))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan exact old global-version identifiers: %v", err)
	}
	return hits
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

func globalVersionConfigFunctionCallCount(fn *ast.FuncDecl, name string) int {
	count := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if globalVersionConfigCallName(call.Fun) == name {
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

func globalVersionConfigProductionReaderList(readers map[string]int) string {
	items := make([]string, 0, len(readers))
	for key, count := range readers {
		items = append(items, fmt.Sprintf("%s:%d", key, count))
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}
