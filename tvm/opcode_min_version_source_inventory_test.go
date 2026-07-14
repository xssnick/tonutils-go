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
	"strconv"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

const (
	expectedOpcodeMinVersionSourceTouchCount = 60
	expectedOpcodeMinVersionSourceTouchHash  = "7f787843427fad9d4dd2706e5e4905a3f80d954371577d7dd0e30a6d20ebc299"
)

func TestOpcodeMinGlobalVersionSourceTouchInventory(t *testing.T) {
	touches := opcodeMinVersionSourceTouchScopes(t)
	if len(touches) != expectedOpcodeMinVersionSourceTouchCount {
		t.Fatalf("opcode min-version source touch count = %d, want %d; got:\n%s", len(touches), expectedOpcodeMinVersionSourceTouchCount, strings.Join(touches, "\n"))
	}
	if hash := opcodeMinVersionSourceTouchHash(touches); hash != expectedOpcodeMinVersionSourceTouchHash {
		t.Fatalf("opcode min-version source touch hash = %s, want %s; got:\n%s", hash, expectedOpcodeMinVersionSourceTouchHash, strings.Join(touches, "\n"))
	}
}

func TestOpcodeMinGlobalVersionSourceLiteralsAreRepresented(t *testing.T) {
	registryVersions := make(map[int]struct{})
	for _, tt := range opcodeMinGlobalVersionBoundaryCases() {
		registryVersions[tt.min] = struct{}{}
	}

	for _, literal := range opcodeMinVersionSourceLiterals(t) {
		if literal <= 0 || literal > vm.MaxSupportedGlobalVersion {
			t.Fatalf("opcode MinVersion source literal %d outside (%d, %d]", literal, 0, vm.MaxSupportedGlobalVersion)
		}
		if _, ok := registryVersions[literal]; !ok {
			t.Fatalf("opcode MinVersion source literal %d has no representative in opcode min-version registry", literal)
		}
	}
}

