package tvm

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	opcellslice "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	opmath "github.com/xssnick/tonutils-go/tvm/op/math"
	opstack "github.com/xssnick/tonutils-go/tvm/op/stack"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMStepUsesBitTrieForNonByteAlignedOpcode(t *testing.T) {
	machine := NewTVM()

	state := &vm.State{
		CurrentCode: opstack.PUSHCTR(0).Serialize().EndCell().MustBeginParse(),
		Reg: vm.Register{
			C: [4]vm.Continuation{
				&vm.QuitContinuation{ExitCode: 7},
			},
		},
		Stack: vm.NewStack(),
		Gas:   vm.NewGas(),
	}

	if err := machine.step(state); err != nil {
		t.Fatal(err)
	}

	cont, err := state.Stack.PopContinuation()
	if err != nil {
		t.Fatal(err)
	}

	quit, ok := cont.(*vm.QuitContinuation)
	if !ok {
		t.Fatalf("expected quit continuation, got %T", cont)
	}
	if quit.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", quit.ExitCode)
	}
}

func TestTVMRegistersSDBEGINSConstPrefix(t *testing.T) {
	machine := NewTVM()
	code := opcellslice.SDBEGINSCONST(cell.BeginCell().MustStoreUInt(0b101, 3).EndCell().MustBeginParse(), false).Serialize().EndCell().MustBeginParse()

	if got := machine.matchOpcode(code); got == nil {
		t.Fatal("expected SDBEGINS const opcode to be registered in trie")
	}
}

func TestTVMMatchesWalletTraceSDBEGINSConstOpcode(t *testing.T) {
	machine := NewTVM()
	raw, err := hex.DecodeString("D72820761E436C20D749C008F2E09320D74AC002F2E09320D71D06C712C2005230B0F2D089D74CD7393001A4")
	if err != nil {
		t.Fatalf("decode trace opcode: %v", err)
	}
	code := cell.BeginCell().MustStoreSlice(raw, 352).EndCell().MustBeginParse()

	if got := machine.matchOpcode(code); got == nil {
		t.Fatal("expected wallet trace D728 opcode to be registered in trie")
	}
}

func TestTVMMatchesF88111AsInMsgParams(t *testing.T) {
	machine := NewTVM()
	code := cell.BeginCell().MustStoreUInt(0xF88111, 24).EndCell().MustBeginParse()

	getter := machine.matchOpcode(code)
	if getter == nil {
		t.Fatal("expected F88111 opcode to be registered in trie")
	}
	op := getter()
	if err := op.Deserialize(code); err != nil {
		t.Fatalf("deserialize F88111: %v", err)
	}
	if got := op.SerializeText(); got != "INMSGPARAMS" {
		t.Fatalf("F88111 matched %q, want INMSGPARAMS", got)
	}
}

func TestTVMOpcodeMatcherSlowPathLongestPrefix(t *testing.T) {
	machine := &TVM{}
	machine.dispatches[MaxSupportedGlobalVersion] = newOpcodeDispatch()
	shortPrefix := cell.BeginCell().MustStoreUInt(0xa5, 8).EndCell().MustBeginParse()
	longPrefix := cell.BeginCell().
		MustStoreUInt(0xa5, 8).
		MustStoreUInt(0, 63).
		MustStoreUInt(1, 1).
		EndCell().
		MustBeginParse()

	machine.addTriePrefix(shortPrefix, trieDispatchTestGetter("short"))
	machine.addTriePrefix(longPrefix, trieDispatchTestGetter("long"))
	if machine.dispatches[MaxSupportedGlobalVersion].maxPrefixLen <= 64 {
		t.Fatalf("max prefix len = %d, want slow matcher path", machine.dispatches[MaxSupportedGlobalVersion].maxPrefixLen)
	}

	longCode := cell.BeginCell().
		MustStoreUInt(0xa5, 8).
		MustStoreUInt(0, 63).
		MustStoreUInt(1, 1).
		EndCell()
	if got := trieDispatchMatchedName(t, machine.matchOpcode(longCode.MustBeginParse()), longCode); got != "tvm.trieDispatchTestOp:long" {
		t.Fatalf("long prefix matched %q, want long", got)
	}

	divergingCode := cell.BeginCell().MustStoreUInt(0x14b, 9).EndCell()
	if got := trieDispatchMatchedName(t, machine.matchOpcode(divergingCode.MustBeginParse()), divergingCode); got != "tvm.trieDispatchTestOp:short" {
		t.Fatalf("diverging prefix matched %q, want short", got)
	}

	missingCode := cell.BeginCell().MustStoreUInt(0x25, 8).EndCell()
	if got := machine.matchOpcode(missingCode.MustBeginParse()); got != nil {
		t.Fatalf("missing prefix matched %q", trieDispatchMatchedName(t, got, missingCode))
	}
}

