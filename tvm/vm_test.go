package tvm

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
	"math/big"
	"testing"
	"time"
)

var MainContractCode = func() *cell.Cell {
	contractCodeBytes, _ := hex.DecodeString("b5ee9c724102180100019a000114ff00f4a413f4bcf2c80b0102016202070202ce0306020120040500691b088831c02456f8007434c0cc1caa42644c383c0074c7f4cfcc4060841fa1d93beea6f4c7cc3e1080683e18bc00b80c2103fcbc20001d3b513434c7c07e1874c7c07e18b460001d4c8f84101cb1ff84201cb1fc9ed548020120080f020120090e0201200a0b000db7203e003f08300201200c0d0047b2e9c160c235c61981fe44407e08efac3ca600fe800c1480aed4cc6eec3ca696284068200057b057bb68bb7efb507b50fb513b517b51e556dc76cc4c3b59fb597b593b58fb5864e936cc7b507b7c407cbfe0002fb829a708e10709320c1059402a402a4e830a420c204e630802012010110099bbfe470ed41ed43ed44ed45ed47935b8064ed67ed65ed64ed63ed618e28758e237a9320c2008e18a5738e1202a420c20524c200b092f237de02a520c101e630e830fe00e431ed41edf101f2ff8020148121702012013140021ad1cbacdb84b80d200d2106102731872400201201516000caad0f001f8420022aacd759c709320c1059401a401a4e830e4000db3aedd646939209e25fbf5")
	code, _ := cell.FromBOC(contractCodeBytes)
	return code
}()

func TestTVMSetGlobalVersionValidatesRange(t *testing.T) {
	v := NewTVM()

	for _, version := range []int{MinSupportedGlobalVersion - 1, MaxSupportedGlobalVersion + 1} {
		if err := v.SetGlobalVersion(version); err == nil {
			t.Fatalf("SetGlobalVersion accepted unsupported global version %d", version)
		}
		if v.globalVersion != vm.DefaultGlobalVersion {
			t.Fatalf("SetGlobalVersion(%d) changed version to %d", version, v.globalVersion)
		}
	}

	if err := v.SetGlobalVersion(MinSupportedGlobalVersion); err != nil {
		t.Fatalf("SetGlobalVersion minimum version: %v", err)
	}
	if v.globalVersion != MinSupportedGlobalVersion {
		t.Fatalf("global version = %d, want minimum %d", v.globalVersion, MinSupportedGlobalVersion)
	}
	if err := v.SetGlobalVersion(MaxSupportedGlobalVersion + 1); err == nil {
		t.Fatal("SetGlobalVersion should reject new unsupported global version")
	}
	if v.globalVersion != MinSupportedGlobalVersion {
		t.Fatalf("unsupported high version changed version to %d", v.globalVersion)
	}

	if err := v.SetGlobalVersion(MaxSupportedGlobalVersion); err != nil {
		t.Fatalf("SetGlobalVersion maximum version: %v", err)
	}
	if v.globalVersion != MaxSupportedGlobalVersion {
		t.Fatalf("global version = %d, want maximum %d", v.globalVersion, MaxSupportedGlobalVersion)
	}
	if err := v.SetGlobalVersion(MinSupportedGlobalVersion - 1); err == nil {
		t.Fatal("SetGlobalVersion should reject old unsupported global version")
	}
	if v.globalVersion != MaxSupportedGlobalVersion {
		t.Fatalf("unsupported low version changed version to %d", v.globalVersion)
	}
}

func TestTVMGlobalVersionConstantsStayAligned(t *testing.T) {
	if MinSupportedGlobalVersion != 0 {
		t.Fatalf("min supported global version = %d, want 0", MinSupportedGlobalVersion)
	}
	if MaxSupportedGlobalVersion != vm.DefaultGlobalVersion {
		t.Fatalf("max supported global version = %d, default VM global version = %d", MaxSupportedGlobalVersion, vm.DefaultGlobalVersion)
	}
}

func TestTVMWithGlobalVersionCopiesShallow(t *testing.T) {
	v := NewTVM()
	baseVersion := MinSupportedGlobalVersion
	if MinSupportedGlobalVersion < MaxSupportedGlobalVersion {
		baseVersion = MinSupportedGlobalVersion + 1
	}
	if err := v.SetGlobalVersion(baseVersion); err != nil {
		t.Fatalf("SetGlobalVersion(%d): %v", baseVersion, err)
	}

	next, err := v.WithGlobalVersion(MinSupportedGlobalVersion)
	if err != nil {
		t.Fatalf("WithGlobalVersion minimum version: %v", err)
	}

	if next.globalVersion != MinSupportedGlobalVersion {
		t.Fatalf("copy global version = %d, want %d", next.globalVersion, MinSupportedGlobalVersion)
	}
	if v.globalVersion != baseVersion {
		t.Fatalf("base global version changed to %d", v.globalVersion)
	}
	if next.dispatches != v.dispatches {
		t.Fatal("WithGlobalVersion should share opcode dispatch tables")
	}
	if next.dispatch != next.dispatches[MinSupportedGlobalVersion] {
		t.Fatal("WithGlobalVersion should point copy to requested dispatch")
	}

	maxCopy, err := v.WithGlobalVersion(MaxSupportedGlobalVersion)
	if err != nil {
		t.Fatalf("WithGlobalVersion maximum version: %v", err)
	}
	if maxCopy.globalVersion != MaxSupportedGlobalVersion {
		t.Fatalf("copy global version = %d, want %d", maxCopy.globalVersion, MaxSupportedGlobalVersion)
	}
	if maxCopy.dispatches != v.dispatches {
		t.Fatal("WithGlobalVersion maximum copy should share opcode dispatch tables")
	}
	if maxCopy.dispatch != maxCopy.dispatches[MaxSupportedGlobalVersion] {
		t.Fatal("WithGlobalVersion maximum copy should point to maximum dispatch")
	}

	for _, version := range []int{MinSupportedGlobalVersion - 1, MaxSupportedGlobalVersion + 1} {
		if _, err = v.WithGlobalVersion(version); err == nil {
			t.Fatalf("WithGlobalVersion accepted unsupported global version %d", version)
		}
		if v.globalVersion != baseVersion {
			t.Fatalf("WithGlobalVersion(%d) changed base version to %d", version, v.globalVersion)
		}
	}
}

func TestTVMPreV4InstructionGasCommitsBeforeOutOfGas(t *testing.T) {
	code := codeFromBuilders(t, funcsop.COMMIT().Serialize())
	data := cell.BeginCell().MustStoreUInt(0xCA, 8).EndCell()

	tests := []struct {
		version       int
		wantCommitted bool
	}{
		{version: 3, wantCommitted: true},
		{version: 4, wantCommitted: false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("v%d", tt.version), func(t *testing.T) {
			machine, err := NewTVM().WithGlobalVersion(tt.version)
			if err != nil {
				t.Fatalf("set global version: %v", err)
			}

			res, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(1), vm.NewStack(), ExecutionConfig{})
			if err != nil {
				t.Fatalf("execute detailed: %v", err)
			}
			if res.ExitCode != ^int64(vmerr.CodeOutOfGas) {
				t.Fatalf("exit code = %d, want %d", res.ExitCode, ^int64(vmerr.CodeOutOfGas))
			}
			if res.Committed != tt.wantCommitted {
				t.Fatalf("committed = %t, want %t", res.Committed, tt.wantCommitted)
			}
		})
	}
}

