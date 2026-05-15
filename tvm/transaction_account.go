package tvm

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func loadTransactionRuntimeAccount(shard *tlb.ShardAccount, fallbackAddr *address.Address, proof *cell.MerkleProofBuilder) (*transactionRuntimeAccount, error) {
	if shard.Account == nil {
		return nil, errors.New("shard account root is nil")
	}

	accountRoot := shard.Account
	if proof != nil {
		accountRoot = proof.Root()
	}

	var acc tlb.AccountState
	if err := tlb.Parse(&acc, accountRoot); err != nil {
		return nil, fmt.Errorf("failed to decode account state: %w", err)
	}

	out := &transactionRuntimeAccount{
		addr:            fallbackAddr,
		status:          tlb.AccountStatusNonExist,
		storageInfo:     tlb.StorageInfo{StorageExtra: tlb.StorageExtraNone{}},
		balance:         big.NewInt(0),
		prevTxHash:      append([]byte(nil), shard.LastTransHash...),
		prevTxLT:        shard.LastTransLT,
		originalCell:    shard.Account,
		extraCurrencies: nil,
	}
	if !acc.IsValid {
		if out.addr == nil {
			return nil, errors.New("account address is required for non-existing shard account")
		}
		return out, nil
	}

	out.addr = acc.Address
	out.status = acc.Status
	out.storageInfo = acc.StorageInfo
	if out.storageInfo.StorageExtra == nil {
		out.storageInfo.StorageExtra = tlb.StorageExtraNone{}
	}
	out.balance = new(big.Int).Set(acc.Balance.Nano())
	out.extraCurrencies = acc.ExtraCurrencies
	out.storageLT = acc.LastTransactionLT
	out.stateHash = append([]byte(nil), acc.StateHash...)
	storageCell, err := tlb.ToCell(&acc.AccountStorage)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize original account storage: %w", err)
	}
	out.storageCell = storageCell
	if acc.StateInit != nil {
		out.code = acc.StateInit.Code
		out.data = acc.StateInit.Data
		out.libraries = acc.StateInit.Lib
		out.stateDepth = transactionCloneUint64(acc.StateInit.Depth)
		out.tickTock = acc.StateInit.TickTock
	}
	return out, nil
}

func transactionPrepareInitialPhases(acc *transactionRuntimeAccount, msg *tlb.Message, storageFee, importFee *big.Int, now uint32, limits transactionStorageDueLimits) (*transactionPreparedPhases, error) {
	extraCurrencies, err := transactionCloneExtraCurrencies(acc.extraCurrencies)
	if err != nil {
		return nil, err
	}
	prepared := &transactionPreparedPhases{
		balance:         new(big.Int).Set(acc.balance),
		extraCurrencies: extraCurrencies,
		msgBalance:      transactionZeroCurrencyBalance(),
		creditFirst:     true,
		status:          transactionInitialComputeStatus(acc.status),
		duePayment:      transactionCoinsPtr(transactionCoinsNano(acc.storageInfo.DuePayment)),
		lastPaid:        acc.storageInfo.LastPaid,
	}

	credit := func(amount *big.Int, extra *cell.Dictionary) error {
		merged, err := transactionAddExtraCurrencies(prepared.extraCurrencies, extra)
		if err != nil {
			return err
		}
		prepared.extraCurrencies = merged
		prepared.balance.Add(prepared.balance, amount)
		prepared.creditPhase = &tlb.CreditPhase{
			Credit: tlb.CurrencyCollection{
				Coins:           tlb.FromNanoTON(amount),
				ExtraCurrencies: extra,
			},
		}
		return nil
	}

	switch msg.MsgType {
	case tlb.MsgTypeInternal:
		in := msg.AsInternal()
		prepared.creditFirst = !in.Bounce
		prepared.msgBalance, err = transactionCurrencyFromParts(in.Amount.Nano(), in.ExtraCurrencies)
		if err != nil {
			return nil, err
		}
		if prepared.creditFirst {
			if err = credit(prepared.msgBalance.grams, in.ExtraCurrencies); err != nil {
				return nil, err
			}
			prepared.applyStoragePhase(acc, storageFee, now, limits, true)
		} else {
			prepared.applyStoragePhase(acc, storageFee, now, limits, false)
			if err = credit(prepared.msgBalance.grams, in.ExtraCurrencies); err != nil {
				return nil, err
			}
		}
	case tlb.MsgTypeExternalIn:
		if importFee.Sign() > 0 {
			if prepared.balance.Cmp(importFee) < 0 {
				return nil, errors.New("external import fees exceed account balance")
			}
			prepared.balance.Sub(prepared.balance, importFee)
		}
		prepared.applyStoragePhase(acc, storageFee, now, limits, false)
	default:
		return nil, fmt.Errorf("unsupported input message type %s", msg.MsgType)
	}

	return prepared, nil
}

