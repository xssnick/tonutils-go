package tvm

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/internal/bigint"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

var errPrecompiledOutOfGas = errors.New("precompiled contract got out of gas in TVM")

// TransactionOptions carries the genuinely per-transaction execution inputs.
// Everything else lives in BlockContext (per block) and PreparedBlockchainConfig (per
// config epoch).
type TransactionOptions struct {
	// LogicalTime is the legacy signed minimal logical time. The effective start
	// LT also respects the account and inbound message LTs. Non-positive values
	// derive it from the account state unless LogicalTimeUint64 is set.
	LogicalTime int64
	// LogicalTimeUint64 is the full-width minimal logical time. When non-zero,
	// it overrides LogicalTime; zero leaves the legacy value in effect. Use it
	// for protocol values above MaxInt64.
	LogicalTimeUint64 uint64
	// RandSeed is the per-account random seed (c7[6]) as raw bytes. When
	// empty, it is derived from the block rand seed and the account address
	// (see BlockContext.AccountRandSeed).
	RandSeed []byte
	// Gas overrides the config-derived gas limits when non-zero.
	Gas vm.Gas
	// OnCellLoad observes the first load of every cell this transaction makes,
	// with the cell in hand. A collator hangs its proof recorder here: the
	// machine has to distinguish a first load from a repeat for gas anyway, so
	// this is an exact record of what execution read, and it holds even when the
	// cell reached the machine through a route that lost the recording trace.
	OnCellLoad func(*cell.Cell)
	// AccountStorageStat is the account storage-stat dictionary carried in
	// from the previous transaction result of the same account.
	AccountStorageStat *cell.Cell
	// BuildProof builds a Merkle usage proof over the account state covering
	// the compute-phase reads (returned in ExecutionResult.Proof).
	BuildProof bool
	// SignatureCheckAlwaysSucceed makes signature checks succeed unconditionally
	// (fee estimation flows).
	SignatureCheckAlwaysSucceed bool
	// TraceHook observes VM execution.
	TraceHook vm.TraceHook
}

// OutMessage is an outbound message emitted by a transaction, in creation
// order. Msg is the parsed form of Cell built during the action phase.
type OutMessage struct {
	Cell *cell.Cell
	Msg  *tlb.Message
}

type TransactionExecutionResult struct {
	ExecutionResult
	Accepted        bool
	TransactionCell *cell.Cell
	// StartLT is the transaction logical time stored in TransactionCell.
	StartLT uint64
	// Burned is the inbound value destroyed by a blackhole account transaction.
	Burned tlb.CurrencyCollection
	// NextAccount is the resulting account state, prepared to feed the next
	// transaction of the same account without any re-parsing.
	NextAccount *PreparedAccount
	// OutMessages are the emitted outbound messages in creation order,
	// including the bounce message when one was produced.
	OutMessages []OutMessage
	// EndLT is the logical time after the transaction (start LT + 1 + number
	// of emitted messages).
	EndLT uint64
	// AccountStorageStat is the account storage-stat dictionary to pass to the
	// next transaction of the same account.
	AccountStorageStat *cell.Cell
	// StorageStatRecomputed reports that the storage-stat dict bound for this
	// transaction (typically from another producer's collated proof) was pruned
	// short of this update's walk, and the stat was recomputed from the account
	// state directly. The result is the same function of the state; the flag
	// exists so a validator can count how often shipped proofs fall short.
	StorageStatRecomputed bool
}

// ParseTransaction parses the built transaction cell into its tlb form. The
// executor itself never needs the parsed transaction, so the parse is done on
// demand only.
func (r *TransactionExecutionResult) ParseTransaction() (*tlb.Transaction, error) {
	if r == nil || r.TransactionCell == nil {
		return nil, errors.New("transaction result has no transaction cell")
	}
	var tx tlb.Transaction
	if err := tlb.Parse(&tx, r.TransactionCell); err != nil {
		return nil, fmt.Errorf("failed to decode built transaction: %w", err)
	}
	tx.Hash = r.TransactionCell.Hash()
	return &tx, nil
}

// PreparedMessage is an inbound message parsed once and reused by the
// executor without re-parsing.
type PreparedMessage struct {
	cell *cell.Cell
	msg  tlb.Message
}

// PrepareMessage parses an inbound message cell once and validates that it can
// be used as a transaction input.
func PrepareMessage(msgCell *cell.Cell) (*PreparedMessage, error) {
	if msgCell == nil {
		return nil, errors.New("input message is required")
	}
	loader, err := msgCell.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to decode input message: %w", err)
	}
	var msg tlb.Message
	if err = tlb.LoadFromCell(&msg, loader); err != nil {
		return nil, fmt.Errorf("failed to decode input message: %w", err)
	}
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return nil, fmt.Errorf("input message has trailing data: %d bits, %d refs", loader.BitsLeft(), loader.RefsNum())
	}
	return prepareParsedMessage(msgCell, msg, false)
}

// PrepareParsedMessage wraps an already-parsed inbound message together with
// its cell; msg must be the parsed form of msgCell, which is verified so that
// execution (driven by the parsed form) and accounting (driven by the cell)
// cannot describe different inputs.
//
// The check reproduces msgCell from msg using the Either layout the cell itself
// used -- init:(Maybe (Either StateInit ^StateInit)) and body:(Either X ^X) are
// both valid and the reference node accepts either, so a body kept in a
// reference (what wallets and the C++ store_msg layout search normally emit)
// must not be rejected merely because it would also fit inline.
func PrepareParsedMessage(msgCell *cell.Cell, msg *tlb.Message) (*PreparedMessage, error) {
	if msgCell == nil {
		return nil, errors.New("input message is required")
	}
	if msg == nil {
		return nil, errors.New("parsed input message is required")
	}
	return prepareParsedMessage(msgCell, *msg, true)
}