func FuzzTVMVersionedInstructionGasCommitBoundary(f *testing.F) {
	for version := int64(MinSupportedGlobalVersion); version <= int64(MaxSupportedGlobalVersion); version++ {
		f.Add(version, int64(1))
		f.Add(version, int64(vm.InstructionBaseGasPrice+16))
	}

	f.Fuzz(func(t *testing.T, rawVersion, rawGasLimit int64) {
		version := tvmFuzzGlobalVersion(rawVersion)
		gasLimit := rawGasLimit % 64
		if gasLimit < 0 {
			gasLimit = -gasLimit
		}
		gasLimit++

		machine, err := NewTVM().WithGlobalVersion(version)
		if err != nil {
			t.Fatalf("set global version: %v", err)
		}

		code := codeFromBuilders(t, funcsop.COMMIT().Serialize())
		data := cell.BeginCell().MustStoreUInt(0xCA, 8).EndCell()
		res, _ := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(gasLimit), vm.NewStack(), ExecutionConfig{})

		commitInstructionGas := vm.InstructionBaseGasPrice + int64(16)
		wantCommitted := gasLimit >= commitInstructionGas || version < 4
		if res.Committed != wantCommitted {
			t.Fatalf("v%d gas=%d committed = %t, want %t", version, gasLimit, res.Committed, wantCommitted)
		}
	})
}

func TestTVMEarlyCodeLoadErrorReturnsExecutionResult(t *testing.T) {
	code, err := cell.BeginCell().
		MustStoreUInt(uint64(cell.LibraryCellType), 8).
		MustStoreSlice(make([]byte, 32), 256).
		EndCellSpecial(true)
	if err != nil {
		t.Fatalf("build library special code: %v", err)
	}
	data := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	res, err := NewTVM().Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(1000), vm.NewStack(), ExecutionConfig{})
	if err != nil {
		t.Fatalf("early VM error should be returned as execution result, got err %v", err)
	}
	if res == nil {
		t.Fatal("missing execution result")
	}
	if res.ExitCode != vmerr.CodeCellUnderflow {
		t.Fatalf("exit code = %d, want cell underflow", res.ExitCode)
	}
	if res.Code != code || res.Data != data || res.Actions == nil || res.Committed {
		t.Fatalf("unexpected early error result: %+v", res)
	}
}

func TestTVMLibraryCodeCellStartupConversionChangesAtV9(t *testing.T) {
	target := codeFromBuilders(t,
		stackop.PUSHINT(big.NewInt(7)).Serialize(),
	)
	code := mustLibraryCellForHash(t, target.Hash())
	libraries := mustLibraryCollection(t, target)

	run := func(version int) *ExecutionResult {
		t.Helper()

		machine, err := NewTVM().WithGlobalVersion(version)
		if err != nil {
			t.Fatalf("WithGlobalVersion(%d): %v", version, err)
		}
		res, err := machine.Execute(code, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(10000), vm.NewStack(), ExecutionConfig{Libraries: []*cell.Cell{libraries}})
		if err != nil {
			t.Fatalf("ExecuteDetailedWithLibraries v%d: %v", version, err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("v%d exit code = %d, want success", version, res.ExitCode)
		}
		got, err := res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("v%d pop result: %v", version, err)
		}
		if got.Int64() != 7 {
			t.Fatalf("v%d result = %d, want 7", version, got.Int64())
		}
		return res
	}

	legacy := run(8)
	direct := run(9)
	if legacy.Steps <= direct.Steps {
		t.Fatalf("v8 steps = %d, v9 steps = %d; v8 should include an implicit jump through the code ref", legacy.Steps, direct.Steps)
	}
	if legacy.GasUsed <= direct.GasUsed {
		t.Fatalf("v8 gas = %d, v9 gas = %d; v8 should charge the implicit code ref jump", legacy.GasUsed, direct.GasUsed)
	}
}

func TestExecutionResultFromStateUsesCommittedOrFallbackData(t *testing.T) {
	code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	fallbackData := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()

	res := executionResultFromState(vmerrCode(errors.New("plain failure")), &vm.State{Stack: vm.NewStack()}, code, fallbackData)
	if res.ExitCode != vmerr.CodeFatal {
		t.Fatalf("generic vmerr code = %d, want fatal", res.ExitCode)
	}
	if res.Code != code || res.Data != fallbackData || res.Actions != nil || res.Committed {
		t.Fatalf("unexpected fallback result: %+v", res)
	}

	regData := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
	regActions := cell.BeginCell().MustStoreUInt(0xDD, 8).EndCell()
	committedData := cell.BeginCell().MustStoreUInt(0xEE, 8).EndCell()
	committedActions := cell.BeginCell().MustStoreUInt(0xFF, 8).EndCell()
	state := &vm.State{
		Reg: vm.Register{
			D: [2]*cell.Cell{regData, regActions},
		},
		Stack: vm.NewStack(),
		Committed: vm.CommittedState{
			Data:      committedData,
			Actions:   committedActions,
			Committed: true,
		},
	}

	res = executionResultFromState(vmerrCode(vmerr.Error(vmerr.CodeRangeCheck, "range")), state, code, fallbackData)
	if res.ExitCode != vmerr.CodeRangeCheck {
		t.Fatalf("vmerr code = %d, want range check", res.ExitCode)
	}
	if res.Data != committedData || res.Actions != committedActions || !res.Committed {
		t.Fatalf("unexpected committed result: %+v", res)
	}
}

func TestTVMErrorNormalizationExitCodes(t *testing.T) {
	if normalizeCellError(nil) != nil {
		t.Fatal("nil cell error should stay nil")
	}

	vmErr := vmerr.Error(vmerr.CodeStackOverflow, "stack")
	if got := normalizeCellError(vmErr); got != vmErr {
		t.Fatal("vm error should pass through unchanged")
	}

	cellCases := []struct {
		name string
		err  error
		code int64
	}{
		{name: "text underflow", err: cell.ErrNotEnoughData(1, 8), code: vmerr.CodeCellUnderflow},
		{name: "no more refs", err: cell.ErrNoMoreRefs, code: vmerr.CodeCellUnderflow},
		{name: "small slice", err: cell.ErrSmallSlice, code: vmerr.CodeCellUnderflow},
		{name: "not fit", err: cell.ErrNotFit1023, code: vmerr.CodeCellOverflow},
		{name: "too many refs", err: cell.ErrTooMuchRefs, code: vmerr.CodeCellOverflow},
		{name: "depth", err: cell.ErrCellDepthLimit, code: vmerr.CodeCellOverflow},
		{name: "nil ref", err: cell.ErrRefCannotBeNil, code: vmerr.CodeCellOverflow},
		{name: "too big value", err: cell.ErrTooBigValue, code: vmerr.CodeRangeCheck},
		{name: "negative", err: cell.ErrNegative, code: vmerr.CodeRangeCheck},
		{name: "invalid size", err: cell.ErrInvalidSize, code: vmerr.CodeRangeCheck},
		{name: "nil big int", err: cell.ErrNilBigInt, code: vmerr.CodeRangeCheck},
		{name: "too big size", err: cell.ErrTooBigSize, code: vmerr.CodeRangeCheck},
	}
	for _, tt := range cellCases {
		t.Run(tt.name, func(t *testing.T) {
			assertNormalizedVMErrorCode(t, normalizeCellError(tt.err), tt.code)
		})
	}

	plain := errors.New("plain failure")
	if got := normalizeCellError(plain); got != plain {
		t.Fatalf("plain error should pass through unchanged, got %v", got)
	}
}

