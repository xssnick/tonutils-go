package tvm

import (
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

const (
	expectedGoRegisteredOpcodeAvailabilityFuzzSeedCount = 585
	expectedGoRegisteredOpcodeAvailabilityFuzzSeedHash  = "0f0f274b312796c46f95171a2689040f2d8ffff4e366d5a43f00f6d5b3f588f2"
)

func fuzzOpcodeVersion(raw int64) int {
	return tvmFuzzGlobalVersion(raw)
}

func TestFuzzOpcodeVersionCoversSupportedRange(t *testing.T) {
	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		if got := fuzzOpcodeVersion(int64(version)); got != version {
			t.Fatalf("version seed %d mapped to %d, want %d", version, got, version)
		}
	}
	if got := fuzzOpcodeVersion(-int64(MaxSupportedGlobalVersion)); got != MaxSupportedGlobalVersion {
		t.Fatalf("negative max version mapped to %d, want %d", got, MaxSupportedGlobalVersion)
	}
}

func TestTVMRegisteredOpcodeAvailabilityAuditInventory(t *testing.T) {
	assertRegisteredOpcodeAvailabilityAuditInventory(t)
}

func TestTVMRegisteredOpcodeAvailabilityNonSerializableInventory(t *testing.T) {
	assertRegisteredOpcodeAvailabilityNonSerializableInventory(t)
}

func TestTVMRegisteredOpcodeAvailabilityAllGlobalVersions(t *testing.T) {
	cases := registeredOpcodeAvailabilityAuditCases()
	for _, tt := range cases {
		baseExit, err := runGoRegisteredOpcodeAvailabilityCase(tt, MinSupportedGlobalVersion)
		if err != nil {
			t.Fatalf("%s v%d execution failed: %v", tt.name, MinSupportedGlobalVersion, err)
		}
		baseInvalid := baseExit == vmerr.CodeInvalidOpcode
		for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
			exit, err := runGoRegisteredOpcodeAvailabilityCase(tt, version)
			if err != nil {
				t.Fatalf("%s v%d execution failed: %v", tt.name, version, err)
			}
			invalid := exit == vmerr.CodeInvalidOpcode
			if invalid != baseInvalid {
				t.Fatalf("%s invalid-opcode classification changed at v%d: got %v, v%d got %v", tt.name, version, invalid, MinSupportedGlobalVersion, baseInvalid)
			}
			if invalid && registeredOpcodeAvailabilityCaseShouldBeValid(tt) {
				t.Fatalf("%s v%d decoded as invalid opcode", tt.name, version)
			}
		}
	}
}

func TestTVMRegisteredOpcodeAvailabilityPartitionsVersionedOpcodes(t *testing.T) {
	stableAuditNames := make(map[string]struct{})
	for _, tt := range registeredOpcodeAvailabilityAuditCases() {
		if strings.HasPrefix(tt.name, "supplemental_") {
			continue
		}
		stableAuditNames[tt.name] = struct{}{}
	}

	versionedCases := opcodeMinGlobalVersionCaseMap(t)
	for idx, opGetter := range vmcore.List {
		op := opGetter()
		_, serializable := registeredOpcodeAvailabilityAuditCode(op)
		name := registeredOpcodeAvailabilityAuditName(idx, op.SerializeText())
		_, inStableAudit := stableAuditNames[name]

		versioned, isVersioned := op.(vmcore.VersionedOp)
		if isVersioned && versioned.MinGlobalVersion() > MinSupportedGlobalVersion {
			if inStableAudit {
				t.Errorf("versioned opcode %s min=%d is present in stable availability audit", name, versioned.MinGlobalVersion())
			}
			if serializable {
				key := opcodeVersionKeyFromRegisteredOp(t, op)
				tt, ok := versionedCases[key]
				if !ok {
					t.Errorf("versioned opcode %s %#x/%d min=%d is missing from min-version registry", name, key.opcode, key.bits, versioned.MinGlobalVersion())
					continue
				}
				if tt.min != versioned.MinGlobalVersion() {
					t.Errorf("versioned opcode %s %#x/%d min=%d, registry has %d", name, key.opcode, key.bits, versioned.MinGlobalVersion(), tt.min)
				}
			}
			continue
		}

		if serializable && !inStableAudit {
			t.Errorf("stable opcode %s is missing from stable availability audit", name)
		}
		if !serializable && inStableAudit {
			t.Errorf("non-serializable opcode %s is present in stable availability audit", name)
		}
	}
}

