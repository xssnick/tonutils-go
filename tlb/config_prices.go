package tlb

import (
	"errors"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type ConfigStoragePrices struct {
	_           Magic  `tlb:"#cc"`
	ValidSince  uint32 `tlb:"## 32"`
	BitPrice    uint64 `tlb:"## 64"`
	CellPrice   uint64 `tlb:"## 64"`
	MCBitPrice  uint64 `tlb:"## 64"`
	MCCellPrice uint64 `tlb:"## 64"`
}

func (c ConfigStoragePrices) ComputeStorageFee(isMasterchain bool, delta, bits, cells uint64) *big.Int {
	var bitPrice uint64
	var cellPrice uint64

	if isMasterchain {
		bitPrice = c.MCBitPrice
		cellPrice = c.MCCellPrice
	} else {
		bitPrice = c.BitPrice
		cellPrice = c.CellPrice
	}

	total := new(big.Int).Mul(new(big.Int).SetUint64(cells), new(big.Int).SetUint64(cellPrice))
	total.Add(total, new(big.Int).Mul(new(big.Int).SetUint64(bits), new(big.Int).SetUint64(bitPrice)))
	total.Mul(total, new(big.Int).SetUint64(delta))
	return configPricesCeilShiftRight(total, 16)
}

type ConfigMsgForwardPrices struct {
	_         Magic  `tlb:"#ea"`
	LumpPrice uint64 `tlb:"## 64"`
	BitPrice  uint64 `tlb:"## 64"`
	CellPrice uint64 `tlb:"## 64"`
	IHRFactor uint32 `tlb:"## 32"`
	FirstFrac uint16 `tlb:"## 16"`
	NextFrac  uint16 `tlb:"## 16"`
}

func (c ConfigMsgForwardPrices) ComputeForwardFee(cells, bits uint64) *big.Int {
	total := new(big.Int).Mul(new(big.Int).SetUint64(c.BitPrice), new(big.Int).SetUint64(bits))
	total.Add(total, new(big.Int).Mul(new(big.Int).SetUint64(c.CellPrice), new(big.Int).SetUint64(cells)))
	total = configPricesCeilShiftRight(total, 16)
	return total.Add(total, new(big.Int).SetUint64(c.LumpPrice))
}

type ConfigGasLimitsPrices struct {
	HasFlatPricing          bool
	FlatGasLimit            uint64
	FlatGasPrice            uint64
	HasSeparateSpecialLimit bool
	GasPrice                uint64
	GasLimit                uint64
	SpecialGasLimit         uint64
	GasCredit               uint64
	BlockGasLimit           uint64
	FreezeDueLimit          uint64
	DeleteDueLimit          uint64
}

func (c *ConfigGasLimitsPrices) LoadFromCell(loader *cell.Slice) error {
	if loader == nil {
		return errors.New("gas prices slice is nil")
	}

	work := loader.Copy()

	out := ConfigGasLimitsPrices{}

	tag, err := work.PreloadUInt(8)
	if err != nil {
		return err
	}
	if tag == 0xD1 {
		out.HasFlatPricing = true

		if _, err = work.LoadUInt(8); err != nil {
			return err
		}
		if out.FlatGasLimit, err = work.LoadUInt(64); err != nil {
			return err
		}
		if out.FlatGasPrice, err = work.LoadUInt(64); err != nil {
			return err
		}
	}

	mainTag, err := work.LoadUInt(8)
	if err != nil {
		return err
	}

	switch mainTag {
	case 0xDD:
		if out.GasPrice, err = work.LoadUInt(64); err != nil {
			return err
		}
		if out.GasLimit, err = work.LoadUInt(64); err != nil {
			return err
		}

		out.SpecialGasLimit = out.GasLimit

		if out.GasCredit, err = work.LoadUInt(64); err != nil {
			return err
		}

	case 0xDE:
		out.HasSeparateSpecialLimit = true

		if out.GasPrice, err = work.LoadUInt(64); err != nil {
			return err
		}
		if out.GasLimit, err = work.LoadUInt(64); err != nil {
			return err
		}
		if out.SpecialGasLimit, err = work.LoadUInt(64); err != nil {
			return err
		}
		if out.GasCredit, err = work.LoadUInt(64); err != nil {
			return err
		}

	default:
		return errors.New("invalid gas prices tag")
	}

	if out.BlockGasLimit, err = work.LoadUInt(64); err != nil {
		return err
	}
	if out.FreezeDueLimit, err = work.LoadUInt(64); err != nil {
		return err
	}
	if out.DeleteDueLimit, err = work.LoadUInt(64); err != nil {
		return err
	}

	*c = out
	return nil
}

func (c ConfigGasLimitsPrices) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()

	if c.HasFlatPricing {
		builder.MustStoreUInt(0xD1, 8).
			MustStoreUInt(c.FlatGasLimit, 64).
			MustStoreUInt(c.FlatGasPrice, 64)
	}

	if c.HasSeparateSpecialLimit {
		builder.MustStoreUInt(0xDE, 8).
			MustStoreUInt(c.GasPrice, 64).
			MustStoreUInt(c.GasLimit, 64).
			MustStoreUInt(c.SpecialGasLimit, 64).
			MustStoreUInt(c.GasCredit, 64)
	} else {
		builder.MustStoreUInt(0xDD, 8).
			MustStoreUInt(c.GasPrice, 64).
			MustStoreUInt(c.GasLimit, 64).
			MustStoreUInt(c.GasCredit, 64)
	}

	return builder.
		MustStoreUInt(c.BlockGasLimit, 64).
		MustStoreUInt(c.FreezeDueLimit, 64).
		MustStoreUInt(c.DeleteDueLimit, 64).
		EndCell(), nil
}

func (c ConfigGasLimitsPrices) ComputeGasPrice(gasUsed uint64) *big.Int {
	if gasUsed <= c.FlatGasLimit {
		return new(big.Int).SetUint64(c.FlatGasPrice)
	}

	diff := gasUsed - c.FlatGasLimit
	total := new(big.Int).Mul(new(big.Int).SetUint64(c.GasPrice), new(big.Int).SetUint64(diff))
	total = configPricesCeilShiftRight(total, 16)
	return total.Add(total, new(big.Int).SetUint64(c.FlatGasPrice))
}

func configPricesCeilShiftRight(x *big.Int, bits uint) *big.Int {
	if x.Sign() == 0 {
		return big.NewInt(0)
	}

	add := new(big.Int).Lsh(big.NewInt(1), bits)
	add.Sub(add, big.NewInt(1))

	y := new(big.Int).Add(x, add)
	return y.Rsh(y, bits)
}
