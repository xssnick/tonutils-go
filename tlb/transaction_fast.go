package tlb

import (
	"fmt"
	"math/big"
	"math/bits"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func loadMaybeCoins(loader *cell.Slice) (*Coins, error) {
	has, err := loader.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}

	coins, err := loadCoins(loader)
	if err != nil {
		return nil, err
	}
	return &coins, nil
}

func storeMaybeCoins(builder *cell.Builder, coins *Coins) error {
	if coins == nil {
		return builder.StoreBoolBit(false)
	}
	if err := builder.StoreBoolBit(true); err != nil {
		return err
	}
	return storeCoins(builder, *coins)
}

func loadMaybeInt32(loader *cell.Slice) (*int32, error) {
	has, err := loader.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}

	val, err := loader.LoadInt(32)
	if err != nil {
		return nil, err
	}
	ret := int32(val)
	return &ret, nil
}

func storeMaybeInt32(builder *cell.Builder, val *int32) error {
	if val == nil {
		return builder.StoreBoolBit(false)
	}
	if err := builder.StoreBoolBit(true); err != nil {
		return err
	}
	return builder.StoreInt(int64(*val), 32)
}

func loadMaybeVarUInt(loader *cell.Slice, sz uint) (*big.Int, error) {
	has, err := loader.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	return loadVarUInt(loader, sz)
}

func storeMaybeVarUInt(builder *cell.Builder, val *big.Int, sz uint) error {
	if val == nil {
		return builder.StoreBoolBit(false)
	}
	if err := builder.StoreBoolBit(true); err != nil {
		return err
	}
	return storeVarUInt(builder, val, sz)
}

func loadVarUInt(loader *cell.Slice, sz uint) (*big.Int, error) {
	if sz == 0 {
		return nil, cell.ErrInvalidSize
	}

	lnBits := uint(bits.Len64(uint64(sz - 1)))
	ln, err := loader.LoadUInt(lnBits)
	if err != nil {
		return nil, err
	}
	if ln >= uint64(sz) {
		return nil, cell.ErrTooBigValue
	}
	if ln <= 8 {
		val, err := loader.LoadUInt(uint(ln * 8))
		if err != nil {
			return nil, err
		}
		return new(big.Int).SetUint64(val), nil
	}
	return loader.LoadBigUInt(uint(ln * 8))
}

func storeVarUInt(builder *cell.Builder, val *big.Int, sz uint) error {
	if val != nil && val.IsUint64() {
		return builder.StoreVarUInt(val.Uint64(), sz)
	}
	return builder.StoreBigVarUInt(val, sz)
}

func storeAccountStatus(builder *cell.Builder, status AccountStatus) error {
	switch status {
	case AccountStatusNonExist:
		return builder.StoreUInt(0b11, 2)
	case AccountStatusActive:
		return builder.StoreUInt(0b10, 2)
	case AccountStatusFrozen:
		return builder.StoreUInt(0b01, 2)
	case AccountStatusUninit:
		return builder.StoreUInt(0b00, 2)
	default:
		return fmt.Errorf("unknown account status %s", status)
	}
}

func storeAccStatusChange(builder *cell.Builder, change AccStatusChange) error {
	switch change.Type {
	case AccStatusChangeUnchanged:
		return builder.StoreUInt(0b0, 1)
	case AccStatusChangeFrozen:
		return builder.StoreUInt(0b10, 2)
	case AccStatusChangeDeleted:
		return builder.StoreUInt(0b11, 2)
	default:
		return fmt.Errorf("unknown state change type %s", change.Type)
	}
}

func (c *CurrencyCollection) LoadFromCell(loader *cell.Slice) error {
	coins, err := loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load coins: %w", err)
	}

	extra, err := loader.LoadDict(32)
	if err != nil {
		return fmt.Errorf("failed to load extra currencies: %w", err)
	}

	c.Coins = coins
	c.ExtraCurrencies = extra
	return nil
}

func (c CurrencyCollection) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeCurrencyCollection(builder, c); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeCurrencyCollection(builder *cell.Builder, c CurrencyCollection) error {
	if err := storeCoins(builder, c.Coins); err != nil {
		return fmt.Errorf("failed to store coins: %w", err)
	}
	if err := builder.StoreDict(c.ExtraCurrencies); err != nil {
		return fmt.Errorf("failed to store extra currencies: %w", err)
	}
	return nil
}

func (h *HashUpdate) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(8)
	if err != nil {
		return fmt.Errorf("failed to load hash update magic: %w", err)
	}
	if magic != 0x72 {
		return fmt.Errorf("invalid hash update magic %x", magic)
	}

	oldHash, err := loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load old hash: %w", err)
	}
	newHash, err := loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load new hash: %w", err)
	}

	h.OldHash = oldHash
	h.NewHash = newHash
	return nil
}

func (h HashUpdate) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeHashUpdate(builder, h); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeHashUpdate(builder *cell.Builder, h HashUpdate) error {
	if err := builder.StoreUInt(0x72, 8); err != nil {
		return fmt.Errorf("failed to store hash update magic: %w", err)
	}
	if err := builder.StoreSlice(h.OldHash, 256); err != nil {
		return fmt.Errorf("failed to store old hash: %w", err)
	}
	if err := builder.StoreSlice(h.NewHash, 256); err != nil {
		return fmt.Errorf("failed to store new hash: %w", err)
	}
	return nil
}

func (s *StoragePhase) LoadFromCell(loader *cell.Slice) error {
	fees, err := loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load collected storage fees: %w", err)
	}
	due, err := loadMaybeCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load due storage fees: %w", err)
	}

	var status AccStatusChange
	if err = status.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load account status change: %w", err)
	}

	s.StorageFeesCollected = fees
	s.StorageFeesDue = due
	s.StatusChange = status
	return nil
}

