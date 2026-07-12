package funcs

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestOpcodeUnderflowPrechecksPreserveShortStack(t *testing.T) {
	pushInt := func(state *vm.State) error {
		return state.Stack.PushInt(big.NewInt(1))
	}
	pushCell := func(state *vm.State) error {
		return state.Stack.PushCell(cell.BeginCell().EndCell())
	}

	tests := []struct {
		name  string
		op    vm.OP
		depth int
		push  func(*vm.State) error
	}{
		{name: "CHKSIGNU", op: CHKSIGNU(), depth: 3, push: pushCell},
		{name: "CHKSIGNS", op: CHKSIGNS(), depth: 3, push: pushCell},
		{name: "ECRECOVER", op: ECRECOVER(), depth: 4, push: pushCell},
		{name: "SECP256K1_XONLY_PUBKEY_TWEAK_ADD", op: SECP256K1_XONLY_PUBKEY_TWEAK_ADD(), depth: 2, push: pushCell},
		{name: "P256_CHKSIGNU", op: P256_CHKSIGNU(), depth: 3, push: pushInt},
		{name: "P256_CHKSIGNS", op: P256_CHKSIGNS(), depth: 3, push: pushInt},
		{name: "RIST255_FROMHASH", op: RIST255_FROMHASH(), depth: 2, push: pushCell},
		{name: "RIST255_ADD", op: RIST255_ADD(), depth: 2, push: pushCell},
		{name: "RIST255_QADD", op: RIST255_QADD(), depth: 2, push: pushCell},
		{name: "RIST255_SUB", op: RIST255_SUB(), depth: 2, push: pushCell},
		{name: "RIST255_QSUB", op: RIST255_QSUB(), depth: 2, push: pushCell},
		{name: "RIST255_MUL", op: RIST255_MUL(), depth: 2, push: pushCell},
		{name: "RIST255_QMUL", op: RIST255_QMUL(), depth: 2, push: pushCell},
		{name: "BLS_VERIFY", op: BLS_VERIFY(), depth: 3, push: pushInt},
		{name: "BLS_FASTAGGREGATEVERIFY", op: BLS_FASTAGGREGATEVERIFY(), depth: 3, push: pushInt},
		{name: "BLS_AGGREGATEVERIFY", op: BLS_AGGREGATEVERIFY(), depth: 2, push: pushInt},
		{name: "BLS_G1_ADD", op: BLS_G1_ADD(), depth: 2, push: pushInt},
		{name: "BLS_G1_SUB", op: BLS_G1_SUB(), depth: 2, push: pushInt},
		{name: "BLS_G1_MUL", op: BLS_G1_MUL(), depth: 2, push: pushCell},
		{name: "BLS_G2_ADD", op: BLS_G2_ADD(), depth: 2, push: pushInt},
		{name: "BLS_G2_SUB", op: BLS_G2_SUB(), depth: 2, push: pushInt},
		{name: "BLS_G2_MUL", op: BLS_G2_MUL(), depth: 2, push: pushCell},
		{name: "STVARINT16", op: STVARINT16(), depth: 2, push: pushCell},
		{name: "STVARUINT32", op: STVARUINT32(), depth: 2, push: pushCell},
		{name: "STVARINT32", op: STVARINT32(), depth: 2, push: pushCell},
		{name: "STSTDADDR", op: STSTDADDR(), depth: 2, push: pushInt},
		{name: "STSTDADDRQ", op: STSTDADDRQ(), depth: 2, push: pushInt},
		{name: "STOPTSTDADDR", op: STOPTSTDADDR(), depth: 2, push: pushInt},
		{name: "STOPTSTDADDRQ", op: STOPTSTDADDRQ(), depth: 2, push: pushInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newFuncTestState(t, nil)
			for i := 1; i < tt.depth; i++ {
				if err := tt.push(state); err != nil {
					t.Fatalf("push short-stack value: %v", err)
				}
			}
			gasBefore := state.Gas.Used()

			err := tt.op.Interpret(state)
			code, ok := vmerr.ErrorCode(err)
			if !ok || code != vmerr.CodeStackUnderflow {
				t.Fatalf("error code = %d (ok=%t), want %d: %v", code, ok, vmerr.CodeStackUnderflow, err)
			}
			if state.Stack.Len() != tt.depth-1 {
				t.Fatalf("short stack was modified, stack len=%d", state.Stack.Len())
			}
			if gas := state.Gas.Used(); gas != gasBefore {
				t.Fatalf("short-stack precheck consumed gas: got %d, want %d", gas, gasBefore)
			}
		})
	}
}