func TestOpcodeMinGlobalVersionDynamicSourcesStayExplicit(t *testing.T) {
	got := opcodeMinVersionDynamicSourceScopes(t)
	want := []string{
		"op/funcs/tonops_params_prng.go:func:prevBlocksInfoAlias:MinVersion=minVersion",
		"op/math/quiet_compound.go:func:qCompoundOP:MinVersion=qCompoundMinVersion(args)",
	}

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("opcode dynamic MinVersion sources:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestOpcodeMinGlobalVersionSourceLiteralScannerCoversSupportedShapes(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "op/fixture.go", `
package fixture

func fixture(op *helpers.SimpleOP, minVersion int) {
	_ = &helpers.SimpleOP{MinVersion: 4}
	_ = &helpers.AdvancedOP{
		MinVersion: 6,
	}
	op.MinVersion = 9
	_ = &helpers.SimpleOP{MinVersion: minVersion}
	op.MinVersion = minVersion
}
`, 0)
	if err != nil {
		t.Fatalf("parse opcode min-version literal scanner fixture: %v", err)
	}

	literals := opcodeMinVersionSourceLiteralsFromFile(file)
	if got := opcodeMinVersionLiteralList(literals); got != "4,6,9" {
		t.Fatalf("opcode min-version literal scanner fixture = %s, want 4,6,9", got)
	}

	dynamic := opcodeMinVersionDynamicSourceScopesFromFile("op/fixture.go", file)
	wantDynamic := "op/fixture.go:func:fixture:MinVersion=minVersion\nop/fixture.go:func:fixture:op.MinVersion=minVersion"
	if got := strings.Join(dynamic, "\n"); got != wantDynamic {
		t.Fatalf("opcode min-version dynamic scanner fixture:\n%s\nwant:\n%s", got, wantDynamic)
	}
}

func opcodeMinVersionSourceTouchScopes(t *testing.T) []string {
	t.Helper()

	var touches []string
	opcodeMinVersionWalkProductionSources(t, func(path string, file *ast.File) {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					touchCount := opcodeMinVersionNodeTouchCount(spec)
					if touchCount == 0 {
						continue
					}
					name := opcodeMinVersionSpecName(spec)
					if name == "" {
						continue
					}
					touches = append(touches, fmt.Sprintf("%s:decl:%s:%d", filepath.ToSlash(path), name, touchCount))
				}
			case *ast.FuncDecl:
				if decl.Body == nil {
					continue
				}
				touchCount := opcodeMinVersionNodeTouchCount(decl.Body)
				if touchCount > 0 {
					touches = append(touches, fmt.Sprintf("%s:func:%s:%d", filepath.ToSlash(path), decl.Name.Name, touchCount))
				}
			}
		}
	})
	if len(touches) == 0 {
		t.Fatal("opcode min-version source touch scan found no scopes")
	}

	sort.Strings(touches)
	return touches
}

func opcodeMinVersionSourceLiterals(t *testing.T) []int {
	t.Helper()

	seen := make(map[int]struct{})
	opcodeMinVersionWalkProductionSources(t, func(path string, file *ast.File) {
		for _, literal := range opcodeMinVersionSourceLiteralsFromFile(file) {
			seen[literal] = struct{}{}
		}
	})
	if len(seen) == 0 {
		t.Fatal("opcode min-version source literal scan found no literals")
	}

	literals := make([]int, 0, len(seen))
	for literal := range seen {
		literals = append(literals, literal)
	}
	sort.Ints(literals)
	return literals
}

func opcodeMinVersionSourceLiteralsFromFile(file *ast.File) []int {
	seen := make(map[int]struct{})
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.KeyValueExpr:
			if !opcodeMinVersionExprIsName(node.Key) {
				return true
			}
			if literal, ok := opcodeMinVersionIntLiteral(node.Value); ok {
				seen[literal] = struct{}{}
			}
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				if i >= len(node.Rhs) || !opcodeMinVersionExprIsName(lhs) {
					continue
				}
				if literal, ok := opcodeMinVersionIntLiteral(node.Rhs[i]); ok {
					seen[literal] = struct{}{}
				}
			}
		}
		return true
	})

	literals := make([]int, 0, len(seen))
	for literal := range seen {
		literals = append(literals, literal)
	}
	sort.Ints(literals)
	return literals
}

func opcodeMinVersionDynamicSourceScopes(t *testing.T) []string {
	t.Helper()

	var scopes []string
	opcodeMinVersionWalkProductionSources(t, func(path string, file *ast.File) {
		scopes = append(scopes, opcodeMinVersionDynamicSourceScopesFromFile(filepath.ToSlash(path), file)...)
	})
	if len(scopes) == 0 {
		t.Fatal("opcode dynamic MinVersion source scan found no scopes")
	}

	sort.Strings(scopes)
	return scopes
}

func opcodeMinVersionDynamicSourceScopesFromFile(path string, file *ast.File) []string {
	var scopes []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.KeyValueExpr:
				if !opcodeMinVersionExprIsName(node.Key) {
					return true
				}
				if _, ok := opcodeMinVersionIntLiteral(node.Value); ok {
					return true
				}
				scopes = append(scopes, path+":func:"+fn.Name.Name+":"+opcodeMinVersionExprString(node.Key)+"="+opcodeMinVersionExprString(node.Value))
			case *ast.AssignStmt:
				for i, lhs := range node.Lhs {
					if i >= len(node.Rhs) || !opcodeMinVersionExprIsName(lhs) {
						continue
					}
					if _, ok := opcodeMinVersionIntLiteral(node.Rhs[i]); ok {
						continue
					}
					scopes = append(scopes, path+":func:"+fn.Name.Name+":"+opcodeMinVersionExprString(lhs)+"="+opcodeMinVersionExprString(node.Rhs[i]))
				}
			}
			return true
		})
	}
	sort.Strings(scopes)
	return scopes
}

func opcodeMinVersionWalkProductionSources(t *testing.T, visit func(path string, file *ast.File)) {
	t.Helper()

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		slashPath := filepath.ToSlash(path)
		if d.IsDir() {
			switch slashPath {
			case ".git", "testdata", "vm/cross-emulate-test":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if !strings.HasPrefix(slashPath, "op/") && slashPath != "vm/state.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		visit(path, file)
		return nil
	})
	if err != nil {
		t.Fatalf("scan opcode min-version source touches: %v", err)
	}
}

func opcodeMinVersionSpecName(spec ast.Spec) string {
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

func opcodeMinVersionNodeTouchCount(node ast.Node) int {
	if node == nil {
		return 0
	}

	count := 0
	ast.Inspect(node, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.Ident:
			if opcodeMinVersionName(node.Name) {
				count++
			}
		case *ast.SelectorExpr:
			if opcodeMinVersionName(node.Sel.Name) {
				count++
				return false
			}
		}
		return true
	})
	return count
}

func opcodeMinVersionExprIsName(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name == "MinVersion"
	case *ast.SelectorExpr:
		return expr.Sel.Name == "MinVersion"
	default:
		return false
	}
}

func opcodeMinVersionName(name string) bool {
	return name == "MinVersion" || name == "MinGlobalVersion"
}

func opcodeMinVersionIntLiteral(expr ast.Expr) (int, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return 0, false
	}
	value, err := strconv.Atoi(lit.Value)
	if err != nil {
		return 0, false
	}
	return value, true
}

func opcodeMinVersionExprString(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		return opcodeMinVersionExprString(expr.X) + "." + expr.Sel.Name
	case *ast.CallExpr:
		args := make([]string, 0, len(expr.Args))
		for _, arg := range expr.Args {
			args = append(args, opcodeMinVersionExprString(arg))
		}
		return opcodeMinVersionExprString(expr.Fun) + "(" + strings.Join(args, ",") + ")"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func opcodeMinVersionSourceTouchHash(touches []string) string {
	sum := sha256.Sum256([]byte(strings.Join(touches, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func opcodeMinVersionLiteralList(literals []int) string {
	items := make([]string, 0, len(literals))
	for _, literal := range literals {
		items = append(items, strconv.Itoa(literal))
	}
	return strings.Join(items, ",")
}