func TestTVMOpcodeDeserializeErrorNormalization(t *testing.T) {
	op := versionGateNoGasOp{}
	if normalizeOpcodeDeserializeError(nil, op) != nil {
		t.Fatal("nil opcode deserialize error should stay nil")
	}

	invalidCases := []struct {
		name string
		err  error
	}{
		{name: "corrupted opcode", err: vm.ErrCorruptedOpcode},
		{name: "no more refs", err: cell.ErrNoMoreRefs},
		{name: "small slice", err: cell.ErrSmallSlice},
		{name: "text underflow", err: cell.ErrNotEnoughData(0, 8)},
	}
	for _, tt := range invalidCases {
		t.Run(tt.name, func(t *testing.T) {
			assertNormalizedVMErrorCode(t, normalizeOpcodeDeserializeError(tt.err, op), vmerr.CodeInvalidOpcode)
		})
	}

	got := normalizeOpcodeDeserializeError(cell.ErrTooBigValue, op)
	if _, ok := vmerr.ErrorCode(got); ok {
		t.Fatalf("range deserialize error should be wrapped as Go error, got VM error %v", got)
	}
	if !errors.Is(got, cell.ErrTooBigValue) {
		t.Fatalf("wrapped deserialize error should preserve cause, got %v", got)
	}
}

func FuzzTVMErrorNormalizationGroups(f *testing.F) {
	for group := byte(0); group < 3; group++ {
		for variant := byte(0); variant < 5; variant++ {
			f.Add(group, variant, false)
			f.Add(group, variant, true)
		}
	}

	f.Fuzz(func(t *testing.T, rawGroup, rawVariant byte, wrap bool) {
		err, wantCode := errorNormalizationFuzzCase(rawGroup, rawVariant)
		if wrap {
			err = fmt.Errorf("wrapped: %w", err)
		}

		assertNormalizedVMErrorCode(t, normalizeCellError(err), wantCode)
	})
}

func TestTVMExecuteConfigChksigAlwaysSucceedPerRun(t *testing.T) {
	code := testOpcodeCell(t, "f910")
	data := cell.BeginCell().EndCell()
	signature := make([]byte, 64)
	signature[0] = 1

	run := func(t *testing.T, always bool) bool {
		t.Helper()

		stack := vm.NewStack()
		if err := stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("push hash: %v", err)
		}
		if err := stack.PushSlice(cell.BeginCell().MustStoreSlice(signature, 512).ToSlice()); err != nil {
			t.Fatalf("push signature: %v", err)
		}
		if err := stack.PushInt(big.NewInt(2)); err != nil {
			t.Fatalf("push key: %v", err)
		}

		res, err := NewTVM().Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(100_000), stack, ExecutionConfig{
			ChksigAlwaysSucceed: always,
		})

		if err != nil {
			t.Fatalf("ExecuteDetailedWithConfig: %v", err)
		}
		if !vm.IsSuccessExitCode(res.ExitCode) {
			t.Fatalf("exit code = %d", res.ExitCode)
		}

		got, err := res.Stack.PopBool()
		if err != nil {
			t.Fatalf("pop result: %v", err)
		}
		return got
	}

	if got := run(t, false); got {
		t.Fatal("default run should reject forged CHKSIGNU")
	}
	if got := run(t, true); !got {
		t.Fatal("configured run should accept forged CHKSIGNU")
	}
}

var versionedOpcodeAvailabilityCases = []struct {
	name       string
	code       string
	minVersion int
}{
	{
		name:       "GASCONSUMED",
		code:       "f807",
		minVersion: 4,
	},
	{
		name:       "RUNVM",
		code:       "db4000",
		minVersion: 4,
	},
	{
		name:       "RUNVMX",
		code:       "db50",
		minVersion: 4,
	},
	{
		name:       "ADDDIVMOD",
		code:       "a900",
		minVersion: 4,
	},
	{
		name:       "QADDDIVMOD",
		code:       "b7a900",
		minVersion: 4,
	},
	{
		name:       "PREVMCBLOCKS",
		code:       "f83400",
		minVersion: 4,
	},
	{
		name:       "PREVKEYBLOCK",
		code:       "f83401",
		minVersion: 4,
	},
	{
		name:       "GLOBALID",
		code:       "f835",
		minVersion: 4,
	},
	{
		name:       "HASHEXT",
		code:       "f90400",
		minVersion: 4,
	},
	{
		name:       "ECRECOVER",
		code:       "f912",
		minVersion: 4,
	},
	{
		name:       "P256_CHKSIGNU",
		code:       "f914",
		minVersion: 4,
	},
	{
		name:       "P256_CHKSIGNS",
		code:       "f915",
		minVersion: 4,
	},
	{
		name:       "RIST255_VALIDATE",
		code:       "f921",
		minVersion: 4,
	},
	{
		name:       "RIST255_QVALIDATE",
		code:       "b7f921",
		minVersion: 4,
	},
	{
		name:       "RIST255_PUSHL",
		code:       "f926",
		minVersion: 4,
	},
	{
		name:       "BLS_VERIFY",
		code:       "f93000",
		minVersion: 4,
	},
	{
		name:       "BLS_FASTAGGREGATEVERIFY",
		code:       "f93002",
		minVersion: 4,
	},
	{
		name:       "BLS_G1_ADD",
		code:       "f93010",
		minVersion: 4,
	},
	{
		name:       "BLS_G2_ADD",
		code:       "f93020",
		minVersion: 4,
	},
	{
		name:       "BLS_PAIRING",
		code:       "f93030",
		minVersion: 4,
	},
	{
		name:       "BLS_PUSHR",
		code:       "f93031",
		minVersion: 4,
	},
	{
		name:       "SENDMSG",
		code:       "fb08",
		minVersion: 4,
	},
	{
		name:       "CLEVEL",
		code:       "d766",
		minVersion: 6,
	},
	{
		name:       "CLEVELMASK",
		code:       "d767",
		minVersion: 6,
	},
	{
		name:       "CHASHI",
		code:       "d768",
		minVersion: 6,
	},
	{
		name:       "CDEPTHI",
		code:       "d76c",
		minVersion: 6,
	},
	{
		name:       "CHASHIX",
		code:       "d770",
		minVersion: 6,
	},
	{
		name:       "CDEPTHIX",
		code:       "d771",
		minVersion: 6,
	},
	{
		name:       "GETGASFEE",
		code:       "f836",
		minVersion: 6,
	},
	{
		name:       "GETSTORAGEFEE",
		code:       "f837",
		minVersion: 6,
	},
	{
		name:       "GETFORWARDFEE",
		code:       "f838",
		minVersion: 6,
	},
	{
		name:       "GETPRECOMPILEDGAS",
		code:       "f839",
		minVersion: 6,
	},
	{
		name:       "GETORIGINALFWDFEE",
		code:       "f83a",
		minVersion: 6,
	},
	{
		name:       "GETGASFEESIMPLE",
		code:       "f83b",
		minVersion: 6,
	},
	{
		name:       "GETFORWARDFEESIMPLE",
		code:       "f83c",
		minVersion: 6,
	},
	{
		name:       "PREVMCBLOCKS_100",
		code:       "f83402",
		minVersion: 9,
	},
	{
		name:       "SECP256K1_XONLY_PUBKEY_TWEAK_ADD",
		code:       "f913",
		minVersion: 9,
	},
	{
		name:       "SETCONTCTRMANY",
		code:       "ede300",
		minVersion: 9,
	},
	{
		name:       "SETCONTCTRMANYX",
		code:       "ede4",
		minVersion: 9,
	},
	{
		name:       "GETEXTRABALANCE",
		code:       "f880",
		minVersion: 10,
	},
	{
		name:       "GETPARAMLONG",
		code:       "f88100",
		minVersion: 11,
	},
	{
		name:       "GETPARAMLONG_AFTER_INMSGPARAMS",
		code:       "f88112",
		minVersion: 11,
	},
	{
		name:       "INMSGPARAMS",
		code:       "f88111",
		minVersion: 11,
	},
	{
		name:       "INMSG_BOUNCE",
		code:       "f890",
		minVersion: 11,
	},
	{
		name:       "INMSG_STATEINIT",
		code:       "f899",
		minVersion: 11,
	},
	{
		name:       "INMSGPARAM",
		code:       "f89a",
		minVersion: 11,
	},
	{
		name:       "BTOS",
		code:       "cf50",
		minVersion: 12,
	},
	{
		name:       "HASHBU",
		code:       "f916",
		minVersion: 12,
	},
	{
		name:       "LDSTDADDR",
		code:       "fa48",
		minVersion: 12,
	},
	{
		name:       "LDSTDADDRQ",
		code:       "fa49",
		minVersion: 12,
	},
	{
		name:       "LDOPTSTDADDR",
		code:       "fa50",
		minVersion: 12,
	},
	{
		name:       "LDOPTSTDADDRQ",
		code:       "fa51",
		minVersion: 12,
	},
	{
		name:       "STSTDADDR",
		code:       "fa52",
		minVersion: 12,
	},
	{
		name:       "STSTDADDRQ",
		code:       "fa53",
		minVersion: 12,
	},
	{
		name:       "STOPTSTDADDR",
		code:       "fa54",
		minVersion: 12,
	},
	{
		name:       "STOPTSTDADDRQ",
		code:       "fa55",
		minVersion: 12,
	},
}

