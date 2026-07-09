package tvm

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// PreparedAccount is an account state parsed once into the representation the
// transaction executor needs. Build it with PrepareAccount at lane start; each
// TransactionExecutionResult carries the follow-up PreparedAccount so
// consecutive transactions of one account never re-parse state.
type PreparedAccount struct {
	shard   *tlb.ShardAccount
	state   *tlb.AccountState
	runtime transactionRuntimeAccount
}

// PrepareAccount parses the shard account state exactly once. addr is the
// account address used for non-existing accounts (a lane always knows its
// account); it may be nil for existing accounts, whose address comes from the
// parsed state.
func PrepareAccount(shard *tlb.ShardAccount, addr *address.Address) (*PreparedAccount, error) {
	if shard == nil {
		return nil, errors.New("shard account is required")
	}
	if shard.Account == nil {
		return nil, errors.New("shard account root is nil")
	}

	var state tlb.AccountState
	if err := tlb.Parse(&state, shard.Account); err != nil {
		return nil, fmt.Errorf("failed to decode account state: %w", err)
	}
	return prepareAccountFromState(shard, &state, addr, nil)
}

// PrepareParsedAccount wraps an already-parsed shard account state; state must
// be the parsed form of shard.Account.
func PrepareParsedAccount(shard *tlb.ShardAccount, state *tlb.AccountState, addr *address.Address) (*PreparedAccount, error) {
	if shard == nil {
		return nil, errors.New("shard account is required")
	}
	if state == nil {
		return nil, errors.New("parsed account state is required")
	}
	return prepareAccountFromState(shard, state, addr, nil)
}

func prepareAccountFromState(shard *tlb.ShardAccount, state *tlb.AccountState, addr *address.Address, storageCell *cell.Cell) (*PreparedAccount, error) {
	runtime, err := loadTransactionRuntimeAccountState(shard, state, addr, storageCell == nil)
	if err != nil {
		return nil, err
	}
	if storageCell != nil {
		runtime.storageCell = storageCell
	}
	return &PreparedAccount{
		shard:   shard,
		state:   state,
		runtime: *runtime,
	}, nil
}

// ShardAccount returns the shard account this state was prepared from
// (for results: the post-transaction shard account).
func (a *PreparedAccount) ShardAccount() *tlb.ShardAccount {
	if a == nil {
		return nil
	}
	return a.shard
}

// ShardAccountCell serializes the shard account into a cell.
func (a *PreparedAccount) ShardAccountCell() *cell.Cell {
	if a == nil || a.shard == nil {
		return nil
	}
	return buildTransactionShardAccountCell(a.shard.Account, a.shard.LastTransHash, a.shard.LastTransLT)
}

// State returns the parsed account state.
func (a *PreparedAccount) State() *tlb.AccountState {
	if a == nil {
		return nil
	}
	return a.state
}

// Address returns the resolved account address.
func (a *PreparedAccount) Address() *address.Address {
	if a == nil {
		return nil
	}
	return a.runtime.addr
}

// runtimeForExecution returns the runtime account view for one emulation. The
// hot path hands out a copy of the pre-parsed representation; the proof path
// re-reads the account through a fresh usage-traced root so that state loads
// are recorded in the proof.
func (a *PreparedAccount) runtimeForExecution(buildProof bool) (*transactionRuntimeAccount, *cell.MerkleProofBuilder, error) {
	if !buildProof {
		runtime := a.runtime
		return &runtime, nil, nil
	}

	if a.shard.Account == nil {
		return nil, nil, errors.New("shard account root is nil")
	}
	proof := cell.NewMerkleProofBuilder(a.shard.Account)

	var state tlb.AccountState
	if err := tlb.Parse(&state, proof.Root()); err != nil {
		return nil, nil, fmt.Errorf("failed to decode account state: %w", err)
	}
	runtime, err := loadTransactionRuntimeAccountState(a.shard, &state, a.runtime.addr, true)
	if err != nil {
		return nil, nil, err
	}
	return runtime, proof, nil
}

