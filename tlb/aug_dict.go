package tlb

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type ShardAccountsAugDict struct {
	*cell.AugmentedDictionary
}

type ShardAccountBlocksAugDict struct {
	*cell.AugmentedDictionary
}

type AccountTransactionsAugDict struct {
	*cell.AugmentedDictionary
}

type OldMcBlocksInfoAugDict struct {
	*cell.AugmentedDictionary
}

type ShardFeesAugDict struct {
	*cell.AugmentedDictionary
}

type ShardFeeCreated struct {
	Fees   CurrencyCollection `tlb:"."`
	Create CurrencyCollection `tlb:"."`
}

func (d *ShardAccountsAugDict) LoadFromCell(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(256, skipDepthBalanceInfoBoundary)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *ShardAccountsAugDict) ToCell() (*cell.Cell, error) {
	return augDictToCell(d)
}

func (d *ShardAccountBlocksAugDict) LoadFromCell(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(256, skipCurrencyCollectionBoundary)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *ShardAccountBlocksAugDict) ToCell() (*cell.Cell, error) {
	return augDictToCell(d)
}

func (d *AccountTransactionsAugDict) LoadFromCell(loader *cell.Slice) error {
	dict, err := loader.ToAugDictWithValue(64, skipCurrencyCollectionBoundary, skipAugRefValue)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *AccountTransactionsAugDict) ToCell() (*cell.Cell, error) {
	return augDictToCell(d)
}

func (d *OldMcBlocksInfoAugDict) LoadFromCell(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(32, skipAugExtra[KeyMaxLt])
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *OldMcBlocksInfoAugDict) ToCell() (*cell.Cell, error) {
	return augDictToCell(d)
}

func (d *ShardFeesAugDict) LoadFromCell(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(96, skipShardFeeCreatedBoundary)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *ShardFeesAugDict) ToCell() (*cell.Cell, error) {
	return augDictToCell(d)
}

func skipAugExtra[T any](loader *cell.Slice) error {
	var value T
	return LoadFromCellAsProof(&value, loader)
}

// Wrapper-level augmented dict loading only needs exact augmentation boundaries.
// Full semantic parsing of CurrencyCollection/DepthBalanceInfo still happens on
// actual leaf payloads, where callers load the returned value through regular TLB.
func skipCurrencyCollectionBoundary(loader *cell.Slice) error {
	if _, err := loader.LoadBigCoins(); err != nil {
		return err
	}
	_, err := loader.LoadMaybeRef()
	return err
}

func skipDepthBalanceInfoBoundary(loader *cell.Slice) error {
	if _, err := loader.LoadUInt(5); err != nil {
		return err
	}
	return skipCurrencyCollectionBoundary(loader)
}

func skipShardFeeCreatedBoundary(loader *cell.Slice) error {
	if err := skipCurrencyCollectionBoundary(loader); err != nil {
		return err
	}
	return skipCurrencyCollectionBoundary(loader)
}

func skipAugRefValue(loader *cell.Slice) error {
	_, err := loader.LoadRefCell()
	return err
}

type augDictCarrier interface {
	getAugmentedDictionary() *cell.AugmentedDictionary
}

func (d *ShardAccountsAugDict) getAugmentedDictionary() *cell.AugmentedDictionary {
	if d == nil {
		return nil
	}
	return d.AugmentedDictionary
}

func (d *ShardAccountBlocksAugDict) getAugmentedDictionary() *cell.AugmentedDictionary {
	if d == nil {
		return nil
	}
	return d.AugmentedDictionary
}

func (d *AccountTransactionsAugDict) getAugmentedDictionary() *cell.AugmentedDictionary {
	if d == nil {
		return nil
	}
	return d.AugmentedDictionary
}

func (d *OldMcBlocksInfoAugDict) getAugmentedDictionary() *cell.AugmentedDictionary {
	if d == nil {
		return nil
	}
	return d.AugmentedDictionary
}

func (d *ShardFeesAugDict) getAugmentedDictionary() *cell.AugmentedDictionary {
	if d == nil {
		return nil
	}
	return d.AugmentedDictionary
}

func augDictToCell(d augDictCarrier) (*cell.Cell, error) {
	if d == nil || d.getAugmentedDictionary() == nil {
		return nil, fmt.Errorf("augmented dict is nil")
	}
	return d.getAugmentedDictionary().ToCell()
}