func TestTVMVersionedOpcodeAvailability(t *testing.T) {
	for _, tt := range versionedOpcodeAvailabilityCases {
		t.Run(tt.name, func(t *testing.T) {
			machine := NewTVM()
			if err := machine.SetGlobalVersion(tt.minVersion - 1); err != nil {
				t.Fatalf("set low global version: %v", err)
			}
			res, err := machine.Execute(testOpcodeCell(t, tt.code), cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(1_000), vm.NewStack(), ExecutionConfig{})
			if err != nil {
				t.Fatalf("execute low global version: %v", err)
			}
			if res.ExitCode != vmerr.CodeInvalidOpcode {
				t.Fatalf("low global version exit code = %d, want invalid opcode", res.ExitCode)
			}

			machine = NewTVM()
			if err = machine.SetGlobalVersion(tt.minVersion); err != nil {
				t.Fatalf("set minimum global version: %v", err)
			}
			res, err = machine.Execute(testOpcodeCell(t, tt.code), cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(1_000), vm.NewStack(), ExecutionConfig{})
			if err != nil {
				t.Fatalf("execute minimum global version: %v", err)
			}
			if res.ExitCode == vmerr.CodeInvalidOpcode {
				t.Fatalf("minimum global version should not reject %s as invalid opcode", tt.name)
			}
		})
	}
}

func TestTVMVersionedOpcodeDynamicSuffixAvailability(t *testing.T) {
	machine := NewTVM()
	if err := machine.SetGlobalVersion(3); err != nil {
		t.Fatalf("set global version: %v", err)
	}

	res, err := machine.Execute(testOpcodeCell(t, "b7a900"), cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(1_000), vm.NewStack(), ExecutionConfig{})
	if err != nil {
		t.Fatalf("execute QADDDIVMOD: %v", err)
	}
	if res.ExitCode != vmerr.CodeInvalidOpcode {
		t.Fatalf("QADDDIVMOD v3 exit code = %d, want invalid opcode", res.ExitCode)
	}

	res, err = machine.Execute(testOpcodeCell(t, "b7a904"), cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(1_000), vm.NewStack(), ExecutionConfig{})
	if err != nil {
		t.Fatalf("execute QDIV: %v", err)
	}
	if res.ExitCode != vmerr.CodeStackUnderflow {
		t.Fatalf("QDIV v3 exit code = %d, want stack underflow", res.ExitCode)
	}
}

func TestTVMVersionGateGasByOpcodeFamily(t *testing.T) {
	cases := []struct {
		name    string
		code    *cell.Cell
		version int
		wantGas int64
	}{
		{
			name:    "short immediate cellslice family charges base only",
			code:    opcodeMinVersionInstructionCode(opcodeMinGlobalVersionCase{opcode: 0x35da, bits: 14, min: 6, name: "CHASHI"}),
			version: 5,
			wantGas: vm.InstructionBaseGasPrice + vm.ExceptionGasPrice,
		},
		{
			name:    "ordinary fee family charges base only",
			code:    testOpcodeCell(t, "f836"),
			version: 5,
			wantGas: vm.InstructionBaseGasPrice + vm.ExceptionGasPrice,
		},
		{
			name:    "a9 math family charges instruction bits",
			code:    testOpcodeCell(t, "a900"),
			version: 3,
			wantGas: vm.InstructionBaseGasPrice + 16 + vm.ExceptionGasPrice,
		},
		{
			name:    "b7a9 quiet math d0 family charges instruction bits",
			code:    testOpcodeCell(t, "b7a900"),
			version: 3,
			wantGas: vm.InstructionBaseGasPrice + 24 + vm.ExceptionGasPrice,
		},
		{
			name:    "ordinary long ton family charges base only",
			code:    testOpcodeCell(t, "f83402"),
			version: 8,
			wantGas: vm.InstructionBaseGasPrice + vm.ExceptionGasPrice,
		},
		{
			name:    "short future long ton opcode gates before suffix decode",
			code:    cell.BeginCell().MustStoreUInt(0xf881, 16).EndCell(),
			version: 10,
			wantGas: vm.InstructionBaseGasPrice + vm.ExceptionGasPrice,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			machine := NewTVM()
			if err := machine.SetGlobalVersion(tt.version); err != nil {
				t.Fatalf("set global version: %v", err)
			}

			res, err := machine.Execute(tt.code, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(1_000), vm.NewStack(), ExecutionConfig{})
			if err != nil {
				t.Fatalf("execute low global version: %v", err)
			}
			if res.ExitCode != vmerr.CodeInvalidOpcode {
				t.Fatalf("exit code = %d, want invalid opcode", res.ExitCode)
			}
			if res.GasUsed != tt.wantGas {
				t.Fatalf("gas used = %d, want %d", res.GasUsed, tt.wantGas)
			}
		})
	}
}