func (s StoragePhase) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeStoragePhase(builder, &s); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeStoragePhase(builder *cell.Builder, s *StoragePhase) error {
	if err := storeCoins(builder, s.StorageFeesCollected); err != nil {
		return fmt.Errorf("failed to store collected storage fees: %w", err)
	}
	if err := storeMaybeCoins(builder, s.StorageFeesDue); err != nil {
		return fmt.Errorf("failed to store due storage fees: %w", err)
	}
	if err := storeAccStatusChange(builder, s.StatusChange); err != nil {
		return fmt.Errorf("failed to store account status change: %w", err)
	}
	return nil
}

func (c *CreditPhase) LoadFromCell(loader *cell.Slice) error {
	due, err := loadMaybeCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load due credit fees: %w", err)
	}

	var credit CurrencyCollection
	if err = credit.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load credit: %w", err)
	}

	c.DueFeesCollected = due
	c.Credit = credit
	return nil
}

func (c CreditPhase) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeCreditPhase(builder, &c); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeCreditPhase(builder *cell.Builder, c *CreditPhase) error {
	if err := storeMaybeCoins(builder, c.DueFeesCollected); err != nil {
		return fmt.Errorf("failed to store due credit fees: %w", err)
	}
	if err := storeCurrencyCollection(builder, c.Credit); err != nil {
		return fmt.Errorf("failed to store credit: %w", err)
	}
	return nil
}

func (s *StorageUsedShort) LoadFromCell(loader *cell.Slice) error {
	cells, err := loadVarUInt(loader, 7)
	if err != nil {
		return fmt.Errorf("failed to load cells used: %w", err)
	}
	bits, err := loadVarUInt(loader, 7)
	if err != nil {
		return fmt.Errorf("failed to load bits used: %w", err)
	}

	s.Cells = cells
	s.Bits = bits
	return nil
}

func (s StorageUsedShort) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeStorageUsedShort(builder, s); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeStorageUsedShort(builder *cell.Builder, s StorageUsedShort) error {
	if err := storeVarUInt(builder, s.Cells, 7); err != nil {
		return fmt.Errorf("failed to store cells used: %w", err)
	}
	if err := storeVarUInt(builder, s.Bits, 7); err != nil {
		return fmt.Errorf("failed to store bits used: %w", err)
	}
	return nil
}

func (d *ComputePhaseVMDetails) LoadFromCell(loader *cell.Slice) error {
	var err error

	d.GasUsed, err = loadVarUInt(loader, 7)
	if err != nil {
		return fmt.Errorf("failed to load gas used: %w", err)
	}
	d.GasLimit, err = loadVarUInt(loader, 7)
	if err != nil {
		return fmt.Errorf("failed to load gas limit: %w", err)
	}
	d.GasCredit, err = loadMaybeVarUInt(loader, 3)
	if err != nil {
		return fmt.Errorf("failed to load gas credit: %w", err)
	}

	mode, err := loader.LoadInt(8)
	if err != nil {
		return fmt.Errorf("failed to load mode: %w", err)
	}
	exitCode, err := loader.LoadInt(32)
	if err != nil {
		return fmt.Errorf("failed to load exit code: %w", err)
	}
	exitArg, err := loadMaybeInt32(loader)
	if err != nil {
		return fmt.Errorf("failed to load exit arg: %w", err)
	}
	steps, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load vm steps: %w", err)
	}
	initHash, err := loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load vm init state hash: %w", err)
	}
	finalHash, err := loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load vm final state hash: %w", err)
	}

	d.Mode = int8(mode)
	d.ExitCode = int32(exitCode)
	d.ExitArg = exitArg
	d.VMSteps = uint32(steps)
	d.VMInitStateHash = initHash
	d.VMFinalStateHash = finalHash
	return nil
}

func (d ComputePhaseVMDetails) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeComputePhaseVMDetails(builder, d); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeComputePhaseVMDetails(builder *cell.Builder, d ComputePhaseVMDetails) error {
	if err := storeVarUInt(builder, d.GasUsed, 7); err != nil {
		return fmt.Errorf("failed to store gas used: %w", err)
	}
	if err := storeVarUInt(builder, d.GasLimit, 7); err != nil {
		return fmt.Errorf("failed to store gas limit: %w", err)
	}
	if err := storeMaybeVarUInt(builder, d.GasCredit, 3); err != nil {
		return fmt.Errorf("failed to store gas credit: %w", err)
	}
	if err := builder.StoreInt(int64(d.Mode), 8); err != nil {
		return fmt.Errorf("failed to store mode: %w", err)
	}
	if err := builder.StoreInt(int64(d.ExitCode), 32); err != nil {
		return fmt.Errorf("failed to store exit code: %w", err)
	}
	if err := storeMaybeInt32(builder, d.ExitArg); err != nil {
		return fmt.Errorf("failed to store exit arg: %w", err)
	}
	if err := builder.StoreUInt(uint64(d.VMSteps), 32); err != nil {
		return fmt.Errorf("failed to store vm steps: %w", err)
	}
	if err := builder.StoreSlice(d.VMInitStateHash, 256); err != nil {
		return fmt.Errorf("failed to store vm init state hash: %w", err)
	}
	if err := builder.StoreSlice(d.VMFinalStateHash, 256); err != nil {
		return fmt.Errorf("failed to store vm final state hash: %w", err)
	}
	return nil
}

func (c *ComputePhaseVM) LoadFromCell(loader *cell.Slice) error {
	isVM, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load compute phase magic: %w", err)
	}
	if !isVM {
		return fmt.Errorf("invalid compute vm phase magic")
	}
	return c.loadFromCellAfterMagic(loader)
}

func (c *ComputePhaseVM) loadFromCellAfterMagic(loader *cell.Slice) error {
	var err error
	c.Success, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load success flag: %w", err)
	}
	c.MsgStateUsed, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load msg_state_used flag: %w", err)
	}
	c.AccountActivated, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load account_activated flag: %w", err)
	}
	c.GasFees, err = loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load gas fees: %w", err)
	}

	detailsRef, err := loader.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load vm details ref: %w", err)
	}
	if err = c.Details.LoadFromCell(detailsRef); err != nil {
		return fmt.Errorf("failed to load vm details: %w", err)
	}
	return nil
}

