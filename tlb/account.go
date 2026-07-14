package tlb

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/crc16"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	Register(StorageExtraNone{})
	Register(StorageExtraInfo{})
}

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

type ShardAccount struct {
	Account       *cell.Cell `tlb:"^"`
	LastTransHash []byte     `tlb:"bits 256"`
	LastTransLT   uint64     `tlb:"## 64"`
}

type AccountStorage struct {
	Status            AccountStatus
	LastTransactionLT uint64
	Balance           Coins
	ExtraCurrencies   *cell.Dictionary `tlb:"dict 32"`

	// has value when active
	StateInit *StateInit
	// has value when frozen
	StateHash []byte
}

type StorageUsed struct {
	CellsUsed *big.Int `tlb:"var uint 7"`
	BitsUsed  *big.Int `tlb:"var uint 7"`
}

type StorageExtraNone struct {
	_ Magic `tlb:"$000"`
}

type StorageExtraInfo struct {
	_        Magic  `tlb:"$001"`
	DictHash []byte `tlb:"bits 256"`
}

type StorageInfo struct {
	StorageUsed  StorageUsed `tlb:"."`
	StorageExtra any         `tlb:"[StorageExtraNone,StorageExtraInfo]"`
	LastPaid     uint32      `tlb:"## 32"`
	DuePayment   *Coins      `tlb:"maybe ."`
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
		err := res.StoreUInt(0b11, 2)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize account status: %w", err)
		}
	case AccountStatusActive:
		err := res.StoreUInt(0b10, 2)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize account status: %w", err)
		}
	case AccountStatusFrozen:
		err := res.StoreUInt(0b01, 2)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize account status: %w", err)
		}
	case AccountStatusUninit:
		err := res.StoreUInt(0b00, 2)
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

func (s *StorageUsed) LoadFromCell(loader *cell.Slice) error {
	cellsUsed, err := loadVarUInt(loader, 7)
	if err != nil {
		return fmt.Errorf("failed to load cells used: %w", err)
	}
	bitsUsed, err := loadVarUInt(loader, 7)
	if err != nil {
		return fmt.Errorf("failed to load bits used: %w", err)
	}

	s.CellsUsed = cellsUsed
	s.BitsUsed = bitsUsed
	return nil
}

func (s StorageUsed) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeStorageUsed(builder, s); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeStorageUsed(builder *cell.Builder, s StorageUsed) error {
	if err := storeVarUInt(builder, s.CellsUsed, 7); err != nil {
		return fmt.Errorf("failed to store cells used: %w", err)
	}
	if err := storeVarUInt(builder, s.BitsUsed, 7); err != nil {
		return fmt.Errorf("failed to store bits used: %w", err)
	}
	return nil
}

func (s *StorageInfo) LoadFromCell(loader *cell.Slice) error {
	if err := s.StorageUsed.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load storage used: %w", err)
	}

	extraMagic, err := loader.LoadUInt(3)
	if err != nil {
		return fmt.Errorf("failed to load storage extra magic: %w", err)
	}
	switch extraMagic {
	case 0b000:
		s.StorageExtra = StorageExtraNone{}
	case 0b001:
		dictHash, err := loader.LoadSlice(256)
		if err != nil {
			return fmt.Errorf("failed to load storage extra dict hash: %w", err)
		}
		s.StorageExtra = StorageExtraInfo{DictHash: dictHash}
	default:
		return fmt.Errorf("unknown storage extra magic %b", extraMagic)
	}

	lastPaid, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load last paid: %w", err)
	}
	s.LastPaid = uint32(lastPaid)

	s.DuePayment, err = loadMaybeCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load due payment: %w", err)
	}
	return nil
}