func FuzzTVMVersionedOpcodeAvailabilityBoundaries(f *testing.F) {
	cases := opcodeMinGlobalVersionBoundaryCases()
	for _, seed := range opcodeMinGlobalVersionBoundaryFuzzSeeds(cases) {
		f.Add(uint16(seed.caseIdx), int64(seed.version))
	}
	for _, seed := range opcodeMinGlobalVersionRepresentativeFuzzSeeds(cases) {
		f.Add(uint16(seed.caseIdx), int64(seed.version))
	}

	f.Fuzz(func(t *testing.T, rawCase uint16, rawVersion int64) {
		tt := cases[int(rawCase)%len(cases)]
		version := fuzzOpcodeVersion(rawVersion)

		machine := NewTVM()
		if err := machine.SetGlobalVersion(version); err != nil {
			t.Fatalf("set global version %d: %v", version, err)
		}
		res, err := machine.Execute(
			opcodeMinVersionInstructionCode(tt),
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.GasWithLimit(1_000_000),
			vmcore.NewStack(), ExecutionConfig{})

		gotInvalid := exitCodeFromResult(res, err) == vmerr.CodeInvalidOpcode
		wantInvalid := version < tt.min
		if gotInvalid != wantInvalid {
			t.Fatalf("%s %#x/%d version=%d invalid=%v want=%v", tt.name, tt.opcode, tt.bits, version, gotInvalid, wantInvalid)
		}
	})
}

func FuzzTVMStableOpcodesAcrossGlobalVersions(f *testing.F) {
	for rawCase := uint8(0); rawCase < 5; rawCase++ {
		for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
			f.Add(rawCase, int64(version))
		}
	}

	f.Fuzz(func(t *testing.T, rawCase uint8, rawVersion int64) {
		version := fuzzOpcodeVersion(rawVersion)
		code, stack, want := stableOpcodeVersionFuzzCase(t, rawCase)

		machine := NewTVM()
		if err := machine.SetGlobalVersion(version); err != nil {
			t.Fatalf("set global version %d: %v", version, err)
		}
		res, err := machine.Execute(
			code,
			cell.BeginCell().EndCell(),
			tuple.Tuple{},
			vmcore.GasWithLimit(1_000_000),
			stack, ExecutionConfig{})

		if err != nil {
			t.Fatalf("execute version %d case %d: %v", version, rawCase, err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("version %d case %d exit = %d, want success", version, rawCase, res.ExitCode)
		}

		got, err := res.Stack.PopInt()
		if err != nil {
			t.Fatalf("version %d case %d pop result: %v", version, rawCase, err)
		}
		if got.Cmp(want) != 0 {
			t.Fatalf("version %d case %d result = %s, want %s", version, rawCase, got, want)
		}
	})
}

func FuzzTVMRegisteredOpcodeAvailabilityAcrossGlobalVersions(f *testing.F) {
	cases := registeredOpcodeAvailabilityAuditCases()
	for _, seed := range registeredOpcodeAvailabilityFuzzSeeds(cases) {
		f.Add(uint16(seed.caseIdx), int64(seed.version))
	}

	f.Fuzz(func(t *testing.T, rawCase uint16, rawVersion int64) {
		tt := cases[int(rawCase)%len(cases)]
		version := fuzzOpcodeVersion(rawVersion)

		exit, err := runGoRegisteredOpcodeAvailabilityCase(tt, version)
		if err != nil {
			t.Fatalf("%s v%d execution failed: %v", tt.name, version, err)
		}
		baseExit, err := runGoRegisteredOpcodeAvailabilityCase(tt, MinSupportedGlobalVersion)
		if err != nil {
			t.Fatalf("%s v%d execution failed: %v", tt.name, MinSupportedGlobalVersion, err)
		}
		invalid := exit == vmerr.CodeInvalidOpcode
		baseInvalid := baseExit == vmerr.CodeInvalidOpcode
		if invalid != baseInvalid {
			t.Fatalf("%s invalid-opcode classification changed at v%d: got %v, v%d got %v", tt.name, version, invalid, MinSupportedGlobalVersion, baseInvalid)
		}
		if invalid && registeredOpcodeAvailabilityCaseShouldBeValid(tt) {
			t.Fatalf("%s v%d decoded as invalid opcode", tt.name, version)
		}
	})
}

