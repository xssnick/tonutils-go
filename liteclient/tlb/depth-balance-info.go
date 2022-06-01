package tlb

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type DepthBalanceInfo struct {
	Depth uint32
	Coins *Grams
}

func (d *DepthBalanceInfo) LoadFromCell(loader *cell.LoadCell) error {

	// depth_balance$_ split_depth:(#<= 30) balance:CurrencyCollection = DepthBalanceInfo;
	depth, err := loader.LoadUInt(5)
	if err != nil {
		return err
	}

	grams, err := loader.LoadBigCoins()
	if err != nil {
		return err
	}

	extraExists, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}

	if extraExists {
		_, err := loader.LoadDict(32)
		if err != nil {
			return err
		}
	}

	d.Depth = uint32(depth)
	d.Coins = new(Grams).FromNanoTON(grams)

	return nil
}