func (p *transactionPreparedPhases) applyStoragePhase(acc *transactionRuntimeAccount, storageFee *big.Int, now uint32, limits transactionStorageDueLimits, adjustMsgValue bool) {
	toPay := transactionBigOrZero(storageFee)
	collected := big.NewInt(0)
	due := big.NewInt(0)
	statusChange := tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged}

	p.duePayment = transactionCoinsPtr(transactionCoinsNano(acc.storageInfo.DuePayment))
	p.lastPaid = now
	if toPay.Sign() > 0 {
		if toPay.Cmp(p.balance) <= 0 {
			collected.Set(toPay)
			p.balance.Sub(p.balance, toPay)
			p.duePayment = nil
		} else {
			collected.Set(p.balance)
			due.Sub(toPay, p.balance)
			p.balance.SetInt64(0)

			switch p.status {
			case tlb.AccountStatusUninit, tlb.AccountStatusFrozen, tlb.AccountStatusNonExist:
				if due.Cmp(limits.deleteDue) > 0 && transactionExtraDictIsEmpty(acc.extraCurrencies) {
					p.deleted = true
					p.status = tlb.AccountStatusNonExist
					statusChange.Type = tlb.AccStatusChangeDeleted
				}
			case tlb.AccountStatusActive:
				if due.Cmp(limits.freezeDue) > 0 {
					p.status = tlb.AccountStatusFrozen
					statusChange.Type = tlb.AccStatusChangeFrozen
				}
			}
			p.duePayment = transactionCoinsPtr(due)
		}
	}

	if adjustMsgValue && p.msgBalance.grams.Cmp(p.balance) > 0 {
		p.msgBalance.grams.Set(p.balance)
	}

	p.storagePhase = &tlb.StoragePhase{
		StorageFeesCollected: tlb.FromNanoTON(collected),
		StorageFeesDue:       transactionCoinsPtr(due),
		StatusChange:         statusChange,
	}
}
func buildTransactionAccountCell(acc *transactionRuntimeAccount, status tlb.AccountStatus, balance *big.Int, extraCurrencies *cell.Dictionary, endLT uint64, lastPaid uint32, duePayment *tlb.Coins, code, data *cell.Cell, libs *cell.Dictionary, stateHash []byte, cfg tlb.BlockchainConfig, accountStorageStat *cell.Cell) (*cell.Cell, *tlb.AccountState, *cell.Cell, error) {
	if status == tlb.AccountStatusNonExist {
		accountState := &tlb.AccountState{
			IsValid: false,
			AccountStorage: tlb.AccountStorage{
				Status:  tlb.AccountStatusNonExist,
				Balance: tlb.FromNanoTON(big.NewInt(0)),
			},
		}
		accountCell, err := tlb.ToCell(accountState)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to serialize account state: %w", err)
		}
		return accountCell, accountState, nil, nil
	}

	stateInit := &tlb.StateInit{
		Depth:    transactionCloneUint64(acc.stateDepth),
		TickTock: acc.tickTock,
		Code:     code,
		Data:     data,
		Lib:      libs,
	}

	accountStorage := tlb.AccountStorage{
		Status:            status,
		LastTransactionLT: endLT,
		Balance:           tlb.FromNanoTON(balance),
		ExtraCurrencies:   extraCurrencies,
	}
	switch status {
	case tlb.AccountStatusActive:
		accountStorage.StateInit = stateInit
	case tlb.AccountStatusFrozen:
		if len(stateHash) == 0 {
			stateInitCell, err := tlb.ToCell(stateInit)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to serialize frozen state init: %w", err)
			}
			stateHash = stateInitCell.Hash()
		}
		accountStorage.StateHash = append([]byte(nil), stateHash...)
	case tlb.AccountStatusUninit:
	default:
		return nil, nil, nil, fmt.Errorf("unsupported final account status %s", status)
	}

	storageCell, err := tlb.ToCell(&accountStorage)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to serialize account storage: %w", err)
	}

	storageCellForStat := storageCell
	if transactionGlobalVersion(cfg) >= 10 {
		storageCellForStat, err = transactionAccountStorageWithoutExtraCurrencies(accountStorage)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	usage, storageExtra, nextStorageStat, err := transactionAccountStorageInfo(acc, storageCellForStat, cfg, accountStorageStat)
	if err != nil {
		return nil, nil, nil, err
	}

	storageInfo := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: new(big.Int).SetUint64(usage.cells),
			BitsUsed:  new(big.Int).SetUint64(usage.bits),
		},
		StorageExtra: storageExtra,
		LastPaid:     lastPaid,
		DuePayment:   duePayment,
	}

	accountState := &tlb.AccountState{
		IsValid:        true,
		Address:        acc.addr,
		StorageInfo:    storageInfo,
		AccountStorage: accountStorage,
	}

	accountCell, err := tlb.ToCell(accountState)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to serialize account state: %w", err)
	}

	return accountCell, accountState, nextStorageStat, nil
}