func (c ComputePhaseVM) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeComputePhaseVM(builder, c); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeComputePhaseVM(builder *cell.Builder, c ComputePhaseVM) error {
	if err := builder.StoreBoolBit(true); err != nil {
		return fmt.Errorf("failed to store compute vm phase magic: %w", err)
	}
	if err := builder.StoreBoolBit(c.Success); err != nil {
		return fmt.Errorf("failed to store success flag: %w", err)
	}
	if err := builder.StoreBoolBit(c.MsgStateUsed); err != nil {
		return fmt.Errorf("failed to store msg_state_used flag: %w", err)
	}
	if err := builder.StoreBoolBit(c.AccountActivated); err != nil {
		return fmt.Errorf("failed to store account_activated flag: %w", err)
	}
	if err := storeCoins(builder, c.GasFees); err != nil {
		return fmt.Errorf("failed to store gas fees: %w", err)
	}

	details, err := c.Details.ToCell()
	if err != nil {
		return fmt.Errorf("failed to serialize vm details: %w", err)
	}
	if err = builder.StoreRef(details); err != nil {
		return fmt.Errorf("failed to store vm details ref: %w", err)
	}
	return nil
}

func (c *ComputePhaseSkipped) LoadFromCell(loader *cell.Slice) error {
	isVM, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load skipped compute phase magic: %w", err)
	}
	if isVM {
		return fmt.Errorf("invalid skipped compute phase magic")
	}
	return c.loadFromCellAfterMagic(loader)
}

func (c *ComputePhaseSkipped) loadFromCellAfterMagic(loader *cell.Slice) error {
	if err := c.Reason.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load compute skip reason: %w", err)
	}
	return nil
}

func (c ComputePhaseSkipped) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeComputePhaseSkipped(builder, c); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeComputePhaseSkipped(builder *cell.Builder, c ComputePhaseSkipped) error {
	if err := builder.StoreBoolBit(false); err != nil {
		return fmt.Errorf("failed to store skipped compute phase magic: %w", err)
	}
	if err := storeComputeSkipReason(builder, c.Reason); err != nil {
		return fmt.Errorf("failed to store compute skip reason: %w", err)
	}
	return nil
}

func storeComputeSkipReason(builder *cell.Builder, reason ComputeSkipReason) error {
	switch reason.Type {
	case ComputeSkipReasonNoState:
		return builder.StoreUInt(0b00, 2)
	case ComputeSkipReasonBadState:
		return builder.StoreUInt(0b01, 2)
	case ComputeSkipReasonNoGas:
		return builder.StoreUInt(0b10, 2)
	case ComputeSkipReasonSuspended:
		return builder.StoreUInt(0b110, 3)
	default:
		return fmt.Errorf("unknown compute skip reason %s", reason.Type)
	}
}

func (c *ComputePhase) LoadFromCell(loader *cell.Slice) error {
	isVM, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load compute phase magic: %w", err)
	}

	if isVM {
		var phase ComputePhaseVM
		if err = phase.loadFromCellAfterMagic(loader); err != nil {
			return err
		}
		c.Phase = phase
		return nil
	}

	var phase ComputePhaseSkipped
	if err = phase.loadFromCellAfterMagic(loader); err != nil {
		return err
	}
	c.Phase = phase
	return nil
}

func (c ComputePhase) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeComputePhase(builder, c); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeComputePhase(builder *cell.Builder, c ComputePhase) error {
	switch phase := c.Phase.(type) {
	case ComputePhaseVM:
		return storeComputePhaseVM(builder, phase)
	case ComputePhaseSkipped:
		return storeComputePhaseSkipped(builder, phase)
	}

	// Compatibility fallback: ComputePhase.Phase is public any and the old path accepted any Marshaller.
	phaseCell, err := ToCell(c.Phase)
	if err != nil {
		return err
	}
	return builder.StoreBuilder(phaseCell.ToBuilder())
}

func (b *BouncePhaseNegFunds) LoadFromCell(loader *cell.Slice) error {
	pfx, err := loader.LoadUInt(2)
	if err != nil {
		return fmt.Errorf("failed to load negative funds bounce phase magic: %w", err)
	}
	if pfx != 0 {
		return fmt.Errorf("invalid negative funds bounce phase magic %b", pfx)
	}
	return nil
}

func (b BouncePhaseNegFunds) ToCell() (*cell.Cell, error) {
	return cell.BeginCell().MustStoreUInt(0, 2).EndCell(), nil
}

func (b *BouncePhaseNoFunds) LoadFromCell(loader *cell.Slice) error {
	pfx, err := loader.LoadUInt(2)
	if err != nil {
		return fmt.Errorf("failed to load no funds bounce phase magic: %w", err)
	}
	if pfx != 0b01 {
		return fmt.Errorf("invalid no funds bounce phase magic %b", pfx)
	}
	return b.loadFromCellAfterMagic(loader)
}

func (b *BouncePhaseNoFunds) loadFromCellAfterMagic(loader *cell.Slice) error {
	if err := b.MsgSize.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load message size: %w", err)
	}
	fees, err := loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load required fwd fees: %w", err)
	}
	b.ReqFwdFees = fees
	return nil
}

func (b BouncePhaseNoFunds) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeBouncePhaseNoFunds(builder, b); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeBouncePhaseNoFunds(builder *cell.Builder, b BouncePhaseNoFunds) error {
	if err := builder.StoreUInt(0b01, 2); err != nil {
		return fmt.Errorf("failed to store no funds bounce phase magic: %w", err)
	}
	if err := storeStorageUsedShort(builder, b.MsgSize); err != nil {
		return fmt.Errorf("failed to store message size: %w", err)
	}
	if err := storeCoins(builder, b.ReqFwdFees); err != nil {
		return fmt.Errorf("failed to store required fwd fees: %w", err)
	}
	return nil
}

func (b *BouncePhaseOk) LoadFromCell(loader *cell.Slice) error {
	isOk, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load ok bounce phase magic: %w", err)
	}
	if !isOk {
		return fmt.Errorf("invalid ok bounce phase magic")
	}
	return b.loadFromCellAfterMagic(loader)
}

