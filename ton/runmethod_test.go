package ton

import (
	"context"
	"encoding/base64"
	"math/big"
	"reflect"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestExecutionResult(t *testing.T) {
	data := []any{
		big.NewInt(1),
		cell.BeginCell(),
		cell.BeginCell().EndCell(),
		cell.BeginCell().EndCell().MustBeginParse(),
		[]any{big.NewInt(2), big.NewInt(3)},
		tlb.StackNaN{},
		nil,
	}

	res := NewExecutionResult(data)

	if _, err := res.Slice(0); err != ErrIncorrectResultType {
		t.Fatal("is slice wrong err")
	}

	if _, err := res.Slice(14); err != ErrResultIndexOutOfRange {
		t.Fatal("is slice wrong err")
	}

	if _, err := res.Int(1); err != ErrIncorrectResultType {
		t.Fatal("is int wrong err")
	}

	if _, err := res.Int(15); err != ErrResultIndexOutOfRange {
		t.Fatal("is int wrong err")
	}

	if _, err := res.Tuple(1); err != ErrIncorrectResultType {
		t.Fatal("is tuple wrong err")
	}

	if _, err := res.Tuple(15); err != ErrResultIndexOutOfRange {
		t.Fatal("is tuple wrong err")
	}

	if _, err := res.Cell(1); err != ErrIncorrectResultType {
		t.Fatal("is cell wrong err")
	}

	if _, err := res.Cell(15); err != ErrResultIndexOutOfRange {
		t.Fatal("is cell wrong err")
	}

	if _, err := res.Builder(2); err != ErrIncorrectResultType {
		t.Fatal("is builder wrong err")
	}

	if _, err := res.Builder(15); err != ErrResultIndexOutOfRange {
		t.Fatal("is builder wrong err")
	}

	if yes, err := res.IsNil(1); yes || err != nil {
		t.Fatal("is nil wrong")
	}

	if _, err := res.IsNil(15); err != ErrResultIndexOutOfRange {
		t.Fatal("is nil err")
	}

	if yes, err := res.IsNil(6); !yes || err != nil {
		t.Fatal("is nil wrong")
	}

	if yes := res.MustIsNil(6); !yes {
		t.Fatal("must is nil wrong")
	}

	if v := res.MustInt(0); v.Uint64() != 1 {
		t.Fatal("must int wrong")
	}

	if v := res.MustSlice(3); v != data[3] {
		t.Fatal("must slice wrong")
	}

	if v := res.MustCell(2); v != data[2] {
		t.Fatal("must cell wrong")
	}

	if v := res.MustBuilder(1); v != data[1] {
		t.Fatal("must builder wrong")
	}

	if v := res.MustTuple(4); v[0].(*big.Int).Uint64() != 2 {
		t.Fatal("must tuple wrong")
	}

	if v := res.AsTuple(); !reflect.DeepEqual(v, data) {
		t.Fatal("as tuple wrong")
	}
}

func TestRunGetMethodByIDUsesProvidedID(t *testing.T) {
	const methodID uint64 = 123456
	var gotMethodID uint64

	api := NewAPIClient(&ValidationMock{
		Response: RunMethodResult{ExitCode: 0},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(*RunSmcMethod)
			if !ok {
				t.Fatalf("unexpected payload type %T", payload)
			}
			gotMethodID = req.MethodID
			return nil
		},
	}, ProofCheckPolicyUnsafe)

	_, err := api.RunGetMethodByID(context.Background(), &BlockIDExt{}, address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c"), methodID)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethodID != methodID {
		t.Fatalf("method id mismatch: got %d, want %d", gotMethodID, methodID)
	}
}