func transactionAccountStorageInfo(acc *transactionRuntimeAccount, storageCellForStat *cell.Cell, cfg tlb.BlockchainConfig, accountStorageStat *cell.Cell) (transactionUsage, any, *cell.Cell, error) {
	version := transactionGlobalVersion(cfg)
	storeStorageDictHash := version >= 11 && !transactionIsMasterchain(acc.addr)

	oldStorageForStat, err := transactionOldAccountStorageForStat(acc, version >= 10)
	if err != nil {
		return transactionUsage{}, nil, nil, err
	}

	oldDictHash := transactionStorageExtraDictHash(acc.storageInfo.StorageExtra)
	storageRefsChanged := !transactionAccountStorageRefsUnchanged(oldStorageForStat, storageCellForStat)
	needMissingDict := storeStorageDictHash && oldDictHash == nil && transactionStorageUsedUint64(acc.storageInfo.StorageUsed.CellsUsed) > 25
	if storageRefsChanged || needMissingDict {
		stat, err := transactionInitAccountStorageStat(accountStorageStat, oldStorageForStat, acc.storageInfo.StorageUsed, oldDictHash)
		if err != nil {
			return transactionUsage{}, nil, nil, err
		}

		var usage transactionUsage
		var nextStorageStat *cell.Cell
		if stat != nil {
			usage, nextStorageStat, err = stat.replaceStorage(storageCellForStat)
		} else {
			usage, nextStorageStat, err = transactionComputeAccountStorageStat(storageCellForStat)
		}
		if err != nil {
			return transactionUsage{}, nil, nil, err
		}

		storageExtra := any(tlb.StorageExtraNone{})
		if storeStorageDictHash && usage.cells >= transactionGetSizeLimits(cfg).accStateCellsForStorageDict {
			storageExtra = tlb.StorageExtraInfo{DictHash: transactionAccountStorageStatRootHash(nextStorageStat)}
		}
		return usage, storageExtra, nextStorageStat, nil
	}

	usage := transactionAccountStorageUsageWithSameRefs(acc.storageInfo.StorageUsed, oldStorageForStat, storageCellForStat)
	storageExtra := any(tlb.StorageExtraNone{})
	if storeStorageDictHash && oldDictHash != nil {
		storageExtra = tlb.StorageExtraInfo{DictHash: append([]byte(nil), oldDictHash...)}
	}
	return usage, storageExtra, accountStorageStat, nil
}