func (b *BouncePhaseOk) loadFromCellAfterMagic(loader *cell.Slice) error {
	if err := b.MsgSize.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load message size: %w", err)
	}
	msgFees, err := loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load message fees: %w", err)
	}
	fwdFees, err := loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load fwd fees: %w", err)
	}

	b.MsgFees = msgFees
	b.FwdFees = fwdFees
	return nil
}

func (b BouncePhaseOk) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeBouncePhaseOk(builder, b); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeBouncePhaseOk(builder *cell.Builder, b BouncePhaseOk) error {
	if err := builder.StoreBoolBit(true); err != nil {
		return fmt.Errorf("failed to store ok bounce phase magic: %w", err)
	}
	if err := storeStorageUsedShort(builder, b.MsgSize); err != nil {
		return fmt.Errorf("failed to store message size: %w", err)
	}
	if err := storeCoins(builder, b.MsgFees); err != nil {
		return fmt.Errorf("failed to store message fees: %w", err)
	}
	if err := storeCoins(builder, b.FwdFees); err != nil {
		return fmt.Errorf("failed to store fwd fees: %w", err)
	}
	return nil
}

func (b *BouncePhase) LoadFromCell(loader *cell.Slice) error {
	isOk, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load bounce phase magic: %w", err)
	}

	if isOk {
		var phase BouncePhaseOk
		if err = phase.loadFromCellAfterMagic(loader); err != nil {
			return err
		}
		b.Phase = phase
		return nil
	}

	hasFunds, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load bounce no-funds flag: %w", err)
	}
	if !hasFunds {
		b.Phase = BouncePhaseNegFunds{}
		return nil
	}

	var phase BouncePhaseNoFunds
	if err = phase.loadFromCellAfterMagic(loader); err != nil {
		return err
	}
	b.Phase = phase
	return nil
}

func (b BouncePhase) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeBouncePhase(builder, b); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeBouncePhase(builder *cell.Builder, b BouncePhase) error {
	switch phase := b.Phase.(type) {
	case BouncePhaseOk:
		return storeBouncePhaseOk(builder, phase)
	case BouncePhaseNoFunds:
		return storeBouncePhaseNoFunds(builder, phase)
	case BouncePhaseNegFunds:
		return builder.StoreUInt(0, 2)
	}

	// Compatibility fallback: BouncePhase.Phase is public any and the old path accepted any Marshaller.
	phaseCell, err := ToCell(b.Phase)
	if err != nil {
		return err
	}
	return builder.StoreBuilder(phaseCell.ToBuilder())
}

func (a *ActionPhase) LoadFromCell(loader *cell.Slice) error {
	var err error
	a.Success, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load success flag: %w", err)
	}
	a.Valid, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load valid flag: %w", err)
	}
	a.NoFunds, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load no_funds flag: %w", err)
	}
	if err = a.StatusChange.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load status change: %w", err)
	}
	a.TotalFwdFees, err = loadMaybeCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load total fwd fees: %w", err)
	}
	a.TotalActionFees, err = loadMaybeCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load total action fees: %w", err)
	}

	resultCode, err := loader.LoadInt(32)
	if err != nil {
		return fmt.Errorf("failed to load result code: %w", err)
	}
	a.ResultArg, err = loadMaybeInt32(loader)
	if err != nil {
		return fmt.Errorf("failed to load result arg: %w", err)
	}
	totalActions, err := loader.LoadUInt(16)
	if err != nil {
		return fmt.Errorf("failed to load total actions: %w", err)
	}
	specActions, err := loader.LoadUInt(16)
	if err != nil {
		return fmt.Errorf("failed to load spec actions: %w", err)
	}
	skippedActions, err := loader.LoadUInt(16)
	if err != nil {
		return fmt.Errorf("failed to load skipped actions: %w", err)
	}
	messagesCreated, err := loader.LoadUInt(16)
	if err != nil {
		return fmt.Errorf("failed to load messages created: %w", err)
	}
	hash, err := loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load action list hash: %w", err)
	}
	if err = a.TotalMsgSize.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load total message size: %w", err)
	}

	a.ResultCode = int32(resultCode)
	a.TotalActions = uint16(totalActions)
	a.SpecActions = uint16(specActions)
	a.SkippedActions = uint16(skippedActions)
	a.MessagesCreated = uint16(messagesCreated)
	a.ActionListHash = hash
	return nil
}

func (a ActionPhase) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeActionPhase(builder, &a); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeActionPhase(builder *cell.Builder, a *ActionPhase) error {
	if err := builder.StoreBoolBit(a.Success); err != nil {
		return fmt.Errorf("failed to store success flag: %w", err)
	}
	if err := builder.StoreBoolBit(a.Valid); err != nil {
		return fmt.Errorf("failed to store valid flag: %w", err)
	}
	if err := builder.StoreBoolBit(a.NoFunds); err != nil {
		return fmt.Errorf("failed to store no_funds flag: %w", err)
	}
	if err := storeAccStatusChange(builder, a.StatusChange); err != nil {
		return fmt.Errorf("failed to store status change: %w", err)
	}
	if err := storeMaybeCoins(builder, a.TotalFwdFees); err != nil {
		return fmt.Errorf("failed to store total fwd fees: %w", err)
	}
	if err := storeMaybeCoins(builder, a.TotalActionFees); err != nil {
		return fmt.Errorf("failed to store total action fees: %w", err)
	}
	if err := builder.StoreInt(int64(a.ResultCode), 32); err != nil {
		return fmt.Errorf("failed to store result code: %w", err)
	}
	if err := storeMaybeInt32(builder, a.ResultArg); err != nil {
		return fmt.Errorf("failed to store result arg: %w", err)
	}
	if err := builder.StoreUInt(uint64(a.TotalActions), 16); err != nil {
		return fmt.Errorf("failed to store total actions: %w", err)
	}
	if err := builder.StoreUInt(uint64(a.SpecActions), 16); err != nil {
		return fmt.Errorf("failed to store spec actions: %w", err)
	}
	if err := builder.StoreUInt(uint64(a.SkippedActions), 16); err != nil {
		return fmt.Errorf("failed to store skipped actions: %w", err)
	}
	if err := builder.StoreUInt(uint64(a.MessagesCreated), 16); err != nil {
		return fmt.Errorf("failed to store messages created: %w", err)
	}
	if err := builder.StoreSlice(a.ActionListHash, 256); err != nil {
		return fmt.Errorf("failed to store action list hash: %w", err)
	}
	if err := storeStorageUsedShort(builder, a.TotalMsgSize); err != nil {
		return fmt.Errorf("failed to store total message size: %w", err)
	}
	return nil
}

