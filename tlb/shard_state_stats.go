package tlb

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// ShardStateStats is the auxiliary data stored in the third reference of an
// unsplit shard state.
type ShardStateStats struct {
	OverloadHistory    uint64
	UnderloadHistory   uint64
	TotalBalance       CurrencyCollection
	TotalValidatorFees CurrencyCollection
	Libraries          *cell.Dictionary
	MasterRef          *ExtBlkRef
}

func (s *ShardStateStats) LoadFromCell(loader *cell.Slice) error {
	overloadHistory, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load overload history: %w", err)
	}
	underloadHistory, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load underload history: %w", err)
	}

	var totalBalance CurrencyCollection
	if err = totalBalance.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load total balance: %w", err)
	}
	var totalValidatorFees CurrencyCollection
	if err = totalValidatorFees.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load total validator fees: %w", err)
	}

	libraries, err := loader.LoadDict(256)
	if err != nil {
		return fmt.Errorf("failed to load libraries: %w", err)
	}
	hasMasterRef, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load master reference flag: %w", err)
	}

	var masterRef *ExtBlkRef
	if hasMasterRef {
		masterRef = new(ExtBlkRef)
		if err = LoadFromCell(masterRef, loader); err != nil {
			return fmt.Errorf("failed to load master reference: %w", err)
		}
	}

	s.OverloadHistory = overloadHistory
	s.UnderloadHistory = underloadHistory
	s.TotalBalance = totalBalance
	s.TotalValidatorFees = totalValidatorFees
	s.Libraries = libraries
	s.MasterRef = masterRef
	return nil
}

func (s ShardStateStats) ToCell() (*cell.Cell, error) {
	if s.Libraries != nil && !s.Libraries.IsEmpty() && s.Libraries.GetKeySize() != 256 {
		return nil, fmt.Errorf("libraries dictionary has key size %d", s.Libraries.GetKeySize())
	}

	b := cell.BeginCell().
		MustStoreUInt(s.OverloadHistory, 64).
		MustStoreUInt(s.UnderloadHistory, 64)
	if err := storeCurrencyCollection(b, s.TotalBalance); err != nil {
		return nil, fmt.Errorf("failed to store total balance: %w", err)
	}
	if err := storeCurrencyCollection(b, s.TotalValidatorFees); err != nil {
		return nil, fmt.Errorf("failed to store total validator fees: %w", err)
	}
	if err := b.StoreDict(s.Libraries); err != nil {
		return nil, fmt.Errorf("failed to store libraries: %w", err)
	}
	// At most 380 bits are stored so far, so the flag and the 608-bit
	// reference-free ExtBlkRef always fit.
	b.MustStoreBoolBit(s.MasterRef != nil)
	if s.MasterRef != nil {
		masterRef, err := ToCell(s.MasterRef)
		if err != nil {
			return nil, fmt.Errorf("failed to store master reference: %w", err)
		}
		b.MustStoreBuilder(masterRef.ToBuilder())
	}
	return b.EndCell(), nil
}