func prepareParsedMessage(msgCell *cell.Cell, msg tlb.Message, verify bool) (*PreparedMessage, error) {
	switch msg.MsgType {
	case tlb.MsgTypeInternal, tlb.MsgTypeExternalIn:
	case tlb.MsgTypeExternalOut:
		return nil, errors.New("external outbound messages cannot be used as transaction input")
	default:
		return nil, fmt.Errorf("unsupported input message type %s", msg.MsgType)
	}
	layout, err := validateBuiltTransactionMessage(msgCell)
	if err != nil {
		return nil, fmt.Errorf("invalid input message: %w", err)
	}
	if verify {
		if err = transactionMessageMatchesCell(msgCell, &msg, layout); err != nil {
			return nil, err
		}
	}
	if layout.bodyInRef {
		var body cell.Slice
		if err = msg.Msg.Payload().BeginParseInto(&body); err != nil {
			return nil, fmt.Errorf("failed to load referenced message body: %w", err)
		}
		if body.IsSpecial() {
			return nil, errors.New("referenced message body is special")
		}
	}
	if err = transactionValidateMessageStateInitLibs(&msg); err != nil {
		return nil, err
	}
	return &PreparedMessage{cell: msgCell, msg: msg}, nil
}

// transactionMessageMatchesCell rebuilds msg with the layout observed in
// msgCell and compares the result bit-for-bit. It deliberately does not
// finalize or hash the builder: a mismatch in any consumed field shows up in
// the raw bits, and hashing an inlined body only to discard the digest would
// cost more than the parse this API exists to avoid.
func transactionMessageMatchesCell(msgCell *cell.Cell, msg *tlb.Message, layout transactionMessageLayout) error {
	builder := cell.BeginCell()
	err := tlb.StoreMessageWithLayout(builder, msg, tlb.MessageLayout{
		StateInitInRef: layout.stateInitInRef,
		BodyInRef:      layout.bodyInRef,
	})
	if err != nil {
		return fmt.Errorf("parsed message does not match the message cell: %w", err)
	}
	if !builder.EqualsCell(msgCell) {
		return errors.New("parsed message does not match the message cell")
	}
	return nil
}

// Cell returns the raw message cell.
func (m *PreparedMessage) Cell() *cell.Cell {
	return m.cell
}

// Message returns the parsed message.
func (m *PreparedMessage) Message() *tlb.Message {
	return &m.msg
}

type transactionRuntimeAccount struct {
	// addr is the effective account identity used as the ShardAccounts key.
	// For an anycast account it contains the rewritten 256-bit address without
	// anycast metadata.
	addr *address.Address
	// addrRaw is the MsgAddressInt stored in the account state (addr_orig plus
	// anycast metadata). addrExact encodes addr without anycast. They are
	// resolved once while preparing the account and reused by c7 and actions.
	addrRaw             *address.Address
	addrExact           *address.Address
	addrRewriteDepth    uint64
	addrIdentityDerived bool
	// Account::compute_my_addr in the reference rebuilds an anycast addr_std
	// with workchain 127 as addr_var. Such an existing account can be unpacked,
	// but any non-empty final account state fails the reference's validation.
	nonCanonicalMyAddr bool
	// removeAnycast is set only after compute preparation crosses the v10
	// disable-anycast boundary; earlier skips preserve the raw account address.
	removeAnycast   bool
	status          tlb.AccountStatus
	storageInfo     tlb.StorageInfo
	balance         *big.Int
	extraCurrencies *cell.Dictionary
	code            *cell.Cell
	data            *cell.Cell
	libraries       *cell.Dictionary
	inMsgLibraries  *cell.Dictionary
	stateDepth      *uint64
	tickTock        *tlb.TickTock
	stateHash       []byte
	storageLT       uint64
	storageCell     *cell.Cell
	// storageCellForStat is storageCell without the extra-currency dict (the
	// extra-currency v2 stat form), threaded from the previous transaction of
	// the account so it is not re-derived per transaction; nil when unknown.
	storageCellForStat *cell.Cell
	// statBoundTo names the storage-for-stat cell that the storage-stat dict
	// emitted alongside this account describes. It is what lets the next
	// transaction reuse the dict incrementally without a storage_dict_hash,
	// which masterchain accounts and pre-v11 configs never have. Only this
	// executor sets it, so a dict that arrived from anywhere else stays
	// untrusted. Zero means "no provenance".
	accountStorageStat *cell.Cell
	statBoundTo        cell.Hash
	// storageStatRecomputed is set when a bound storage-stat dict could not
	// serve this transaction's update — its Merkle-proof pruning followed the
	// producer's own walk — and the stat was recomputed from the state by the
	// direct walk instead.
	storageStatRecomputed bool
	prevTxHash            []byte
	prevTxLT              uint64
	originalCell          *cell.Cell
	isSpecial             bool
}

func (a *transactionRuntimeAccount) rawAddress() *address.Address {
	if a.addrIdentityDerived {
		return a.addrRaw
	}
	return a.addr
}

func (a *transactionRuntimeAccount) exactAddress() *address.Address {
	if a.addrIdentityDerived {
		return a.addrExact
	}
	if exact, err := transactionAccountIDAddr(a.addr); err == nil {
		return exact
	}
	return a.addr
}

func (a *transactionRuntimeAccount) vmAddress(globalVersion uint32) *address.Address {
	if globalVersion >= 10 {
		return a.exactAddress()
	}
	return a.rawAddress()
}

func (a *transactionRuntimeAccount) rewriteDepth() uint64 {
	if a.addrIdentityDerived {
		return a.addrRewriteDepth
	}
	if anycast := a.addr.Anycast(); anycast != nil {
		return uint64(anycast.Depth())
	}
	return 0
}

type transactionUsage struct {
	cells uint64
	bits  uint64
}

type transactionStorageDueLimits struct {
	freezeDue *big.Int
	deleteDue *big.Int
}

type transactionPreparedPhases struct {
	balance         *big.Int
	extraCurrencies *cell.Dictionary
	msgBalance      *transactionCurrencyBalance
	blackholeBurned *big.Int
	creditPhase     *tlb.CreditPhase
	creditFirst     bool
	storagePhase    *tlb.StoragePhase
	status          tlb.AccountStatus
	deleted         bool
	destroyed       bool
	duePayment      *tlb.Coins
	lastPaid        uint32

	// originalBalance is the RAWRESERVE mode&4 base. Below global version 9
	// it is the pre-transaction balance minus the fees collected so far, and
	// it becomes invalid (nil) when that goes negative, which fails the
	// reserve action outright.
	originalBalance      *transactionCurrencyBalance
	originalBalanceValid bool
}