func loadMaybeStoragePhase(loader *cell.Slice) (*StoragePhase, error) {
	has, err := loader.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}

	var phase StoragePhase
	if err = phase.LoadFromCell(loader); err != nil {
		return nil, err
	}
	return &phase, nil
}

func storeMaybeStoragePhase(builder *cell.Builder, phase *StoragePhase) error {
	if phase == nil {
		return builder.StoreBoolBit(false)
	}
	if err := builder.StoreBoolBit(true); err != nil {
		return err
	}
	return storeStoragePhase(builder, phase)
}

func loadMaybeCreditPhase(loader *cell.Slice) (*CreditPhase, error) {
	has, err := loader.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}

	var phase CreditPhase
	if err = phase.LoadFromCell(loader); err != nil {
		return nil, err
	}
	return &phase, nil
}

func storeMaybeCreditPhase(builder *cell.Builder, phase *CreditPhase) error {
	if phase == nil {
		return builder.StoreBoolBit(false)
	}
	if err := builder.StoreBoolBit(true); err != nil {
		return err
	}
	return storeCreditPhase(builder, phase)
}

func loadMaybeActionPhaseRef(loader *cell.Slice) (*ActionPhase, error) {
	has, err := loader.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}

	ref, err := loader.LoadRef()
	if err != nil {
		return nil, err
	}
	var phase ActionPhase
	if err = phase.LoadFromCell(ref); err != nil {
		return nil, err
	}
	return &phase, nil
}

func storeMaybeActionPhaseRef(builder *cell.Builder, phase *ActionPhase) error {
	if phase == nil {
		return builder.StoreBoolBit(false)
	}

	phaseCell, err := phase.ToCell()
	if err != nil {
		return err
	}
	if err = builder.StoreBoolBit(true); err != nil {
		return err
	}
	return builder.StoreRef(phaseCell)
}

func loadMaybeBouncePhase(loader *cell.Slice) (*BouncePhase, error) {
	has, err := loader.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}

	var phase BouncePhase
	if err = phase.LoadFromCell(loader); err != nil {
		return nil, err
	}
	return &phase, nil
}

func storeMaybeBouncePhase(builder *cell.Builder, phase *BouncePhase) error {
	if phase == nil {
		return builder.StoreBoolBit(false)
	}
	if err := builder.StoreBoolBit(true); err != nil {
		return err
	}
	return storeBouncePhase(builder, *phase)
}

func (d *TransactionDescriptionOrdinary) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load ordinary transaction magic: %w", err)
	}
	if magic != 0 {
		return fmt.Errorf("invalid ordinary transaction magic %b", magic)
	}
	return d.loadFromCellAfterMagic(loader)
}

func (d *TransactionDescriptionOrdinary) loadFromCellAfterMagic(loader *cell.Slice) error {
	var err error
	d.CreditFirst, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load credit_first flag: %w", err)
	}
	d.StoragePhase, err = loadMaybeStoragePhase(loader)
	if err != nil {
		return fmt.Errorf("failed to load storage phase: %w", err)
	}
	d.CreditPhase, err = loadMaybeCreditPhase(loader)
	if err != nil {
		return fmt.Errorf("failed to load credit phase: %w", err)
	}
	if err = d.ComputePhase.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load compute phase: %w", err)
	}
	d.ActionPhase, err = loadMaybeActionPhaseRef(loader)
	if err != nil {
		return fmt.Errorf("failed to load action phase: %w", err)
	}
	d.Aborted, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load aborted flag: %w", err)
	}
	d.BouncePhase, err = loadMaybeBouncePhase(loader)
	if err != nil {
		return fmt.Errorf("failed to load bounce phase: %w", err)
	}
	d.Destroyed, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load destroyed flag: %w", err)
	}
	return nil
}

func (d TransactionDescriptionOrdinary) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := builder.StoreUInt(0, 4); err != nil {
		return nil, fmt.Errorf("failed to store ordinary transaction magic: %w", err)
	}
	if err := builder.StoreBoolBit(d.CreditFirst); err != nil {
		return nil, fmt.Errorf("failed to store credit_first flag: %w", err)
	}
	if err := storeMaybeStoragePhase(builder, d.StoragePhase); err != nil {
		return nil, fmt.Errorf("failed to store storage phase: %w", err)
	}
	if err := storeMaybeCreditPhase(builder, d.CreditPhase); err != nil {
		return nil, fmt.Errorf("failed to store credit phase: %w", err)
	}
	if err := storeComputePhase(builder, d.ComputePhase); err != nil {
		return nil, fmt.Errorf("failed to store compute phase: %w", err)
	}
	if err := storeMaybeActionPhaseRef(builder, d.ActionPhase); err != nil {
		return nil, fmt.Errorf("failed to store action phase: %w", err)
	}
	if err := builder.StoreBoolBit(d.Aborted); err != nil {
		return nil, fmt.Errorf("failed to store aborted flag: %w", err)
	}
	if err := storeMaybeBouncePhase(builder, d.BouncePhase); err != nil {
		return nil, fmt.Errorf("failed to store bounce phase: %w", err)
	}
	if err := builder.StoreBoolBit(d.Destroyed); err != nil {
		return nil, fmt.Errorf("failed to store destroyed flag: %w", err)
	}
	return builder.EndCell(), nil
}