func loadTransactionRuntimeAccountState(shard *tlb.ShardAccount, acc *tlb.AccountState, fallbackAddr *address.Address, buildStorageCell bool) (*transactionRuntimeAccount, error) {
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
	if buildStorageCell {
		storageCell, err := buildTransactionAccountStorageCell(acc.Status, acc.LastTransactionLT, acc.Balance.Nano(), acc.ExtraCurrencies, acc.StateInit, acc.StateHash)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize original account storage: %w", err)
		}
		out.storageCell = storageCell
	}
	if acc.StateInit != nil {
		out.code = acc.StateInit.Code
		out.data = acc.StateInit.Data
		out.libraries = acc.StateInit.Lib
		out.stateDepth = transactionCloneUint64(acc.StateInit.Depth)
		out.tickTock = acc.StateInit.TickTock
	}
	return out, nil
}

func transactionPrepareInitialPhases(acc *transactionRuntimeAccount, msg *tlb.Message, storageFee, importFee *big.Int, now uint32, cfg *PreparedBlockchainConfig, limits transactionStorageDueLimits) (*transactionPreparedPhases, error) {
	globalVersion := cfg.globalVersion()
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
		if globalVersion < 12 {
			prepared.msgBalance.grams.Add(prepared.msgBalance.grams, transactionBigOrZero(in.IHRFee.Nano()))
		}
		if cfg.isBlackHoleAccount(acc.addr) {
			prepared.msgBalance.grams.SetInt64(0)
		}
		if prepared.creditFirst {
			if err = credit(prepared.msgBalance.grams, in.ExtraCurrencies); err != nil {
				return nil, err
			}
			prepared.applyStoragePhase(acc, storageFee, now, globalVersion, limits, true)
		} else {
			prepared.applyStoragePhase(acc, storageFee, now, globalVersion, limits, false)
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
		prepared.applyStoragePhase(acc, storageFee, now, globalVersion, limits, false)
	default:
		return nil, fmt.Errorf("unsupported input message type %s", msg.MsgType)
	}

	return prepared, nil
}