// preV9OriginalBalance returns the RAWRESERVE base for global versions below
// 9, or nil when the pre-transaction balance did not cover the fees collected
// so far.
func (p *transactionPreparedPhases) preV9OriginalBalance() *transactionCurrencyBalance {
	if !p.originalBalanceValid {
		return nil
	}
	return p.originalBalance
}

type transactionCurrencyBalance struct {
	grams *big.Int
	extra map[uint32]*big.Int
}

type transactionSizeLimits struct {
	maxMsgBits                  uint64
	maxMsgCells                 uint64
	maxTotalMsgBits             uint64
	maxTotalMsgCells            uint64
	maxLibraryCells             uint64
	maxExtMsgDepth              uint16
	maxAccStateCells            uint64
	maxMCAccStateCells          uint64
	maxAccPublicLibraries       uint64
	maxMsgExtraCurrencies       uint64
	maxAccFixedPrefixLength     uint64
	accStateCellsForStorageDict uint64
	maxTransactionLibraryLoads  *uint32
	maxVMDataDepth              uint16
}

// transactionExecEnv is the assembled per-transaction execution environment:
// block context plus derived phase values feeding the c7 tuple.
type transactionExecEnv struct {
	block *BlockContext
	cfg   *PreparedBlockchainConfig
	opts  *TransactionOptions

	acc     *transactionRuntimeAccount
	msg     *tlb.Message
	msgCell *cell.Cell

	startLT      uint64
	blockLT      *big.Int
	balance      *big.Int
	balanceExtra *cell.Cell

	incomingValue       tuple.Tuple
	storageFees         *big.Int
	duePayment          *big.Int
	inMsgParams         tuple.Tuple
	precompiledGasUsage *big.Int

	proof        *cell.MerkleProofBuilder
	stopOnAccept bool
}

// transactionReportedGas picks the limits the compute phase reports: the
// precompiled fallback only replaces the limits the VM actually runs under,
// while the serialized phase keeps the limits derived from the balance.
func transactionReportedGas(working vm.Gas, reported *vm.Gas) vm.Gas {
	if reported != nil {
		return *reported
	}
	return working
}

func newTransactionExecEnv(block *BlockContext, opts *TransactionOptions, acc *transactionRuntimeAccount, msg *tlb.Message, msgCell *cell.Cell, prepared *transactionPreparedPhases, startLT, executionLT uint64) *transactionExecEnv {
	env := &transactionExecEnv{
		block:   block,
		cfg:     block.cfg,
		opts:    opts,
		acc:     acc,
		msg:     msg,
		msgCell: msgCell,
		startLT: startLT,
		blockLT: new(big.Int),
		balance: bigint.Set(prepared.balance),
	}
	if block.blockLTU64 != 0 {
		env.blockLT.SetUint64(block.blockLTU64)
	} else {
		env.blockLT.SetInt64(block.blockLT)
	}
	if prepared.extraCurrencies != nil && !prepared.extraCurrencies.IsEmpty() {
		env.balanceExtra = prepared.extraCurrencies.AsCell()
	}
	if block.blockLT == 0 && block.blockLTU64 == 0 {
		env.blockLT.SetUint64(transactionBlockLogicalTime(executionLT))
	}
	env.incomingValue = prepared.msgBalance.asTuple()
	env.storageFees = transactionSharedBigOrZero(prepared.storagePhase.StorageFeesCollected.NanoRef())
	env.duePayment = transactionSharedBigOrZero(transactionCoinsNanoRef(prepared.duePayment))
	env.inMsgParams = transactionBuildInMsgParams(msg, prepared.msgBalance)
	return env
}

func (env *transactionExecEnv) c7Input(code *cell.Cell, balance *big.Int) (emulationC7Input, error) {
	seed, err := transactionSeed(env.block, env.opts, env.acc.addr)
	if err != nil {
		return emulationC7Input{}, err
	}
	return emulationC7Input{
		addr:                env.acc.vmAddress(env.cfg.version),
		code:                code,
		now:                 env.block.now,
		blockLT:             env.blockLT,
		logicalTime:         bigint.FromUint64(env.startLT),
		balance:             balance,
		balanceExtra:        env.balanceExtra,
		seed:                seed,
		configRoot:          env.cfg.root,
		incomingValue:       env.incomingValue,
		storageFees:         env.storageFees,
		prevBlocks:          env.block.prevBlocksValue(),
		unpackedConfig:      env.block.unpackedConfig,
		duePayment:          transactionMaybeBigValue(env.duePayment),
		precompiledGasUsage: env.precompiledGasUsage,
		inMsgParams:         env.inMsgParams,
		globalVersion:       env.cfg.version,
	}, nil
}

// transactionSeed resolves the c7 rand seed: the explicit per-account seed
// when provided, otherwise derived from the block seed and account address.
func transactionSeed(block *BlockContext, opts *TransactionOptions, addr *address.Address) (*big.Int, error) {
	if len(opts.RandSeed) > 0 {
		return new(big.Int).SetBytes(opts.RandSeed), nil
	}
	seed, err := accountRandSeedBytes(block.randSeed, addr, block.cfg.version)
	if err != nil {
		return nil, err
	}
	if len(seed) == 0 {
		return bigint.FromInt64(0), nil
	}
	return new(big.Int).SetBytes(seed), nil
}

func transactionMaybeBigValue(v *big.Int) any {
	if v == nil {
		return nil
	}
	return v
}

// transactionExecutionLibraries combines compute-phase library collections.
// Since global version 15, account-private and inbound StateInit libraries are
// excluded from the VM context.
func transactionExecutionLibraries(acc *transactionRuntimeAccount, blockLibraries []*cell.Cell, globalVersion uint32) []*cell.Cell {
	if globalVersion >= 15 {
		return blockLibraries
	}

	var inMsgLibrary, accountLibrary *cell.Cell
	if acc.inMsgLibraries != nil {
		inMsgLibrary = acc.inMsgLibraries.AsCell()
	}
	if acc.libraries != nil {
		accountLibrary = acc.libraries.AsCell()
	}
	if inMsgLibrary == nil && accountLibrary == nil {
		return blockLibraries
	}

	librariesNum := len(blockLibraries)
	if inMsgLibrary != nil {
		librariesNum++
	}
	if accountLibrary != nil {
		librariesNum++
	}

	libraries := make([]*cell.Cell, librariesNum)
	idx := 0
	if inMsgLibrary != nil {
		libraries[idx] = inMsgLibrary
		idx++
	}
	if accountLibrary != nil {
		libraries[idx] = accountLibrary
		idx++
	}
	copy(libraries[idx:], blockLibraries)
	return libraries
}

