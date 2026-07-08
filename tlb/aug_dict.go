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

	// wrapped reports that the underlying dictionary was created in the
	// HashmapAugE-style wrapped form (constructor) rather than parsed from the
	// inline HashmapAug 64 representation of an AccountBlock.
	wrapped bool
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
	dict, err := loader.LoadAugDict(256, AugShardAccounts{}, false)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *ShardAccountsAugDict) LoadFromCellAsProof(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(256, AugShardAccounts{}, true)
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
	dict, err := loader.LoadAugDict(256, AugShardAccountBlocks{}, false)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *ShardAccountBlocksAugDict) LoadFromCellAsProof(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(256, AugShardAccountBlocks{}, true)
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
	dict, err := loader.ToAugDictWithValueAndAugmentation(64, AugAccountTransactions{}, skipAugRefValue)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	d.wrapped = false
	return nil
}

func (d *AccountTransactionsAugDict) ToCell() (*cell.Cell, error) {
	return augDictToCell(d)
}

// InlineCell returns the dictionary in the inline HashmapAug 64 form used
// inside an AccountBlock. A TLB Hashmap (unlike HashmapE) has no empty
// representation, so an empty dictionary cannot be serialized inline and
// returns an error.
func (d *AccountTransactionsAugDict) InlineCell() (*cell.Cell, error) {
	if d == nil || d.AugmentedDictionary == nil {
		return nil, fmt.Errorf("augmented dict is nil")
	}
	if d.IsEmpty() {
		return nil, fmt.Errorf("inline HashmapAug 64 of AccountBlock cannot be empty: TLB Hashmap (non-E) has no empty representation")
	}

	root, err := d.AugmentedDictionary.ToCell()
	if err != nil {
		return nil, err
	}
	if !d.wrapped {
		// parsed inline dictionaries serialize back to the inline root directly
		return root, nil
	}

	// constructor-created dictionaries serialize in the HashmapAugE wrapped
	// form: root flag bit, root ref, root extra
	s, err := root.BeginParse()
	if err != nil {
		return nil, err
	}
	if _, err = s.LoadBoolBit(); err != nil {
		return nil, err
	}
	return s.LoadRefCell()
}

func (d *OldMcBlocksInfoAugDict) LoadFromCell(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(32, cell.ReadOnlyAugmentation{SkipExtraFn: skipAugExtra[KeyMaxLt]}, false)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *OldMcBlocksInfoAugDict) LoadFromCellAsProof(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(32, cell.ReadOnlyAugmentation{SkipExtraFn: skipAugExtra[KeyMaxLt]}, true)
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
	dict, err := loader.LoadAugDict(96, AugShardFees{}, false)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *ShardFeesAugDict) LoadFromCellAsProof(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(96, AugShardFees{}, true)
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
