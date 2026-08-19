package tvm

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/internal/bigint"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
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
	if err := validateTransactionShardAccount(shard); err != nil {
		return nil, err
	}

	var state tlb.AccountState
	if err := parseTransactionAccountStateExact(&state, shard.Account); err != nil {
		return nil, err
	}
	return prepareAccountFromState(shard, &state, addr, nil, nil)
}

// PrepareParsedAccount wraps an already-parsed shard account state; state must
// be the parsed form of shard.Account.
func PrepareParsedAccount(shard *tlb.ShardAccount, state *tlb.AccountState, addr *address.Address) (*PreparedAccount, error) {
	if err := validateTransactionShardAccount(shard); err != nil {
		return nil, err
	}
	if state == nil {
		return nil, errors.New("parsed account state is required")
	}
	if err := validateTransactionAccountStructure(shard.Account); err != nil {
		return nil, fmt.Errorf("invalid account state structure: %w", err)
	}

	stateCell, err := state.ToCell()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize parsed account state: %w", err)
	}
	if stateCell.HashKey() != shard.Account.HashKey() {
		return nil, errors.New("parsed account state does not match shard account root")
	}

	return prepareAccountFromState(shard, state, addr, nil, nil)
}

func validateTransactionShardAccount(shard *tlb.ShardAccount) error {
	if shard == nil {
		return errors.New("shard account is required")
	}
	if shard.Account == nil {
		return errors.New("shard account root is nil")
	}
	if len(shard.LastTransHash) != 32 {
		return fmt.Errorf("shard account last transaction hash must be 32 bytes, got %d", len(shard.LastTransHash))
	}
	return nil
}

func parseTransactionAccountStateExact(state *tlb.AccountState, root *cell.Cell) error {
	if err := validateTransactionAccountStructure(root); err != nil {
		return fmt.Errorf("invalid account state structure: %w", err)
	}

	loader, err := root.BeginParse()
	if err != nil {
		return fmt.Errorf("failed to decode account state: %w", err)
	}
	if err = tlb.LoadFromCell(state, loader); err != nil {
		return fmt.Errorf("failed to decode account state: %w", err)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return fmt.Errorf("account state has trailing data: %d bits, %d refs", loader.BitsLeft(), loader.RefsNum())
	}
	return nil
}

// transactionValidateOpsBudget is the operation budget the reference threads
// through this very walk: block::tlb::t_ShardAccount.validate_csr(10000, ...)
// (validator/impl/validate-query.cpp:3085). One operation buys entry into one
// cell -- TLB::validate_ref_internal refuses to descend once the budget is
// exhausted (crypto/tl/tlblib.cpp:128-147) -- so the ceiling is a cell count,
// not a field count.
//
// Only two places in the walk descend into a cell. The ^Account reference is
// the first, and the extra-currency dictionary spends one per hashmap node
// (HashmapE::validate -> Hashmap::validate_skip -> HashmapNode::validate_skip,
// crypto/block/block-parse.cpp:495-525). StateInit code/data/library are
// ^Cell, i.e. RefAnything, whose inherited TLB::validate never follows the
// reference (crypto/tl/tlblib.hpp:1026-1034), so they cost nothing on either
// side and bounding the dictionary bounds the whole walk.
const transactionValidateOpsBudget = 10000

// validateTransactionAccountStructure checks shard-account structure and the
// stricter stored-account address rule. StateInit.library remains opaque here;
// only message StateInitWithLibs uses the HashmapE 256 SimpleLib schema.
func validateTransactionAccountStructure(root *cell.Cell) error {
	var loader cell.Slice
	if err := root.BeginParseInto(&loader); err != nil {
		return err
	}

	exists, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if !exists {
		return transactionRequireEmptySlice(&loader)
	}

	if err = validateTransactionAccountAddress(&loader); err != nil {
		return err
	}
	if err = validateTransactionVarUInteger(&loader, 7, 3, false); err != nil {
		return fmt.Errorf("invalid storage cells usage: %w", err)
	}
	if err = validateTransactionVarUInteger(&loader, 7, 3, false); err != nil {
		return fmt.Errorf("invalid storage bits usage: %w", err)
	}

	storageExtra, err := loader.LoadUInt(3)
	if err != nil {
		return err
	}
	switch storageExtra {
	case 0:
	case 1:
		if err = loader.SkipBits(256); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid storage extra tag %d", storageExtra)
	}
	if err = loader.SkipBits(32); err != nil {
		return err
	}

	hasDuePayment, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if hasDuePayment {
		if err = validateTransactionVarUInteger(&loader, 16, 4, false); err != nil {
			return fmt.Errorf("invalid due payment: %w", err)
		}
	}

	if err = loader.SkipBits(64); err != nil {
		return err
	}
	if err = validateTransactionVarUInteger(&loader, 16, 4, false); err != nil {
		return fmt.Errorf("invalid account balance: %w", err)
	}
	if err = validateTransactionExtraCurrencies(&loader); err != nil {
		return err
	}
	if err = validateTransactionAccountState(&loader); err != nil {
		return err
	}
	return transactionRequireEmptySlice(&loader)
}

func validateTransactionAccountAddress(loader *cell.Slice) error {
	tag, err := loader.LoadUInt(2)
	if err != nil {
		return err
	}
	if tag != 2 {
		return fmt.Errorf("existing account address must be addr_std, got tag %d", tag)
	}

	hasAnycast, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if hasAnycast {
		depth, err := loader.LoadUInt(5)
		if err != nil {
			return err
		}
		if depth == 0 || depth > 30 {
			return fmt.Errorf("invalid account anycast depth %d", depth)
		}
		if err = loader.SkipBits(uint(depth)); err != nil {
			return err
		}
	}
	return loader.SkipBits(8 + 256)
}

func validateTransactionVarUInteger(loader *cell.Slice, limit uint64, lengthBits uint, positive bool) error {
	length, err := loader.LoadUInt(lengthBits)
	if err != nil {
		return err
	}
	if length >= limit || (positive && length == 0) {
		return fmt.Errorf("invalid length %d", length)
	}
	if length == 0 {
		return nil
	}

	first, err := loader.PreloadUInt(8)
	if err != nil {
		return err
	}
	if first == 0 {
		return errors.New("leading zero byte")
	}
	return loader.SkipBits(uint(length * 8))
}

func validateTransactionExtraCurrencies(loader *cell.Slice) error {
	hasExtra, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if !hasExtra {
		return nil
	}

	root, err := loader.LoadRefCell()
	if err != nil {
		return err
	}
	// The dictionary walk below is the only unbounded traversal of this
	// validation; charge it against the same budget the reference spends, minus
	// the one operation the ^Account reference already cost, so a dictionary too
	// large for validate_csr(10000) is refused before it is walked instead of
	// after.
	if err = transactionSpendCellBudget(root, transactionValidateOpsBudget-1); err != nil {
		return fmt.Errorf("invalid extra currencies dictionary: %w", err)
	}
	ok, err := root.AsDict(32).ValidateCheck(validateTransactionExtraCurrencyValue, false)
	if err != nil {
		return fmt.Errorf("invalid extra currencies dictionary: %w", err)
	}
	if !ok {
		return errors.New("invalid extra currencies dictionary")
	}
	return nil
}