func transactionOldAccountStorageForStat(acc *transactionRuntimeAccount, extraCurrencyV2 bool) (*cell.Cell, error) {
	if acc == nil || acc.storageCell == nil {
		return nil, nil
	}
	if !extraCurrencyV2 {
		return acc.storageCell, nil
	}

	var storage tlb.AccountStorage
	if err := tlb.Parse(&storage, acc.storageCell); err != nil {
		return nil, fmt.Errorf("failed to decode old account storage for stats: %w", err)
	}
	return transactionAccountStorageWithoutExtraCurrencies(storage)
}

func transactionAccountStorageWithoutExtraCurrencies(storage tlb.AccountStorage) (*cell.Cell, error) {
	storage.ExtraCurrencies = nil
	storageCell, err := tlb.ToCell(&storage)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize account storage without extra currencies: %w", err)
	}
	return storageCell, nil
}

func transactionStorageExtraDictHash(extra any) []byte {
	switch v := extra.(type) {
	case tlb.StorageExtraInfo:
		if len(v.DictHash) == 32 {
			return v.DictHash
		}
	case *tlb.StorageExtraInfo:
		if v != nil && len(v.DictHash) == 32 {
			return v.DictHash
		}
	}
	return nil
}

type transactionAccountStorageStatEntry struct {
	loaded         bool
	refCount       uint32
	maxMerkleDepth uint32
	dirty          bool
}

type transactionAccountStorageStat struct {
	dict       *cell.Dictionary
	roots      []*cell.Cell
	entries    map[cell.Hash]*transactionAccountStorageStatEntry
	totalCells uint64
	totalBits  uint64
}

func transactionInitAccountStorageStat(dictRoot, storageCell *cell.Cell, storageUsed tlb.StorageUsed, dictHash []byte) (*transactionAccountStorageStat, error) {
	if dictRoot == nil {
		return nil, nil
	}
	if dictHash != nil && !transactionHashIsZero(dictHash) && !bytes.Equal(dictRoot.Hash(), dictHash) {
		return nil, errors.New("account storage stat root hash does not match account storage extra")
	}

	totalCells := transactionStorageUsedUint64(storageUsed.CellsUsed)
	if totalCells > 0 {
		totalCells--
	}
	totalBits := transactionStorageUsedUint64(storageUsed.BitsUsed)
	if storageCell != nil {
		rootBits := uint64(storageCell.BitsSize())
		if totalBits < rootBits {
			return nil, errors.New("account storage used bits is smaller than account storage root")
		}
		totalBits -= rootBits
	}

	return &transactionAccountStorageStat{
		dict:       dictRoot.AsDict(256),
		roots:      transactionAccountStorageRootRefs(storageCell),
		entries:    map[cell.Hash]*transactionAccountStorageStatEntry{},
		totalCells: totalCells,
		totalBits:  totalBits,
	}, nil
}

func transactionComputeAccountStorageStat(storageCell *cell.Cell) (transactionUsage, *cell.Cell, error) {
	stat := transactionAccountStorageStat{
		dict:    cell.NewDict(256),
		entries: map[cell.Hash]*transactionAccountStorageStatEntry{},
	}
	if storageCell != nil {
		for i := 0; i < int(storageCell.RefsNum()); i++ {
			if _, err := stat.addCell(storageCell.MustPeekRef(i)); err != nil {
				return transactionUsage{}, nil, err
			}
		}
	}

	usage := transactionUsage{
		cells: stat.totalCells,
		bits:  stat.totalBits,
	}
	if storageCell != nil {
		usage.cells++
		usage.bits += uint64(storageCell.BitsSize())
	}

	dictRoot, err := stat.dictRoot()
	if err != nil {
		return transactionUsage{}, nil, err
	}
	return usage, dictRoot, nil
}