func TestTVMExplicitGlobalVersionZeroDoesNotDefault(t *testing.T) {
	machine := NewTVM()
	if err := machine.SetGlobalVersion(0); err != nil {
		t.Fatalf("set global version 0: %v", err)
	}

	res, err := machine.Execute(testOpcodeCell(t, "f807"), cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(1_000), vm.NewStack(), ExecutionConfig{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.ExitCode != vmerr.CodeInvalidOpcode {
		t.Fatalf("explicit version 0 exit code = %d, want invalid opcode", res.ExitCode)
	}
}

func FuzzTVMVersionedOpcodeAvailability(f *testing.F) {
	for i := range versionedOpcodeAvailabilityCases {
		for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
			f.Add(uint8(i), int64(version))
		}
	}
	f.Fuzz(func(t *testing.T, idx uint8, rawVersion int64) {
		tt := versionedOpcodeAvailabilityCases[int(idx)%len(versionedOpcodeAvailabilityCases)]
		version := tvmFuzzGlobalVersion(rawVersion)

		machine := NewTVM()
		if err := machine.SetGlobalVersion(version); err != nil {
			t.Fatalf("set global version %d: %v", version, err)
		}
		res, err := machine.Execute(testOpcodeCell(t, tt.code), cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(1_000), vm.NewStack(), ExecutionConfig{})
		if err != nil {
			t.Fatalf("execute version %d opcode %s: %v", version, tt.code, err)
		}
		if version < tt.minVersion && res.ExitCode != vmerr.CodeInvalidOpcode {
			t.Fatalf("version %d opcode %s exit = %d, want invalid opcode", version, tt.code, res.ExitCode)
		}
		if version >= tt.minVersion && res.ExitCode == vmerr.CodeInvalidOpcode {
			t.Fatalf("version %d opcode %s should be available", version, tt.code)
		}
	})
}

func FuzzTVMVersionedOpcodeTruncatedDispatchBoundary(f *testing.F) {
	for i, tt := range versionedOpcodeAvailabilityCases {
		fullBits := uint16(len(tt.code) * 4)
		for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
			f.Add(uint8(i), int64(version), fullBits)
		}
		f.Add(uint8(i), int64(tt.minVersion-1), uint16(1))
		f.Add(uint8(i), int64(tt.minVersion-1), fullBits/2)
		f.Add(uint8(i), int64(tt.minVersion-1), fullBits-1)
		f.Add(uint8(i), int64(tt.minVersion), fullBits-1)
		f.Add(uint8(i), int64(tt.minVersion), fullBits)
		f.Add(uint8(i), int64(MaxSupportedGlobalVersion), fullBits)
	}

	f.Fuzz(func(t *testing.T, idx uint8, rawVersion int64, rawBits uint16) {
		tt := versionedOpcodeAvailabilityCases[int(idx)%len(versionedOpcodeAvailabilityCases)]
		fullBits := uint(len(tt.code) * 4)
		if fullBits == 0 {
			t.Fatalf("%s has empty opcode fixture", tt.name)
		}
		bits := uint(rawBits) % (fullBits + 1)
		if bits == 0 {
			return
		}

		code := testOpcodePrefixCell(t, tt.code, bits)
		getter := NewTVM().matchOpcode(code.MustBeginParse())
		if getter == nil {
			return
		}
		versioned, ok := getter().(vm.VersionedOp)
		if !ok || versioned.MinGlobalVersion() != tt.minVersion {
			return
		}

		version := tvmFuzzGlobalVersion(rawVersion)
		machine := NewTVM()
		if err := machine.SetGlobalVersion(version); err != nil {
			t.Fatalf("set global version %d: %v", version, err)
		}
		res, err := machine.Execute(code, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(1_000), vm.NewStack(), ExecutionConfig{})
		if err != nil {
			t.Fatalf("execute version %d opcode %s/%d bits: %v", version, tt.code, bits, err)
		}

		if version < tt.minVersion {
			if res.ExitCode != vmerr.CodeInvalidOpcode {
				t.Fatalf("truncated %s/%d bits version=%d exit=%d, want invalid opcode before v%d", tt.name, bits, version, res.ExitCode, tt.minVersion)
			}
			return
		}
		if bits < fullBits && res.ExitCode != vmerr.CodeInvalidOpcode {
			t.Fatalf("truncated %s/%d bits version=%d exit=%d, want invalid opcode after deserialize failure", tt.name, bits, version, res.ExitCode)
		}
		if bits == fullBits && res.ExitCode == vmerr.CodeInvalidOpcode {
			t.Fatalf("full %s version=%d decoded as invalid opcode", tt.name, version)
		}
	})
}

func FuzzTVMGetMethodGlobalVersionBoundary(f *testing.F) {
	for version := int64(MinSupportedGlobalVersion); version <= int64(MaxSupportedGlobalVersion); version++ {
		f.Add(version)
	}

	f.Fuzz(func(t *testing.T, rawVersion int64) {
		version := tvmFuzzGlobalVersion(rawVersion)

		machine, err := NewTVM().WithGlobalVersion(version)
		if err != nil {
			t.Fatalf("with global version %d: %v", version, err)
		}
		stack := vm.NewStack()
		if err = stack.PushSmallInt(0); err != nil {
			t.Fatalf("push method id: %v", err)
		}

		res, err := machine.ExecuteGetMethod(testOpcodeCell(t, "30f807"), cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(100_000), stack, ExecutionConfig{})
		if err != nil {
			t.Fatalf("execute get method version %d: %v", version, err)
		}
		if version < 4 {
			if res.ExitCode != vmerr.CodeInvalidOpcode {
				t.Fatalf("version %d exit = %d, want invalid opcode", version, res.ExitCode)
			}
			return
		}

		if !vm.IsSuccessExitCode(res.ExitCode) {
			t.Fatalf("version %d exit = %d, want success", version, res.ExitCode)
		}
		if _, err = res.Stack.PopInt(); err != nil {
			t.Fatalf("version %d expected GASCONSUMED result: %v", version, err)
		}
	})
}

func FuzzTVMLibraryCodeCellStartupConversion(f *testing.F) {
	for version := int64(MinSupportedGlobalVersion); version <= int64(MaxSupportedGlobalVersion); version++ {
		f.Add(version, true)
		f.Add(version, false)
	}

	f.Fuzz(func(t *testing.T, rawVersion int64, withLibrary bool) {
		version := tvmFuzzGlobalVersion(rawVersion)
		target := codeFromBuilders(t,
			stackop.PUSHINT(big.NewInt(7)).Serialize(),
		)
		code := mustLibraryCellForHash(t, target.Hash())

		var libraries []*cell.Cell
		if withLibrary {
			libraries = []*cell.Cell{mustLibraryCollection(t, target)}
		}

		machine, err := NewTVM().WithGlobalVersion(version)
		if err != nil {
			t.Fatalf("WithGlobalVersion(%d): %v", version, err)
		}
		res, err := machine.Execute(code, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(10000), vm.NewStack(), ExecutionConfig{Libraries: libraries})
		if err != nil {
			t.Fatalf("ExecuteDetailedWithLibraries v%d withLibrary=%t: %v", version, withLibrary, err)
		}

		if !withLibrary {
			if res.ExitCode != vmerr.CodeCellUnderflow {
				t.Fatalf("v%d missing library exit = %d, want cell underflow", version, res.ExitCode)
			}
			return
		}

		if res.ExitCode != 0 {
			t.Fatalf("v%d library code exit = %d, want success", version, res.ExitCode)
		}
		got, err := res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("v%d pop result: %v", version, err)
		}
		if got.Int64() != 7 {
			t.Fatalf("v%d result = %d, want 7", version, got.Int64())
		}
	})
}