// transactionSpendCellBudget charges one operation per cell entered in the tree
// rooted at root, the way TLB::validate_ref_internal does: a subtree reachable
// by several references is charged once per reference, and the walk stops as
// soon as the budget would go negative.
//
// Every reference is followed, which for a well-formed dictionary is exactly
// the set of hashmap nodes: leaves hold VarUInteger 32 inline and forks are
// required to hold nothing but their two branches, since validate_ref_internal
// only accepts a cell it consumed completely (cs.empty_ext()). A dictionary the
// reference accepts therefore costs the same here as it does there.
//
// The walk is deliberately trace-free: the account structure is also validated
// through the usage-traced root that builds the collated proof, and counting
// cells must not widen that read set.
func transactionSpendCellBudget(root *cell.Cell, budget int) error {
	if root == nil {
		return nil
	}

	// A dictionary an account really carries is a handful of nodes deep, so the
	// pending set stays on the goroutine stack for everything but an attack.
	var pending [32]*cell.Cell
	stack := append(pending[:0], root.WithoutTrace())
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if budget <= 0 {
			return fmt.Errorf("exceeds the %d cell validation budget", transactionValidateOpsBudget)
		}
		budget--

		for i := 0; i < int(current.RefsNum()); i++ {
			ref, err := current.PeekRef(i)
			if err != nil {
				return err
			}
			stack = append(stack, ref)
		}
	}
	return nil
}

func validateTransactionExtraCurrencyValue(value *cell.Slice, _ *cell.Cell) (bool, error) {
	if err := validateTransactionVarUInteger(value, 32, 5, true); err != nil {
		return false, err
	}
	if err := transactionRequireEmptySlice(value); err != nil {
		return false, err
	}
	return true, nil
}

func validateTransactionAccountState(loader *cell.Slice) error {
	active, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if active {
		return validateTransactionStateInit(loader)
	}

	frozen, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if frozen {
		return loader.SkipBits(256)
	}
	return nil
}

func validateTransactionStateInit(loader *cell.Slice) error {
	for _, bits := range [...]uint{5, 2} {
		hasValue, err := loader.LoadBoolBit()
		if err != nil {
			return err
		}
		if hasValue {
			if err = loader.SkipBits(bits); err != nil {
				return err
			}
		}
	}
	for range 3 {
		hasRef, err := loader.LoadBoolBit()
		if err != nil {
			return err
		}
		if hasRef {
			if _, err = loader.LoadRefCell(); err != nil {
				return err
			}
		}
	}
	return nil
}

