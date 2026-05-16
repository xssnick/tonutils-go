package tvm

import "testing"

func markRegistered(node *trieNode, want map[string]bool) {
	if node == nil {
		return
	}
	if node.op != nil {
		name := node.op().SerializeText()
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

	markRegistered(machine.trie, want)

	for name, ok := range want {
		if !ok {
			t.Fatalf("tuple opcode %s was not registered in TVM", name)
		}
	}
}