// EmulateTransaction executes an ordinary transaction of acc with the inbound
// message msg. The result feeds the next transaction of the same account
// through NextAccount and AccountStorageStat.
func (tvm *TVM) EmulateTransaction(block *BlockContext, acc *PreparedAccount, msg *PreparedMessage, opts TransactionOptions) (*TransactionExecutionResult, error) {
	if block == nil {
		return nil, errors.New("block context is required")
	}
	if acc == nil {
		return nil, errors.New("prepared account is required")
	}
	if msg == nil {
		return nil, errors.New("prepared message is required")
	}
	blockchainCfg := block.cfg

	runtimeAcc, proof, err := acc.runtimeForExecution(opts.BuildProof)
	if err != nil {
		return nil, err
	}
	if opts.AccountStorageStat != nil {
		if err = transactionUseAccountStorageStat(runtimeAcc, opts.AccountStorageStat, blockchainCfg); err != nil {
			return nil, fmt.Errorf("invalid account storage stat: %w", err)
		}
	}

	now := block.now
	if err = transactionValidateInboundExternalMessage(msg.cell, &msg.msg, blockchainCfg); err != nil {
		return nil, err
	}
	isSpecial := blockchainCfg.isSpecialAccount(runtimeAcc.addr)
	runtimeAcc.isSpecial = isSpecial

	storageDueLimits := blockchainCfg.storageDueLimitsFor(transactionIsMasterchain(runtimeAcc.addr))

	storageFee, err := transactionComputeStorageFee(blockchainCfg, runtimeAcc, now)
	if err != nil {
		return nil, err
	}
	importFee := bigint.FromInt64(0)
	if !isSpecial {
		importFee, err = transactionComputeImportFee(blockchainCfg, runtimeAcc.addr, &msg.msg, msg.cell)
		if err != nil {
			return nil, err
		}
	}
	prepared, err := transactionPrepareInitialPhases(runtimeAcc, &msg.msg, storageFee, importFee, now, blockchainCfg, storageDueLimits)
	if err != nil {
		return nil, err
	}
	if isSpecial {
		prepared.lastPaid = 0
	}

	executionLT := transactionExecutionLogicalTime(runtimeAcc.prevTxLT, opts.LogicalTime, opts.LogicalTimeUint64)
	startLT := transactionStartLT(runtimeAcc.storageLT, executionLT, &msg.msg)
	env := newTransactionExecEnv(block, &opts, runtimeAcc, &msg.msg, msg.cell, prepared, startLT, executionLT)
	env.proof = proof

	computeAcc := runtimeAcc
	msgStateUsed := false
	removeAnycast := false
	var skipReason *tlb.ComputeSkipReason
	var gas vm.Gas
	var reportedGas *vm.Gas
	if prepared.balance.Sign() <= 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	} else {
		gas = transactionMessageGas(opts.Gas, now, blockchainCfg, runtimeAcc.addr, prepared.balance, prepared.msgBalance.grams, msg.msg.MsgType, isSpecial)
		if gas.Limit == 0 && gas.Credit == 0 {
			skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
		} else {
			addressSuspended := false
			if !prepared.deleted && (prepared.status == tlb.AccountStatusUninit || prepared.status == tlb.AccountStatusNonExist) && transactionMessageStateInit(&msg.msg) != nil {
				addressSuspended = blockchainCfg.isAddressSuspended(now, runtimeAcc.addr)
			}

			computeAcc, msgStateUsed, skipReason, err = transactionPrepareComputeAccount(runtimeAcc, prepared.status, prepared.deleted, &msg.msg, addressSuspended, blockchainCfg)
			if err != nil {
				return nil, err
			}
			if opts.BuildProof && msgStateUsed && skipReason == nil {
				return nil, errors.New("account execution proof cannot be built for code loaded from message state init")
			}
			removeAnycast = computeAcc.removeAnycast
			if skipReason == nil {
				// the precompiled fallback replaces only the limits the VM
				// runs under; the compute phase still reports the limits
				// derived from the balance
				beforeFallback := gas
				reportedGas = &beforeFallback
				gas, skipReason = transactionApplyPrecompiledGasConfig(blockchainCfg, computeAcc.code, runtimeAcc.addr, isSpecial, gas, env)
			}
		}
	}

	var msgRes *MessageExecutionResult
	accountActivated := false
	if skipReason == nil {
		msgRes, err = tvm.executeTransactionMessage(computeAcc, env, gas, prepared.msgBalance.grams)
		if err != nil {
			return nil, err
		}
		transactionNormalizeGasUsage(msgRes)
		if err = transactionApplyPrecompiledGasUsage(msgRes, env.precompiledGasUsage); err != nil {
			return nil, err
		}
		if msgStateUsed && msgRes.Accepted {
			accountActivated = true
			prepared.status = tlb.AccountStatusActive
			prepared.deleted = false
			prepared.destroyed = false
		}
	}

	out := &TransactionExecutionResult{StartLT: startLT}
	if prepared.blackholeBurned != nil {
		out.Burned.Coins = tlb.FromNanoTON(prepared.blackholeBurned)
	}
	if msgRes != nil {
		out.ExecutionResult = msgRes.ExecutionResult
		out.Accepted = msgRes.Accepted
	}
	if msg.msg.MsgType == tlb.MsgTypeExternalIn && (msgRes == nil || !msgRes.Accepted) {
		return out, nil
	}

	endLT := startLT + 1
	nextCode := computeAcc.code
	nextData := computeAcc.data
	nextLibraries := computeAcc.libraries
	nextExtraCurrencies := prepared.extraCurrencies
	msgBalanceRemaining := prepared.msgBalance
	var outMessages []OutMessage
	var actionPhase *tlb.ActionPhase
	var actionBounce bool
	var actionDeleted bool
	actionFine := bigint.FromInt64(0)
	gasFees := bigint.FromInt64(0)
	if msgRes != nil && msgRes.Accepted && !isSpecial {
		gasFees = transactionComputeGasFee(blockchainCfg, runtimeAcc.addr, uint64(msgRes.GasUsed))
	}
	finalBalance := new(big.Int).Sub(prepared.balance, gasFees)
	if finalBalance.Sign() < 0 {
		return nil, errors.New("transaction fees exceed account balance")
	}

	actionFees := bigint.FromInt64(0)
	if msgRes != nil {
		actionRes, applyErr := transactionApplyActions(computeAcc, msgRes, startLT, now, blockchainCfg, finalBalance, nextExtraCurrencies, prepared.msgBalance, gasFees, prepared.preV9OriginalBalance())
		if applyErr != nil {
			return nil, applyErr
		}
		outMessages = actionRes.outMsgs
		actionPhase = actionRes.phase
		nextCode = actionRes.nextCode
		nextLibraries = actionRes.nextLibraries
		nextExtraCurrencies = actionRes.extraCurrencies
		endLT = actionRes.endLT
		finalBalance = actionRes.balance
		actionFees = actionRes.actionFees
		actionFine = actionRes.actionFine
		actionBounce = actionRes.bounce
		actionDeleted = actionRes.deleteAccount
		msgBalanceRemaining = actionRes.msgBalanceRemaining
	}

	// Nano already returns a private copy to accumulate into.
	totalFees := prepared.storagePhase.StorageFeesCollected.Nano()
	totalFees.Add(totalFees, gasFees)
	totalFees.Add(totalFees, importFee)
	totalFees.Add(totalFees, actionFees)

	computeSuccess := transactionComputeSucceeded(msgRes)
	actionSuccess := actionPhase != nil && actionPhase.Success
	aborted := skipReason != nil || !(computeSuccess && actionSuccess)
	var bouncePhase *tlb.BouncePhase
	if aborted && transactionShouldBounce(&msg.msg, skipReason, computeSuccess, actionBounce) {
		bounceRes, bounceErr := transactionPrepareBouncePhase(&msg.msg, runtimeAcc.addr, finalBalance, nextExtraCurrencies, msgBalanceRemaining, gasFees, actionFine, startLT, now, len(outMessages), blockchainCfg, skipReason, msgRes, actionPhase)
		if bounceErr != nil {
			return nil, bounceErr
		}
		if bounceRes != nil {
			bouncePhase = bounceRes.phase
			finalBalance = bounceRes.balance
			nextExtraCurrencies = bounceRes.extraCurrencies
			totalFees.Add(totalFees, bounceRes.msgFees)
			if bounceRes.outMsg != nil {
				outMessages = append(outMessages, OutMessage{Cell: bounceRes.outMsg, Msg: bounceRes.outMsgParsed})
				endLT++
			}
		}
	}

	if computeSuccess && actionSuccess {
		nextData = msgRes.Data
	}
	if nextCode == nil {
		nextCode = computeAcc.code
	}
	if nextLibraries == nil {
		nextLibraries = computeAcc.libraries
	}

	if actionDeleted {
		prepared.deleted = true
		prepared.destroyed = true
	}
	finalStatus := transactionFinalizeAccountStatus(prepared.status, prepared.deleted, finalBalance, nextExtraCurrencies, accountActivated)
	nextStateHash := runtimeAcc.stateHash
	if finalStatus == tlb.AccountStatusFrozen && (runtimeAcc.status == tlb.AccountStatusActive || accountActivated) {
		nextStateHash = nil
	}
	accountStatus := finalStatus
	finalStatus, accountStatus, nextStateHash, err = transactionNormalizeFrozenFinalState(computeAcc, finalStatus, nextCode, nextData, nextLibraries, nextStateHash, blockchainCfg)
	if err != nil {
		return nil, err
	}
	if err = transactionValidateLogicalTimeRange(runtimeAcc.storageLT, startLT, endLT); err != nil {
		return nil, err
	}
	var loadedCells *vm.LoadedCells
	if msgRes != nil {
		loadedCells = &msgRes.loadedCells
	}
	nextAccount, err := buildTransactionAccountCell(computeAcc, accountStatus, finalBalance, nextExtraCurrencies, endLT, prepared.lastPaid, prepared.duePayment, nextCode, nextData, nextLibraries, nextStateHash, removeAnycast, blockchainCfg, runtimeAcc.accountStorageStat, loadedCells)
	if err != nil {
		return nil, err
	}
	txCell, err := buildTransactionCell(transactionBuildParams{
		accountAddr: runtimeAcc.addr,
		startLT:     startLT,
		prevTxHash:  runtimeAcc.prevTxHash,
		prevTxLT:    runtimeAcc.prevTxLT,
		now:         now,
		origStatus:  runtimeAcc.status,
		endStatus:   finalStatus,
		inMsg:       msg.cell,
		outMsgs:     outMessages,
		totalFees:   totalFees,
		oldHash:     runtimeAcc.originalCell.Hash(),
		newHash:     nextAccount.cell.Hash(),
		description: transactionBuildDescriptionParams{
			msg:           &msg.msg,
			creditFirst:   prepared.creditFirst,
			storagePhase:  prepared.storagePhase,
			creditPhase:   prepared.creditPhase,
			computeResult: msgRes,
			computeGas:    transactionReportedGas(gas, reportedGas),
			gasFees:       gasFees,
			actionPhase:   actionPhase,
			bouncePhase:   bouncePhase,
			skipReason:    skipReason,
			msgStateUsed:  msgStateUsed && runtimeAcc.status != tlb.AccountStatusActive,
			activated:     accountActivated && runtimeAcc.status != tlb.AccountStatusActive,
			destroyed:     prepared.destroyed,
		},
	})
	if err != nil {
		return nil, err
	}

	return out, fillTransactionExecutionResult(out, txCell, acc, nextAccount, outMessages, startLT, endLT)
}

