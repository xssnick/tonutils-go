package vm

// Gas prices constants
const (
	CellLoadGasPrice       = 100
	CellReloadGasPrice     = 25
	CellCreateGasPrice     = 500
	ExceptionGasPrice      = 50
	TupleEntryGasPrice     = 1
	ImplicitJmprefGasPrice = 10
	ImplicitRetGasPrice    = 5
	FreeStackDepth         = 32
	StackEntryGasPrice     = 1
	RunvmGasPrice          = 40
	HashExtEntryGasPrice   = 1
	FreeNestedContJump     = 8

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
	amt := uint64(s.Len())
	if amt < FreeStackDepth {
		amt = FreeStackDepth
	}
	return g.Consume((amt - FreeStackDepth) * StackEntryGasPrice)
}