func prepareAccountFromState(shard *tlb.ShardAccount, state *tlb.AccountState, addr *address.Address, storageCell, storageCellForStat *cell.Cell) (*PreparedAccount, error) {
	runtime, err := loadTransactionRuntimeAccountState(shard, state, addr, storageCell == nil)
	if err != nil {
		return nil, err
	}
	if storageCell != nil {
		runtime.storageCell = storageCell
		runtime.storageCellForStat = storageCellForStat
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

// ComputeAccountStorageStat builds and binds the content-addressed storage-stat
// dictionary for account. The binding records that this executor computed the
// dictionary from this exact state, including states without storage_dict_hash.
func (b *BlockContext) ComputeAccountStorageStat(account *PreparedAccount) (*cell.Cell, error) {
	storage, err := transactionOldAccountStorageForConfig(&account.runtime, b.cfg)
	if err != nil {
		return nil, err
	}
	_, root, err := transactionComputeAccountStorageStat(storage,
		transactionStorageUsedUint64(account.runtime.storageInfo.StorageUsed.CellsUsed))
	if err != nil {
		return nil, err
	}
	if root != nil && storage != nil {
		account.runtime.accountStorageStat = root
		account.runtime.statBoundTo = storage.HashKey()
	}
	return root, nil
}

func transactionBindAccountStorageStat(acc *transactionRuntimeAccount, root *cell.Cell, cfg *PreparedBlockchainConfig) error {
	storage, err := transactionOldAccountStorageForConfig(acc, cfg)
	if err != nil {
		return err
	}
	stat, err := transactionInitAccountStorageStat(
		root,
		storage,
		acc.storageInfo.StorageUsed,
		transactionStorageExtraDictHash(acc.storageInfo.StorageExtra),
		false,
	)
	if err != nil {
		return err
	}
	if stat == nil {
		return errors.New("account state does not authenticate the storage stat")
	}

	acc.accountStorageStat = root
	acc.statBoundTo = storage.HashKey()
	return nil
}

func transactionUseAccountStorageStat(acc *transactionRuntimeAccount, root *cell.Cell, cfg *PreparedBlockchainConfig) error {
	if acc.accountStorageStat != nil && acc.accountStorageStat.HashKey() == root.HashKey() {
		// Keep the established state binding, but use the caller's traced view
		// so storage-stat dictionary reads enter its Merkle proof.
		acc.accountStorageStat = root
		return nil
	}
	return transactionBindAccountStorageStat(acc, root, cfg)
}

func transactionOldAccountStorageForConfig(acc *transactionRuntimeAccount, cfg *PreparedBlockchainConfig) (*cell.Cell, error) {
	return transactionOldAccountStorageForStat(acc, cfg.globalVersion() >= 10)
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
	if err := parseTransactionAccountStateExact(&state, proof.Root()); err != nil {
		return nil, nil, err
	}
	runtime, err := loadTransactionRuntimeAccountState(a.shard, &state, a.runtime.addr, true)
	if err != nil {
		return nil, nil, err
	}
	runtime.accountStorageStat = a.runtime.accountStorageStat
	runtime.statBoundTo = a.runtime.statBoundTo
	return runtime, proof, nil
}

func loadTransactionRuntimeAccountState(shard *tlb.ShardAccount, acc *tlb.AccountState, fallbackAddr *address.Address, buildStorageCell bool) (*transactionRuntimeAccount, error) {
	out := &transactionRuntimeAccount{
		status:          tlb.AccountStatusNonExist,
		storageInfo:     tlb.StorageInfo{StorageExtra: tlb.StorageExtraNone{}},
		prevTxHash:      append([]byte(nil), shard.LastTransHash...),
		prevTxLT:        shard.LastTransLT,
		originalCell:    shard.Account,
		extraCurrencies: nil,
	}
	if err := out.setAddressIdentity(fallbackAddr); err != nil {
		return nil, err
	}
	if !acc.IsValid {
		// A non-existing account holds nothing; an existing one overwrites this
		// below with its own balance, so only one of the two is ever allocated.
		out.balance = bigint.FromInt64(0)
		if out.addr == nil {
			return nil, errors.New("account address is required for non-existing shard account")
		}
		out.forgetAddressRewrite()
		out.stateHash = append([]byte(nil), out.addr.Data()...)
		return out, nil
	}

	fallbackIdentity := out.addr
	out.nonCanonicalMyAddr = acc.Address != nil && acc.Address.Type() == address.StdAddress &&
		acc.Address.Workchain() == 127 && acc.Address.Anycast() != nil
	if err := out.setAddressIdentity(acc.Address); err != nil {
		return nil, err
	}
	if fallbackIdentity != nil {
		if fallbackIdentity.Type() != address.StdAddress || out.addr.Type() != address.StdAddress ||
			fallbackIdentity.Workchain() != out.addr.Workchain() || fallbackIdentity.BitsLen() != 256 || out.addr.BitsLen() != 256 ||
			!bytes.Equal(fallbackIdentity.Data(), out.addr.Data()) {
			return nil, errors.New("existing account address does not match requested account")
		}
	}
	out.status = acc.Status
	out.storageInfo = acc.StorageInfo
	if out.storageInfo.StorageExtra == nil {
		out.storageInfo.StorageExtra = tlb.StorageExtraNone{}
	}
	// Nano already hands back a private copy; Set on top of it copied twice.
	out.balance = acc.Balance.Nano()
	out.extraCurrencies = acc.ExtraCurrencies
	out.storageLT = acc.LastTransactionLT
	// Existing accounts require max(storage.last_trans_lt, 1) to be later than
	// the logical time stored by the shard account.
	if lt := max(out.storageLT, 1); lt <= out.prevTxLT {
		return nil, fmt.Errorf("account storage last transaction lt %d is not after shard account lt %d", out.storageLT, out.prevTxLT)
	}
	out.stateHash = append([]byte(nil), acc.StateHash...)
	if out.status == tlb.AccountStatusUninit {
		out.forgetAddressRewrite()
		out.stateHash = append([]byte(nil), out.addr.Data()...)
	}
	if buildStorageCell {
		storageCell, err := buildTransactionAccountStorageCell(acc.Status, acc.LastTransactionLT, acc.Balance.NanoRef(), acc.ExtraCurrencies, acc.StateInit, acc.StateHash)
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

func (a *transactionRuntimeAccount) setAddressIdentity(raw *address.Address) error {
	a.addrRaw = raw
	a.addrExact = raw
	a.addr = raw
	a.addrRewriteDepth = 0
	a.addrIdentityDerived = true
	if raw == nil {
		return nil
	}

	anycast := raw.Anycast()
	if anycast == nil {
		return nil
	}
	a.addrRewriteDepth = uint64(anycast.Depth())

	exact, err := transactionAccountIDAddr(raw)
	if err != nil {
		return err
	}
	a.addr = exact
	a.addrExact = exact
	return nil
}

// forgetAddressRewrite matches Account::forget_addr_rewrite_length: the
// effective shard key becomes the stored address and anycast metadata is lost.
func (a *transactionRuntimeAccount) forgetAddressRewrite() {
	a.addrRaw = a.addrExact
	a.addrRewriteDepth = 0
}

// applyPreV9OriginalBalance computes the RAWRESERVE mode&4 base used below
// global version 9: the balance the account held before the transaction minus
// the fees collected so far (the inbound forward fee and the storage fees).
// Going negative invalidates the value, which later fails the reserve action.
func (p *transactionPreparedPhases) applyPreV9OriginalBalance(acc *transactionRuntimeAccount, inFwdFee *big.Int) error {
	before, err := transactionCurrencyFromParts(acc.balance, acc.extraCurrencies)
	if err != nil {
		return err
	}

	if inFwdFee != nil {
		before.grams.Sub(before.grams, inFwdFee)
	}
	if p.storagePhase != nil {
		before.grams.Sub(before.grams, p.storagePhase.StorageFeesCollected.NanoRef())
	}
	p.originalBalance = before
	p.originalBalanceValid = before.grams.Sign() >= 0
	return nil
}

func transactionPrepareInitialPhases(acc *transactionRuntimeAccount, msg *tlb.Message, storageFee, importFee *big.Int, now uint32, cfg *PreparedBlockchainConfig, limits transactionStorageDueLimits) (*transactionPreparedPhases, error) {
	globalVersion := cfg.globalVersion()
	extraCurrencies, err := transactionCloneExtraCurrencies(acc.extraCurrencies)
	if err != nil {
		return nil, err
	}
	prepared := &transactionPreparedPhases{
		balance:         bigint.Set(acc.balance),
		extraCurrencies: extraCurrencies,
		creditFirst:     true,
		status:          transactionInitialComputeStatus(acc.status),
		duePayment:      transactionCoinsClonePtr(acc.storageInfo.DuePayment),
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

	// msgBalance is whatever the branch below decides: an internal message
	// carries one, anything else leaves the account with an empty one. Building
	// a zero balance up front only to replace it is an allocation per internal
	// message for nothing.
	switch msg.MsgType {
	case tlb.MsgTypeInternal:
		in := msg.AsInternal()
		prepared.creditFirst = !in.Bounce
		prepared.msgBalance, err = transactionCurrencyFromOwnedParts(in.Amount.Nano(), in.ExtraCurrencies)
		if err != nil {
			return nil, err
		}
		if globalVersion < 12 {
			prepared.msgBalance.grams.Add(prepared.msgBalance.grams, in.IHRFee.NanoRef())
		}
		if cfg.isBlackHoleAccount(acc.addr) {
			prepared.blackholeBurned = bigint.Set(prepared.msgBalance.grams)
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
		prepared.msgBalance = transactionZeroCurrencyBalance()
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

	if globalVersion < 9 {
		if err = prepared.applyPreV9OriginalBalance(acc, importFee); err != nil {
			return nil, err
		}
	}
	return prepared, nil
}

func (p *transactionPreparedPhases) applyStoragePhase(acc *transactionRuntimeAccount, storageFee *big.Int, now uint32, globalVersion uint32, limits transactionStorageDueLimits, adjustMsgValue bool) {
	collected := bigint.FromInt64(0)
	due := bigint.FromInt64(0)
	statusChange := tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged}

	p.duePayment = transactionCoinsClonePtr(acc.storageInfo.DuePayment)
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

			if !acc.isSpecial {
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
	}

	if adjustMsgValue && p.msgBalance.grams.Cmp(p.balance) > 0 {
		p.msgBalance.grams.Set(p.balance)
	}

	p.storagePhase = &tlb.StoragePhase{
		// collected was built by this function and is dead after this line.
		StorageFeesCollected: tlb.FromOwnedNanoTON(collected),
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
	// storageStatBound says storageStat was computed by this executor for
	// exactly storageCellForStat, so the next transaction may reuse it even
	// when the account carries no storage_dict_hash.
	storageStatBound bool
	// storageStatRecomputed says the bound dict could not serve this update
	// (its proof was pruned short of this walk) and the stat came from the
	// direct state walk instead.
	storageStatRecomputed bool
	storageCell           *cell.Cell
	storageCellForStat    *cell.Cell
}

func buildTransactionAccountCell(acc *transactionRuntimeAccount, status tlb.AccountStatus, balance *big.Int, extraCurrencies *cell.Dictionary, endLT uint64, lastPaid uint32, duePayment *tlb.Coins, code, data *cell.Cell, libs *cell.Dictionary, stateHash []byte, removeAnycast bool, cfg *PreparedBlockchainConfig, accountStorageStat *cell.Cell, loaded *vm.LoadedCells) (*builtTransactionAccount, error) {
	if status == tlb.AccountStatusNonExist {
		accountState := &tlb.AccountState{
			IsValid: false,
			AccountStorage: tlb.AccountStorage{
				Status:  tlb.AccountStatusNonExist,
				Balance: tlb.FromNanoTON(bigint.FromInt64(0)),
			},
		}
		return &builtTransactionAccount{
			cell:  cell.BeginCell().MustStoreBoolBit(false).EndCell(),
			state: accountState,
		}, nil
	}
	if acc.nonCanonicalMyAddr {
		return nil, errors.New("final account address is not canonical")
	}

	stateDepth := transactionFinalStateDepth(acc, cfg)

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

	accountAddr := acc.rawAddress()
	if removeAnycast {
		accountAddr = acc.exactAddress()
	}
	storageBuilder, err := buildTransactionAccountStorageBuilder(status, endLT, balance, extraCurrencies, accountStorage.StateInit, accountStorage.StateHash)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize account storage: %w", err)
	}
	storageCell := storageBuilder.EndCell()

	storageCellForStat := storageCell
	extraCurrencyV2 := cfg.globalVersion() >= 10
	if extraCurrencyV2 && extraCurrencies != nil && extraCurrencies.AsCell() != nil {
		// The stat form differs from storageCell only when an extra-currency
		// dict is actually stored: with no dict both serializations store the
		// same maybe-bit 0, so storageCell is reused as is.
		storageCellForStat, err = buildTransactionAccountStorageCell(status, endLT, balance, nil, accountStorage.StateInit, accountStorage.StateHash)
		if err != nil {
			return nil, err
		}
	}

	usage, storageExtra, storageExtraDictHash, nextStorageStat, nextStorageStatBound, err := transactionAccountStorageInfo(acc, storageCellForStat, cfg, accountStorageStat, loaded)
	if err != nil {
		return nil, err
	}

	storageInfo := tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: bigint.FromUint64(usage.cells),
			BitsUsed:  bigint.FromUint64(usage.bits),
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

	built := &builtTransactionAccount{
		cell:                  accountCell,
		state:                 accountState,
		storageStat:           nextStorageStat,
		storageStatBound:      nextStorageStatBound,
		storageStatRecomputed: acc.storageStatRecomputed,
		storageCell:           storageCell,
	}
	if extraCurrencyV2 {
		// Thread the extra-currency-free storage forward so the next
		// transaction of this account gets it without another probe/rebuild.
		built.storageCellForStat = storageCellForStat
	}
	return built, nil
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

// transactionAccountStorageInfo returns, besides the storage info, whether the
// storage-stat dict it hands back is bound to the state it describes, i.e.
// whether the next transaction of this account may reuse it without a
// storage_dict_hash.
func transactionAccountStorageInfo(acc *transactionRuntimeAccount, storageCellForStat *cell.Cell, cfg *PreparedBlockchainConfig, accountStorageStat *cell.Cell, loaded *vm.LoadedCells) (transactionUsage, any, []byte, *cell.Cell, bool, error) {
	// The flag describes this transaction only; the runtime copy it sits on
	// can be a reused snapshot of the account.
	acc.storageStatRecomputed = false
	version := cfg.globalVersion()
	storeStorageDictHash := version >= 11 && !transactionIsMasterchain(acc.addr)

	oldStorageForStat, err := transactionOldAccountStorageForStat(acc, version >= 10)
	if err != nil {
		return transactionUsage{}, nil, nil, nil, false, err
	}

	// The carried binding names the state the dict was computed for; it is
	// meaningless once the account state has moved on some other way.
	//
	// An authenticated dict is then carried on regardless of how small the
	// state has become, which is what the reference does: it keeps
	// account_storage_stat across transactions with no size test at all and
	// gates only the dict hash on the threshold (crypto/block/transaction.cpp,
	// Transaction::compute_state, storage section). Gating the carry on size
	// instead is a divergence with teeth: an account that dips under the
	// threshold mid-block and then changes storage again drops to a full
	// recompute and reads no dictionary cell, so the proof this collation
	// ships covers none of them, while a reference validator keeps walking the
	// dict and rejects the candidate over the pruned branches it finds. The
	// size test would only ever fire on that dipped state — a dict exists at
	// all only above the threshold — so all it bought was a cheaper walk of a
	// state small enough for the difference to be immaterial.
	oldDictHash := transactionStorageExtraDictHash(acc.storageInfo.StorageExtra)
	var zeroHash cell.Hash
	bindingMatches := accountStorageStat != nil && acc.statBoundTo != zeroHash &&
		oldStorageForStat != nil && acc.statBoundTo == oldStorageForStat.HashKey()
	hashMatches := false
	if accountStorageStat != nil && oldDictHash != nil && !transactionHashIsZero(oldDictHash) {
		rootHash := accountStorageStat.HashKey()
		hashMatches = bytes.Equal(rootHash[:], oldDictHash)
	}
	statAuthenticated := bindingMatches || hashMatches
	useIncrementalStat := statAuthenticated

	storageRefsUnchanged, err := transactionAccountStorageRefsUnchanged(oldStorageForStat, storageCellForStat)
	if err != nil {
		return transactionUsage{}, nil, nil, nil, false, err
	}
	storageRefsChanged := !storageRefsUnchanged
	needMissingDict := storeStorageDictHash && oldDictHash == nil && transactionStorageUsedUint64(acc.storageInfo.StorageUsed.CellsUsed) > 25
	if storageRefsChanged || needMissingDict {
		stat, err := transactionInitAccountStorageStat(accountStorageStat, oldStorageForStat, acc.storageInfo.StorageUsed, oldDictHash, useIncrementalStat)
		if err != nil {
			return transactionUsage{}, nil, nil, nil, false, err
		}

		var usage transactionUsage
		var nextStorageStat *cell.Cell
		if stat != nil {
			if loaded != nil {
				err = stat.addHint(*loaded)
			}
			if err == nil {
				usage, nextStorageStat, err = stat.replaceStorage(storageCellForStat)
			}
			if err != nil && errors.Is(err, cell.ErrDictHasSpecialCells) {
				// A dict bound from another producer's collated proof is pruned
				// along that producer's own update walk; replaying the update
				// here in a different order can need a node — most often a
				// delete-merge sibling — the producer never loaded. The new
				// state's dictionary does not depend on the update order, so
				// recompute it from the state directly, the same way an absent
				// proof is handled, and leave the verdict to the commitment
				// comparison downstream.
				acc.storageStatRecomputed = true
				usage, nextStorageStat, err = transactionComputeAccountStorageStat(storageCellForStat,
					transactionStorageUsedUint64(acc.storageInfo.StorageUsed.CellsUsed))
			}
		} else {
			// Measure first and build the dictionary only if this state turns
			// out to be one that keeps it. The threshold decides both whether
			// the account records a dict hash and whether the next transaction
			// may go incremental, so below it the dictionary has no reader;
			// above it the measurement is repeated once, which is the rare
			// case and still leaves the common one a single cheap walk.
			dictThreshold := transactionGetSizeLimits(cfg).accStateCellsForStorageDict
			var needsDict bool
			usage, needsDict, err = transactionCountAccountStorageUsage(storageCellForStat, dictThreshold)
			if err == nil && needsDict {
				usage, nextStorageStat, err = transactionComputeAccountStorageStat(storageCellForStat,
					transactionStorageUsedUint64(acc.storageInfo.StorageUsed.CellsUsed))
			}
		}
		if err != nil {
			return transactionUsage{}, nil, nil, nil, false, err
		}

		storageExtra := any(tlb.StorageExtraNone{})
		var storageExtraDictHash []byte
		if storeStorageDictHash && usage.cells >= transactionGetSizeLimits(cfg).accStateCellsForStorageDict {
			storageExtraDictHash = transactionAccountStorageStatRootHash(nextStorageStat)
			storageExtra = tlb.StorageExtraInfo{DictHash: storageExtraDictHash}
		}
		// Either path above produced the dict from this state, so the result is
		// bound regardless of how the input got here.
		return usage, storageExtra, storageExtraDictHash, nextStorageStat, nextStorageStat != nil, nil
	}

	usage, err := transactionAccountStorageUsageWithSameRefs(acc.storageInfo.StorageUsed, oldStorageForStat, storageCellForStat)
	if err != nil {
		return transactionUsage{}, nil, nil, nil, false, err
	}
	storageExtra := any(tlb.StorageExtraNone{})
	var storageExtraDictHash []byte
	if storeStorageDictHash && oldDictHash != nil {
		storageExtraDictHash = append([]byte(nil), oldDictHash...)
		storageExtra = tlb.StorageExtraInfo{DictHash: storageExtraDictHash}
	}
	// The refs are identical, so the dict still describes the new state --
	// but only re-point the binding when the dict was trusted coming in.
	return usage, storageExtra, storageExtraDictHash, accountStorageStat, statAuthenticated, nil
}

func transactionOldAccountStorageForStat(acc *transactionRuntimeAccount, extraCurrencyV2 bool) (*cell.Cell, error) {
	if acc == nil || acc.storageCell == nil {
		return nil, nil
	}
	if !extraCurrencyV2 {
		return acc.storageCell, nil
	}
	if acc.storageCellForStat != nil {
		return acc.storageCellForStat, nil
	}

	// acc.storageCell is always this emulator's own canonical serialization
	// (loadTransactionRuntimeAccountState or the previous transaction), so
	// probe the account_storage layout with raw bit reads: last_trans_lt
	// (64 bits), Grams (4-bit byte length + bytes), then the extra-currency
	// dict maybe-bit. Bit 0 means the cell already equals its serialization
	// without extra currencies, no rebuild needed.
	var sl cell.Slice
	if err := acc.storageCell.BeginParseIntoWithoutTrace(&sl); err != nil {
		return nil, fmt.Errorf("failed to probe old account storage for stats: %w", err)
	}
	if err := sl.SkipBits(64); err != nil {
		return nil, fmt.Errorf("failed to probe old account storage for stats: %w", err)
	}
	gramsLen, err := sl.LoadUInt(4)
	if err != nil {
		return nil, fmt.Errorf("failed to probe old account storage for stats: %w", err)
	}
	if err = sl.SkipBits(uint(gramsLen) * 8); err != nil {
		return nil, fmt.Errorf("failed to probe old account storage for stats: %w", err)
	}
	hasExtraCurrencies, err := sl.LoadBoolBit()
	if err != nil {
		return nil, fmt.Errorf("failed to probe old account storage for stats: %w", err)
	}
	if !hasExtraCurrencies {
		return acc.storageCell, nil
	}

	var storage tlb.AccountStorage
	if err := tlb.Parse(&storage, acc.storageCell); err != nil {
		return nil, fmt.Errorf("failed to decode old account storage for stats: %w", err)
	}
	return transactionAccountStorageWithoutExtraCurrencies(storage)
}

func transactionAccountStorageWithoutExtraCurrencies(storage tlb.AccountStorage) (*cell.Cell, error) {
	storageCell, err := buildTransactionAccountStorageCell(storage.Status, storage.LastTransactionLT, storage.Balance.NanoRef(), nil, storage.StateInit, storage.StateHash)
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
	existsKnown         bool
	exists              bool
	refCountKnown       bool
	refCount            uint32
	refCountDiff        int64
	maxMerkleDepthKnown bool
	maxMerkleDepth      uint32
	hintGeneration      uint32
}

type transactionAccountStorageStat struct {
	dict           *cell.Dictionary
	roots          [4]*cell.Cell
	rootsNum       int
	entries        map[cell.Hash]*transactionAccountStorageStatEntry
	totalCells     uint64
	totalBits      uint64
	hintGeneration uint32
}

// transactionInitAccountStorageStat adopts a carried-in storage-stat dict.
//
// The dict is only safe to reuse when it provably describes storageCell: a
// wrong one makes replaceStorage skip or double-count subtrees and silently
// produces the wrong storage_used, i.e. the wrong storage fee. Two independent
// proofs are accepted:
//
//   - the account records storage_dict_hash (global version >= 11, basechain)
//     and it matches the dict root, or
//   - this executor computed the dict itself for exactly this state, which
//     bound reports. That mirrors block::Account::account_storage_stat, which
//     the reference carries across transactions without revalidating because it
//     owns the object (crypto/block/transaction.cpp, Transaction::compute_state).
//
// Anything else -- a hand-made dict, or one persisted next to a different state
// -- falls back to a full recompute.
func transactionInitAccountStorageStat(dictRoot, storageCell *cell.Cell, storageUsed tlb.StorageUsed, dictHash []byte, bound bool) (*transactionAccountStorageStat, error) {
	if dictRoot == nil {
		return nil, nil
	}
	if dictHash == nil || transactionHashIsZero(dictHash) {
		if !bound {
			return nil, nil
		}
	} else {
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
		entries:    make(map[cell.Hash]*transactionAccountStorageStatEntry, storageStatEntryCapacity(totalCells)),
		totalCells: totalCells,
		totalBits:  totalBits,
	}, nil
}

// storageStatEntryHintCap bounds what a cell-count hint is allowed to
// preallocate. The hint is the account's own declared size, which this walk is
// in the middle of recomputing and has therefore not verified, so an
// implausible value must cost a little memory and nothing else. The ceiling is
// twice the default max_acc_state_cells, which covers every account a standard
// config admits; a larger one merely rehashes a few times, as it does today.
const storageStatEntryHintCap = 1 << 17

// storageStatEntryCapacity turns a cell-count hint into an entry map size.
//
// The walk keeps one entry per distinct cell of the state, so a map that
// starts empty grows through a dozen doublings on a large account, rehashing
// 32-byte keys every time. Sizing it upfront is most of what the walk costs:
// measured on a 4000-cell account it runs 38% faster and allocates 43% fewer
// bytes, and on 32000 cells 33% faster.
//
// The hint must be the real size rather than a generous guess. An oversized
// one allocates buckets that are never filled, wins nothing, and on a large
// account measured slower than no hint at all.
func storageStatEntryCapacity(cells uint64) int {
	if cells > storageStatEntryHintCap {
		cells = storageStatEntryHintCap
	}
	// the storage root itself and its immediate refs are outside the count
	return int(cells) + 8
}

func transactionComputeAccountStorageStat(storageCell *cell.Cell, cellsHint uint64) (transactionUsage, *cell.Cell, error) {
	stat := transactionAccountStorageStat{
		dict:    cell.NewDict(256),
		entries: make(map[cell.Hash]*transactionAccountStorageStatEntry, storageStatEntryCapacity(cellsHint)),
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

// transactionCountAccountStorageUsage measures the account state the way
// transactionComputeAccountStorageStat does, without assembling the
// storage-stat dictionary that measurement normally carries along.
//
// Only a state at or above accStateCellsForStorageDict keeps such a dictionary:
// below that the account records StorageExtraNone, and the following
// transaction refuses the incremental path on the very same threshold, so the
// dictionary built for a small account is written nowhere and read by nobody.
// Real accounts sit far below the threshold — a wallet is a handful of cells —
// which makes that the common case rather than the exceptional one.
//
// The traversal is deliberately identical, including loading each cell through
// BeginParseInto rather than its untraced sibling: these loads are what a
// candidate's proof is built from, so measuring the state must touch exactly
// the cells the full computation touches or the proof changes.
// The walk stops as soon as the state is known to reach the threshold, because
// from there the dictionary has to be built anyway and the exact count comes
// from that build. Bounding it that way bounds the set of visited hashes too,
// so it fits an array the walk carries on its stack and the measurement costs
// no allocation at all. Linear search over a few dozen 32-byte keys is cheaper
// here than hashing them into a map.
const transactionStorageCountInlineCap = 48

func transactionCountAccountStorageUsage(storageCell *cell.Cell, limit uint64) (transactionUsage, bool, error) {
	if limit > transactionStorageCountInlineCap {
		return transactionUsage{}, true, nil
	}

	counter := transactionStorageUsageCounter{limit: limit}
	if storageCell != nil {
		loadedStorage, roots, rootsNum, err := transactionLoadAccountStorageRootRefs(storageCell)
		if err != nil {
			return transactionUsage{}, false, err
		}
		for i := 0; i < rootsNum; i++ {
			if err = counter.add(roots[i]); err != nil {
				return transactionUsage{}, false, err
			}
			if counter.exceeded {
				return transactionUsage{}, true, nil
			}
		}
		storageCell = loadedStorage
	}

	usage := transactionUsage{cells: counter.cells, bits: counter.bits}
	if storageCell != nil {
		usage.cells++
		usage.bits += uint64(storageCell.BitsSize())
	}
	if usage.cells >= limit {
		return transactionUsage{}, true, nil
	}
	return usage, false, nil
}

type transactionStorageUsageCounter struct {
	seen     [transactionStorageCountInlineCap]cell.Hash
	seenNum  int
	limit    uint64
	cells    uint64
	bits     uint64
	exceeded bool
}

func (c *transactionStorageUsageCounter) add(cl *cell.Cell) error {
	if cl == nil || c.exceeded {
		return nil
	}
	key := cl.HashKey()
	for i := 0; i < c.seenNum; i++ {
		if c.seen[i] == key {
			return nil
		}
	}
	if c.seenNum == len(c.seen) {
		c.exceeded = true
		return nil
	}
	c.seen[c.seenNum] = key
	c.seenNum++

	var sl cell.Slice
	if err := cl.BeginParseInto(&sl); err != nil {
		return err
	}
	loaded := sl.BaseCell()
	refsNum := sl.RefsNum()
	for i := 0; i < refsNum; i++ {
		ref, err := sl.LoadRefCell()
		if err != nil {
			return err
		}
		if err = c.add(ref); err != nil {
			return err
		}
		if c.exceeded {
			return nil
		}
	}
	c.cells++
	c.bits += uint64(loaded.BitsSize())
	if c.cells >= c.limit {
		c.exceeded = true
	}
	return nil
}

func (s *transactionAccountStorageStat) replaceStorage(storageCell *cell.Cell) (transactionUsage, *cell.Cell, error) {
	loadedStorage, newRoots, newRootsNum, err := transactionLoadAccountStorageRootRefs(storageCell)
	if err != nil {
		return transactionUsage{}, nil, err
	}
	storageCell = loadedStorage

	if _, err = s.replaceRoots(newRoots, newRootsNum); err != nil {
		return transactionUsage{}, nil, err
	}

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

// replaceRoots applies the same hash-sorted root diff as the reference
// AccountStorageStat and returns the greatest Merkle depth among added roots.
// Unchanged roots are deliberately never traversed.
func (s *transactionAccountStorageStat) replaceRoots(newRoots [4]*cell.Cell, newRootsNum int) (uint32, error) {
	toAdd, toAddNum, toDel, toDelNum := transactionAccountStorageRootDiff(s.roots, s.rootsNum, newRoots, newRootsNum)
	var maxMerkleDepth uint32
	for i := 0; i < toAddNum; i++ {
		depth, err := s.addCell(toAdd[i])
		if err != nil {
			return 0, err
		}
		if depth > maxMerkleDepth {
			maxMerkleDepth = depth
		}
	}
	for i := 0; i < toDelNum; i++ {
		if err := s.removeCell(toDel[i]); err != nil {
			return 0, err
		}
	}
	s.roots = newRoots
	s.rootsNum = newRootsNum
	return maxMerkleDepth, nil
}

// addHint mirrors AccountStorageStat::add_hint. A VM-loaded zero-depth
// predecessor subtree is known to exist, so root replacement may reuse its
// descendants without looking up their reference counts in the stat dict.
// This read set is consensus-relevant for sparse collated proofs.
func (s *transactionAccountStorageStat) addHint(loaded vm.LoadedCells) error {
	s.hintGeneration++
	if s.hintGeneration == 0 {
		for _, entry := range s.entries {
			entry.hintGeneration = 0
		}
		s.hintGeneration = 1
	}
	generation := s.hintGeneration
	var visit func(*cell.Cell, bool) error
	visit = func(c *cell.Cell, root bool) error {
		key := c.HashKey()
		entry := s.entry(key)
		if entry.hintGeneration == generation {
			return nil
		}
		entry.hintGeneration = generation

		entry.existsKnown = true
		entry.exists = true
		if root {
			if err := s.fetchEntry(key, entry); err != nil {
				return err
			}
			if entry.maxMerkleDepthKnown && entry.maxMerkleDepth != 0 {
				return nil
			}
		}
		entry.maxMerkleDepthKnown = true
		entry.maxMerkleDepth = 0
		if !loaded.Contains(key) {
			return nil
		}

		var sl cell.Slice
		if err := c.BeginParseInto(&sl); err != nil {
			return err
		}
		for sl.RefsNum() > 0 {
			child, err := sl.LoadRefCell()
			if err != nil {
				return err
			}
			if err = visit(child, false); err != nil {
				return err
			}
		}
		return nil
	}

	for i := 0; i < s.rootsNum; i++ {
		if err := visit(s.roots[i], true); err != nil {
			return err
		}
	}
	return nil
}

func (s *transactionAccountStorageStat) addCell(c *cell.Cell) (uint32, error) {
	if c == nil {
		return 0, nil
	}

	key := c.HashKey()
	entry := s.entry(key)
	if !entry.existsKnown {
		if err := s.fetchEntry(key, entry); err != nil {
			return 0, err
		}
	}
	if entry.refCountDiff == math.MaxInt64 {
		return 0, errors.New("account storage cell refcount overflow")
	}
	entry.refCountDiff++
	if entry.exists || entry.refCountDiff > 1 {
		if !entry.maxMerkleDepthKnown {
			if err := s.fetchEntry(key, entry); err != nil {
				return 0, err
			}
		}
		return entry.maxMerkleDepth, nil
	}

	var sl cell.Slice
	if err := c.BeginParseInto(&sl); err != nil {
		return 0, err
	}
	loaded := sl.BaseCell()
	var refs [4]*cell.Cell
	refsNum := sl.RefsNum()
	for i := 0; i < refsNum; i++ {
		ref, err := sl.LoadRefCell()
		if err != nil {
			return 0, err
		}
		refs[i] = ref
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
	entry.maxMerkleDepthKnown = true
	s.totalCells++
	s.totalBits += uint64(loaded.BitsSize())
	return maxDepth, nil
}

func (s *transactionAccountStorageStat) removeCell(c *cell.Cell) error {
	key := c.HashKey()
	entry := s.entry(key)
	if !entry.existsKnown {
		if err := s.fetchEntry(key, entry); err != nil {
			return err
		}
	}
	if !entry.exists {
		return fmt.Errorf("account storage stat cannot remove absent cell %x", key)
	}
	entry.refCountDiff--
	if entry.refCountDiff < 0 && !entry.refCountKnown {
		if err := s.fetchEntry(key, entry); err != nil {
			return err
		}
	}
	if entry.refCountDiff >= 0 || int64(entry.refCount)+entry.refCountDiff != 0 {
		return nil
	}

	var sl cell.Slice
	if err := c.BeginParseInto(&sl); err != nil {
		return err
	}
	loaded := sl.BaseCell()
	var refs [4]*cell.Cell
	refsNum := sl.RefsNum()
	for i := 0; i < refsNum; i++ {
		ref, err := sl.LoadRefCell()
		if err != nil {
			return err
		}
		refs[i] = ref
	}

	for i := 0; i < refsNum; i++ {
		if err := s.removeCell(refs[i]); err != nil {
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

func (s *transactionAccountStorageStat) entry(key cell.Hash) *transactionAccountStorageStatEntry {
	entry := s.entries[key]
	if entry != nil {
		return entry
	}

	entry = &transactionAccountStorageStatEntry{}
	s.entries[key] = entry
	return entry
}

func (s *transactionAccountStorageStat) fetchEntry(key cell.Hash, entry *transactionAccountStorageStatEntry) error {
	if entry.existsKnown && entry.refCountKnown && (!entry.exists || entry.maxMerkleDepthKnown) {
		return nil
	}
	if s.dict == nil || s.dict.IsEmpty() {
		entry.existsKnown = true
		entry.exists = false
		entry.refCountKnown = true
		entry.refCount = 0
		return nil
	}

	value, err := s.dict.LoadValueByBytesKey(key[:])
	if errors.Is(err, cell.ErrNoSuchKeyInDict) {
		entry.existsKnown = true
		entry.exists = false
		entry.refCountKnown = true
		entry.refCount = 0
		return nil
	}
	if err != nil {
		return fmt.Errorf("load account storage stat record %x: %w", key, err)
	}
	if value.BitsLeft() != 34 || value.RefsNum() != 0 {
		return fmt.Errorf("invalid account storage stat record for cell %x", key)
	}
	entry.refCount = uint32(value.MustLoadUInt(32))
	entry.maxMerkleDepth = uint32(value.MustLoadUInt(2))
	if entry.refCount == 0 {
		return fmt.Errorf("invalid zero account storage stat refcount for cell %x", key)
	}
	entry.existsKnown = true
	entry.exists = true
	entry.refCountKnown = true
	entry.maxMerkleDepthKnown = true
	return nil
}

func (s *transactionAccountStorageStat) dictRoot() (*cell.Cell, error) {
	if s.dict == nil || s.dict.IsEmpty() {
		return s.dictRootFromScratch()
	}

	dirtyKeys := make([]cell.Hash, 0, min(len(s.entries), 16))
	for key, entry := range s.entries {
		if entry.refCountDiff == 0 {
			continue
		}
		dirtyKeys = append(dirtyKeys, key)
	}
	slices.SortFunc(dirtyKeys, func(left, right cell.Hash) int {
		return bytes.Compare(left[:], right[:])
	})

	// The whole commit goes in as one batch, which is what the reference does
	// (AccountStorageStat::get_dict_root -> vm::Dictionary::multiset). Besides
	// walking the tree once instead of once per key, the batch decides the
	// shape of the Merkle proof this collation ships: a subtree the batch does
	// not descend into is carried over unread, so a validator replaying the
	// same batch needs exactly the cells we read. A key-at-a-time loop reads
	// more — a delete that empties a fork merges its sibling in even when a
	// later key of the same commit lands right back under it — and a proof cut
	// to a batch walk cannot serve that.
	updates := make([]cell.DictBulkKV, 0, len(dirtyKeys))
	for i := range dirtyKeys {
		key := dirtyKeys[i]
		entry := s.entries[key]
		if err := s.fetchEntry(key, entry); err != nil {
			return nil, err
		}
		refCount := int64(entry.refCount) + entry.refCountDiff
		if refCount < 0 || refCount > math.MaxUint32 {
			return nil, fmt.Errorf("invalid account storage stat refcount for cell %x", key)
		}
		if refCount == 0 {
			updates = append(updates, cell.DictBulkKV{Key: dirtyKeys[i][:]})
			entry.exists = false
			entry.refCount = 0
			entry.refCountDiff = 0
			continue
		}
		if !entry.maxMerkleDepthKnown {
			return nil, fmt.Errorf("unknown account storage stat Merkle depth for cell %x", key)
		}
		updates = append(updates, cell.DictBulkKV{
			Key: dirtyKeys[i][:],
			Value: cell.BeginCell().
				MustStoreUInt(uint64(refCount), 32).
				MustStoreUInt(uint64(entry.maxMerkleDepth), 2),
		})
		entry.exists = true
		entry.refCount = uint32(refCount)
		entry.refCountKnown = true
		entry.refCountDiff = 0
	}
	if err := s.dict.Multiset(updates); err != nil {
		return nil, err
	}
	return s.dict.AsCell(), nil
}

// dictRootFromScratch serializes all live entries in one bottom-up bulk build:
// with an empty dict every entry has to be inserted anyway, and the bulk build
// finalizes (hashes) each tree node exactly once instead of re-hashing the
// whole insert path per entry.
func (s *transactionAccountStorageStat) dictRootFromScratch() (*cell.Cell, error) {
	keys := make([]cell.Hash, 0, len(s.entries))
	items := make([]cell.DictBulkKV, 0, len(s.entries))
	for key, entry := range s.entries {
		if err := s.fetchEntry(key, entry); err != nil {
			return nil, err
		}
		refCount := int64(entry.refCount) + entry.refCountDiff
		if refCount < 0 || refCount > math.MaxUint32 {
			return nil, fmt.Errorf("invalid account storage stat refcount for cell %x", key)
		}
		entry.refCountDiff = 0
		entry.refCount = uint32(refCount)
		entry.refCountKnown = true
		entry.exists = refCount != 0
		if refCount == 0 {
			continue
		}
		if !entry.maxMerkleDepthKnown {
			return nil, fmt.Errorf("unknown account storage stat Merkle depth for cell %x", key)
		}

		keys = append(keys, key)
		value := cell.BeginCell().
			MustStoreUInt(uint64(refCount), 32).
			MustStoreUInt(uint64(entry.maxMerkleDepth), 2)
		items = append(items, cell.DictBulkKV{Key: keys[len(keys)-1][:], Value: value})
	}

	dict, err := cell.NewDictFromItems(256, items)
	if err != nil {
		return nil, err
	}
	s.dict = dict
	return s.dict.AsCell(), nil
}

func transactionAccountStorageStatRootHash(root *cell.Cell) []byte {
	if root == nil {
		return make([]byte, 32)
	}
	return root.Hash()
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

	var sl cell.Slice
	err := storage.BeginParseIntoWithoutTrace(&sl)
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
	// AccountStorage::storage_used uses uint64 counters in the reference. Keep
	// both operations separate so a malformed underreported input wraps before
	// the new root size is added; serialization rejects it if it no longer fits
	// VarUInteger 7.
	bits -= oldRootBits
	bits += newRootBits
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

func transactionFinalStateDepth(acc *transactionRuntimeAccount, cfg *PreparedBlockchainConfig) *uint64 {
	if cfg.globalVersion() >= 10 {
		return transactionCloneUint64(acc.stateDepth)
	}

	depth := acc.rewriteDepth()
	if depth == 0 {
		return nil
	}
	return &depth
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
	originalAddr := acc.rawAddress()
	if status != tlb.AccountStatusFrozen || originalAddr == nil {
		return status, status, stateHash, nil
	}
	addrData := originalAddr.Data()
	if len(addrData) != 32 {
		return status, status, stateHash, nil
	}

	if len(stateHash) == 0 {
		stateInit := &tlb.StateInit{
			Depth:    transactionFinalStateDepth(acc, cfg),
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
	disableAnycast := cfg.globalVersion() >= 10
	removeAnycast := disableAnycast && acc.rawAddress().Anycast() != nil
	if deleted {
		return acc, false, &tlb.ComputeSkipReason{Type: transactionNoStateSkipReason(stateInit)}, nil
	}
	if status == tlb.AccountStatusActive {
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
			next.removeAnycast = removeAnycast
			return &next, false, nil, nil
		}
		if removeAnycast {
			next := *acc
			next.removeAnycast = true
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
		stateHash := stateCell.HashKey()
		if !transactionStateInitMatchesAddress(stateHash[:], acc.addr, stateInit.Depth) {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
		if msg.MsgType == tlb.MsgTypeExternalIn && cfg.globalVersion() < 8 && !bytes.Equal(stateHash[:], acc.addr.Data()) {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
	case tlb.AccountStatusFrozen:
		if acc.status != tlb.AccountStatusFrozen {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
		var stateDepth uint64
		if stateInit.Depth != nil {
			stateDepth = *stateInit.Depth
		}
		accountDepth := acc.rewriteDepth()
		if cfg.globalVersion() < 16 && stateDepth != accountDepth {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
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
	if status == tlb.AccountStatusUninit && transactionIsMasterchain(acc.addr) {
		publicLibraries, err := transactionPublicLibrariesCountChecked(stateInit.Lib)
		if err != nil {
			return nil, false, nil, fmt.Errorf("failed to validate message state libraries: %w", err)
		}
		if publicLibraries > 0 {
			return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
		}
	}
	exceedsLimits, err := transactionAccountStateExceedsLimits(acc, stateInit.Code, stateInit.Data, stateInit.Lib, cfg, false)
	if err != nil {
		return nil, false, nil, err
	}
	if exceedsLimits {
		return acc, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
	}
	if (status == tlb.AccountStatusUninit || status == tlb.AccountStatusNonExist) && disableAnycast && stateInit.Depth != nil && *stateInit.Depth > transactionGetSizeLimits(cfg).maxAccFixedPrefixLength {
		next := *acc
		next.removeAnycast = removeAnycast
		return &next, false, &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonBadState}, nil
	}

	next := *acc
	next.removeAnycast = removeAnycast
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
	if state == nil || state.Lib == nil || state.Lib.AsCell() == nil {
		return nil
	}

	iterator, err := state.Lib.Iterator(false, false)
	if err != nil {
		return err
	}
	for iterator.Next() {
		value := iterator.View().Value
		if _, err = value.LoadBoolBit(); err != nil {
			return fmt.Errorf("invalid StateInit library entry: %w", err)
		}
		if _, err = value.LoadRefCell(); err != nil {
			return fmt.Errorf("invalid StateInit library entry: %w", err)
		}
		if value.BitsLeft() != 0 || value.RefsNum() != 0 {
			return errors.New("invalid StateInit library entry")
		}
	}
	if err = iterator.Err(); err != nil {
		return err
	}
	return nil
}
