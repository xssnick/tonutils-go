package tlb

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/sigurn/crc16"
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

type Account struct {
	IsActive   bool
	State      *AccountState
	Data       *cell.Cell
	Code       *cell.Cell
	LastTxLT   uint64
	LastTxHash []byte
}

type CurrencyCollection struct {
	Coins           Coins            `tlb:"."`
	ExtraCurrencies *cell.Dictionary `tlb:"dict 32"`
}

type DepthBalanceInfo struct {
	Depth      uint32             `tlb:"## 5"`
	Currencies CurrencyCollection `tlb:"."`
}

type AccountStorage struct {
	Status            AccountStatus
	LastTransactionLT uint64
	Balance           Coins

	// has value when active
	StateInit *StateInit
	// has value when frozen
	StateHash []byte
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

func (g AccountStatus) ToCell() (*cell.Cell, error) {
	res := cell.BeginCell()
	switch string(g) {
	case AccountStatusNonExist:
		err := res.StoreInt(0b11, 2)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize account status: %w", err)
		}
	case AccountStatusActive:
		err := res.StoreInt(0b10, 2)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize account status: %w", err)
		}
	case AccountStatusFrozen:
		err := res.StoreInt(0b01, 2)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize account status: %w", err)
		}
	case AccountStatusUninit:
		err := res.StoreInt(0b00, 2)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize account status: %w", err)
		}
	}

	return res.EndCell(), nil
}

func (g *AccountStatus) LoadFromCell(loader *cell.Slice) error {
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

func (a *AccountState) LoadFromCell(loader *cell.Slice) error {
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

func (s *StorageUsed) LoadFromCell(loader *cell.Slice) error {
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

func (s *StorageInfo) LoadFromCell(loader *cell.Slice) error {
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

func (s *AccountStorage) LoadFromCell(loader *cell.Slice) error {
	lastTransaction, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load last tx lt: %w", err)
	}

	coins, err := loader.LoadBigCoins()
	if err != nil {
		return fmt.Errorf("failed to load coins balance: %w", err)
	}

	extraExists, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load extra exists bit: %w", err)
	}

	if extraExists {
		return errors.New("extra currency info is not supported for AccountStorage")
	}

	isStatusActive, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load active bit: %w", err)
	}

	if isStatusActive {
		s.Status = AccountStatusActive
		var stInit StateInit
		err = LoadFromCell(&stInit, loader)
		if err != nil {
			return fmt.Errorf("failed to load state init: %w", err)
		}
		s.StateInit = &stInit
	} else {
		isStatusFrozen, err := loader.LoadBoolBit()
		if err != nil {
			return fmt.Errorf("failed to load frozen bit: %w", err)
		}

		if isStatusFrozen {
			s.Status = AccountStatusFrozen
			stateHash, err := loader.LoadSlice(256)
			if err != nil {
				return fmt.Errorf("failed to load frozen state hash: %w", err)
			}
			s.StateHash = stateHash
		} else {
			s.Status = AccountStatusUninit
		}
	}

	s.LastTransactionLT = lastTransaction
	s.Balance = FromNanoTON(coins)

	return nil
}

func (a *Account) HasGetMethod(name string) bool {
	if a.Code == nil {
		return false
	}

	var hash int64
	switch name {
	// reserved names cannot be used for get methods
	case "recv_internal", "main", "recv_external", "run_ticktock":
		return false
	default:
		hash = int64(MethodNameHash(name))
	}

	code := a.Code.BeginParse()
	hdr, err := code.LoadSlice(56)
	if err != nil {
		return false
	}

	// header contains methods dictionary
	// SETCP0
	// 19 DICTPUSHCONST
	// DICTIGETJMPZ
	if !bytes.Equal(hdr, []byte{0xFF, 0x00, 0xF4, 0xA4, 0x13, 0xF4, 0xBC}) {
		return false
	}

	ref, err := code.LoadRef()
	if err != nil {
		return false
	}

	dict, err := ref.ToDict(19)
	if err != nil {
		return false
	}

	if dict.GetByIntKey(big.NewInt(hash)) != nil {
		return true
	}
	return false
}

func MethodNameHash(name string) uint64 {
	// https://github.com/ton-blockchain/ton/blob/24dc184a2ea67f9c47042b4104bbb4d82289fac1/crypto/smc-envelope/SmartContract.h#L75
	return uint64(crc16.Checksum([]byte(name), crc16.MakeTable(crc16.CRC16_XMODEM))) | 0x10000
}
