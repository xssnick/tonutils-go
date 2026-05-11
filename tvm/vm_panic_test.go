package tvm

import (
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

type panicTestOP struct{}

func (o *panicTestOP) GetPrefixes() []*cell.Slice {
	return []*cell.Slice{cell.BeginCell().MustStoreUInt(0xFF, 8).EndCell().BeginParse()}
}

func (o *panicTestOP) Deserialize(code *cell.Slice) error {
	_, err := code.LoadUInt(8)
	return err
}

func (o *panicTestOP) Serialize() *cell.Builder {
	return cell.BeginCell().MustStoreUInt(0xFF, 8)
}

func (o *panicTestOP) SerializeText() string {
	return "PANIC-TEST"
}

func (o *panicTestOP) Interpret(state *vmcore.State) error {
	panic("panic test")
}

func TestVMRecoversOpcodePanicsAsFatalError(t *testing.T) {
	orig := vmcore.List
	vmcore.List = append(append([]vmcore.OPGetter(nil), orig...), func() vmcore.OP {
		return &panicTestOP{}
	})
	defer func() {
		vmcore.List = orig
	}()

	vm := NewTVM()
	code := cell.BeginCell().MustStoreUInt(0xFF, 8).EndCell()

	res, err := vm.ExecuteDetailed(code, nil, tuple.Tuple{}, vmcore.NewGas(), vmcore.NewStack())
	if err == nil {
		t.Fatal("expected fatal error")
	}
	if res != nil {
		t.Fatalf("expected nil result on fatal execution error, got %#v", res)
	}
	if !strings.Contains(err.Error(), "vm panic: panic test") {
		t.Fatalf("expected panic context in error, got %v", err)
	}
}
