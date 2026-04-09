package tvm

import (
	"errors"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
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

	var vmErr vmerr.VMError
	if !errors.As(err, &vmErr) || vmErr.Code != vmerr.CodeFatal {
		t.Fatalf("expected fatal VM error, got %v", err)
	}
	if res == nil || res.ExitCode != vmerr.CodeFatal {
		t.Fatalf("expected fatal exit code, got %#v", res)
	}
}
