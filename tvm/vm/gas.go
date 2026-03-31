package vm

import "github.com/xssnick/tonutils-go/tvm/vmerr"

// Gas prices constants.
const (
	GasInfinite          int64 = 1<<63 - 1
	DefaultGlobalVersion       = 9

	CellLoadGasPrice           = 100
	CellReloadGasPrice         = 25
	CellCreateGasPrice         = 500
	ExceptionGasPrice          = 50
	InstructionBaseGasPrice    = 10
	InstructionCellRefGasPrice = 5
	TupleEntryGasPrice         = 1
	ImplicitJmprefGasPrice     = 10
	ImplicitRetGasPrice        = 5
	FreeStackDepth             = 32
	StackEntryGasPrice         = 1
	RunvmGasPrice              = 40
	HashExtEntryGasPrice       = 1
	FreeNestedContJump         = 8

	Rist255MulGasPrice      = 2000
	Rist255MulbaseGasPrice  = 750
	Rist255AddGasPrice      = 600
	Rist255FromhashGasPrice = 600
	Rist255ValidateGasPrice = 200

	EcrecoverGasPrice                    = 1500
	Secp256k1XonlyPubkeyTweakAddGasPrice = 1250
	ChksgnFreeCount                      = 10
	ChksgnGasPrice                       = 4000
	P256ChksgnGasPrice                   = 3500

	BlsVerifyGasPrice                     = 61000
	BlsAggregateBaseGasPrice              = -2650
	BlsAggregateElementGasPrice           = 4350
	BlsFastAggregateVerifyBaseGasPrice    = 58000
	BlsFastAggregateVerifyElementGasPrice = 3000
	BlsAggregateVerifyBaseGasPrice        = 38500
	BlsAggregateVerifyElementGasPrice     = 22500

	BlsG1AddSubGasPrice  = 3900
	BlsG1NegGasPrice     = 750
	BlsG1MulGasPrice     = 5200
	BlsMapToG1GasPrice   = 2350
	BlsG1InGroupGasPrice = 2950

	BlsG2AddSubGasPrice  = 6100
	BlsG2NegGasPrice     = 1550
	BlsG2MulGasPrice     = 10550
	BlsMapToG2GasPrice   = 7950
	BlsG2InGroupGasPrice = 4250

	BlsG1MultiexpBaseGasPrice  = 11375
	BlsG1MultiexpCoef1GasPrice = 630
	BlsG1MultiexpCoef2GasPrice = 8820
	BlsG2MultiexpBaseGasPrice  = 30388
	BlsG2MultiexpCoef1GasPrice = 1280
	BlsG2MultiexpCoef2GasPrice = 22840

	BlsPairingBaseGasPrice    = 20000
	BlsPairingElementGasPrice = 11800
)

type Gas struct {
	Max          int64
	Limit        int64
	Credit       int64
	Remaining    int64
	Base         int64
	FreeConsumed int64
}

type GasConfig struct {
	Max    int64
	Limit  int64
	Credit int64
}

func NewGas(cfg ...GasConfig) Gas {
	c := GasConfig{}
	if len(cfg) > 0 {
		c = cfg[0]
	}

	max := c.Max
	if max == 0 {
		max = GasInfinite
	}
	limit := c.Limit
	if limit == 0 {
		limit = max
	}
	if limit > max {
		limit = max
	}

	base := limit + c.Credit
	return Gas{
		Max:       max,
		Limit:     limit,
		Credit:    c.Credit,
		Base:      base,
		Remaining: base,
	}
}

func GasWithLimit(limit int64, max ...int64) Gas {
	cfg := GasConfig{Limit: limit}
	if len(max) > 0 {
		cfg.Max = max[0]
	}
	return NewGas(cfg)
}

func (g *Gas) SetLimits(max, limit int64, credit ...int64) {
	creditVal := int64(0)
	if len(credit) > 0 {
		creditVal = credit[0]
	}
	g.Max = max
	g.Limit = limit
	g.Credit = creditVal
	g.Base = limit + creditVal
	g.Remaining = g.Base
	g.FreeConsumed = 0
}

func (g *Gas) ChangeBase(base int64) {
	g.Remaining += base - g.Base
	g.Base = base
}

func (g *Gas) ChangeLimit(limit int64) {
	if limit < 0 {
		limit = 0
	}
	if limit > g.Max {
		limit = g.Max
	}
	g.Credit = 0
	g.Limit = limit
	g.ChangeBase(limit)
}

func (g *Gas) Consume(amount int64) error {
	g.Remaining -= amount
	if g.Remaining < 0 {
		return vmerr.Error(vmerr.CodeOutOfGas)
	}
	return nil
}

func (g *Gas) Check() error {
	if g.Remaining < 0 {
		return vmerr.Error(vmerr.CodeOutOfGas)
	}
	return nil
}

func (g *Gas) ConsumeFree(amount int64) {
	g.FreeConsumed += amount
}

func (g *Gas) FlushFree() error {
	if g.FreeConsumed == 0 {
		return nil
	}

	amt := g.FreeConsumed
	g.FreeConsumed = 0
	return g.Consume(amt)
}

func (g *Gas) Used() int64 {
	return g.Base - g.Remaining
}