func FuzzTVMLibraryCodeCellStartupV9BoundaryCosts(f *testing.F) {
	f.Add(uint64(0), uint8(1))
	f.Add(uint64(0x7F), uint8(2))
	f.Add(uint64(0x80), uint8(3))
	f.Add(uint64(0x11223344), uint8(4))

	f.Fuzz(func(t *testing.T, rawSeed uint64, rawCount uint8) {
		target, values := fuzzLibraryStartupTarget(t, rawSeed, rawCount)
		code := mustLibraryCellForHash(t, target.Hash())
		libraries := mustLibraryCollection(t, target)

		legacy := executeLibraryStartupTarget(t, 8, code, libraries)
		direct := executeLibraryStartupTarget(t, 9, code, libraries)
		assertLibraryStartupStack(t, legacy, values)
		assertLibraryStartupStack(t, direct, values)

		if legacy.Steps <= direct.Steps {
			t.Fatalf("v8 steps = %d, v9 steps = %d; v8 should include an implicit jump through the code ref", legacy.Steps, direct.Steps)
		}
		if legacy.GasUsed <= direct.GasUsed {
			t.Fatalf("v8 gas = %d, v9 gas = %d; v8 should charge the implicit code ref jump", legacy.GasUsed, direct.GasUsed)
		}
	})
}

func fuzzLibraryStartupTarget(t *testing.T, rawSeed uint64, rawCount uint8) (*cell.Cell, []int64) {
	t.Helper()

	count := int(rawCount%4) + 1
	builders := make([]*cell.Builder, 0, count)
	values := make([]int64, count)
	for i := range count {
		value := int64(int8(rawSeed >> uint((i%8)*8)))
		builders = append(builders, stackop.PUSHINT(big.NewInt(value)).Serialize())
		values[i] = value
	}
	return codeFromBuilders(t, builders...), values
}

func executeLibraryStartupTarget(t *testing.T, version int, code, libraries *cell.Cell) *ExecutionResult {
	t.Helper()

	machine, err := NewTVM().WithGlobalVersion(version)
	if err != nil {
		t.Fatalf("WithGlobalVersion(%d): %v", version, err)
	}
	res, err := machine.Execute(code, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.GasWithLimit(10000), vm.NewStack(), ExecutionConfig{Libraries: []*cell.Cell{libraries}})
	if err != nil {
		t.Fatalf("ExecuteDetailedWithLibraries v%d: %v", version, err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("v%d exit code = %d, want success", version, res.ExitCode)
	}
	return res
}

func assertLibraryStartupStack(t *testing.T, res *ExecutionResult, values []int64) {
	t.Helper()

	if res.Stack.Len() != len(values) {
		t.Fatalf("stack len = %d, want %d", res.Stack.Len(), len(values))
	}
	for i := len(values) - 1; i >= 0; i-- {
		got, err := res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("pop result %d: %v", i, err)
		}
		if got.Int64() != values[i] {
			t.Fatalf("result %d = %d, want %d", i, got.Int64(), values[i])
		}
	}
}

func testOpcodeCell(t *testing.T, hexCode string) *cell.Cell {
	t.Helper()

	code, err := hex.DecodeString(hexCode)
	if err != nil {
		t.Fatalf("decode opcode: %v", err)
	}
	return cell.BeginCell().MustStoreSlice(code, uint(len(code))*8).EndCell()
}

func testOpcodePrefixCell(t *testing.T, hexCode string, bits uint) *cell.Cell {
	t.Helper()

	code, err := hex.DecodeString(hexCode)
	if err != nil {
		t.Fatalf("decode opcode: %v", err)
	}
	if bits > uint(len(code))*8 {
		t.Fatalf("opcode prefix bits = %d, want <= %d", bits, len(code)*8)
	}
	return cell.BeginCell().MustStoreSlice(code, bits).EndCell()
}

func assertNormalizedVMErrorCode(t *testing.T, err error, want int64) {
	t.Helper()

	got, ok := vmerr.ErrorCode(err)
	if !ok {
		t.Fatalf("missing VM error code in %v", err)
	}
	if got != want {
		t.Fatalf("VM error code = %d, want %d", got, want)
	}
}

func errorNormalizationFuzzCase(rawGroup, rawVariant byte) (error, int64) {
	switch rawGroup % 3 {
	case 0:
		switch rawVariant % 3 {
		case 0:
			return cell.ErrNotEnoughData(3, 8), vmerr.CodeCellUnderflow
		case 1:
			return cell.ErrNoMoreRefs, vmerr.CodeCellUnderflow
		default:
			return cell.ErrSmallSlice, vmerr.CodeCellUnderflow
		}
	case 1:
		switch rawVariant % 4 {
		case 0:
			return cell.ErrNotFit1023, vmerr.CodeCellOverflow
		case 1:
			return cell.ErrTooMuchRefs, vmerr.CodeCellOverflow
		case 2:
			return cell.ErrCellDepthLimit, vmerr.CodeCellOverflow
		default:
			return cell.ErrRefCannotBeNil, vmerr.CodeCellOverflow
		}
	default:
		switch rawVariant % 5 {
		case 0:
			return cell.ErrTooBigValue, vmerr.CodeRangeCheck
		case 1:
			return cell.ErrNegative, vmerr.CodeRangeCheck
		case 2:
			return cell.ErrInvalidSize, vmerr.CodeRangeCheck
		case 3:
			return cell.ErrNilBigInt, vmerr.CodeRangeCheck
		default:
			return cell.ErrTooBigSize, vmerr.CodeRangeCheck
		}
	}
}

type versionGateNoGasOp struct{}

func (versionGateNoGasOp) GetPrefixes() []*cell.Slice    { return nil }
func (versionGateNoGasOp) Deserialize(*cell.Slice) error { return nil }
func (versionGateNoGasOp) Serialize() *cell.Builder      { return cell.BeginCell() }
func (versionGateNoGasOp) SerializeText() string         { return "VERSION_GATE_NO_GAS" }
func (versionGateNoGasOp) Interpret(*vm.State) error     { return nil }

func TestTVM_Execute(t *testing.T) {
	v := NewTVM()

	walletV3CodeBytes, _ := hex.DecodeString("b5ee9c720101010100710000deff0020dd2082014c97ba218201339cbab19f71b0ed44d0d31fd31f31d70bffe304e0a4f2608308d71820d31fd31fd31ff82313bbf263ed44d0d31fd31fd3ffd15132baf2a15144baf2a204f901541055f910f2a3f8009320d74a96d307d402fb00e8d101a4c8cb1fcb1fcbffc9ed54")
	code, _ := cell.FromBOC(walletV3CodeBytes)

	walletV3DataBytes, _ := hex.DecodeString("b5ee9c7201010101002a0000500000002a29a9a317f7a26c623ca2429ae80fbb17786b0b523ba71c1dbb13fbcdb8ded762a71cd867")
	data, _ := cell.FromBOC(walletV3DataBytes)

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(85143))

	_, err := v.Execute(code, data, tuple.Tuple{}, vm.NewGas(), s, ExecutionConfig{})
	if err != nil {
		t.Fatal(err)
	}

	t.Log(s.String())
}