func TestRunGetMethodHashesName(t *testing.T) {
	const method = "seqno"
	var gotMethodID uint64

	api := NewAPIClient(&ValidationMock{
		Response: RunMethodResult{ExitCode: 0},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(*RunSmcMethod)
			if !ok {
				t.Fatalf("unexpected payload type %T", payload)
			}
			gotMethodID = req.MethodID
			return nil
		},
	}, ProofCheckPolicyUnsafe)

	_, err := api.RunGetMethod(context.Background(), &BlockIDExt{}, address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c"), method)
	if err != nil {
		t.Fatal(err)
	}
	want := tlb.MethodNameHash(method)
	if gotMethodID != want {
		t.Fatalf("method id mismatch: got %d, want %d", gotMethodID, want)
	}
}

func TestRunGetMethodParsesContinuationResult(t *testing.T) {
	fixtureBOC, err := base64.StdEncoding.DecodeString("te6ccgEBAQEACAAACwaAAAAACA==")
	if err != nil {
		t.Fatalf("decode C++ fixture: %v", err)
	}
	fixture, err := cell.FromBOC(fixtureBOC)
	if err != nil {
		t.Fatalf("parse C++ fixture: %v", err)
	}
	resultStack := cell.BeginCell().
		MustStoreUInt(1, 24).
		MustStoreRef(cell.BeginCell().EndCell()).
		MustStoreBuilder(fixture.ToBuilder()).
		EndCell()

	api := NewAPIClient(&ValidationMock{
		Response: RunMethodResult{ExitCode: 0, Result: resultStack},
		CheckQuery: func(payload tl.Serializable) error {
			return nil
		},
	}, ProofCheckPolicyUnsafe)

	result, err := api.RunGetMethodByID(
		context.Background(),
		&BlockIDExt{},
		address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c"),
		0,
	)
	if err != nil {
		t.Fatalf("run get method: %v", err)
	}
	values := result.AsTuple()
	if len(values) != 1 {
		t.Fatalf("result length = %d, want 1", len(values))
	}
	quit, ok := values[0].(*vm.QuitContinuation)
	if !ok || quit.ExitCode != 0 {
		t.Fatalf("result = %#v, want quit continuation with exit code 0", values[0])
	}
}

func TestRunGetMethodPreservesEmptyTuple(t *testing.T) {
	resultStack := tlb.NewStack()
	resultStack.Push([]any{})
	resultCell, err := resultStack.ToCell()
	if err != nil {
		t.Fatalf("serialize result stack: %v", err)
	}

	api := NewAPIClient(&ValidationMock{
		Response: RunMethodResult{ExitCode: 0, Result: resultCell},
		CheckQuery: func(payload tl.Serializable) error {
			req, ok := payload.(*RunSmcMethod)
			if !ok {
				t.Fatalf("unexpected payload type %T", payload)
			}

			paramsCell, err := cell.FromBOC(req.Params)
			if err != nil {
				t.Fatalf("parse parameter BOC: %v", err)
			}
			var params tlb.Stack
			if err = params.LoadFromCell(paramsCell.MustBeginParse()); err != nil {
				t.Fatalf("parse parameter stack: %v", err)
			}
			value, err := params.Pop()
			if err != nil {
				t.Fatalf("pop parameter: %v", err)
			}
			empty, ok := value.([]any)
			if !ok || empty == nil || len(empty) != 0 {
				t.Fatalf("parameter = %#v, want non-nil empty tuple", value)
			}

			return nil
		},
	}, ProofCheckPolicyUnsafe)

	result, err := api.RunGetMethodByID(
		context.Background(),
		&BlockIDExt{},
		address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c"),
		0,
		tuple.NewTupleValue(),
	)
	if err != nil {
		t.Fatalf("run get method: %v", err)
	}
	empty, err := result.Tuple(0)
	if err != nil {
		t.Fatalf("get result tuple: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("result tuple = %#v, want non-nil empty tuple", empty)
	}
	isNil, err := result.IsNil(0)
	if err != nil {
		t.Fatalf("check result nil: %v", err)
	}
	if isNil {
		t.Fatal("empty tuple reported as null")
	}
}
