package tvm

import (
	"testing"

	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestInitialC3CarriesCurrentCodepage(t *testing.T) {
	code := codeFromBuilders(t, execop.PUSHCTR(3).Serialize())
	_, result, err := runRawCode(code)
	if err != nil {
		t.Fatal(err)
	}
	cont, err := result.Stack.PopContinuation()
	if err != nil {
		t.Fatal(err)
	}
	ordinary, ok := cont.(*vm.OrdinaryContinuation)
	if !ok {
		t.Fatalf("c3 type = %T, want ordinary continuation", cont)
	}
	if ordinary.Data.CP != 0 {
		t.Fatalf("c3 codepage = %d, want 0", ordinary.Data.CP)
	}
}
