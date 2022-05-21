package tlb

import (
	"errors"
	"math"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type DepthBalanceInfo struct {
	Depth uint32
	Coins Grams
}

func (d *DepthBalanceInfo) LoadFromCell(loader *cell.LoadCell) error {
	// depth_balance$_ split_depth:(#<= 30) balance:CurrencyCollection = DepthBalanceInfo;
	depthLen := int(math.Ceil(math.Log2(30)))

	depth, err := loader.LoadUInt(depthLen)
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
		return errors.New("extra currently is not supported for DepthBalanceInfo")
	}

	d.Depth = uint32(depth)
	d.Coins = Grams{val: grams}

	return nil
}