func TestTVMRegisteredOpcodeAvailabilityFuzzSeedInventory(t *testing.T) {
	cases := registeredOpcodeAvailabilityAuditCases()
	seeds := registeredOpcodeAvailabilityFuzzSeeds(cases)
	assertRegisteredOpcodeAvailabilityFuzzSeedInventory(
		t,
		cases,
		seeds,
		expectedGoRegisteredOpcodeAvailabilityFuzzSeedCount,
		expectedGoRegisteredOpcodeAvailabilityFuzzSeedHash,
	)
}

func registeredOpcodeAvailabilityCaseShouldBeValid(tt registeredOpcodeAvailabilityAuditCase) bool {
	if strings.HasPrefix(tt.name, "supplemental_") {
		return true
	}
	_, ok := registeredOpcodeAvailabilityRequiredCaseNames()[tt.name]
	return ok
}

func runGoRegisteredOpcodeAvailabilityCase(tt registeredOpcodeAvailabilityAuditCase, version int) (int64, error) {
	stack := vmcore.NewStack()
	if err := stack.PushSmallInt(0); err != nil {
		return 0, err
	}

	machine := NewTVM()
	if err := machine.SetGlobalVersion(version); err != nil {
		return 0, err
	}
	res, err := machine.Execute(
		tt.code,
		cell.BeginCell().EndCell(),
		tuple.Tuple{},
		vmcore.GasWithLimit(registeredOpcodeAvailabilityAuditGasLimit),
		stack, ExecutionConfig{})

	if err != nil {
		if _, ok := vmerr.ErrorCode(err); !ok {
			return 0, err
		}
	}
	return exitCodeFromResult(res, err), nil
}

func stableOpcodeVersionFuzzCase(t *testing.T, rawCase uint8) (*cell.Cell, *vmcore.Stack, *big.Int) {
	t.Helper()

	stack := vmcore.NewStack()
	switch rawCase % 5 {
	case 0:
		return codeFromBuilders(t,
			stackop.PUSHINT(big.NewInt(7)).Serialize(),
			stackop.PUSHINT(big.NewInt(35)).Serialize(),
			mathop.SUM().Serialize(),
		), stack, big.NewInt(42)
	case 1:
		return codeFromBuilders(t,
			stackop.PUSHINT(big.NewInt(-6)).Serialize(),
			stackop.PUSHINT(big.NewInt(7)).Serialize(),
			mathop.MUL().Serialize(),
		), stack, big.NewInt(-42)
	case 2:
		if err := stack.PushInt(big.NewInt(50)); err != nil {
			t.Fatalf("push initial stack value: %v", err)
		}
		if err := stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push initial stack value: %v", err)
		}
		return codeFromBuilders(t, mathop.SUB().Serialize()), stack, big.NewInt(42)
	case 3:
		if err := stack.PushSlice(mustSliceKey(t, 0x12, 8)); err != nil {
			t.Fatalf("push dict hit key: %v", err)
		}
		if err := stack.PushCell(mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8)); err != nil {
			t.Fatalf("push dict hit root: %v", err)
		}
		if err := stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push dict hit key length: %v", err)
		}
		return codeFromOpcodes(t, 0xF40A), stack, big.NewInt(-1)
	default:
		if err := stack.PushSlice(mustSliceKey(t, 0x99, 8)); err != nil {
			t.Fatalf("push dict miss key: %v", err)
		}
		if err := stack.PushCell(mustPlainDictCell(t, 8, map[uint64]uint64{0x12: 0x34}, 8)); err != nil {
			t.Fatalf("push dict miss root: %v", err)
		}
		if err := stack.PushInt(big.NewInt(8)); err != nil {
			t.Fatalf("push dict miss key length: %v", err)
		}
		return codeFromOpcodes(t, 0xF40A), stack, big.NewInt(0)
	}
}
