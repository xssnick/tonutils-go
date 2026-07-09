package tvm

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	dictop "github.com/xssnick/tonutils-go/tvm/op/dict"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

const registeredOpcodeAvailabilityAuditGasLimit = 1_000
const expectedRegisteredOpcodeAvailabilityAuditCases = 788
const expectedRegisteredOpcodeAvailabilityAuditHash = "22b112e9c0b5c1d74594ede15f88ae8666c914ef457fcff28a0ca5148f76a3db"
const expectedRegisteredOpcodeAvailabilityNonSerializableCount = 22
const expectedRegisteredOpcodeAvailabilityNonSerializableHash = "47c53f605953b7ae5e8d1988ade76ef35a4abb8b5712afdfbb3077cce085252d"

type registeredOpcodeAvailabilityAuditCase struct {
	name   string
	opName string
	code   *cell.Cell
}

type registeredOpcodeAvailabilityFuzzSeed struct {
	caseIdx int
	version int
}

func opcodeMinVersionInstructionCode(tt opcodeMinGlobalVersionCase) *cell.Cell {
	switch tt.name {
	case "CHASHI":
		return cellsliceop.CHASHI(0).Serialize().EndCell()
	case "CDEPTHI":
		return cellsliceop.CDEPTHI(0).Serialize().EndCell()
	case "RUNVM":
		return execop.RUNVM(0).Serialize().EndCell()
	case "SETCONTCTRMANY":
		return execop.SETCONTCTRMANY(0).Serialize().EndCell()
	case "HASHEXT":
		return funcsop.HASHEXT(0).Serialize().EndCell()
	default:
		return cell.BeginCell().MustStoreUInt(tt.opcode, tt.bits).EndCell()
	}
}

func registeredOpcodeAvailabilityAuditCases() []registeredOpcodeAvailabilityAuditCase {
	var cases []registeredOpcodeAvailabilityAuditCase
	for idx, opGetter := range vm.List {
		op := opGetter()
		if versioned, ok := op.(vm.VersionedOp); ok && versioned.MinGlobalVersion() > 0 {
			continue
		}

		code, ok := registeredOpcodeAvailabilityAuditCode(op)
		if !ok {
			continue
		}

		opName := op.SerializeText()
		cases = append(cases, registeredOpcodeAvailabilityAuditCase{
			name:   registeredOpcodeAvailabilityAuditName(idx, opName),
			opName: opName,
			code:   code,
		})
	}
	cases = append(cases, registeredOpcodeAvailabilitySupplementalCases()...)
	return cases
}

func assertRegisteredOpcodeAvailabilityAuditInventory(t testing.TB) {
	t.Helper()

	cases := registeredOpcodeAvailabilityAuditCases()
	if len(cases) != expectedRegisteredOpcodeAvailabilityAuditCases {
		t.Fatalf("registered opcode availability audit case count = %d, want %d", len(cases), expectedRegisteredOpcodeAvailabilityAuditCases)
	}
	if got := registeredOpcodeAvailabilityAuditHash(cases); got != expectedRegisteredOpcodeAvailabilityAuditHash {
		t.Fatalf("registered opcode availability audit hash = %s, want %s", got, expectedRegisteredOpcodeAvailabilityAuditHash)
	}

	required := registeredOpcodeAvailabilityRequiredCaseNames()
	seen := make(map[string]struct{}, len(cases))
	for _, tt := range cases {
		if tt.name == "" {
			t.Fatal("registered opcode availability audit has empty case name")
		}
		if tt.code == nil {
			t.Fatalf("registered opcode availability audit case %s has nil code", tt.name)
		}
		if _, ok := seen[tt.name]; ok {
			t.Fatalf("registered opcode availability audit has duplicate case %s", tt.name)
		}
		seen[tt.name] = struct{}{}
	}
	for name := range required {
		if _, ok := seen[name]; !ok {
			t.Fatalf("registered opcode availability audit is missing required case %s", name)
		}
	}
}