// EmulateTickTockTransaction executes the explicitly selected tick or tock
// transaction. Eligibility and the stored tick/tock flags are scheduling
// concerns and are not rechecked by the reference emulator entry point.
func (tvm *TVM) EmulateTickTockTransaction(block *BlockContext, acc *PreparedAccount, isTock bool, opts TransactionOptions) (*TransactionExecutionResult, error) {
	if block == nil {
		return nil, errors.New("block context is required")
	}
	if acc == nil {
		return nil, errors.New("prepared account is required")
	}
	if acc.runtime.status == tlb.AccountStatusNonExist {
		return nil, errors.New("cannot run tick/tock transaction on non-existing account")
	}
	blockchainCfg := block.cfg

	runtimeAcc, proof, err := acc.runtimeForExecution(opts.BuildProof)
	if err != nil {
		return nil, err
	}
	if opts.AccountStorageStat != nil {
		if err = transactionUseAccountStorageStat(runtimeAcc, opts.AccountStorageStat, blockchainCfg); err != nil {
			return nil, fmt.Errorf("invalid account storage stat: %w", err)
		}
	}

	now := block.now
	isSpecial := blockchainCfg.isSpecialAccount(runtimeAcc.addr)
	runtimeAcc.isSpecial = isSpecial
	storageDueLimits := blockchainCfg.storageDueLimitsFor(transactionIsMasterchain(runtimeAcc.addr))
	storageFee, err := transactionComputeStorageFee(blockchainCfg, runtimeAcc, now)
	if err != nil {
		return nil, err
	}
	extraCurrencies, err := transactionCloneExtraCurrencies(runtimeAcc.extraCurrencies)
	if err != nil {
		return nil, err
	}
	prepared := &transactionPreparedPhases{
		balance:         bigint.Set(runtimeAcc.balance),
		extraCurrencies: extraCurrencies,
		msgBalance:      transactionZeroCurrencyBalance(),
		status:          runtimeAcc.status,
		duePayment:      transactionCoinsClonePtr(runtimeAcc.storageInfo.DuePayment),
		lastPaid:        runtimeAcc.storageInfo.LastPaid,
	}
	prepared.applyStoragePhase(runtimeAcc, storageFee, now, blockchainCfg.globalVersion(), storageDueLimits, false)
	if blockchainCfg.globalVersion() < 9 {
		if err = prepared.applyPreV9OriginalBalance(runtimeAcc, nil); err != nil {
			return nil, err
		}
	}
	if isSpecial {
		prepared.lastPaid = 0
	}

	executionLT := transactionExecutionLogicalTime(runtimeAcc.prevTxLT, opts.LogicalTime, opts.LogicalTimeUint64)
	startLT := transactionStartLT(runtimeAcc.storageLT, executionLT, nil)
	env := newTransactionExecEnv(block, &opts, runtimeAcc, nil, nil, prepared, startLT, executionLT)
	env.proof = proof

	var skipReason *tlb.ComputeSkipReason
	gas := transactionTickTockGas(opts.Gas, now, blockchainCfg, runtimeAcc.addr, prepared.balance, isSpecial)
	if prepared.balance.Sign() <= 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	} else if gas.Limit == 0 && gas.Credit == 0 {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}
	} else if prepared.status != tlb.AccountStatusActive || prepared.deleted {
		skipReason = &tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoState}
	}

	var msgRes *MessageExecutionResult
	// A tick/tock skipped before VM startup keeps the raw anycast address.
	removeAnycast := skipReason == nil && blockchainCfg.globalVersion() >= 10 && runtimeAcc.rawAddress().Anycast() != nil
	if skipReason == nil {
		msgRes, err = tvm.executeTickTockTransaction(runtimeAcc, isTock, env, gas)
		if err != nil {
			return nil, err
		}
		transactionNormalizeGasUsage(msgRes)
		if err = transactionApplyPrecompiledGasUsage(msgRes, env.precompiledGasUsage); err != nil {
			return nil, err
		}
	}

	out := &TransactionExecutionResult{StartLT: startLT}
	if msgRes != nil {
		out.ExecutionResult = msgRes.ExecutionResult
		out.Accepted = msgRes.Accepted
	}

	endLT := startLT + 1
	nextCode := runtimeAcc.code
	nextData := runtimeAcc.data
	nextLibraries := runtimeAcc.libraries
	nextExtraCurrencies := prepared.extraCurrencies
	var outMessages []OutMessage
	var actionPhase *tlb.ActionPhase
	var actionDeleted bool
	gasFees := bigint.FromInt64(0)
	if msgRes != nil && msgRes.Accepted && !isSpecial {
		gasFees = transactionComputeGasFee(blockchainCfg, runtimeAcc.addr, uint64(msgRes.GasUsed))
	}
	finalBalance := new(big.Int).Sub(prepared.balance, gasFees)
	if finalBalance.Sign() < 0 {
		return nil, errors.New("transaction fees exceed account balance")
	}

	actionFees := bigint.FromInt64(0)
	if transactionComputeSucceeded(msgRes) {
		actionRes, applyErr := transactionApplyActions(runtimeAcc, msgRes, startLT, now, blockchainCfg, finalBalance, nextExtraCurrencies, prepared.msgBalance, gasFees, prepared.preV9OriginalBalance())
		if applyErr != nil {
			return nil, applyErr
		}
		outMessages = actionRes.outMsgs
		actionPhase = actionRes.phase
		nextCode = actionRes.nextCode
		nextLibraries = actionRes.nextLibraries
		nextExtraCurrencies = actionRes.extraCurrencies
		endLT = actionRes.endLT
		finalBalance = actionRes.balance
		actionFees = actionRes.actionFees
		actionDeleted = actionRes.deleteAccount
	}

	// Nano already returns a private copy to accumulate into.
	totalFees := prepared.storagePhase.StorageFeesCollected.Nano()
	totalFees.Add(totalFees, gasFees)
	totalFees.Add(totalFees, actionFees)

	computeSuccess := transactionComputeSucceeded(msgRes)
	actionSuccess := actionPhase != nil && actionPhase.Success
	if computeSuccess && actionSuccess {
		nextData = msgRes.Data
	}
	if nextCode == nil {
		nextCode = runtimeAcc.code
	}
	if nextLibraries == nil {
		nextLibraries = runtimeAcc.libraries
	}

	if actionDeleted {
		prepared.deleted = true
		prepared.destroyed = true
	}
	finalStatus := transactionFinalizeAccountStatus(prepared.status, prepared.deleted, finalBalance, nextExtraCurrencies, false)
	nextStateHash := runtimeAcc.stateHash
	if finalStatus == tlb.AccountStatusFrozen && runtimeAcc.status == tlb.AccountStatusActive {
		nextStateHash = nil
	}
	accountStatus := finalStatus
	finalStatus, accountStatus, nextStateHash, err = transactionNormalizeFrozenFinalState(runtimeAcc, finalStatus, nextCode, nextData, nextLibraries, nextStateHash, blockchainCfg)
	if err != nil {
		return nil, err
	}
	if err = transactionValidateLogicalTimeRange(runtimeAcc.storageLT, startLT, endLT); err != nil {
		return nil, err
	}
	var loadedCells *vm.LoadedCells
	if msgRes != nil {
		loadedCells = &msgRes.loadedCells
	}
	nextAccount, err := buildTransactionAccountCell(runtimeAcc, accountStatus, finalBalance, nextExtraCurrencies, endLT, prepared.lastPaid, prepared.duePayment, nextCode, nextData, nextLibraries, nextStateHash, removeAnycast, blockchainCfg, runtimeAcc.accountStorageStat, loadedCells)
	if err != nil {
		return nil, err
	}
	txCell, err := buildTransactionCell(transactionBuildParams{
		accountAddr: runtimeAcc.addr,
		startLT:     startLT,
		prevTxHash:  runtimeAcc.prevTxHash,
		prevTxLT:    runtimeAcc.prevTxLT,
		now:         now,
		origStatus:  runtimeAcc.status,
		endStatus:   finalStatus,
		outMsgs:     outMessages,
		totalFees:   totalFees,
		oldHash:     runtimeAcc.originalCell.Hash(),
		newHash:     nextAccount.cell.Hash(),
		description: transactionBuildDescriptionParams{
			tickTock:      true,
			isTock:        isTock,
			storagePhase:  prepared.storagePhase,
			computeResult: msgRes,
			computeGas:    gas,
			gasFees:       gasFees,
			actionPhase:   actionPhase,
			skipReason:    skipReason,
			destroyed:     prepared.destroyed,
		},
	})
	if err != nil {
		return nil, err
	}

	return out, fillTransactionExecutionResult(out, txCell, acc, nextAccount, outMessages, startLT, endLT)
}