func TestTVM_ExecuteRetAltIsSuccessful(t *testing.T) {
	v := NewTVM()

	code := cell.BeginCell().MustStoreSlice([]byte{0xDB, 0x31}, 16).EndCell()

	_, err := v.Execute(code, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.NewGas(), vm.NewStack(), ExecutionConfig{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTVM_ExecuteJetton(t *testing.T) {
	v := NewTVM()

	jettonCodeBytes, _ := hex.DecodeString("b5ee9c72010214010003b7000114ff00f4a413f4bcf2c80b0102016202030202ca0405020120121302012006070201d2101101ddd0831c02497c138007434c0c05c6c2544d7c0fc08783e903e900c7e800c5c75c87e800c7e800c1cea6d0000b4c7e08403e29fa954882ea54c4d167c07b8208405e3514654882ea58c511100fc07f80d60841657c1ef14842ea50c167c08381b08a0842cf6ecbe2eb8c096e103fcbc208020120090a0060ed44d0d300fa00fa40fa40d430345144c705f28d048040d721d30030443404c8cb005003fa0201cf1601cf16ccc9ed540011bdf48860e175e5c29b0201f40b0c01f700f4cffe803e90087c05bb513434c03e803e903e90350c093c96d44de8548af1c17cb8b04a70bffcb8b0950d549c15180104d50541413232c01400fe808073c58073c5b332487232c044fd0004bd0032c032483e401c1d3232c0b281f2fff274017e903d010c7e800835d270803cb8b11de0063232c1540273c59c200d01f33b513434c03e803e903e90350c0274cffe80145468017e903e9014d731c1551cdb9c15180104d50541413232c01400fe808073c58073c5b332487232c044fd0004bd0032c0327e401c1d3232c0b281f2fff2741403b1c1476c7cb8b0c2fe80146e6860822625a020822625a004ad822860823938702806684a200e00c2fa0218cb6b13cc8210178d4519c8cb1f1acb3f5008fa0222cf165007cf1626fa025004cf16c95006cc2491729171e25009a814a08208e4e1c0aa008208989680a0a015bcf2e2c505c98040fb00504404c8cb005003fa0201cf1601cf16ccc9ed5401f68e38528aa019a182107362d09cc8cb1f5230cb3f58fa025008cf165008cf16c9718010c8cb0524cf165007fa0216cb6a15ccc971fb001035103497104a1039385f04e226d70b01c30024c200b08e238210d53276db708010c8cb055009cf165005fa0217cb6a13cb1f13cb3fc972fb0050039410266c32e24003040f002404c8cb005003fa0201cf1601cf16ccc9ed5400e53b513434c03e803e903e90350c0234cffe803e900c1454685492b1c17cb8b04a30bffcb8b0a0823938702a8005e805ef3cb8b0e0841ef765f7b232c7c5b2cfd4013e8088f3c58073c5b25c60063232c14973c59c3e80b2dab33260103ec01409013232c01400fe808073c58073c5b3327b5520008f200835c87b513434c03e803e903e90350c0174c7e08405e3514654882ea0841ef765f784ee84ac7cb8b174cfcc7e800c04e800d409013232c01400fe808073c58073c5b3327b55200023bfd8176a26869807d007d207d206a18360a40023be1b576a26869807d007d207d206a182f824")
	code, _ := cell.FromBOC(jettonCodeBytes)

	jettonDataBytes, _ := hex.DecodeString("b5ee9c720102150100040200018f21dcd65004001e5bddddeed8df4a41249e4659ad2f258fa34d2219ac52c6f12e4b71e2d145618005401b901d3598d73c65306697791a78e2bdd28b1d06508007239279ff2b33df30010114ff00f4a413f4bcf2c80b0202016203040202ca050602012013140201200708008fd600835c87b513434c03e803e903e90350c0174c7e08405e3514654882ea0841ef765f784ee84ac7cb8b174cfcc7e800c04e800d409013232c01400fe808073c58073c5b3327b55201ddd0831c02497c138007434c0c05c6c2544d7c0fc08383e903e900c7e800c5c75c87e800c7e800c1cea6d0000b4c7e08403e29fa954882ea54c4d167c0778208405e3514654882ea58c511100fc07b80d60841657c1ef14842ea50c167c07f81b08a0842cf6ecbe2eb8c096e103fcbc2090201200a0b0060ed44d0d300fa00fa40fa40d430345144c705f28d048040d721d30030443404c8cb005003fa0201cf1601cf16ccc9ed540011bdf48860e175e5c29b0201580c0d01f7503d33ffa00fa4021f016ed44d0d300fa00fa40fa40d43024f25b5137a1522bc705f2e2c129c2fff2e2c2543552705460041354150504c8cb005003fa0201cf1601cf16ccc921c8cb0113f40012f400cb00c920f9007074c8cb02ca07cbffc9d005fa40f40431fa0020d749c200f2e2c4778018c8cb055009cf167080e0201200f1000c2fa0218cb6b13cc8210178d4519c8cb1f1acb3f5008fa0222cf165007cf1626fa025004cf16c95006cc2491729171e25009a814a08208e4e1c0aa008208989680a0a015bcf2e2c505c98040fb00504404c8cb005003fa0201cf1601cf16ccc9ed5401f33b513434c03e803e903e90350c0274cffe80145468017e903e9014d731c1551cdb9c15180104d50541413232c01400fe808073c58073c5b332487232c044fd0004bd0032c0327e401c1d3232c0b281f2fff2741403b1c1476c7cb8b0c2fe80146e6860822625a020822625a004ad822860823938702806684a201100e53b513434c03e803e903e90350c0234cffe803e900c1454685492b1c17cb8b04a30bffcb8b0a0823938702a8005e805ef3cb8b0e0841ef765f7b232c7c5b2cfd4013e8088f3c58073c5b25c60063232c14973c59c3e80b2dab33260103ec01409013232c01400fe808073c58073c5b3327b552001f68e38528aa019a182107362d09cc8cb1f5230cb3f58fa025008cf165008cf16c9718010c8cb0524cf165007fa0216cb6a15ccc971fb001035103497104a1039385f04e226d70b01c30024c200b08e238210d53276db708010c8cb055009cf165005fa0217cb6a13cb1f13cb3fc972fb0050039410266c32e240030412002404c8cb005003fa0201cf1601cf16ccc9ed540023bfd8176a26869807d007d207d206a18360a40023be1b576a26869807d007d207d206a182f824")
	data, _ := cell.FromBOC(jettonDataBytes)

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(115562))

	_, err := v.Execute(code, data, tuple.Tuple{}, vm.NewGas(), s, ExecutionConfig{})
	if err != nil {
		t.Fatal(err)
	}

	t.Log(s.String())
}

func TestTVM_ExecuteTvmTests(t *testing.T) {
	v := NewTVM()

	contractCodeBytes, _ := hex.DecodeString("b5ee9c7241010c0100bf000114ff00f4a413f4bcf2c80b0102016202070202ce0306020120040500691b088831c02456f8007434c0cc1caa42644c383c0074c7f4cfcc4060841fa1d93beea6f4c7cc3e1080683e18bc00b80c2103fcbc20001d3b513434c7c07e1874c7c07e18b460001d4c8f84101cb1ff84201cb1fc9ed548020120080b020148090a000db7203e003f08300057b62bddb45dbf7da83da87da89da8bda8f2ab6e3b66261dacfdacbdac9dac7dac32749b663da83dbe203e5ff0000dbe5687800fc2144543bc0e")
	code, _ := cell.FromBOC(contractCodeBytes)

	contractDataBytes, _ := hex.DecodeString("b5ee9c720102150100040200018f21dcd65004001e5bddddeed8df4a41249e4659ad2f258fa34d2219ac52c6f12e4b71e2d145618005401b901d3598d73c65306697791a78e2bdd28b1d06508007239279ff2b33df30010114ff00f4a413f4bcf2c80b0202016203040202ca050602012013140201200708008fd600835c87b513434c03e803e903e90350c0174c7e08405e3514654882ea0841ef765f784ee84ac7cb8b174cfcc7e800c04e800d409013232c01400fe808073c58073c5b3327b55201ddd0831c02497c138007434c0c05c6c2544d7c0fc08383e903e900c7e800c5c75c87e800c7e800c1cea6d0000b4c7e08403e29fa954882ea54c4d167c0778208405e3514654882ea58c511100fc07b80d60841657c1ef14842ea50c167c07f81b08a0842cf6ecbe2eb8c096e103fcbc2090201200a0b0060ed44d0d300fa00fa40fa40d430345144c705f28d048040d721d30030443404c8cb005003fa0201cf1601cf16ccc9ed540011bdf48860e175e5c29b0201580c0d01f7503d33ffa00fa4021f016ed44d0d300fa00fa40fa40d43024f25b5137a1522bc705f2e2c129c2fff2e2c2543552705460041354150504c8cb005003fa0201cf1601cf16ccc921c8cb0113f40012f400cb00c920f9007074c8cb02ca07cbffc9d005fa40f40431fa0020d749c200f2e2c4778018c8cb055009cf167080e0201200f1000c2fa0218cb6b13cc8210178d4519c8cb1f1acb3f5008fa0222cf165007cf1626fa025004cf16c95006cc2491729171e25009a814a08208e4e1c0aa008208989680a0a015bcf2e2c505c98040fb00504404c8cb005003fa0201cf1601cf16ccc9ed5401f33b513434c03e803e903e90350c0274cffe80145468017e903e9014d731c1551cdb9c15180104d50541413232c01400fe808073c58073c5b332487232c044fd0004bd0032c0327e401c1d3232c0b281f2fff2741403b1c1476c7cb8b0c2fe80146e6860822625a020822625a004ad822860823938702806684a201100e53b513434c03e803e903e90350c0234cffe803e900c1454685492b1c17cb8b04a30bffcb8b0a0823938702a8005e805ef3cb8b0e0841ef765f7b232c7c5b2cfd4013e8088f3c58073c5b25c60063232c14973c59c3e80b2dab33260103ec01409013232c01400fe808073c58073c5b3327b552001f68e38528aa019a182107362d09cc8cb1f5230cb3f58fa025008cf165008cf16c9718010c8cb0524cf165007fa0216cb6a15ccc971fb001035103497104a1039385f04e226d70b01c30024c200b08e238210d53276db708010c8cb055009cf165005fa0217cb6a13cb1f13cb3fc972fb0050039410266c32e240030412002404c8cb005003fa0201cf1601cf16ccc9ed540023bfd8176a26869807d007d207d206a18360a40023be1b576a26869807d007d207d206a182f824")
	data, _ := cell.FromBOC(contractDataBytes)

	id := tlb.MethodNameHash("tryCatch")
	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(2))
	_ = s.PushInt(big.NewInt(int64(id)))

	_, err := v.Execute(code, data, tuple.Tuple{}, vm.NewGas(), s, ExecutionConfig{})
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 3 {
		t.Fatal("result is not 3:", res.Int64())
	}
}

func TestTVM_ExecuteTvmTestLoops(t *testing.T) {
	v := NewTVM()

	/*
		get repeatWhileUntil(x: int): int {
		    var result: int = 0;
		    try {
		        repeat (5) {
		            var v: int = 10;
		            while (v > 0) {
		                v -= 1;
		                var f: int = 3;
		                do {
		                    result += 1;
		                    if ((result > 5) & (x > 0)) {
		                        throw 55;
		                    }
							f -= 1;
		                } while (f > 0);
		            }
					dumpStack();
		        }
		    } catch {
		        result = 100;
		    }
		    return result;
		}
	*/

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(0))
	_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash("tryRepeatWhileUntil"))))

	tm := time.Now()
	_, err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.NewGas(), s, ExecutionConfig{})
	if err != nil {
		t.Fatal(err)
	}
	println(">>>", time.Since(tm).String())

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 150 {
		t.Fatal("result is not 150:", res.Int64())
	}
}