func TestTVMOpcodeMatcherRegisteredPrefixesFastSlowParity(t *testing.T) {
	machine := NewTVM()

	for _, getOp := range vm.List {
		op := getOp()
		for _, prefix := range op.GetPrefixes() {
			bits := prefix.BitsLeft()
			code := cell.BeginCell().MustStoreSlice(prefix.MustPreloadSlice(bits), bits).EndCell()

			fast := trieDispatchMatchedName(t, machine.matchOpcodeFast(code.MustBeginParse(), bits), code)
			slow := trieDispatchMatchedName(t, machine.matchOpcodeSlow(code.MustBeginParse(), bits), code)
			if fast != slow {
				t.Fatalf("fast/slow mismatch for %s prefix %s: fast=%q slow=%q", op.SerializeText(), code.Dump(), fast, slow)
			}
		}
	}
}

func TestTVMOpcodeMatcherCompoundInvalidCatchAll(t *testing.T) {
	machine := NewTVM()

	tests := []struct {
		name string
		code *cell.Cell
		text string
	}{
		{name: "valid_adddivmod", code: opmath.ADDDIVMOD().Serialize().EndCell(), text: "ADDDIVMOD"},
		{name: "invalid_divmod_suffix", code: cell.BeginCell().MustStoreUInt(0xa903, 16).EndCell(), text: "DIV/MOD<invalid>"},
		{name: "valid_addrshift_code_mod", code: opmath.ADDRSHIFTCODEMOD(0).Serialize().EndCell(), text: "1 ADDRSHIFT#MOD"},
		{name: "invalid_addrshift_code_mod_suffix", code: cell.BeginCell().MustStoreUInt(0xa93300, 24).EndCell(), text: "SHR#/MOD<invalid>"},
		{name: "valid_mulrshift_code_mod", code: opmath.MULRSHIFTCODEMOD(0).Serialize().EndCell(), text: "1 MULRSHIFT#MOD"},
		{name: "invalid_mulrshift_code_mod_suffix", code: cell.BeginCell().MustStoreUInt(0xa9b300, 24).EndCell(), text: "MULSHR#/MOD<invalid>"},
		{name: "valid_lshiftdivmod_code", code: cell.BeginCell().MustStoreUInt(0xa9dc00, 24).EndCell(), text: "1 LSHIFTDIVMOD#"},
		{name: "invalid_lshiftdivmod_code_suffix", code: cell.BeginCell().MustStoreUInt(0xa9d300, 24).EndCell(), text: "SHLDIV#/MOD<invalid>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trieDispatchMatchedName(t, machine.matchOpcode(tt.code.MustBeginParse()), tt.code); got != "*helpers.AdvancedOP:"+tt.text && got != "*helpers.SimpleOP:"+tt.text {
				t.Fatalf("matched %q, want %q", got, tt.text)
			}
		})
	}
}

func FuzzTVMOpcodeMatcherFastSlowParity(f *testing.F) {
	f.Add(uint64(0xf88111), byte(23))
	f.Add(uint64(0xdb4), byte(11))
	f.Add(uint64(0xa900), byte(15))
	f.Add(uint64(0xd72820761e436c20), byte(63))
	f.Add(uint64(0), byte(0))

	machine := NewTVM()
	f.Fuzz(func(t *testing.T, raw uint64, rawBits byte) {
		bits := uint(rawBits%64) + 1
		value := raw
		if bits < 64 {
			value &= (uint64(1) << bits) - 1
		}
		code := cell.BeginCell().MustStoreUInt(value, bits).EndCell()

		fast := trieDispatchMatchedName(t, machine.matchOpcodeFast(code.MustBeginParse(), bits), code)
		slow := trieDispatchMatchedName(t, machine.matchOpcodeSlow(code.MustBeginParse(), bits), code)
		if fast != slow {
			t.Fatalf("fast/slow mismatch for %s: fast=%q slow=%q", code.Dump(), fast, slow)
		}
	})
}

type trieDispatchTestOp struct {
	name string
}

func trieDispatchTestGetter(name string) vm.OPGetter {
	return func() vm.OP {
		return trieDispatchTestOp{name: name}
	}
}

func (op trieDispatchTestOp) GetPrefixes() []*cell.Slice {
	return nil
}

func (op trieDispatchTestOp) Deserialize(*cell.Slice) error {
	return nil
}

func (op trieDispatchTestOp) Serialize() *cell.Builder {
	return cell.BeginCell()
}

func (op trieDispatchTestOp) SerializeText() string {
	return op.name
}

func (op trieDispatchTestOp) Interpret(*vm.State) error {
	return nil
}

func trieDispatchMatchedName(t *testing.T, getter vm.OPGetter, code *cell.Cell) string {
	t.Helper()

	if getter == nil {
		return "<nil>"
	}
	op := getter()
	err := op.Deserialize(code.MustBeginParse())
	if err != nil {
		return fmt.Sprintf("%T:%v", op, err)
	}
	return fmt.Sprintf("%T:%s", op, op.SerializeText())
}