func (d *TransactionDescriptionStorage) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load storage transaction magic: %w", err)
	}
	if magic != 1 {
		return fmt.Errorf("invalid storage transaction magic %b", magic)
	}
	return d.loadFromCellAfterMagic(loader)
}

func (d *TransactionDescriptionStorage) loadFromCellAfterMagic(loader *cell.Slice) error {
	if err := d.StoragePhase.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load storage phase: %w", err)
	}
	return nil
}

func (d TransactionDescriptionStorage) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := builder.StoreUInt(1, 4); err != nil {
		return nil, fmt.Errorf("failed to store storage transaction magic: %w", err)
	}
	if err := storeStoragePhase(builder, &d.StoragePhase); err != nil {
		return nil, fmt.Errorf("failed to store storage phase: %w", err)
	}
	return builder.EndCell(), nil
}

func (d *TransactionDescriptionTickTock) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(3)
	if err != nil {
		return fmt.Errorf("failed to load tick-tock transaction magic: %w", err)
	}
	if magic != 0b001 {
		return fmt.Errorf("invalid tick-tock transaction magic %b", magic)
	}
	return d.loadFromCellAfterMagic(loader)
}

func (d *TransactionDescriptionTickTock) loadFromCellAfterMagic(loader *cell.Slice) error {
	var err error
	d.IsTock, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load is_tock flag: %w", err)
	}
	if err = d.StoragePhase.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load storage phase: %w", err)
	}
	if err = d.ComputePhase.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load compute phase: %w", err)
	}
	d.ActionPhase, err = loadMaybeActionPhaseRef(loader)
	if err != nil {
		return fmt.Errorf("failed to load action phase: %w", err)
	}
	d.Aborted, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load aborted flag: %w", err)
	}
	d.Destroyed, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load destroyed flag: %w", err)
	}
	return nil
}

func (d TransactionDescriptionTickTock) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := builder.StoreUInt(0b001, 3); err != nil {
		return nil, fmt.Errorf("failed to store tick-tock transaction magic: %w", err)
	}
	if err := builder.StoreBoolBit(d.IsTock); err != nil {
		return nil, fmt.Errorf("failed to store is_tock flag: %w", err)
	}
	if err := storeStoragePhase(builder, &d.StoragePhase); err != nil {
		return nil, fmt.Errorf("failed to store storage phase: %w", err)
	}
	if err := storeComputePhase(builder, d.ComputePhase); err != nil {
		return nil, fmt.Errorf("failed to store compute phase: %w", err)
	}
	if err := storeMaybeActionPhaseRef(builder, d.ActionPhase); err != nil {
		return nil, fmt.Errorf("failed to store action phase: %w", err)
	}
	if err := builder.StoreBoolBit(d.Aborted); err != nil {
		return nil, fmt.Errorf("failed to store aborted flag: %w", err)
	}
	if err := builder.StoreBoolBit(d.Destroyed); err != nil {
		return nil, fmt.Errorf("failed to store destroyed flag: %w", err)
	}
	return builder.EndCell(), nil
}

func (s *SplitMergeInfo) LoadFromCell(loader *cell.Slice) error {
	curShardPfxLen, err := loader.LoadUInt(6)
	if err != nil {
		return fmt.Errorf("failed to load current shard prefix len: %w", err)
	}
	accSplitDepth, err := loader.LoadUInt(6)
	if err != nil {
		return fmt.Errorf("failed to load account split depth: %w", err)
	}
	thisAddr, err := loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load this address: %w", err)
	}
	siblingAddr, err := loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load sibling address: %w", err)
	}

	s.CurShardPfxLen = uint8(curShardPfxLen)
	s.AccSplitDepth = uint8(accSplitDepth)
	s.ThisAddr = thisAddr
	s.SiblingAddr = siblingAddr
	return nil
}

func (s SplitMergeInfo) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeSplitMergeInfo(builder, s); err != nil {
		return nil, err
	}
	return builder.EndCell(), nil
}

func storeSplitMergeInfo(builder *cell.Builder, s SplitMergeInfo) error {
	if err := builder.StoreUInt(uint64(s.CurShardPfxLen), 6); err != nil {
		return fmt.Errorf("failed to store current shard prefix len: %w", err)
	}
	if err := builder.StoreUInt(uint64(s.AccSplitDepth), 6); err != nil {
		return fmt.Errorf("failed to store account split depth: %w", err)
	}
	if err := builder.StoreSlice(s.ThisAddr, 256); err != nil {
		return fmt.Errorf("failed to store this address: %w", err)
	}
	if err := builder.StoreSlice(s.SiblingAddr, 256); err != nil {
		return fmt.Errorf("failed to store sibling address: %w", err)
	}
	return nil
}

func (d *TransactionDescriptionSplitPrepare) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load split prepare transaction magic: %w", err)
	}
	if magic != 0b0100 {
		return fmt.Errorf("invalid split prepare transaction magic %b", magic)
	}
	return d.loadFromCellAfterMagic(loader)
}

func (d *TransactionDescriptionSplitPrepare) loadFromCellAfterMagic(loader *cell.Slice) error {
	var err error
	if err = d.SplitInfo.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load split info: %w", err)
	}
	d.StoragePhase, err = loadMaybeStoragePhase(loader)
	if err != nil {
		return fmt.Errorf("failed to load storage phase: %w", err)
	}
	if err = d.ComputePhase.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load compute phase: %w", err)
	}
	d.ActionPhase, err = loadMaybeActionPhaseRef(loader)
	if err != nil {
		return fmt.Errorf("failed to load action phase: %w", err)
	}
	d.Aborted, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load aborted flag: %w", err)
	}
	d.Destroyed, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load destroyed flag: %w", err)
	}
	return nil
}