func TestTVM_ExecuteTvmTestSimpleRepeat(t *testing.T) {
	v := NewTVM()

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(2))
	_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash("simpleRepeat"))))

	_, err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.NewGas(), s, ExecutionConfig{})
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 7 {
		t.Fatal("result is not 7:", res.Int64())
	}
}

func TestTVM_ExecuteTvmTestSimpleRepeatWhile(t *testing.T) {
	v := NewTVM()

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(2))
	_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash("simpleRepeatWhile"))))

	_, err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.NewGas(), s, ExecutionConfig{})
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 27 {
		t.Fatal("result is not 7:", res.Int64())
	}
}

func TestTVM_ExecuteTvmTestSimpleUntilWhile(t *testing.T) {
	v := NewTVM()

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(2))
	_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash("simpleUntilWhile"))))

	_, err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.NewGas(), s, ExecutionConfig{})
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 27 {
		t.Fatal("result is not 7:", res.Int64())
	}
}

func TestTVM_ExecuteTvmTestSimpleRepeatUntil(t *testing.T) {
	v := NewTVM()

	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(2))
	_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash("simpleRepeatUntil"))))

	_, err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.NewGas(), s, ExecutionConfig{})
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.PopInt()
	if err != nil {
		t.Fatal(err)
	}

	if res.Int64() != 27 {
		t.Fatal("result is not 7:", res.Int64())
	}
}

func TestTVM_SuperContract(t *testing.T) {
	type args struct {
		name   string
		input  int64
		result int64
	}
	tests := []struct {
		args    args
		wantErr int64
	}{
		{
			args: args{
				name:   "simpleRepeatUntil",
				input:  2,
				result: 27,
			},
			wantErr: 0,
		},
		{
			args: args{
				name:   "simpleUntilWhile",
				input:  3,
				result: 28,
			},
			wantErr: 0,
		},
		{
			args: args{
				name:   "simpleRepeatWhile",
				input:  2,
				result: 27,
			},
			wantErr: 0,
		},
		{
			args: args{
				name:   "simpleRepeat",
				input:  2,
				result: 7,
			},
			wantErr: 0,
		},
		{
			args: args{
				name:   "tryRepeatWhileUntil",
				input:  0,
				result: 150,
			},
			wantErr: 0,
		},
		{
			args: args{
				name:   "tryRepeatWhileUntil",
				input:  1,
				result: 100,
			},
			wantErr: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.args.name, func(t *testing.T) {
			v := NewTVM()

			s := vm.NewStack()
			_ = s.PushInt(big.NewInt(tt.args.input))
			_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash(tt.args.name))))

			_, err := v.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.NewGas(), s, ExecutionConfig{})
			if err != nil {
				if tt.wantErr >= 1 {
					var e vmerr.VMError
					if errors.As(err, &e) {
						if e.Code == tt.wantErr {
							return
						}
					}
				}
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr >= 1 {
				t.Errorf("Execute() no error, wantErr %v", tt.wantErr)
			}

			res, err := s.PopInt()
			if err != nil {
				t.Errorf("PopInt() error = %v", err)
			}

			if res.Int64() != tt.args.result {
				t.Errorf("result %v, but want %v", res.Int64(), tt.args.result)
			}
		})
	}
}