func (s *transactionAccountStorageStat) replaceStorage(storageCell *cell.Cell) (transactionUsage, *cell.Cell, error) {
	newRoots := transactionAccountStorageRootRefs(storageCell)
	toAdd, toDel := transactionAccountStorageRootDiff(s.roots, newRoots)

	for _, root := range toAdd {
		if _, err := s.addCell(root); err != nil {
			return transactionUsage{}, nil, err
		}
	}
	for _, root := range toDel {
		if err := s.removeCell(root); err != nil {
			return transactionUsage{}, nil, err
		}
	}
	s.roots = newRoots

	usage := transactionUsage{cells: s.totalCells, bits: s.totalBits}
	if storageCell != nil {
		usage.cells++
		usage.bits += uint64(storageCell.BitsSize())
	}

	dictRoot, err := s.dictRoot()
	if err != nil {
		return transactionUsage{}, nil, err
	}
	return usage, dictRoot, nil
}

func (s *transactionAccountStorageStat) addCell(c *cell.Cell) (uint32, error) {
	if c == nil {
		return 0, nil
	}

	key := c.HashKey()
	entry, err := s.entry(key)
	if err != nil {
		return 0, err
	}
	if entry.refCount == math.MaxUint32 {
		return 0, errors.New("account storage cell refcount overflow")
	}
	entry.refCount++
	entry.dirty = true
	if entry.refCount > 1 {
		return entry.maxMerkleDepth, nil
	}

	var maxDepth uint32
	for i := 0; i < int(c.RefsNum()); i++ {
		depth, err := s.addCell(c.MustPeekRef(i))
		if err != nil {
			return 0, err
		}
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	switch c.GetType() {
	case cell.MerkleProofCellType, cell.MerkleUpdateCellType:
		maxDepth++
	}
	if maxDepth > 3 {
		maxDepth = 3
	}

	entry.maxMerkleDepth = maxDepth
	s.totalCells++
	s.totalBits += uint64(c.BitsSize())
	return maxDepth, nil
}

func (s *transactionAccountStorageStat) removeCell(c *cell.Cell) error {
	if c == nil {
		return nil
	}

	key := c.HashKey()
	entry, err := s.entry(key)
	if err != nil {
		return err
	}
	if entry.refCount == 0 {
		return fmt.Errorf("account storage stat cannot remove absent cell %x", key)
	}
	entry.refCount--
	entry.dirty = true
	if entry.refCount > 0 {
		return nil
	}

	for i := 0; i < int(c.RefsNum()); i++ {
		if err = s.removeCell(c.MustPeekRef(i)); err != nil {
			return err
		}
	}
	if s.totalCells > 0 {
		s.totalCells--
	}
	bits := uint64(c.BitsSize())
	if s.totalBits >= bits {
		s.totalBits -= bits
	} else {
		s.totalBits = 0
	}
	return nil
}

func (s *transactionAccountStorageStat) entry(key cell.Hash) (*transactionAccountStorageStatEntry, error) {
	entry := s.entries[key]
	if entry != nil {
		return entry, nil
	}

	entry = &transactionAccountStorageStatEntry{loaded: true}
	s.entries[key] = entry
	if s.dict == nil || s.dict.IsEmpty() {
		return entry, nil
	}

	value, err := s.dict.LoadValue(transactionAccountStorageStatKey(key))
	if errors.Is(err, cell.ErrNoSuchKeyInDict) {
		return entry, nil
	}
	if err != nil {
		return nil, err
	}
	if value.BitsLeft() != 34 || value.RefsNum() != 0 {
		return nil, fmt.Errorf("invalid account storage stat record for cell %x", key)
	}
	entry.refCount = uint32(value.MustLoadUInt(32))
	entry.maxMerkleDepth = uint32(value.MustLoadUInt(2))
	if entry.refCount == 0 {
		return nil, fmt.Errorf("invalid zero account storage stat refcount for cell %x", key)
	}
	return entry, nil
}

func (s *transactionAccountStorageStat) dictRoot() (*cell.Cell, error) {
	if s.dict == nil {
		s.dict = cell.NewDict(256)
	}
	for key, entry := range s.entries {
		if !entry.dirty {
			continue
		}
		keyCell := transactionAccountStorageStatKey(key)
		if entry.refCount == 0 {
			if err := s.dict.Delete(keyCell); err != nil {
				return nil, err
			}
			entry.dirty = false
			continue
		}
		value := cell.BeginCell().
			MustStoreUInt(uint64(entry.refCount), 32).
			MustStoreUInt(uint64(entry.maxMerkleDepth), 2).
			EndCell()
		if err := s.dict.Set(keyCell, value); err != nil {
			return nil, err
		}
		entry.dirty = false
	}
	return s.dict.AsCell(), nil
}

func transactionAccountStorageStatRootHash(root *cell.Cell) []byte {
	if root == nil {
		return make([]byte, 32)
	}
	return append([]byte(nil), root.Hash()...)
}

func transactionAccountStorageStatKey(key cell.Hash) *cell.Cell {
	return cell.BeginCell().MustStoreSlice(key[:], 256).EndCell()
}

func transactionHashIsZero(hash []byte) bool {
	for _, b := range hash {
		if b != 0 {
			return false
		}
	}
	return len(hash) > 0
}

func transactionAccountStorageRootRefs(storage *cell.Cell) []*cell.Cell {
	if storage == nil || storage.RefsNum() == 0 {
		return nil
	}
	refs := make([]*cell.Cell, storage.RefsNum())
	for i := range refs {
		refs[i] = storage.MustPeekRef(i)
	}
	return refs
}

func transactionAccountStorageRootDiff(oldRoots, newRoots []*cell.Cell) (toAdd, toDel []*cell.Cell) {
	oldRoots = transactionSortedAccountStorageRoots(oldRoots)
	newRoots = transactionSortedAccountStorageRoots(newRoots)

	var oldIdx, newIdx int
	for oldIdx < len(oldRoots) && newIdx < len(newRoots) {
		cmp := bytes.Compare(oldRoots[oldIdx].Hash(), newRoots[newIdx].Hash())
		switch {
		case cmp == 0:
			oldIdx++
			newIdx++
		case cmp < 0:
			toDel = append(toDel, oldRoots[oldIdx])
			oldIdx++
		default:
			toAdd = append(toAdd, newRoots[newIdx])
			newIdx++
		}
	}
	toDel = append(toDel, oldRoots[oldIdx:]...)
	toAdd = append(toAdd, newRoots[newIdx:]...)
	return toAdd, toDel
}

func transactionSortedAccountStorageRoots(roots []*cell.Cell) []*cell.Cell {
	if len(roots) == 0 {
		return nil
	}
	out := append([]*cell.Cell(nil), roots...)
	sort.Slice(out, func(i, j int) bool {
		return bytes.Compare(out[i].Hash(), out[j].Hash()) < 0
	})
	return out
}

func transactionAccountStorageRefsUnchanged(oldStorage, newStorage *cell.Cell) bool {
	if oldStorage == nil || newStorage == nil || oldStorage.RefsNum() != newStorage.RefsNum() {
		return false
	}
	for i := 0; i < int(oldStorage.RefsNum()); i++ {
		if !bytes.Equal(oldStorage.MustPeekRef(i).Hash(), newStorage.MustPeekRef(i).Hash()) {
			return false
		}
	}
	return true
}

func transactionAccountStorageUsageWithSameRefs(old tlb.StorageUsed, oldStorage, newStorage *cell.Cell) transactionUsage {
	cells := transactionStorageUsedUint64(old.CellsUsed)
	bits := transactionStorageUsedUint64(old.BitsUsed)
	if oldStorage == nil || newStorage == nil {
		return transactionUsage{cells: cells, bits: bits}
	}

	oldRootBits := uint64(oldStorage.BitsSize())
	newRootBits := uint64(newStorage.BitsSize())
	switch {
	case newRootBits >= oldRootBits:
		bits += newRootBits - oldRootBits
	case bits >= oldRootBits-newRootBits:
		bits -= oldRootBits - newRootBits
	default:
		bits = 0
	}
	return transactionUsage{cells: cells, bits: bits}
}

func transactionStorageUsedUint64(v *big.Int) uint64 {
	if v == nil || v.Sign() <= 0 {
		return 0
	}
	if !v.IsUint64() {
		return ^uint64(0)
	}
	return v.Uint64()
}

func transactionCloneUint64(v *uint64) *uint64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
func transactionInitialComputeStatus(status tlb.AccountStatus) tlb.AccountStatus {
	if status == tlb.AccountStatusNonExist {
		return tlb.AccountStatusUninit
	}
	return status
}

func transactionFinalizeAccountStatus(status tlb.AccountStatus, deleted bool, balance *big.Int, extraCurrencies *cell.Dictionary, activated bool) tlb.AccountStatus {
	if deleted {
		if (balance == nil || balance.Sign() == 0) && transactionExtraDictIsEmpty(extraCurrencies) {
			return tlb.AccountStatusNonExist
		}
		return tlb.AccountStatusUninit
	}
	if status == tlb.AccountStatusUninit && !activated && (balance == nil || balance.Sign() == 0) && transactionExtraDictIsEmpty(extraCurrencies) {
		return tlb.AccountStatusNonExist
	}
	return status
}

func transactionPrepareComputeAccount(acc *transactionRuntimeAccount, status tlb.AccountStatus, deleted bool, msg *tlb.Message) (*transactionRuntimeAccount, bool, *tlb.ComputeSkipReason, error) {
	stateInit := transactionMessageStateInit(msg)
	if deleted {
		return acc, false, &tlb.ComputeSkipReason{Type: transactionNoStateSkipReason(stateInit)}, nil
	}
	if status == tlb.AccountStatusActive {
		if acc.code == nil {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}, nil
		}
		if stateInit != nil && msg.MsgType == tlb.MsgTypeExternalIn {
			stateCell, err := tlb.ToCell(stateInit)
			if err != nil {
				return nil, false, nil, fmt.Errorf("failed to serialize inbound state init: %w", err)
			}
			if !bytes.Equal(stateCell.Hash(), acc.addr.Data()) {
				return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
			}
		}
		return acc, false, nil, nil
	}
	if stateInit == nil {
		return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}, nil
	}

	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		return nil, false, nil, fmt.Errorf("failed to serialize inbound state init: %w", err)
	}
	switch status {
	case tlb.AccountStatusUninit, tlb.AccountStatusNonExist:
		if !transactionStateInitMatchesAddress(stateCell.Hash(), acc.addr, stateInit.Depth) {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
	case tlb.AccountStatusFrozen:
		if !bytes.Equal(stateCell.Hash(), acc.stateHash) {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
	default:
		return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}, nil
	}
	if stateInit.Code == nil {
		return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
	}

	next := *acc
	next.status = tlb.AccountStatusActive
	next.code = stateInit.Code
	next.data = stateInit.Data
	next.libraries = stateInit.Lib
	next.stateDepth = transactionCloneUint64(stateInit.Depth)
	next.tickTock = stateInit.TickTock
	next.stateHash = nil
	return &next, true, nil, nil
}