func transactionNormalizeGasUsage(res *MessageExecutionResult) {
	if res.GasUsed > res.Gas.Base {
		res.GasUsed = res.Gas.Base
	}
}

func transactionNoCodeExecutionResult(code, data *cell.Cell, gas vm.Gas) *MessageExecutionResult {
	return &MessageExecutionResult{
		ExecutionResult: ExecutionResult{
			ExitCode: -vmerr.CodeOutOfGas,
			Gas:      gas,
			Code:     code,
			Data:     data,
		},
		Accepted: gas.Credit == 0,
	}
}

func transactionApplyPrecompiledGasUsage(res *MessageExecutionResult, value *big.Int) error {
	precompiledGas, ok, err := transactionPrecompiledGasUsage(value)
	if err != nil || !ok {
		return err
	}
	if res.ExitCode == ^int64(vmerr.CodeOutOfGas) {
		return errPrecompiledOutOfGas
	}

	res.GasUsed = precompiledGas
	res.Steps = 0
	return nil
}

func (tvm *TVM) executeTransactionMessage(acc *transactionRuntimeAccount, env *transactionExecEnv, gas vm.Gas, msgBalance *big.Int) (*MessageExecutionResult, error) {
	msg := env.msg
	body := messageBodyCell(msg.Msg.Payload())
	stack := vm.NewStack()
	balance := bigint.Set(env.balance)

	switch msg.MsgType {
	case tlb.MsgTypeExternalIn:
		if err := stack.PushOwnedInt(balance); err != nil {
			return nil, err
		}
		if err := stack.PushSmallInt(0); err != nil {
			return nil, err
		}
		if err := stack.PushCell(env.msgCell); err != nil {
			return nil, err
		}
		bodySlice, err := body.BeginParse()
		if err != nil {
			return nil, err
		}
		if err := stack.PushOwnedSlice(bodySlice); err != nil {
			return nil, err
		}
		if err := stack.PushSmallInt(-1); err != nil {
			return nil, err
		}
	case tlb.MsgTypeInternal:
		if err := stack.PushOwnedInt(balance); err != nil {
			return nil, err
		}
		if err := stack.PushOwnedInt(transactionBigOrZero(msgBalance)); err != nil {
			return nil, err
		}
		if err := stack.PushCell(env.msgCell); err != nil {
			return nil, err
		}
		bodySlice, err := body.BeginParse()
		if err != nil {
			return nil, err
		}
		if err := stack.PushOwnedSlice(bodySlice); err != nil {
			return nil, err
		}
		if err := stack.PushSmallInt(0); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported input message type %s", msg.MsgType)
	}

	if acc.code == nil {
		return transactionNoCodeExecutionResult(acc.code, acc.data, gas), nil
	}
	c7In, err := env.c7Input(acc.code, balance)
	if err != nil {
		return nil, err
	}

	libraries := transactionExecutionLibraries(acc, env.block.libraries, env.cfg.version)
	return tvm.executeMessageEmulation(acc.code, acc.data, c7In, gas, stack, env.stopOnAccept, env.opts.SignatureCheckAlwaysSucceed, env.proof, env.opts.TraceHook, env.opts.OnCellLoad, env.cfg, env.cfg.sizeLimits.maxTransactionLibraryLoads, libraries...)
}