func (p *transactionPreparedPhases) applyStoragePhase(acc *transactionRuntimeAccount, storageFee *big.Int, now uint32, globalVersion uint32, limits transactionStorageDueLimits, adjustMsgValue bool) {
	collected := big.NewInt(0)
	due := big.NewInt(0)
	statusChange := tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged}

	p.duePayment = transactionCoinsPtr(transactionCoinsNano(acc.storageInfo.DuePayment))
	p.lastPaid = now
	if storageFee != nil && storageFee.Sign() > 0 {
		if storageFee.Cmp(p.balance) <= 0 {
			collected.Set(storageFee)
			p.balance.Sub(p.balance, storageFee)
			if globalVersion >= 7 {
				p.duePayment = nil
			}
		} else {
			collected.Set(p.balance)
			due.Sub(storageFee, p.balance)
			p.balance.SetInt64(0)

			switch p.status {
			case tlb.AccountStatusUninit, tlb.AccountStatusFrozen, tlb.AccountStatusNonExist:
				if due.Cmp(limits.deleteDue) > 0 && transactionExtraDictIsEmpty(p.extraCurrencies) {
					p.deleted = true
					p.destroyed = globalVersion >= 13
					p.status = tlb.AccountStatusNonExist
					statusChange.Type = tlb.AccStatusChangeDeleted
				}
			case tlb.AccountStatusActive:
				if due.Cmp(limits.freezeDue) > 0 {
					p.status = tlb.AccountStatusFrozen
					statusChange.Type = tlb.AccStatusChangeFrozen
				}
			}
			if globalVersion >= 4 {
				p.duePayment = transactionCoinsPtr(due)
			}
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

// builtTransactionAccount is the serialized post-transaction account state
// along with its parsed form and the pieces needed to prepare the follow-up
// account without re-parsing.
type builtTransactionAccount struct {
	cell        *cell.Cell
	state       *tlb.AccountState
	storageStat *cell.Cell
	storageCell *cell.Cell
}

func buildTransactionAccountCell(acc *transactionRuntimeAccount, status tlb.AccountStatus, balance *big.Int, extraCurrencies *cell.Dictionary, endLT uint64, lastPaid uint32, duePayment *tlb.Coins, code, data *cell.Cell, libs *cell.Dictionary, stateHash []byte, cfg *PreparedBlockchainConfig, accountStorageStat *cell.Cell) (*builtTransactionAccount, error) {
	if status == tlb.AccountStatusNonExist {
		accountState := &tlb.AccountState{
			IsValid: false,
			AccountStorage: tlb.AccountStorage{
				Status:  tlb.AccountStatusNonExist,
				Balance: tlb.FromNanoTON(big.NewInt(0)),
			},
		}
		return &builtTransactionAccount{
			cell:  cell.BeginCell().MustStoreBoolBit(false).EndCell(),
			state: accountState,
		}, nil
	}

	stateDepth := transactionCloneUint64(acc.stateDepth)
	if cfg.globalVersion() < 10 && acc.addr != nil && acc.addr.Anycast() != nil {
		depth := uint64(acc.addr.Anycast().Depth())
		stateDepth = &depth
	}

	stateInit := &tlb.StateInit{
		Depth:    stateDepth,
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
			stateInitCell, err := buildTransactionStateInitCell(stateInit)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize frozen state init: %w", err)
			}
			stateHash = stateInitCell.Hash()
		}
		accountStorage.StateHash = append([]byte(nil), stateHash...)
	case tlb.AccountStatusUninit:
	default:
		return nil, fmt.Errorf("unsupported final account status %s", status)
	}

	accountAddr, err := transactionAccountSerializationAddr(acc.addr, cfg)
	if err != nil {
		return nil, err
	}
	storageBuilder, err := buildTransactionAccountStorageBuilder(status, endLT, balance, extraCurrencies, accountStorage.StateInit, accountStorage.StateHash)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize account storage: %w", err)
	}
	storageCell := storageBuilder.EndCell()

	storageCellForStat := storageCell
	if cfg.globalVersion() >= 10 {
		storageCellForStat, err = buildTransactionAccountStorageCell(status, endLT, balance, nil, accountStorage.StateInit, accountStorage.StateHash)
		if err != nil {
			return nil, err
		}
	}

	usage, storageExtra, storageExtraDictHash, nextStorageStat, err := transactionAccountStorageInfo(acc, storageCellForStat, cfg, accountStorageStat)
	if err != nil {
		return nil, err
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
		Address:        accountAddr,
		StorageInfo:    storageInfo,
		AccountStorage: accountStorage,
	}

	accountCell, err := buildTransactionAccountStateCell(accountAddr, storageInfo.StorageUsed, storageExtraDictHash, lastPaid, duePayment, storageBuilder)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize account state: %w", err)
	}

	return &builtTransactionAccount{
		cell:        accountCell,
		state:       accountState,
		storageStat: nextStorageStat,
		storageCell: storageCell,
	}, nil
}

func transactionAccountSerializationAddr(addr *address.Address, cfg *PreparedBlockchainConfig) (*address.Address, error) {
	if addr == nil || addr.Anycast() == nil {
		return addr, nil
	}
	if cfg.globalVersion() < 10 {
		return addr, nil
	}

	data, err := transactionRewrittenAccountAddressData(addr)
	if err != nil {
		return nil, err
	}

	out := addr.Copy()
	copy(out.Data(), data)
	out.SetAnycast(nil)
	return out, nil
}

func transactionAccountIDAddr(addr *address.Address) (*address.Address, error) {
	if addr == nil || addr.Anycast() == nil {
		return addr, nil
	}

	data, err := transactionRewrittenAccountAddressData(addr)
	if err != nil {
		return nil, err
	}

	out := addr.Copy()
	copy(out.Data(), data)
	out.SetAnycast(nil)
	return out, nil
}

