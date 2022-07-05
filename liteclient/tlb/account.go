package tlb

import (
	"errors"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type AccountStatus string

const (
	AccountStatusActive   = "ACTIVE"
	AccountStatusUninit   = "UNINIT"
	AccountStatusFrozen   = "FROZEN"
	AccountStatusNonExist = "NON_EXIST"
)

type CurrencyCollection struct {
	Coins           *Grams           `tlb:"."`
	ExtraCurrencies *cell.Dictionary `tlb:"maybe ^dict 32"`
}

type DepthBalanceInfo struct {
	Depth      uint32             `tlb:"## 5"`
	Currencies CurrencyCollection `tlb:"."`
}

type AccountStorage struct {
	Status            AccountStatus
	LastTransactionLT uint64
	Balance           *Grams
}

type StorageUsed struct {
	BitsUsed        uint64
	CellsUsed       uint64
	PublicCellsUsed uint64
}

type StorageInfo struct {
	StorageUsed StorageUsed
	LastPaid    uint32
	DuePayment  *big.Int
}

type AccountState struct {
	IsValid     bool
	Address     *address.Address
	StorageInfo StorageInfo

	AccountStorage
}

func (g *AccountStatus) LoadFromCell(loader *cell.LoadCell) error {
	state, err := loader.LoadUInt(2)
	if err != nil {
		return err
	}

	switch state {
	case 0b11:
		*g = AccountStatusNonExist
	case 0b10:
		*g = AccountStatusActive
	case 0b01:
		*g = AccountStatusFrozen
	case 0b00:
		*g = AccountStatusUninit
	}

	return nil
}

func (a *AccountState) LoadFromCell(loader *cell.LoadCell) error {
	isAccount, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}

	if !isAccount {
		return nil
	}

	addr, err := loader.LoadAddr()
	if err != nil {
		return err
	}

	var info StorageInfo
	err = info.LoadFromCell(loader)
	if err != nil {
		return err
	}

	var store AccountStorage
	err = store.LoadFromCell(loader)
	if err != nil {
		return err
	}

	*a = AccountState{
		IsValid:        true,
		Address:        addr,
		StorageInfo:    info,
		AccountStorage: store,
	}

	return nil
}

func (s *StorageUsed) LoadFromCell(loader *cell.LoadCell) error {
	cells, err := loader.LoadVarUInt(7)
	if err != nil {
		return err
	}

	bits, err := loader.LoadVarUInt(7)
	if err != nil {
		return err
	}

	pubCells, err := loader.LoadVarUInt(7)
	if err != nil {
		return err
	}

	s.CellsUsed = cells.Uint64()
	s.BitsUsed = bits.Uint64()
	s.PublicCellsUsed = pubCells.Uint64()

	return nil
}

func (s *StorageInfo) LoadFromCell(loader *cell.LoadCell) error {
	var used StorageUsed
	err := used.LoadFromCell(loader)
	if err != nil {
		return err
	}

	lastPaid, err := loader.LoadUInt(32)
	if err != nil {
		return err
	}

	isDuePayment, err := loader.LoadUInt(1)
	if err != nil {
		return err
	}

	var duePayment *big.Int
	if isDuePayment == 1 {
		duePayment, err = loader.LoadBigCoins()
		if err != nil {
			return err
		}
	}

	s.StorageUsed = used
	s.DuePayment = duePayment
	s.LastPaid = uint32(lastPaid)

	return nil
}

func (s *AccountStorage) LoadFromCell(loader *cell.LoadCell) error {
	lastTransaction, err := loader.LoadUInt(64)
	if err != nil {
		return err
	}

	coins, err := loader.LoadBigCoins()
	if err != nil {
		return err
	}

	extraExists, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}

	if extraExists {
		return errors.New("extra currency info is not supported for AccountStorage")
	}

	isStatusActive, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}

	if isStatusActive {
		s.Status = AccountStatusActive
	} else {
		isStatusFrozen, err := loader.LoadBoolBit()
		if err != nil {
			return err
		}

		if isStatusFrozen {
			s.Status = AccountStatusFrozen
		} else {
			s.Status = AccountStatusUninit
		}
	}

	s.LastTransactionLT = lastTransaction
	s.Balance = FromNanoTON(coins)

	return nil
}