func (tvm *TVM) executeTickTockTransaction(acc *transactionRuntimeAccount, isTock bool, env *transactionExecEnv, gas vm.Gas) (*MessageExecutionResult, error) {
	accAddr, err := messageEmulationAccountAddr(acc.addr)
	if err != nil {
		return nil, err
	}

	stack := vm.NewStack()
	balance := bigint.Set(env.balance)
	if err = stack.PushOwnedInt(balance); err != nil {
		return nil, err
	}
	if err = stack.PushOwnedInt(accAddr); err != nil {
		return nil, err
	}
	if err = stack.PushBool(isTock); err != nil {
		return nil, err
	}
	if err = stack.PushSmallInt(-2); err != nil {
		return nil, err
	}

	if acc.code == nil {
		return transactionNoCodeExecutionResult(acc.code, acc.data, gas), nil
	}
	c7In, err := env.c7Input(acc.code, balance)
	if err != nil {
		return nil, err
	}

	libraries := transactionExecutionLibraries(acc, env.block.libraries, env.cfg.version)
	return tvm.executeMessageEmulation(acc.code, acc.data, c7In, gas, stack, false, env.opts.SignatureCheckAlwaysSucceed, env.proof, env.opts.TraceHook, env.opts.OnCellLoad, env.cfg, env.cfg.sizeLimits.maxTransactionLibraryLoads, libraries...)
}