func transactionStateInitMatchesAddress(stateHash []byte, addr *address.Address, fixedPrefixLength *uint64) bool {
	if addr == nil || len(stateHash) != 32 || len(addr.Data()) != 32 {
		return false
	}
	depth := 0
	if fixedPrefixLength != nil {
		if *fixedPrefixLength > 30 {
			return false
		}
		depth = int(*fixedPrefixLength)
	}
	for i := depth; i < 256; i++ {
		if transactionBit(stateHash, i) != transactionBit(addr.Data(), i) {
			return false
		}
	}
	return true
}

func transactionBit(src []byte, idx int) byte {
	return (src[idx/8] >> (7 - uint(idx%8))) & 1
}

func transactionNoStateSkipReason(stateInit *tlb.StateInit) tlb.ComputeSkipReasonType {
	if stateInit != nil {
		return tlb.ComputeSkipReasonBadState
	}
	return tlb.ComputeSkipReasonNoState
}

func transactionMessageStateInit(msg *tlb.Message) *tlb.StateInit {
	if msg == nil {
		return nil
	}
	switch msg.MsgType {
	case tlb.MsgTypeInternal:
		return msg.AsInternal().StateInit
	case tlb.MsgTypeExternalIn:
		return msg.AsExternalIn().StateInit
	default:
		return nil
	}
}