func (d TransactionDescriptionSplitPrepare) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := builder.StoreUInt(0b0100, 4); err != nil {
		return nil, fmt.Errorf("failed to store split prepare transaction magic: %w", err)
	}
	if err := storeSplitMergeInfo(builder, d.SplitInfo); err != nil {
		return nil, fmt.Errorf("failed to store split info: %w", err)
	}
	if err := storeMaybeStoragePhase(builder, d.StoragePhase); err != nil {
		return nil, fmt.Errorf("failed to store storage phase: %w", err)
	}
	if err := storeComputePhase(builder, d.ComputePhase); err != nil {
		return nil, fmt.Errorf("failed to store compute phase: %w", err)
	}
	if err := storeMaybeActionPhaseRef(builder, d.ActionPhase); err != nil {
		return nil, fmt.Errorf("failed to store action phase: %w", err)
	}
	if err := builder.StoreBoolBit(d.Aborted); err != nil {
		return nil, fmt.Errorf("failed to store aborted flag: %w", err)
	}
	if err := builder.StoreBoolBit(d.Destroyed); err != nil {
		return nil, fmt.Errorf("failed to store destroyed flag: %w", err)
	}
	return builder.EndCell(), nil
}

func (d *TransactionDescriptionSplitInstall) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load split install transaction magic: %w", err)
	}
	if magic != 0b0101 {
		return fmt.Errorf("invalid split install transaction magic %b", magic)
	}
	return d.loadFromCellAfterMagic(loader)
}

func (d *TransactionDescriptionSplitInstall) loadFromCellAfterMagic(loader *cell.Slice) error {
	if err := d.SplitInfo.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load split info: %w", err)
	}

	txRef, err := loader.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load prepare transaction ref: %w", err)
	}
	var tx Transaction
	if err = tx.LoadFromCell(txRef); err != nil {
		return fmt.Errorf("failed to load prepare transaction: %w", err)
	}
	installed, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load installed flag: %w", err)
	}

	d.PrepareTransaction = &tx
	d.Installed = installed
	return nil
}

func (d TransactionDescriptionSplitInstall) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := builder.StoreUInt(0b0101, 4); err != nil {
		return nil, fmt.Errorf("failed to store split install transaction magic: %w", err)
	}
	if err := storeSplitMergeInfo(builder, d.SplitInfo); err != nil {
		return nil, fmt.Errorf("failed to store split info: %w", err)
	}
	txCell, err := d.PrepareTransaction.ToCell()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize prepare transaction: %w", err)
	}
	if err = builder.StoreRef(txCell); err != nil {
		return nil, fmt.Errorf("failed to store prepare transaction ref: %w", err)
	}
	if err = builder.StoreBoolBit(d.Installed); err != nil {
		return nil, fmt.Errorf("failed to store installed flag: %w", err)
	}
	return builder.EndCell(), nil
}

func (d *TransactionDescriptionMergePrepare) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load merge prepare transaction magic: %w", err)
	}
	if magic != 0b0110 {
		return fmt.Errorf("invalid merge prepare transaction magic %b", magic)
	}
	return d.loadFromCellAfterMagic(loader)
}

func (d *TransactionDescriptionMergePrepare) loadFromCellAfterMagic(loader *cell.Slice) error {
	if err := d.SplitInfo.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load split info: %w", err)
	}
	if err := d.StoragePhase.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load storage phase: %w", err)
	}
	aborted, err := loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load aborted flag: %w", err)
	}
	d.Aborted = aborted
	return nil
}

func (d TransactionDescriptionMergePrepare) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := builder.StoreUInt(0b0110, 4); err != nil {
		return nil, fmt.Errorf("failed to store merge prepare transaction magic: %w", err)
	}
	if err := storeSplitMergeInfo(builder, d.SplitInfo); err != nil {
		return nil, fmt.Errorf("failed to store split info: %w", err)
	}
	if err := storeStoragePhase(builder, &d.StoragePhase); err != nil {
		return nil, fmt.Errorf("failed to store storage phase: %w", err)
	}
	if err := builder.StoreBoolBit(d.Aborted); err != nil {
		return nil, fmt.Errorf("failed to store aborted flag: %w", err)
	}
	return builder.EndCell(), nil
}

func (d *TransactionDescriptionMergeInstall) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load merge install transaction magic: %w", err)
	}
	if magic != 0b0111 {
		return fmt.Errorf("invalid merge install transaction magic %b", magic)
	}
	return d.loadFromCellAfterMagic(loader)
}

func (d *TransactionDescriptionMergeInstall) loadFromCellAfterMagic(loader *cell.Slice) error {
	var err error
	if err = d.SplitInfo.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load split info: %w", err)
	}

	txRef, err := loader.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load prepare transaction ref: %w", err)
	}
	var tx Transaction
	if err = tx.LoadFromCell(txRef); err != nil {
		return fmt.Errorf("failed to load prepare transaction: %w", err)
	}
	d.PrepareTransaction = &tx

	d.StoragePhase, err = loadMaybeStoragePhase(loader)
	if err != nil {
		return fmt.Errorf("failed to load storage phase: %w", err)
	}
	d.CreditPhase, err = loadMaybeCreditPhase(loader)
	if err != nil {
		return fmt.Errorf("failed to load credit phase: %w", err)
	}
	if err = d.ComputePhase.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load compute phase: %w", err)
	}
	d.ActionPhase, err = loadMaybeActionPhaseRef(loader)
	if err != nil {
		return fmt.Errorf("failed to load action phase: %w", err)
	}
	d.Aborted, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load aborted flag: %w", err)
	}
	d.Destroyed, err = loader.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load destroyed flag: %w", err)
	}
	return nil
}

