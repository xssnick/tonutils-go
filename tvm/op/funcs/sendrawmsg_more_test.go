package funcs

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestSendRawMsgMoreEdges(t *testing.T) {
	t.Run("mode range checks run before cell handling", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushCell(cell.BeginCell().EndCell()); err != nil {
			t.Fatalf("PushCell failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(256)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := SENDRAWMSG().Interpret(st); err == nil {
			t.Fatal("SENDRAWMSG should reject modes above 255")
		}
	})

	t.Run("missing message cells propagate stack errors", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := SENDRAWMSG().Interpret(st); err == nil {
			t.Fatal("SENDRAWMSG should fail when the message cell is missing")
		}
	})
}