func transactionAccountStorageInfo(acc *transactionRuntimeAccount, storageCellForStat *cell.Cell, cfg *PreparedBlockchainConfig, accountStorageStat *cell.Cell) (transactionUsage, any, []byte, *cell.Cell, error) {
	version := cfg.globalVersion()
	storeStorageDictHash := version >= 11 && !transactionIsMasterchain(acc.addr)

	oldStorageForStat, err := transactionOldAccountStorageForStat(acc, version >= 10)
	if err != nil {
		return transactionUsage{}, nil, nil, nil, err
	}

	oldDictHash := transactionStorageExtraDictHash(acc.storageInfo.StorageExtra)
	storageRefsUnchanged, err := transactionAccountStorageRefsUnchanged(oldStorageForStat, storageCellForStat)
	if err != nil {
		return transactionUsage{}, nil, nil, nil, err
	}
	storageRefsChanged := !storageRefsUnchanged
	needMissingDict := storeStorageDictHash && oldDictHash == nil && transactionStorageUsedUint64(acc.storageInfo.StorageUsed.CellsUsed) > 25
	if storageRefsChanged || needMissingDict {
		stat, err := transactionInitAccountStorageStat(accountStorageStat, oldStorageForStat, acc.storageInfo.StorageUsed, oldDictHash)
		if err != nil {
			return transactionUsage{}, nil, nil, nil, err
		}

		var usage transactionUsage
		var nextStorageStat *cell.Cell
		if stat != nil {
			usage, nextStorageStat, err = stat.replaceStorage(storageCellForStat)
		} else {
			usage, nextStorageStat, err = transactionComputeAccountStorageStat(storageCellForStat)
		}
		if err != nil {
			return transactionUsage{}, nil, nil, nil, err
		}

		storageExtra := any(tlb.StorageExtraNone{})
		var storageExtraDictHash []byte
		if storeStorageDictHash && usage.cells >= transactionGetSizeLimits(cfg).accStateCellsForStorageDict {
			storageExtraDictHash = transactionAccountStorageStatRootHash(nextStorageStat)
			storageExtra = tlb.StorageExtraInfo{DictHash: storageExtraDictHash}
		}
		return usage, storageExtra, storageExtraDictHash, nextStorageStat, nil
	}

	usage, err := transactionAccountStorageUsageWithSameRefs(acc.storageInfo.StorageUsed, oldStorageForStat, storageCellForStat)
	if err != nil {
		return transactionUsage{}, nil, nil, nil, err
	}
	storageExtra := any(tlb.StorageExtraNone{})
	var storageExtraDictHash []byte
	if storeStorageDictHash && oldDictHash != nil {
		storageExtraDictHash = append([]byte(nil), oldDictHash...)
		storageExtra = tlb.StorageExtraInfo{DictHash: storageExtraDictHash}
	}
	return usage, storageExtra, storageExtraDictHash, accountStorageStat, nil
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
	storageCell, err := buildTransactionAccountStorageCell(storage.Status, storage.LastTransactionLT, storage.Balance.Nano(), nil, storage.StateInit, storage.StateHash)
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
	roots      [4]*cell.Cell
	rootsNum   int
	entries    map[cell.Hash]*transactionAccountStorageStatEntry
	totalCells uint64
	totalBits  uint64
}

