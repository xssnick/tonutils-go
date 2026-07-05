package tvm

import (
	"crypto/sha256"
	"encoding/hex"
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

// Direct cell parses in opcode packages bypass the CellManager gas facade:
// gas is charged only when a trace happens to be attached to the cell. Every
// such call site must be reviewed explicitly (some are intentionally free,
// e.g. PUSHCONT code, deserialize/serialize helpers), so the inventory below
// is pinned: adding a new direct parse requires a conscious decision instead
// of silently skipping gas accounting.
const expectedOpcodeDirectCellParseCount = 14

const expectedOpcodeDirectCellParseHash = "c135d7748080fbbecddd7ff07376d14e6befcb442a03c83405eed8ae8a6c2a76"

func TestOpcodeDirectCellParsesStayInventoried(t *testing.T) {
	sites := opcodeDirectCellParseSites(t)
	if len(sites) != expectedOpcodeDirectCellParseCount {
		t.Fatalf("direct cell parse count in op/ = %d, want %d:\n%s",
			len(sites), expectedOpcodeDirectCellParseCount, strings.Join(sites, "\n"))
	}
	if got := opcodeDirectCellParseHash(sites); got != expectedOpcodeDirectCellParseHash {
		t.Fatalf("direct cell parse inventory hash = %s, want %s:\n%s",
			got, expectedOpcodeDirectCellParseHash, strings.Join(sites, "\n"))
	}
}

func opcodeDirectCellParseSites(t *testing.T) []string {
	t.Helper()

	var sites []string
	fset := token.NewFileSet()
	err := filepath.WalkDir("op", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "BeginParse", "MustBeginParse", "BeginParseWithTrace":
			default:
				return true
			}
			if receiverGoesThroughCellManager(sel.X) {
				return true
			}
			pos := fset.Position(call.Pos())
			sites = append(sites, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(path), pos.Line, sel.Sel.Name))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(sites)
	return sites
}

func receiverGoesThroughCellManager(expr ast.Expr) bool {
	for {
		switch x := expr.(type) {
		case *ast.SelectorExpr:
			if x.Sel.Name == "Cells" {
				return true
			}
			expr = x.X
		case *ast.CallExpr:
			expr = x.Fun
		case *ast.Ident:
			return false
		default:
			return false
		}
	}
}

func opcodeDirectCellParseHash(sites []string) string {
	sum := sha256.Sum256([]byte(strings.Join(sites, "\n")))
	return hex.EncodeToString(sum[:])
}