func (d TransactionDescriptionMergeInstall) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := builder.StoreUInt(0b0111, 4); err != nil {
		return nil, fmt.Errorf("failed to store merge install transaction magic: %w", err)
	}
	if err := storeSplitMergeInfo(builder, d.SplitInfo); err != nil {
		return nil, fmt.Errorf("failed to store split info: %w", err)
	}
	txCell, err := d.PrepareTransaction.ToCell()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize prepare transaction: %w", err)
	}
	if err = builder.StoreRef(txCell); err != nil {
		return nil, fmt.Errorf("failed to store prepare transaction ref: %w", err)
	}
	if err := storeMaybeStoragePhase(builder, d.StoragePhase); err != nil {
		return nil, fmt.Errorf("failed to store storage phase: %w", err)
	}
	if err := storeMaybeCreditPhase(builder, d.CreditPhase); err != nil {
		return nil, fmt.Errorf("failed to store credit phase: %w", err)
	}
	if err := storeComputePhase(builder, d.ComputePhase); err != nil {
		return nil, fmt.Errorf("failed to store compute phase: %w", err)
	}
	if err := storeMaybeActionPhaseRef(builder, d.ActionPhase); err != nil {
		return nil, fmt.Errorf("failed to store action phase: %w", err)
	}
	if err := builder.StoreBoolBit(d.Aborted); err != nil {
		return nil, fmt.Errorf("failed to store aborted flag: %w", err)
	}
	if err := builder.StoreBoolBit(d.Destroyed); err != nil {
		return nil, fmt.Errorf("failed to store destroyed flag: %w", err)
	}
	return builder.EndCell(), nil
}

func loadTransactionDescription(loader *cell.Slice) (any, error) {
	pfx, err := loader.LoadUInt(3)
	if err != nil {
		return nil, fmt.Errorf("failed to load transaction description magic: %w", err)
	}

	switch pfx {
	case 0b000:
		isStorage, err := loader.LoadBoolBit()
		if err != nil {
			return nil, fmt.Errorf("failed to load transaction description storage flag: %w", err)
		}
		if isStorage {
			var desc TransactionDescriptionStorage
			if err = desc.loadFromCellAfterMagic(loader); err != nil {
				return nil, err
			}
			return desc, nil
		}
		var desc TransactionDescriptionOrdinary
		if err = desc.loadFromCellAfterMagic(loader); err != nil {
			return nil, err
		}
		return desc, nil
	case 0b001:
		var desc TransactionDescriptionTickTock
		if err = desc.loadFromCellAfterMagic(loader); err != nil {
			return nil, err
		}
		return desc, nil
	case 0b010:
		isInstall, err := loader.LoadBoolBit()
		if err != nil {
			return nil, fmt.Errorf("failed to load split transaction install flag: %w", err)
		}
		if isInstall {
			var desc TransactionDescriptionSplitInstall
			if err = desc.loadFromCellAfterMagic(loader); err != nil {
				return nil, err
			}
			return desc, nil
		}
		var desc TransactionDescriptionSplitPrepare
		if err = desc.loadFromCellAfterMagic(loader); err != nil {
			return nil, err
		}
		return desc, nil
	case 0b011:
		isInstall, err := loader.LoadBoolBit()
		if err != nil {
			return nil, fmt.Errorf("failed to load merge transaction install flag: %w", err)
		}
		if isInstall {
			var desc TransactionDescriptionMergeInstall
			if err = desc.loadFromCellAfterMagic(loader); err != nil {
				return nil, err
			}
			return desc, nil
		}
		var desc TransactionDescriptionMergePrepare
		if err = desc.loadFromCellAfterMagic(loader); err != nil {
			return nil, err
		}
		return desc, nil
	default:
		return nil, fmt.Errorf("unknown transaction description magic prefix %b", pfx)
	}
}

func storeTransactionDescription(builder *cell.Builder, desc any) error {
	var descCell *cell.Cell
	var err error

	switch d := desc.(type) {
	case TransactionDescriptionOrdinary:
		descCell, err = d.ToCell()
	case TransactionDescriptionStorage:
		descCell, err = d.ToCell()
	case TransactionDescriptionTickTock:
		descCell, err = d.ToCell()
	case TransactionDescriptionSplitPrepare:
		descCell, err = d.ToCell()
	case TransactionDescriptionSplitInstall:
		descCell, err = d.ToCell()
	case TransactionDescriptionMergePrepare:
		descCell, err = d.ToCell()
	case TransactionDescriptionMergeInstall:
		descCell, err = d.ToCell()
	default:
		// Compatibility fallback: Transaction.Description is public any and older code serialized any Marshaller.
		descCell, err = ToCell(desc)
	}
	if err != nil {
		return err
	}
	return builder.StoreRef(descCell)
}

func (t *Transaction) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load transaction magic: %w", err)
	}
	if magic != 0b0111 {
		return fmt.Errorf("invalid transaction magic %b", magic)
	}

	accountAddr, err := loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load account address: %w", err)
	}
	lt, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load lt: %w", err)
	}
	prevTxHash, err := loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load previous tx hash: %w", err)
	}
	prevTxLT, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load previous tx lt: %w", err)
	}
	now, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load now: %w", err)
	}
	outMsgCount, err := loader.LoadUInt(15)
	if err != nil {
		return fmt.Errorf("failed to load output message count: %w", err)
	}

	var origStatus AccountStatus
	if err = origStatus.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load original status: %w", err)
	}
	var endStatus AccountStatus
	if err = endStatus.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load end status: %w", err)
	}

	ioRef, err := loader.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load transaction io ref: %w", err)
	}
	var io TransactionIO
	if err = io.LoadFromCell(ioRef); err != nil {
		return fmt.Errorf("failed to load transaction io: %w", err)
	}

	var totalFees CurrencyCollection
	if err = totalFees.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load total fees: %w", err)
	}

	stateUpdateRef, err := loader.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load state update ref: %w", err)
	}
	var stateUpdate HashUpdate
	if err = stateUpdate.LoadFromCell(stateUpdateRef); err != nil {
		return fmt.Errorf("failed to load state update: %w", err)
	}

	descRef, err := loader.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load description ref: %w", err)
	}
	desc, err := loadTransactionDescription(descRef)
	if err != nil {
		return fmt.Errorf("failed to load description: %w", err)
	}

	t.AccountAddr = accountAddr
	t.LT = lt
	t.PrevTxHash = prevTxHash
	t.PrevTxLT = prevTxLT
	t.Now = uint32(now)
	t.OutMsgCount = uint16(outMsgCount)
	t.OrigStatus = origStatus
	t.EndStatus = endStatus
	t.IO.In = io.In
	t.IO.Out = io.Out
	t.TotalFees = totalFees
	t.StateUpdate = stateUpdate
	t.Description = desc
	return nil
}