func assertRegisteredOpcodeAvailabilityNonSerializableInventory(t testing.TB) {
	t.Helper()

	inventory := registeredOpcodeAvailabilityNonSerializableInventory()
	if len(inventory) != expectedRegisteredOpcodeAvailabilityNonSerializableCount {
		t.Fatalf("registered opcode availability non-serializable count = %d, want %d:\n%s", len(inventory), expectedRegisteredOpcodeAvailabilityNonSerializableCount, strings.Join(inventory, "\n"))
	}
	if got := registeredOpcodeAvailabilityInventoryHash(inventory); got != expectedRegisteredOpcodeAvailabilityNonSerializableHash {
		t.Fatalf("registered opcode availability non-serializable hash = %s, want %s:\n%s", got, expectedRegisteredOpcodeAvailabilityNonSerializableHash, strings.Join(inventory, "\n"))
	}

	nonSerializable := registeredOpcodeAvailabilityNonSerializableIndexes(t)
	supplemental := registeredOpcodeAvailabilitySupplementalIndexes(t)
	for idx, name := range nonSerializable {
		if _, ok := supplemental[idx]; !ok {
			t.Fatalf("non-serializable registered opcode %s has no supplemental availability case", name)
		}
	}
	for idx, name := range supplemental {
		if _, ok := nonSerializable[idx]; !ok {
			t.Fatalf("supplemental availability case %s no longer mirrors a non-serializable registered opcode", name)
		}
	}
}

func registeredOpcodeAvailabilityAuditHash(cases []registeredOpcodeAvailabilityAuditCase) string {
	items := make([]string, 0, len(cases))
	for _, tt := range cases {
		items = append(items, fmt.Sprintf("%s:%s:%x", tt.name, tt.opName, tt.code.Hash()))
	}
	return registeredOpcodeAvailabilityInventoryHash(items)
}

func registeredOpcodeAvailabilityInventoryHash(items []string) string {
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func registeredOpcodeAvailabilityNonSerializableInventory() []string {
	var inventory []string
	for idx, opGetter := range vm.List {
		op := opGetter()
		if _, ok := registeredOpcodeAvailabilityAuditCode(op); ok {
			continue
		}
		version := 0
		if versioned, ok := op.(vm.VersionedOp); ok {
			version = versioned.MinGlobalVersion()
		}
		inventory = append(inventory, fmt.Sprintf("%s:min=%d", registeredOpcodeAvailabilityAuditName(idx, op.SerializeText()), version))
	}
	sort.Strings(inventory)
	return inventory
}

func registeredOpcodeAvailabilityNonSerializableIndexes(t testing.TB) map[string]string {
	t.Helper()

	indexes := make(map[string]string)
	for idx, opGetter := range vm.List {
		op := opGetter()
		if _, ok := registeredOpcodeAvailabilityAuditCode(op); ok {
			continue
		}
		key := fmt.Sprintf("%03d", idx)
		name := registeredOpcodeAvailabilityAuditName(idx, op.SerializeText())
		if prev, ok := indexes[key]; ok {
			t.Fatalf("duplicate non-serializable registered opcode index %s: %s and %s", key, prev, name)
		}
		indexes[key] = name
	}
	if len(indexes) == 0 {
		t.Fatal("registered opcode availability non-serializable index inventory is empty")
	}
	return indexes
}

func registeredOpcodeAvailabilitySupplementalIndexes(t testing.TB) map[string]string {
	t.Helper()

	indexes := make(map[string]string)
	for _, tt := range registeredOpcodeAvailabilitySupplementalCases() {
		idx := registeredOpcodeAvailabilitySupplementalIndex(t, tt.name)
		if prev, ok := indexes[idx]; ok {
			t.Fatalf("duplicate supplemental availability case index %s: %s and %s", idx, prev, tt.name)
		}
		indexes[idx] = tt.name
	}
	if len(indexes) == 0 {
		t.Fatal("registered opcode availability supplemental index inventory is empty")
	}
	return indexes
}

func registeredOpcodeAvailabilitySupplementalIndex(t testing.TB, name string) string {
	t.Helper()

	const prefix = "supplemental_"
	if !strings.HasPrefix(name, prefix) || len(name) < len(prefix)+3 {
		t.Fatalf("supplemental availability case %s does not start with %sNNN", name, prefix)
	}
	idx := name[len(prefix) : len(prefix)+3]
	for _, r := range idx {
		if r < '0' || r > '9' {
			t.Fatalf("supplemental availability case %s has malformed index %s", name, idx)
		}
	}
	return idx
}

func registeredOpcodeAvailabilityRequiredCaseNames() map[string]struct{} {
	return map[string]struct{}{
		"045_XLOAD":                      {},
		"046_XLOADQ":                     {},
		"152_DICTGET":                    {},
		"273_PFXDICTGETQ":                {},
		"420_CHKSIGNU":                   {},
		"421_CHKSIGNS":                   {},
		"482_LDMSGADDR":                  {},
		"486_REWRITESTDADDR":             {},
		"528_ADD":                        {},
		"831_NOP":                        {},
		"supplemental_089_STREFCONST":    {},
		"supplemental_100_LDI":           {},
		"supplemental_103_LDU":           {},
		"supplemental_132_PLDU":          {},
		"supplemental_138_STI":           {},
		"supplemental_143_STU":           {},
		"supplemental_285_PFXDICTSWITCH": {},
		"supplemental_339_IFBITJMPREF":   {},
		"supplemental_349_CALLREF":       {},
		"supplemental_350_JMPREF":        {},
		"supplemental_351_JMPREFDATA":    {},
		"supplemental_352_IFREF":         {},
		"supplemental_353_IFNOTREF":      {},
		"supplemental_354_IFJMPREF":      {},
		"supplemental_355_IFNOTJMPREF":   {},
		"supplemental_356_IFREFELSE":     {},
		"supplemental_357_IFELSEREF":     {},
		"supplemental_358_IFREFELSEREF":  {},
		"supplemental_812_DICTPUSHCONST": {},
		"supplemental_841_PUSHCONT":      {},
		"supplemental_843_PUSHINT":       {},
		"supplemental_845_PUSHREF":       {},
	}
}

func registeredOpcodeAvailabilityFuzzSeeds(cases []registeredOpcodeAvailabilityAuditCase) []registeredOpcodeAvailabilityFuzzSeed {
	required := registeredOpcodeAvailabilityRequiredCaseNames()
	var seeds []registeredOpcodeAvailabilityFuzzSeed
	for i, tt := range cases {
		if _, ok := required[tt.name]; !ok && !strings.HasPrefix(tt.name, "supplemental_") && i%97 != 0 {
			continue
		}
		for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
			seeds = append(seeds, registeredOpcodeAvailabilityFuzzSeed{
				caseIdx: i,
				version: version,
			})
		}
	}
	return seeds
}

