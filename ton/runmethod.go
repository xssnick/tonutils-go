package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/big"
)

var ErrIncorrectResultType = errors.New("incorrect result type")
var ErrResultIndexOutOfRange = errors.New("result index is out of range")

type ExecutionResult struct {
	result []any
}

func NewExecutionResult(data []any) *ExecutionResult {
	return &ExecutionResult{data}
}

func (c *APIClient) RunGetMethod(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...any) (*ExecutionResult, error) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 0b00000100)

	data = append(data, blockInfo.Serialize()...)

	chain := make([]byte, 4)
	binary.LittleEndian.PutUint32(chain, uint32(addr.Workchain()))

	data = append(data, chain...)
	data = append(data, addr.Data()...)

	mName := make([]byte, 8)
	binary.LittleEndian.PutUint64(mName, tlb.MethodNameHash(method))
	data = append(data, mName...)

	var stack tlb.Stack
	for i := len(params) - 1; i >= 0; i-- {
		// push args in reverse order
		stack.Push(params[i])
	}

	req, err := stack.ToCell()
	if err != nil {
		return nil, fmt.Errorf("build stack err: %w", err)
	}

	// param
	data = append(data, tl.ToBytes(req.ToBOCWithFlags(false))...)

	resp, err := c.client.Do(ctx, _RunContractGetMethod, data)
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _RunQueryResult:
		// TODO: mode
		_ = binary.LittleEndian.Uint32(resp.Data)

		resp.Data = resp.Data[4:]

		b := new(tlb.BlockInfo)
		resp.Data, err = b.Load(resp.Data)
		if err != nil {
			return nil, err
		}

		shard := new(tlb.BlockInfo)
		resp.Data, err = shard.Load(resp.Data)
		if err != nil {
			return nil, err
		}

		// TODO: check proofs mode

		exitCode := binary.LittleEndian.Uint32(resp.Data)
		if exitCode > 1 {
			return nil, ContractExecError{
				exitCode,
			}
		}
		resp.Data = resp.Data[4:]

		var state []byte
		state, resp.Data, err = tl.FromBytes(resp.Data)
		if err != nil {
			return nil, err
		}

		cl, err := cell.FromBOC(state)
		if err != nil {
			return nil, err
		}

		var resStack tlb.Stack
		err = resStack.LoadFromCell(cl.BeginParse())
		if err != nil {
			return nil, err
		}

		var result []any

		for resStack.Depth() > 0 {
			v, err := resStack.Pop()
			if err != nil {
				return nil, err
			}
			result = append(result, v)
		}

		return NewExecutionResult(result), nil
	case _LSError:
		lsErr := new(LSError)
		_, err = tl.Parse(lsErr, resp.Data, false)
		if err != nil {
			return nil, fmt.Errorf("failed to parse error, err: %w", err)
		}
		return nil, lsErr
	}

	return nil, errors.New("unknown response type")
}

func (r ExecutionResult) AsTuple() []any {
	return r.result
}

func (r ExecutionResult) Int(index uint) (*big.Int, error) {
	if uint(len(r.result)) <= index {
		return nil, ErrResultIndexOutOfRange
	}

	val, ok := r.result[index].(*big.Int)
	if !ok {
		return nil, ErrIncorrectResultType
	}
	return val, nil
}

func (r ExecutionResult) Cell(index uint) (*cell.Cell, error) {
	if uint(len(r.result)) <= index {
		return nil, ErrResultIndexOutOfRange
	}

	val, ok := r.result[index].(*cell.Cell)
	if !ok {
		return nil, ErrIncorrectResultType
	}
	return val, nil
}

func (r ExecutionResult) Slice(index uint) (*cell.Slice, error) {
	if uint(len(r.result)) <= index {
		return nil, ErrResultIndexOutOfRange
	}

	val, ok := r.result[index].(*cell.Slice)
	if !ok {
		return nil, ErrIncorrectResultType
	}
	return val, nil
}

func (r ExecutionResult) Builder(index uint) (*cell.Builder, error) {
	if uint(len(r.result)) <= index {
		return nil, ErrResultIndexOutOfRange
	}

	val, ok := r.result[index].(*cell.Builder)
	if !ok {
		return nil, ErrIncorrectResultType
	}
	return val, nil
}

func (r ExecutionResult) IsNil(index uint) (bool, error) {
	if uint(len(r.result)) <= index {
		return false, ErrResultIndexOutOfRange
	}

	return r.result[index] == nil, nil
}

func (r ExecutionResult) Tuple(index uint) ([]any, error) {
	if uint(len(r.result)) <= index {
		return nil, ErrResultIndexOutOfRange
	}

	val, ok := r.result[index].([]any)
	if !ok {
		return nil, ErrIncorrectResultType
	}
	return val, nil
}

func (r ExecutionResult) MustCell(index uint) *cell.Cell {
	res, err := r.Cell(index)
	if err != nil {
		panic(err)
	}
	return res
}

func (r ExecutionResult) MustSlice(index uint) *cell.Slice {
	res, err := r.Slice(index)
	if err != nil {
		panic(err)
	}
	return res
}

func (r ExecutionResult) MustBuilder(index uint) *cell.Builder {
	res, err := r.Builder(index)
	if err != nil {
		panic(err)
	}
	return res
}

func (r ExecutionResult) MustInt(index uint) *big.Int {
	res, err := r.Int(index)
	if err != nil {
		panic(err)
	}
	return res
}

func (r ExecutionResult) MustTuple(index uint) []any {
	res, err := r.Tuple(index)
	if err != nil {
		panic(err)
	}
	return res
}

func (r ExecutionResult) MustIsNil(index uint) bool {
	res, err := r.IsNil(index)
	if err != nil {
		panic(err)
	}
	return res
}