const transactionLTAlignment = uint64(1_000_000)

func transactionExecutionLogicalTime(prevTxLT uint64, configured int64, configuredUint64 uint64) uint64 {
	if configuredUint64 != 0 {
		return configuredUint64
	}
	if configured > 0 {
		return uint64(configured)
	}
	return (prevTxLT/transactionLTAlignment + 1) * transactionLTAlignment
}

func transactionBlockLogicalTime(startLT uint64) uint64 {
	return startLT - startLT%transactionLTAlignment
}

func (c *transactionCurrencyBalance) asTuple() tuple.Tuple {
	if c == nil {
		return tuple.NewTupleValue(bigint.FromInt64(0), nil)
	}
	extra, err := c.extraDict()
	if err != nil || extra == nil || extra.IsEmpty() {
		return tuple.NewTupleValue(transactionSharedBigOrZero(c.grams), nil)
	}
	return tuple.NewTupleValue(transactionSharedBigOrZero(c.grams), extra.AsCell())
}

func transactionBuildInMsgParams(msg *tlb.Message, msgBalance *transactionCurrencyBalance) tuple.Tuple {
	if msg == nil {
		return messageInMsgParams(tuple.Tuple{}, nil)
	}

	stateInitCell := transactionMaybeStateInitCell(transactionMessageStateInit(msg))
	value := transactionSharedBigOrZero(msgBalance.grams)
	valueExtra := transactionCurrencyExtraCell(msgBalance)
	switch msg.MsgType {
	case tlb.MsgTypeInternal:
		in := msg.AsInternal()
		return tuple.NewTupleValue(
			messageTupleBool(in.Bounce),
			messageTupleBool(in.Bounced),
			cell.BeginCell().MustStoreAddr(in.SrcAddr).ToSlice(),
			messageTupleCoins(in.FwdFee),
			messageTupleUint(in.CreatedLT),
			messageTupleUint(uint64(in.CreatedAt)),
			messageTupleCoins(in.Amount),
			value,
			valueExtra,
			stateInitCell,
		)
	case tlb.MsgTypeExternalIn:
		in := msg.AsExternalIn()
		return tuple.NewTupleValue(
			messageTupleInt(0),
			messageTupleInt(0),
			cell.BeginCell().MustStoreAddr(in.SrcAddr).ToSlice(),
			messageTupleInt(0),
			messageTupleInt(0),
			messageTupleInt(0),
			messageTupleInt(0),
			value,
			valueExtra,
			stateInitCell,
		)
	default:
		return messageInMsgParams(tuple.Tuple{}, nil)
	}
}

// messageTupleTrue/messageTupleFalse are the VM's shared instances for the two
// TVM boolean constants. They are only ever stored into c7 tuples, which hand
// out cloned leaves (tuple.Tuple.Index) and never mutate in place.
var (
	messageTupleTrue  = vm.StaticInt(-1)
	messageTupleFalse = vm.StaticInt(0)
)

func messageTupleBool(v bool) *big.Int {
	if v {
		return messageTupleTrue
	}
	return messageTupleFalse
}

// messageTupleCoins puts an amount carried by the inbound message into a c7
// leaf without copying it. It is the same sharing argument the static pool
// above relies on: leaves leave c7 only through tuple.Tuple.Index, which clones
// every *big.Int, so no operand mutated in place is ever the instance held
// here. The message the amount belongs to is read-only for the whole
// emulation — tlb.Coins has no mutating method, and the outbound copies built
// from it assign fresh amounts rather than writing through this one.
func messageTupleCoins(c tlb.Coins) *big.Int {
	return c.NanoRef()
}

func transactionCurrencyExtraCell(value *transactionCurrencyBalance) *cell.Cell {
	if value == nil {
		return nil
	}
	extra, err := value.extraDict()
	if err != nil || extra == nil || extra.IsEmpty() {
		return nil
	}
	return extra.AsCell()
}

func transactionMaybeStateInitCell(stateInit *tlb.StateInit) *cell.Cell {
	if stateInit == nil {
		return nil
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		return nil
	}
	return stateCell
}

// accountRandSeedBytes derives the per-account c7 random seed, including the
// pre-v8 layout when needed.
func accountRandSeedBytes(blockSeed []byte, addr *address.Address, globalVersion uint32) ([]byte, error) {
	if len(blockSeed) == 0 {
		return nil, nil
	}
	addrData, err := transactionRewrittenAccountAddressData(addr)
	if err != nil {
		return nil, err
	}

	h := sha256.New()
	h.Write(transactionBits256(blockSeed))
	if globalVersion < 8 {
		h.Write(addrData[:4])
		h.Write(addrData[:28])
	} else {
		h.Write(addrData)
	}
	return h.Sum(nil), nil
}

func transactionRewrittenAccountAddressData(addr *address.Address) ([]byte, error) {
	if addr == nil || addr.Type() != address.StdAddress || len(addr.Data()) != 32 {
		return nil, errors.New("transaction rand seed requires std 256-bit account address")
	}

	data := append([]byte(nil), addr.Data()...)
	anycast := addr.Anycast()
	if anycast == nil {
		return data, nil
	}

	depth := anycast.Depth()
	prefix := anycast.Prefix()
	if depth == 0 || depth > 30 || uint(len(prefix)*8) < depth {
		return nil, errors.New("transaction rand seed requires valid account anycast")
	}
	for i := uint(0); i < depth; i++ {
		mask := byte(1 << (7 - i%8))
		if transactionBit(prefix, int(i)) == 1 {
			data[i/8] |= mask
		} else {
			data[i/8] &^= mask
		}
	}
	return data, nil
}

func transactionStartLT(storageLT, logicalTime uint64, msg *tlb.Message) uint64 {
	start := logicalTime

	if storageLT > start {
		start = storageLT
	}

	if msg != nil && msg.MsgType == tlb.MsgTypeInternal {
		if createdLT := msg.AsInternal().CreatedLT; createdLT >= start {
			start = createdLT + 1
		}
	}

	return start
}

func transactionValidateLogicalTimeRange(storageLT, startLT, endLT uint64) error {
	if storageLT > startLT || startLT >= endLT {
		return errors.New("cannot commit transaction with invalid logical time range")
	}
	return nil
}