func transactionInitAccountStorageStat(dictRoot, storageCell *cell.Cell, storageUsed tlb.StorageUsed, dictHash []byte) (*transactionAccountStorageStat, error) {
	if dictRoot == nil {
		return nil, nil
	}
	if dictHash != nil && !transactionHashIsZero(dictHash) {
		rootHash := dictRoot.HashKey()
		if !bytes.Equal(rootHash[:], dictHash) {
			return nil, errors.New("account storage stat root hash does not match account storage extra")
		}
	}

	totalCells := transactionStorageUsedUint64(storageUsed.CellsUsed)
	if totalCells > 0 {
		totalCells--
	}
	totalBits := transactionStorageUsedUint64(storageUsed.BitsUsed)
	var roots [4]*cell.Cell
	var rootsNum int
	if storageCell != nil {
		loadedStorage, storageRoots, storageRootsNum, err := transactionLoadAccountStorageRootRefs(storageCell)
		if err != nil {
			return nil, err
		}
		roots = storageRoots
		rootsNum = storageRootsNum
		rootBits := uint64(loadedStorage.BitsSize())
		if totalBits < rootBits {
			return nil, errors.New("account storage used bits is smaller than account storage root")
		}
		totalBits -= rootBits
	}

	return &transactionAccountStorageStat{
		dict:       dictRoot.AsDict(256),
		roots:      roots,
		rootsNum:   rootsNum,
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
		loadedStorage, roots, rootsNum, err := transactionLoadAccountStorageRootRefs(storageCell)
		if err != nil {
			return transactionUsage{}, nil, err
		}

		for i := 0; i < rootsNum; i++ {
			if _, err := stat.addCell(roots[i]); err != nil {
				return transactionUsage{}, nil, err
			}
		}
		storageCell = loadedStorage
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
	loadedStorage, newRoots, newRootsNum, err := transactionLoadAccountStorageRootRefs(storageCell)
	if err != nil {
		return transactionUsage{}, nil, err
	}
	storageCell = loadedStorage
	toAdd, toAddNum, toDel, toDelNum := transactionAccountStorageRootDiff(s.roots, s.rootsNum, newRoots, newRootsNum)

	for i := 0; i < toAddNum; i++ {
		if _, err := s.addCell(toAdd[i]); err != nil {
			return transactionUsage{}, nil, err
		}
	}
	for i := 0; i < toDelNum; i++ {
		if err := s.removeCell(toDel[i]); err != nil {
			return transactionUsage{}, nil, err
		}
	}
	s.roots = newRoots
	s.rootsNum = newRootsNum

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

	sl, err := c.BeginParseWithoutTrace()
	if err != nil {
		return 0, err
	}

	loaded := sl.BaseCell()
	var refs [4]*cell.Cell
	refsNum := sl.RefsNum()
	for i := 0; i < refsNum; i++ {
		refs[i], err = sl.LoadRefCell()
		if err != nil {
			return 0, err
		}
	}

	key := loaded.HashKey()
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
	for i := 0; i < refsNum; i++ {
		depth, err := s.addCell(refs[i])
		if err != nil {
			return 0, err
		}
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	switch loaded.GetType() {
	case cell.MerkleProofCellType, cell.MerkleUpdateCellType:
		maxDepth++
	}
	if maxDepth > 3 {
		maxDepth = 3
	}

	entry.maxMerkleDepth = maxDepth
	s.totalCells++
	s.totalBits += uint64(loaded.BitsSize())
	return maxDepth, nil
}

func (s *transactionAccountStorageStat) removeCell(c *cell.Cell) error {
	sl, err := c.BeginParseWithoutTrace()
	if err != nil {
		return err
	}

	loaded := sl.BaseCell()
	var refs [4]*cell.Cell
	refsNum := sl.RefsNum()
	for i := 0; i < refsNum; i++ {
		refs[i], err = sl.LoadRefCell()
		if err != nil {
			return err
		}
	}

	key := loaded.HashKey()
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

	for i := 0; i < refsNum; i++ {
		if err = s.removeCell(refs[i]); err != nil {
			return err
		}
	}
	if s.totalCells > 0 {
		s.totalCells--
	}
	bits := uint64(loaded.BitsSize())
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
			MustStoreUInt(uint64(entry.maxMerkleDepth), 2)
		if err := s.dict.SetBuilder(keyCell, value); err != nil {
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
	return root.Hash()
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

func transactionLoadAccountStorageRootRefs(storage *cell.Cell) (*cell.Cell, [4]*cell.Cell, int, error) {
	var refs [4]*cell.Cell
	if storage == nil {
		return nil, refs, 0, nil
	}

	sl, err := storage.BeginParseWithoutTrace()
	if err != nil {
		return nil, refs, 0, err
	}

	loaded := sl.BaseCell()
	refsNum := sl.RefsNum()
	for i := 0; i < refsNum; i++ {
		refs[i], err = sl.LoadRefCell()
		if err != nil {
			return nil, refs, 0, err
		}
	}
	return loaded, refs, refsNum, nil
}

func transactionAccountStorageRootDiff(oldRoots [4]*cell.Cell, oldRootsNum int, newRoots [4]*cell.Cell, newRootsNum int) (toAdd [4]*cell.Cell, toAddNum int, toDel [4]*cell.Cell, toDelNum int) {
	oldSorted := transactionSortedAccountStorageRoots(oldRoots, oldRootsNum)
	newSorted := transactionSortedAccountStorageRoots(newRoots, newRootsNum)

	var oldIdx, newIdx int
	for oldIdx < oldRootsNum && newIdx < newRootsNum {
		cmp := bytes.Compare(oldSorted[oldIdx].hash[:], newSorted[newIdx].hash[:])
		switch {
		case cmp == 0:
			oldIdx++
			newIdx++
		case cmp < 0:
			toDel[toDelNum] = oldSorted[oldIdx].root
			toDelNum++
			oldIdx++
		default:
			toAdd[toAddNum] = newSorted[newIdx].root
			toAddNum++
			newIdx++
		}
	}
	for ; oldIdx < oldRootsNum; oldIdx++ {
		toDel[toDelNum] = oldSorted[oldIdx].root
		toDelNum++
	}
	for ; newIdx < newRootsNum; newIdx++ {
		toAdd[toAddNum] = newSorted[newIdx].root
		toAddNum++
	}
	return toAdd, toAddNum, toDel, toDelNum
}

type transactionAccountStorageRootHash struct {
	root *cell.Cell
	hash cell.Hash
}

func transactionSortedAccountStorageRoots(roots [4]*cell.Cell, rootsNum int) [4]transactionAccountStorageRootHash {
	var out [4]transactionAccountStorageRootHash
	if rootsNum == 0 {
		return out
	}

	for i := 0; i < rootsNum; i++ {
		root := roots[i]
		out[i] = transactionAccountStorageRootHash{
			root: root,
			hash: root.HashKey(),
		}
	}

	for i := 1; i < rootsNum; i++ {
		next := out[i]
		j := i - 1
		for ; j >= 0 && bytes.Compare(out[j].hash[:], next.hash[:]) > 0; j-- {
			out[j+1] = out[j]
		}
		out[j+1] = next
	}
	return out
}

func transactionAccountStorageRefsUnchanged(oldStorage, newStorage *cell.Cell) (bool, error) {
	_, oldRefs, oldRefsNum, err := transactionLoadAccountStorageRootRefs(oldStorage)
	if err != nil {
		return false, err
	}
	_, newRefs, newRefsNum, err := transactionLoadAccountStorageRootRefs(newStorage)
	if err != nil {
		return false, err
	}
	if oldStorage == nil || newStorage == nil || oldRefsNum != newRefsNum {
		return false, nil
	}
	for i := 0; i < oldRefsNum; i++ {
		if oldRefs[i].HashKey() != newRefs[i].HashKey() {
			return false, nil
		}
	}
	return true, nil
}

func transactionAccountStorageUsageWithSameRefs(old tlb.StorageUsed, oldStorage, newStorage *cell.Cell) (transactionUsage, error) {
	cells := transactionStorageUsedUint64(old.CellsUsed)
	bits := transactionStorageUsedUint64(old.BitsUsed)
	if oldStorage == nil || newStorage == nil {
		return transactionUsage{cells: cells, bits: bits}, nil
	}

	loadedOldStorage, err := transactionLoadedCell(oldStorage)
	if err != nil {
		return transactionUsage{}, err
	}
	loadedNewStorage, err := transactionLoadedCell(newStorage)
	if err != nil {
		return transactionUsage{}, err
	}

	oldRootBits := uint64(loadedOldStorage.BitsSize())
	newRootBits := uint64(loadedNewStorage.BitsSize())
	switch {
	case newRootBits >= oldRootBits:
		bits += newRootBits - oldRootBits
	case bits >= oldRootBits-newRootBits:
		bits -= oldRootBits - newRootBits
	default:
		bits = 0
	}
	return transactionUsage{cells: cells, bits: bits}, nil
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

func transactionNormalizeFrozenFinalState(acc *transactionRuntimeAccount, status tlb.AccountStatus, code, data *cell.Cell, libs *cell.Dictionary, stateHash []byte, cfg *PreparedBlockchainConfig) (tlb.AccountStatus, tlb.AccountStatus, []byte, error) {
	if status != tlb.AccountStatusFrozen || acc.addr == nil {
		return status, status, stateHash, nil
	}
	addrData := acc.addr.Data()
	if len(addrData) != 32 {
		return status, status, stateHash, nil
	}

	if len(stateHash) == 0 {
		stateInit := &tlb.StateInit{
			Depth:    transactionCloneUint64(acc.stateDepth),
			TickTock: acc.tickTock,
			Code:     code,
			Data:     data,
			Lib:      libs,
		}
		stateCell, err := buildTransactionStateInitCell(stateInit)
		if err != nil {
			return status, status, nil, err
		}
		stateHash = stateCell.Hash()
	}
	if bytes.Equal(stateHash, addrData) {
		if cfg.globalVersion() >= 13 {
			return tlb.AccountStatusUninit, tlb.AccountStatusUninit, nil, nil
		}
		return status, tlb.AccountStatusUninit, nil, nil
	}
	return status, status, stateHash, nil
}

func transactionPrepareComputeAccount(acc *transactionRuntimeAccount, status tlb.AccountStatus, deleted bool, msg *tlb.Message, addressSuspended bool, cfg *PreparedBlockchainConfig) (*transactionRuntimeAccount, bool, *tlb.ComputeSkipReason, error) {
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
			stateHash := stateCell.HashKey()
			if !bytes.Equal(stateHash[:], acc.addr.Data()) {
				return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
			}
		}
		if stateInit != nil && stateInit.Lib != nil && !stateInit.Lib.IsEmpty() {
			next := *acc
			next.inMsgLibraries = stateInit.Lib
			return &next, false, nil, nil
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
		if addressSuspended {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonSuspended}, nil
		}
		if cfg.globalVersion() >= 10 && stateInit.Depth != nil && *stateInit.Depth > transactionGetSizeLimits(cfg).maxAccFixedPrefixLength {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
		stateHash := stateCell.HashKey()
		if !transactionStateInitMatchesAddress(stateHash[:], acc.addr, stateInit.Depth) {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
	case tlb.AccountStatusFrozen:
		stateHash := stateCell.HashKey()
		if !bytes.Equal(stateHash[:], acc.stateHash) {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
		if msg.MsgType == tlb.MsgTypeExternalIn && cfg.globalVersion() < 8 && !bytes.Equal(stateHash[:], acc.addr.Data()) {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
	default:
		return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}, nil
	}
	if cfg.globalVersion() >= 15 && (status == tlb.AccountStatusUninit || status == tlb.AccountStatusNonExist) && stateInit.Lib != nil && !stateInit.Lib.IsEmpty() {
		return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
	}
	if status == tlb.AccountStatusUninit && transactionIsMasterchain(acc.addr) && transactionPublicLibrariesCount(stateInit.Lib) > 0 {
		return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
	}
	exceedsLimits, err := transactionAccountStateExceedsLimits(acc, stateInit.Code, stateInit.Data, stateInit.Lib, cfg)
	if err != nil {
		return nil, false, nil, err
	}
	if exceedsLimits {
		return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
	}

	next := *acc
	next.status = tlb.AccountStatusActive
	next.code = stateInit.Code
	next.data = stateInit.Data
	next.libraries = stateInit.Lib
	next.stateDepth = nil
	if cfg.globalVersion() >= 10 && stateInit.Depth != nil && *stateInit.Depth > 0 {
		next.stateDepth = transactionCloneUint64(stateInit.Depth)
	}
	next.tickTock = stateInit.TickTock
	next.stateHash = nil
	return &next, true, nil, nil
}

func transactionStateInitMatchesAddress(stateHash []byte, addr *address.Address, fixedPrefixLength *uint64) bool {
	if addr == nil || len(stateHash) != 32 {
		return false
	}
	addrData := addr.Data()
	if len(addrData) != 32 {
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
		if transactionBit(stateHash, i) != transactionBit(addrData, i) {
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

func transactionValidateMessageStateInitLibs(msg *tlb.Message) error {
	state := transactionMessageStateInit(msg)
	if state == nil || state.Lib == nil || state.Lib.IsEmpty() {
		return nil
	}

	items, err := state.Lib.LoadAll()
	if err != nil {
		return err
	}
	for _, item := range items {
		if _, err = item.Value.LoadBoolBit(); err != nil {
			return fmt.Errorf("invalid StateInit library entry: %w", err)
		}
		if _, err = item.Value.LoadRefCell(); err != nil {
			return fmt.Errorf("invalid StateInit library entry: %w", err)
		}
		if item.Value.BitsLeft() != 0 || item.Value.RefsNum() != 0 {
			return errors.New("invalid StateInit library entry")
		}
	}
	return nil
}
