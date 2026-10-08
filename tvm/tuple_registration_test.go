package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/vm"
)

// text names the entry without decoding anything, for a caller that only wants
// to know which opcode sits at a prefix. Walking the table by name is something
// only tests do, so it lives next to its user instead of in the dispatcher.
func (e *dispatchEntry) text() string {
	if e.arg != nil {
		return e.arg.SerializeArgsText(0)
	}
	return e.get().SerializeText()
}

func markRegistered(node *trieNode, want map[string]bool) {
	if node == nil {
		return
	}
	if node.op != nil {
		name := node.op.text()
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	markRegistered(node.next[0], want)
	markRegistered(node.next[1], want)
}

func TestNewTVMRegistersTupleOpcodes(t *testing.T) {
	machine := NewTVM()

	want := map[string]bool{
		"PUSHNULL": false,
		"ISNULL":   false,
		"ISTUPLE":  false,
	}

	markRegistered(machine.dispatches[vm.MaxSupportedGlobalVersion].root, want)

	for name, ok := range want {
		if !ok {
			t.Fatalf("tuple opcode %s was not registered in TVM", name)
		}
	}
}