func assertRegisteredOpcodeAvailabilityFuzzSeedInventory(t testing.TB, cases []registeredOpcodeAvailabilityAuditCase, seeds []registeredOpcodeAvailabilityFuzzSeed, expectedCount int, expectedHash string) {
	t.Helper()

	inventory := registeredOpcodeAvailabilityFuzzSeedInventory(cases, seeds)
	if len(seeds) == 0 {
		t.Fatal("registered opcode availability fuzz seeds are empty")
	}
	if len(seeds) != expectedCount {
		t.Fatalf("registered opcode availability fuzz seed count = %d, want %d:\n%s", len(seeds), expectedCount, strings.Join(inventory, "\n"))
	}
	if got := registeredOpcodeAvailabilityFuzzSeedInventoryHash(inventory); got != expectedHash {
		t.Fatalf("registered opcode availability fuzz seed hash = %s, want %s:\n%s", got, expectedHash, strings.Join(inventory, "\n"))
	}

	seen := make(map[int]map[int]struct{}, len(cases))
	for _, seed := range seeds {
		if seed.caseIdx < 0 || seed.caseIdx >= len(cases) {
			t.Fatalf("registered opcode availability fuzz seed case index %d outside [0, %d)", seed.caseIdx, len(cases))
		}
		if seed.version < 0 || seed.version > vm.MaxSupportedGlobalVersion {
			t.Fatalf("registered opcode availability fuzz seed %s version %d outside [%d, %d]", cases[seed.caseIdx].name, seed.version, 0, vm.MaxSupportedGlobalVersion)
		}
		if seen[seed.caseIdx] == nil {
			seen[seed.caseIdx] = make(map[int]struct{})
		}
		if _, ok := seen[seed.caseIdx][seed.version]; ok {
			t.Fatalf("duplicate registered opcode availability fuzz seed %s v%d", cases[seed.caseIdx].name, seed.version)
		}
		seen[seed.caseIdx][seed.version] = struct{}{}
	}

	required := registeredOpcodeAvailabilityRequiredCaseNames()
	for i, tt := range cases {
		versions := seen[i]
		if _, ok := required[tt.name]; ok && len(versions) == 0 {
			t.Fatalf("registered opcode availability fuzz seeds do not cover required case %s", tt.name)
		}
		if strings.HasPrefix(tt.name, "supplemental_") && len(versions) == 0 {
			t.Fatalf("registered opcode availability fuzz seeds do not cover supplemental case %s", tt.name)
		}
		for version := 0; version <= vm.MaxSupportedGlobalVersion; version++ {
			if _, ok := versions[version]; len(versions) > 0 && !ok {
				t.Fatalf("registered opcode availability fuzz seeds do not cover %s v%d", tt.name, version)
			}
		}
	}
}

