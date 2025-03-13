package vm

import (
	"errors"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type Register struct {
	C  [4]Continuation
	D  [2]*cell.Cell
	C7 tuple.Tuple
}

func (r *Register) AdjustWith(r2 *Register) {
	for i := 0; i < 4; i++ {
		if r2.C[i] == nil {
			continue
		}
		r.C[i] = r2.C[i]
	}
	for i := 0; i < 2; i++ {
		if r2.D[i] == nil {
			continue
		}
		r.D[i] = r2.D[i]
	}
	if r2.C7.Len() > 0 {
		r.C7 = r2.C7
	}
}

func (r *Register) Get(i int) any {
	if i < 4 {
		return r.C[i]
	}
	if i < 6 {
		return r.D[i-4]
	}
	if i == 7 {
		return r.C7
	}
	return Null{}
}

type Gas struct {
	Consumed uint64
	Limit    uint64
	Credit   uint64
	Price    uint64
}

func (g *Gas) Consume(amt uint64) error {
	g.Consumed += amt
	if g.Consumed > g.Limit || (g.Credit > 0 && g.Consumed > g.Credit) {
		// TODO: enable when gas ready
		// return vmerr.ErrOutOfGas
	}
	return nil
}

func (g *Gas) ConsumeStackGas(s *Stack) error {
	const pricePerStackEntry = 1
	const freeDepth = 32

	amt := uint64(s.Len())
	if amt < freeDepth {
		amt = freeDepth
	}
	return g.Consume((amt - freeDepth) * pricePerStackEntry)
}

const ControlDataAllArgs = -1

const CP = -1

type ControlData struct {
	Save    Register
	Stack   *Stack
	NumArgs int
	CP      int
}

type State struct {
	CP          int
	CurrentCode *cell.Slice
	Reg         Register
	Gas         Gas
	Stack       *Stack
}

type OPGetter func() OP
type OP interface {
	GetPrefixes() []*cell.Slice
	Deserialize(code *cell.Slice) error
	Serialize() *cell.Builder
	SerializeText() string
	Interpret(state *State) error
}

var List []OPGetter

var ErrCorruptedOpcode = errors.New("corrupted opcode")

func (s *State) GetParam(idx int) (any, error) {
	params, err := s.Reg.C7.Index(0)
	if err != nil {
		return nil, err
	}

	p, ok := params.(tuple.Tuple)
	if !ok {
		return nil, vmerr.ErrTypeCheck
	}

	v, err := p.Index(idx)
	if err != nil {
		return nil, err
	}

	return v, nil
}