func (s StorageInfo) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeStorageInfo(builder, s); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeStorageInfo(builder *cell.Builder, s StorageInfo) error {
	if err := storeStorageUsed(builder, s.StorageUsed); err != nil {
		return err
	}

	switch extra := s.StorageExtra.(type) {
	case StorageExtraNone:
		if err := builder.StoreUInt(0b000, 3); err != nil {
			return fmt.Errorf("failed to store storage extra magic: %w", err)
		}
	case StorageExtraInfo:
		if err := builder.StoreUInt(0b001, 3); err != nil {
			return fmt.Errorf("failed to store storage extra magic: %w", err)
		}
		if err := builder.StoreSlice(extra.DictHash, 256); err != nil {
			return fmt.Errorf("failed to store storage extra dict hash: %w", err)
		}
	default:
		return fmt.Errorf("unknown storage extra type %T", s.StorageExtra)
	}

	if err := builder.StoreUInt(uint64(s.LastPaid), 32); err != nil {
		return fmt.Errorf("failed to store last paid: %w", err)
	}
	if err := storeMaybeCoins(builder, s.DuePayment); err != nil {
		return fmt.Errorf("failed to store due payment: %w", err)
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
		return fmt.Errorf("failed to load storage info: %w", err)
	}

	var store AccountStorage
	err = store.LoadFromCell(loader)
	if err != nil {
		return fmt.Errorf("failed to load account storage: %w", err)
	}

	*a = AccountState{
		IsValid:        true,
		Address:        addr,
		StorageInfo:    info,
		AccountStorage: store,
	}

	return nil
}

func (a AccountState) ToCell() (*cell.Cell, error) {
	if !a.IsValid || a.Status == AccountStatusNonExist {
		return cell.BeginCell().
			MustStoreBoolBit(false).
			EndCell(), nil
	}

	if a.Address == nil {
		return nil, fmt.Errorf("account address is nil")
	}

	builder := cell.BeginCell().
		MustStoreBoolBit(true).
		MustStoreAddr(a.Address)

	if err := storeStorageInfo(builder, a.StorageInfo); err != nil {
		return nil, fmt.Errorf("failed to serialize storage info: %w", err)
	}

	storageCell, err := a.AccountStorage.ToCell()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize account storage: %w", err)
	}

	return builder.
		MustStoreBuilder(storageCell.ToBuilder()).
		EndCell(), nil
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

	s.ExtraCurrencies, err = loader.LoadDict(32)
	if err != nil {
		return fmt.Errorf("failed to load extra currencies: %w", err)
	}

	isStatusActive, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load active bit: %w", err)
	}

	if isStatusActive {
		s.Status = AccountStatusActive
		var stInit StateInit
		err = stInit.LoadFromCell(loader)
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

func (s AccountStorage) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell().
		MustStoreUInt(s.LastTransactionLT, 64).
		MustStoreBigCoins(s.Balance.Nano()).
		MustStoreDict(s.ExtraCurrencies)

	switch s.Status {
	case AccountStatusActive:
		if s.StateInit == nil {
			return nil, fmt.Errorf("active account state init is nil")
		}

		stateInitCell, err := s.StateInit.ToCell()
		if err != nil {
			return nil, fmt.Errorf("failed to serialize state init: %w", err)
		}

		return builder.
			MustStoreBoolBit(true).
			MustStoreBuilder(stateInitCell.ToBuilder()).
			EndCell(), nil

	case AccountStatusFrozen:
		return builder.
			MustStoreBoolBit(false).
			MustStoreBoolBit(true).
			MustStoreSlice(normalizeAccountStateHash(s.StateHash), 256).
			EndCell(), nil

	case AccountStatusUninit:
		return builder.
			MustStoreBoolBit(false).
			MustStoreBoolBit(false).
			EndCell(), nil

	case AccountStatusNonExist:
		return nil, fmt.Errorf("non-existing account cannot be serialized as account storage")

	default:
		return nil, fmt.Errorf("unknown account status %s", s.Status)
	}
}

func normalizeAccountStateHash(src []byte) []byte {
	if len(src) == 32 {
		return src
	}

	out := make([]byte, 32)
	if len(src) >= 32 {
		copy(out, src[len(src)-32:])
		return out
	}

	copy(out[32-len(src):], src)
	return out
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

	code, err := a.Code.BeginParse()
	if err != nil {
		return false
	}

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

	if _, err = dict.LoadValueByIntKey(big.NewInt(hash)); err == nil {
		return true
	}
	return false
}

func MethodNameHash(name string) uint64 {
	// https://github.com/ton-blockchain/ton/blob/24dc184a2ea67f9c47042b4104bbb4d82289fac1/crypto/smc-envelope/SmartContract.h#L75
	return uint64(crc16.ChecksumXMODEM([]byte(name))) | 0x10000
}