func registeredOpcodeAvailabilityFuzzSeedInventory(cases []registeredOpcodeAvailabilityAuditCase, seeds []registeredOpcodeAvailabilityFuzzSeed) []string {
	items := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		name := "<invalid>"
		if seed.caseIdx >= 0 && seed.caseIdx < len(cases) {
			name = cases[seed.caseIdx].name
		}
		items = append(items, fmt.Sprintf("%s:v%d", name, seed.version))
	}
	sort.Strings(items)
	return items
}

func registeredOpcodeAvailabilityFuzzSeedInventoryHash(items []string) string {
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return fmt.Sprintf("%x", sum[:])
}

func registeredOpcodeAvailabilitySupplementalCases() []registeredOpcodeAvailabilityAuditCase {
	ref := cell.BeginCell().EndCell()
	return []registeredOpcodeAvailabilityAuditCase{
		registeredOpcodeAvailabilitySupplementalCase("supplemental_089_STREFCONST", cellsliceop.STREFCONST(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_100_LDI", cellsliceop.LDI(1)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_103_LDU", cellsliceop.LDU(1)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_132_PLDU", cellsliceop.PLDU(1)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_138_STI", cellsliceop.STI(1)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_143_STU", cellsliceop.STU(1)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_285_PFXDICTSWITCH", dictop.PFXDICTSWITCH(ref, 0)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_339_IFBITJMPREF", execop.IFBITJMPREF(0, ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_349_CALLREF", execop.CALLREF(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_350_JMPREF", execop.JMPREF(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_351_JMPREFDATA", execop.JMPREFDATA(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_352_IFREF", execop.IFREF(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_353_IFNOTREF", execop.IFNOTREF(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_354_IFJMPREF", execop.IFJMPREF(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_355_IFNOTJMPREF", execop.IFNOTJMPREF(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_356_IFREFELSE", execop.IFREFELSE(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_357_IFELSEREF", execop.IFELSEREF(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_358_IFREFELSEREF", execop.IFREFELSEREF(ref, ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_812_DICTPUSHCONST", stackop.DICTPUSHCONST(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_841_PUSHCONT", stackop.PUSHCONT(ref)),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_843_PUSHINT", stackop.PUSHINT(big.NewInt(11))),
		registeredOpcodeAvailabilitySupplementalCase("supplemental_845_PUSHREF", stackop.PUSHREF(ref)),
	}
}

func registeredOpcodeAvailabilitySupplementalCase(name string, op vm.OP) registeredOpcodeAvailabilityAuditCase {
	return registeredOpcodeAvailabilityAuditCase{
		name:   name,
		opName: op.SerializeText(),
		code:   opcodeVersionPrependMethodDrop(op.Serialize().EndCell()),
	}
}

func registeredOpcodeAvailabilityAuditName(idx int, name string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_", "#", "n", "<", "lt", ">", "gt")
	return fmt.Sprintf("%03d_%s", idx, replacer.Replace(name))
}

func registeredOpcodeAvailabilityAuditCode(op vm.OP) (code *cell.Cell, ok bool) {
	defer func() {
		if recover() != nil {
			code = nil
			ok = false
		}
	}()

	return opcodeVersionPrependMethodDrop(op.Serialize().EndCell()), true
}

func opcodeVersionPrependMethodDrop(code *cell.Cell) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(0x30, 8).
		MustStoreBuilder(code.ToBuilder()).
		EndCell()
}
