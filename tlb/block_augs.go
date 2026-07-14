// Package tlb: writable augmentation semantics for the block/state augmented
// dictionaries.
//
// Every augmentation below mirrors the corresponding C++ Aug_* implementation
// from the TON reference sources. Citations point into the local checkout at
// /Users/xssnick/dev/ton/ton (crypto/block/block-parse.{h,cpp} and
// crypto/block/block.tlb). The generic fork/empty behaviour of all
// AugmentationCheckData subclasses is
//
//	eval_fork  = extra_type.add_values (block-parse.h:223-225)
//	eval_empty = extra_type.null_value (block-parse.h:226-228)
//
// so per-augmentation only eval_leaf (and explicit overrides) differ.
package tlb

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// ImportFees is the import_fees$_ TLB type (crypto/block/block.tlb:186-187):
//
//	import_fees$_ fees_collected:Grams value_imported:CurrencyCollection = ImportFees;
//
// It is the augmentation extra of InMsgDescr (HashmapAugE 256 InMsg ImportFees).
type ImportFees struct {
	FeesCollected Coins              `tlb:"."`
	ValueImported CurrencyCollection `tlb:"."`
}

func (f *ImportFees) LoadFromCell(loader *cell.Slice) error {
	fees, err := loadCoins(loader)
	if err != nil {
		return fmt.Errorf("failed to load fees collected: %w", err)
	}

	var imported CurrencyCollection
	if err = imported.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load value imported: %w", err)
	}

	f.FeesCollected = fees
	f.ValueImported = imported
	return nil
}

func (f ImportFees) ToCell() (*cell.Cell, error) {
	builder := cell.BeginCell()
	if err := storeCoins(builder, f.FeesCollected); err != nil {
		return nil, fmt.Errorf("failed to store fees collected: %w", err)
	}
	if err := storeCurrencyCollection(builder, f.ValueImported); err != nil {
		return nil, fmt.Errorf("failed to store value imported: %w", err)
	}
	return builder.EndCell(), nil
}

// ===========================================================================
// shared low-level helpers
// ===========================================================================

// rawGrams captures a Grams (VarUInteger 16) field bit-exactly: 4-bit byte
// length plus the value bytes. It allows re-appending the original bits the
// same way C++ cb.append_cellslice_bool does, while also exposing the numeric
// value. Chain-validated data is always canonically encoded
// (VarUInteger::validate_skip requires a non-zero leading byte,
// block-parse.cpp:303-306), so raw and canonical encodings coincide for it.
type rawGrams struct {
	ln   uint64
	data []byte
	val  *big.Int
}

func loadRawGrams(loader *cell.Slice) (rawGrams, error) {
	ln, err := loader.LoadUInt(4)
	if err != nil {
		return rawGrams{}, fmt.Errorf("failed to load grams length: %w", err)
	}
	if ln == 0 {
		return rawGrams{ln: 0, val: big.NewInt(0)}, nil
	}
	data, err := loader.LoadSlice(uint(ln) * 8)
	if err != nil {
		return rawGrams{}, fmt.Errorf("failed to load grams value: %w", err)
	}
	return rawGrams{ln: ln, data: data, val: new(big.Int).SetBytes(data)}, nil
}

func (g rawGrams) appendTo(b *cell.Builder) error {
	if err := b.StoreUInt(g.ln, 4); err != nil {
		return err
	}
	if g.ln == 0 {
		return nil
	}
	return b.StoreSlice(g.data, uint(g.ln)*8)
}

// rawExtraDict captures the ExtraCurrencyCollection part of a
// CurrencyCollection bit-exactly: HashmapE root presence bit plus the optional
// root ref.
type rawExtraDict struct {
	present bool
	root    *cell.Cell
}

func loadRawExtraDict(loader *cell.Slice) (rawExtraDict, error) {
	present, err := loader.LoadBoolBit()
	if err != nil {
		return rawExtraDict{}, fmt.Errorf("failed to load extra currencies flag: %w", err)
	}
	if !present {
		return rawExtraDict{}, nil
	}
	root, err := loader.LoadRefCell()
	if err != nil {
		return rawExtraDict{}, fmt.Errorf("failed to load extra currencies root: %w", err)
	}
	return rawExtraDict{present: true, root: root}, nil
}

func (d rawExtraDict) appendTo(b *cell.Builder) error {
	if err := b.StoreBoolBit(d.present); err != nil {
		return err
	}
	if !d.present {
		return nil
	}
	return b.StoreRef(d.root)
}

func (d rawExtraDict) dictionary() *cell.Dictionary {
	if !d.present {
		return nil
	}
	return d.root.AsDict(32)
}

// storeCanonicalGrams stores a Grams value canonically, mirroring
// VarUInteger::store_integer_value (block-parse.cpp:319-322): minimal byte
// length, used by C++ t_Grams.store_integer_ref / add_values.
func storeCanonicalGrams(b *cell.Builder, v *big.Int) error {
	if v == nil {
		return fmt.Errorf("grams value is nil")
	}
	if v.Sign() < 0 {
		return fmt.Errorf("grams value is negative")
	}
	return b.StoreBigVarUInt(v, 16)
}

// addCurrencyCollectionSlices consumes one CurrencyCollection from each slice
// and appends their sum, mirroring CurrencyCollection::add_values
// (block-parse.cpp:619-621): the grams sum is stored canonically
// (tlblib.hpp:217-220) and extra currency dictionaries are merged with
// per-entry VarUIntegerPos addition (HashmapE::add_values,
// block-parse.cpp:514-527); subtrees unique to one side are reused as is.
func addCurrencyCollectionSlices(b *cell.Builder, left, right *cell.Slice) error {
	lg, err := left.LoadBigCoins()
	if err != nil {
		return fmt.Errorf("failed to load left grams: %w", err)
	}
	ld, err := left.LoadDict(32)
	if err != nil {
		return fmt.Errorf("failed to load left extra currencies: %w", err)
	}
	rg, err := right.LoadBigCoins()
	if err != nil {
		return fmt.Errorf("failed to load right grams: %w", err)
	}
	rd, err := right.LoadDict(32)
	if err != nil {
		return fmt.Errorf("failed to load right extra currencies: %w", err)
	}

	if err = storeCanonicalGrams(b, new(big.Int).Add(lg, rg)); err != nil {
		return fmt.Errorf("failed to store grams sum: %w", err)
	}

	extra, err := addExtraCurrencyDicts(ld, rd)
	if err != nil {
		return fmt.Errorf("failed to merge extra currencies: %w", err)
	}
	if err = b.StoreDict(extra); err != nil {
		return fmt.Errorf("failed to store extra currencies: %w", err)
	}
	return nil
}

// storeEmptyCurrencyCollection appends the CurrencyCollection null value:
// zero grams (4 zero bits) plus an empty extra dict (1 zero bit), as stored by
// C++ t_CurrencyCollection.null_value (Grams null 4 bits + HashmapE null 1 bit,
// see ImportFees::null_value storing 4+4+1 zero bits, block-parse.h:797-799).
func storeEmptyCurrencyCollection(b *cell.Builder) error {
	return b.StoreUInt(0, 5)
}

// ===========================================================================
// message helpers shared by InMsgDescr / OutMsgDescr / OutMsgQueue leaves
// ===========================================================================

// skipMaybeAnycast skips Maybe Anycast and returns the anycast rewrite depth
// (0 when absent), per Maybe_Anycast::skip_get_depth (block-parse.cpp:75-79)
// and Anycast::skip_get_depth (block-parse.h:43-45): depth:(#<= 30) is a 5-bit
// integer <= 30 followed by depth rewrite bits.
func skipMaybeAnycast(s *cell.Slice) (uint64, error) {
	present, err := s.LoadBoolBit()
	if err != nil {
		return 0, fmt.Errorf("failed to load anycast flag: %w", err)
	}
	if !present {
		return 0, nil
	}
	depth, err := s.LoadUInt(5)
	if err != nil {
		return 0, fmt.Errorf("failed to load anycast depth: %w", err)
	}
	if depth > 30 {
		return 0, fmt.Errorf("anycast depth %d is above 30", depth)
	}
	if _, err = s.LoadSlice(uint(depth)); err != nil {
		return 0, fmt.Errorf("failed to skip anycast rewrite prefix: %w", err)
	}
	return depth, nil
}

// skipMsgAddressIntGetDepth skips MsgAddressInt and returns the anycast depth,
// mirroring MsgAddressInt::skip_get_depth (block-parse.cpp:101-115).
func skipMsgAddressIntGetDepth(s *cell.Slice) (uint64, error) {
	tag, err := s.LoadUInt(2)
	if err != nil {
		return 0, fmt.Errorf("failed to load address tag: %w", err)
	}
	switch tag {
	case 0b10: // addr_std$10
		depth, err := skipMaybeAnycast(s)
		if err != nil {
			return 0, err
		}
		if _, err = s.LoadSlice(8 + 256); err != nil {
			return 0, fmt.Errorf("failed to skip std address body: %w", err)
		}
		return depth, nil
	case 0b11: // addr_var$11
		depth, err := skipMaybeAnycast(s)
		if err != nil {
			return 0, err
		}
		addrLen, err := s.LoadUInt(9)
		if err != nil {
			return 0, fmt.Errorf("failed to load var address length: %w", err)
		}
		if _, err = s.LoadSlice(32 + uint(addrLen)); err != nil {
			return 0, fmt.Errorf("failed to skip var address body: %w", err)
		}
		return depth, nil
	default:
		return 0, fmt.Errorf("invalid internal address tag %b", tag)
	}
}

func skipMsgAddressInt(s *cell.Slice) error {
	_, err := skipMsgAddressIntGetDepth(s)
	return err
}

// skipMsgAddressExt mirrors MsgAddressExt (block-parse.cpp:59-70):
// addr_none$00 or addr_extern$01 len:(## 9) external_address:(bits len).
func skipMsgAddressExt(s *cell.Slice) error {
	tag, err := s.LoadUInt(2)
	if err != nil {
		return fmt.Errorf("failed to load external address tag: %w", err)
	}
	switch tag {
	case 0b00:
		return nil
	case 0b01:
		ln, err := s.LoadUInt(9)
		if err != nil {
			return fmt.Errorf("failed to load external address length: %w", err)
		}
		if _, err = s.LoadSlice(uint(ln)); err != nil {
			return fmt.Errorf("failed to skip external address: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("invalid external address tag %b", tag)
	}
}

// intMsgInfoView is the part of int_msg_info$0 needed by the augmentation
// leaves, per CommonMsgInfo::unpack for Record_int_msg_info
// (block-parse.cpp:684-690).
type intMsgInfoView struct {
	valueGrams *big.Int     // value:CurrencyCollection grams part
	valueExtra rawExtraDict // value:CurrencyCollection extra part, raw
	ihrFee     rawGrams
	createdLT  uint64
}

func parseIntMsgInfoView(msg *cell.Cell) (intMsgInfoView, error) {
	s, err := msg.BeginParse()
	if err != nil {
		return intMsgInfoView{}, fmt.Errorf("failed to parse message: %w", err)
	}

	flags, err := s.LoadUInt(4) // int_msg_info$0 ihr_disabled:Bool bounce:Bool bounced:Bool
	if err != nil {
		return intMsgInfoView{}, fmt.Errorf("failed to load message flags: %w", err)
	}
	if flags&0b1000 != 0 {
		return intMsgInfoView{}, fmt.Errorf("message is not int_msg_info")
	}

	if err = skipMsgAddressInt(s); err != nil {
		return intMsgInfoView{}, fmt.Errorf("failed to skip src address: %w", err)
	}
	if err = skipMsgAddressInt(s); err != nil {
		return intMsgInfoView{}, fmt.Errorf("failed to skip dest address: %w", err)
	}

	valueGrams, err := loadRawGrams(s)
	if err != nil {
		return intMsgInfoView{}, fmt.Errorf("failed to load message value grams: %w", err)
	}
	valueExtra, err := loadRawExtraDict(s)
	if err != nil {
		return intMsgInfoView{}, fmt.Errorf("failed to load message value extra: %w", err)
	}
	ihrFee, err := loadRawGrams(s)
	if err != nil {
		return intMsgInfoView{}, fmt.Errorf("failed to load ihr fee: %w", err)
	}
	if _, err = loadRawGrams(s); err != nil { // fwd_fee
		return intMsgInfoView{}, fmt.Errorf("failed to load fwd fee: %w", err)
	}
	createdLT, err := s.LoadUInt(64)
	if err != nil {
		return intMsgInfoView{}, fmt.Errorf("failed to load created lt: %w", err)
	}

	return intMsgInfoView{
		valueGrams: valueGrams.val,
		valueExtra: valueExtra,
		ihrFee:     ihrFee,
		createdLT:  createdLT,
	}, nil
}

// messageCreatedLT mirrors CommonMsgInfo::get_created_lt
// (block-parse.cpp:714-737): defined for int_msg_info$0 and ext_out_msg_info$11,
// fails for ext_in_msg_info$10.
func messageCreatedLT(msg *cell.Cell) (uint64, error) {
	s, err := msg.BeginParse()
	if err != nil {
		return 0, fmt.Errorf("failed to parse message: %w", err)
	}

	isExt, err := s.LoadBoolBit()
	if err != nil {
		return 0, fmt.Errorf("failed to load message tag: %w", err)
	}
	if !isExt { // int_msg_info$0
		if _, err = s.LoadUInt(3); err != nil { // ihr_disabled, bounce, bounced
			return 0, fmt.Errorf("failed to load message flags: %w", err)
		}
		if err = skipMsgAddressInt(s); err != nil {
			return 0, fmt.Errorf("failed to skip src address: %w", err)
		}
		if err = skipMsgAddressInt(s); err != nil {
			return 0, fmt.Errorf("failed to skip dest address: %w", err)
		}
		if err = skipCurrencyCollectionBoundary(s); err != nil {
			return 0, fmt.Errorf("failed to skip value: %w", err)
		}
		if _, err = s.LoadBigCoins(); err != nil { // ihr_fee
			return 0, fmt.Errorf("failed to skip ihr fee: %w", err)
		}
		if _, err = s.LoadBigCoins(); err != nil { // fwd_fee
			return 0, fmt.Errorf("failed to skip fwd fee: %w", err)
		}
		return s.LoadUInt(64)
	}

	isOut, err := s.LoadBoolBit()
	if err != nil {
		return 0, fmt.Errorf("failed to load message tag: %w", err)
	}
	if !isOut { // ext_in_msg_info$10 has no created_lt
		return 0, fmt.Errorf("external inbound message has no created lt")
	}
	// ext_out_msg_info$11
	if err = skipMsgAddressInt(s); err != nil {
		return 0, fmt.Errorf("failed to skip src address: %w", err)
	}
	if err = skipMsgAddressExt(s); err != nil {
		return 0, fmt.Errorf("failed to skip dest address: %w", err)
	}
	return s.LoadUInt(64)
}

// msgEnvelopeView is the part of a MsgEnvelope needed by augmentation leaves,
// per MsgEnvelope::unpack (block-parse.cpp:832-839). Both msg_envelope#4
// (crypto/block/block.tlb:167-169) and the current upstream msg_envelope_v2#5
// (not present in the local block.tlb; layout matches this package's
// MsgEnvelope parser) are accepted.
type msgEnvelopeView struct {
	fwdFeeRemaining rawGrams
	msg             *cell.Cell
	emittedLT       *uint64 // msg_envelope_v2 only
}

func parseMsgEnvelopeView(env *cell.Cell) (msgEnvelopeView, error) {
	s, err := env.BeginParse()
	if err != nil {
		return msgEnvelopeView{}, fmt.Errorf("failed to parse message envelope: %w", err)
	}

	tag, err := s.LoadUInt(4)
	if err != nil {
		return msgEnvelopeView{}, fmt.Errorf("failed to load message envelope tag: %w", err)
	}
	if tag != 4 && tag != 5 {
		return msgEnvelopeView{}, fmt.Errorf("unsupported message envelope tag %d", tag)
	}

	if err = skipIntermediateAddress(s); err != nil {
		return msgEnvelopeView{}, fmt.Errorf("failed to skip current intermediate address: %w", err)
	}
	if err = skipIntermediateAddress(s); err != nil {
		return msgEnvelopeView{}, fmt.Errorf("failed to skip next intermediate address: %w", err)
	}

	fwdFeeRemaining, err := loadRawGrams(s)
	if err != nil {
		return msgEnvelopeView{}, fmt.Errorf("failed to load remaining forward fee: %w", err)
	}

	msg, err := s.LoadRefCell()
	if err != nil {
		return msgEnvelopeView{}, fmt.Errorf("failed to load message ref: %w", err)
	}

	view := msgEnvelopeView{fwdFeeRemaining: fwdFeeRemaining, msg: msg}
	if tag == 4 {
		return view, nil
	}

	hasEmittedLT, err := s.LoadBoolBit()
	if err != nil {
		return msgEnvelopeView{}, fmt.Errorf("failed to load emitted lt flag: %w", err)
	}
	if hasEmittedLT {
		emittedLT, err := s.LoadUInt(64)
		if err != nil {
			return msgEnvelopeView{}, fmt.Errorf("failed to load emitted lt: %w", err)
		}
		view.emittedLT = &emittedLT
	}
	return view, nil
}

// ===========================================================================
// AugShardAccounts: HashmapAugE 256 ShardAccount DepthBalanceInfo
// ===========================================================================

// AugShardAccounts implements the augmentation of ShardAccounts
// (crypto/block/block.tlb:263: _ (HashmapAugE 256 ShardAccount DepthBalanceInfo)).
//
// Leaf rule (Aug_ShardAccounts::eval_leaf, block-parse.cpp:1161-1169 ->
// Account::skip_copy_depth_balance, block-parse.cpp:987-999): the extra of a
// ShardAccount leaf is depth_balance$_ split_depth:(#<= 30)
// balance:CurrencyCollection where split_depth is the anycast rewrite depth of
// the account address (0 without anycast) and balance is the account balance
// copied bit-exactly; account_none yields the DepthBalanceInfo null value.
//
// Fork rule (DepthBalanceInfo::add_values, block-parse.cpp:1153-1157):
// split_depth = max(left, right), balance = left + right.
//
// Empty value (DepthBalanceInfo::null_value, block-parse.cpp:1148-1150):
// zero split_depth and zero balance (10 zero bits).
type AugShardAccounts struct{}

func (AugShardAccounts) SkipExtra(loader *cell.Slice) error {
	return skipDepthBalanceInfoBoundary(loader)
}

func (AugShardAccounts) EmptyExtra() (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := b.StoreUInt(0, 5); err != nil { // split_depth:(#<= 30)
		return nil, err
	}
	if err := storeEmptyCurrencyCollection(b); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

func (AugShardAccounts) LeafExtra(value *cell.Slice) (*cell.Cell, error) {
	v := value.Copy()
	// ShardAccount: account_descr$_ account:^Account last_trans_hash:bits256
	// last_trans_lt:uint64 (block.tlb:259-260); eval_leaf prefetches the account
	// ref without touching the bits (block-parse.cpp:1162-1164).
	account, err := v.PeekRefCell()
	if err != nil {
		return nil, fmt.Errorf("failed to load account ref of shard account: %w", err)
	}

	s, err := account.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse account: %w", err)
	}

	isAccount, err := s.LoadBoolBit()
	if err != nil {
		return nil, fmt.Errorf("failed to load account tag: %w", err)
	}

	b := cell.BeginCell()
	if !isAccount {
		// account_none$0 -> DepthBalanceInfo null value (block-parse.cpp:990-991).
		if err = b.StoreUInt(0, 5); err != nil {
			return nil, err
		}
		if err = storeEmptyCurrencyCollection(b); err != nil {
			return nil, err
		}
		return b.EndCell(), nil
	}

	// account$1 addr:MsgAddressInt storage_stat:StorageInfo storage:AccountStorage
	depth, err := skipMsgAddressIntGetDepth(s)
	if err != nil {
		return nil, fmt.Errorf("failed to read account address: %w", err)
	}
	if err = b.StoreUInt(depth, 5); err != nil { // split_depth:(#<= 30)
		return nil, err
	}

	// storage_stat:StorageInfo. The local C++ checkout predates the current
	// StorageInfo layout; the skip below follows the schema this package parses
	// (StorageUsed cells/bits as VarUInteger 7, StorageExtraInfo, last_paid,
	// Maybe due_payment), matching current mainnet blocks, cf. tlb/account.go
	// StorageInfo.
	if _, err = loadVarUInt(s, 7); err != nil {
		return nil, fmt.Errorf("failed to skip storage cells used: %w", err)
	}
	if _, err = loadVarUInt(s, 7); err != nil {
		return nil, fmt.Errorf("failed to skip storage bits used: %w", err)
	}
	storageExtraTag, err := s.LoadUInt(3)
	if err != nil {
		return nil, fmt.Errorf("failed to load storage extra tag: %w", err)
	}
	switch storageExtraTag {
	case 0b000: // storage_extra_none$000
	case 0b001: // storage_extra_info$001 dict_hash:uint256
		if _, err = s.LoadSlice(256); err != nil {
			return nil, fmt.Errorf("failed to skip storage extra dict hash: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown storage extra tag %b", storageExtraTag)
	}
	if _, err = s.LoadUInt(32); err != nil { // last_paid
		return nil, fmt.Errorf("failed to skip storage last paid: %w", err)
	}
	hasDue, err := s.LoadBoolBit()
	if err != nil {
		return nil, fmt.Errorf("failed to load due payment flag: %w", err)
	}
	if hasDue {
		if _, err = s.LoadBigCoins(); err != nil {
			return nil, fmt.Errorf("failed to skip due payment: %w", err)
		}
	}

	// storage:AccountStorage = last_trans_lt:uint64 balance:CurrencyCollection
	// state:AccountState; the balance is copied bit-exactly
	// (AccountStorage::skip_copy_balance, block-parse.cpp:937-939).
	if _, err = s.LoadUInt(64); err != nil {
		return nil, fmt.Errorf("failed to skip last transaction lt: %w", err)
	}
	balanceGrams, err := loadRawGrams(s)
	if err != nil {
		return nil, fmt.Errorf("failed to load account balance grams: %w", err)
	}
	balanceExtra, err := loadRawExtraDict(s)
	if err != nil {
		return nil, fmt.Errorf("failed to load account balance extra: %w", err)
	}
	if err = balanceGrams.appendTo(b); err != nil {
		return nil, err
	}
	if err = balanceExtra.appendTo(b); err != nil {
		return nil, err
	}

	// state:AccountState is skipped for structural validation only, mirroring
	// t_AccountState.skip at the end of skip_copy_balance.
	if err = skipAccountState(s); err != nil {
		return nil, fmt.Errorf("failed to skip account state: %w", err)
	}

	return b.EndCell(), nil
}

func (AugShardAccounts) CombineExtra(leftExtra, rightExtra *cell.Slice) (*cell.Cell, error) {
	// DepthBalanceInfo::add_values (block-parse.cpp:1153-1157).
	d1, err := leftExtra.LoadUInt(5)
	if err != nil {
		return nil, fmt.Errorf("failed to load left split depth: %w", err)
	}
	d2, err := rightExtra.LoadUInt(5)
	if err != nil {
		return nil, fmt.Errorf("failed to load right split depth: %w", err)
	}
	if d1 > 30 || d2 > 30 {
		return nil, fmt.Errorf("split depth above 30")
	}

	b := cell.BeginCell()
	depth := d1
	if d2 > depth {
		depth = d2
	}
	if err = b.StoreUInt(depth, 5); err != nil {
		return nil, err
	}
	if err = addCurrencyCollectionSlices(b, leftExtra, rightExtra); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

// skipAccountState skips AccountState (block.tlb:265-268):
// account_uninit$00, account_active$1 _:StateInit, account_frozen$01
// state_hash:bits256.
func skipAccountState(s *cell.Slice) error {
	active, err := s.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load account state tag: %w", err)
	}
	if active { // account_active$1 _:StateInit
		return skipStateInit(s)
	}
	frozen, err := s.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load account state tag: %w", err)
	}
	if frozen { // account_frozen$01 state_hash:bits256
		if _, err = s.LoadSlice(256); err != nil {
			return fmt.Errorf("failed to skip frozen state hash: %w", err)
		}
	}
	return nil // account_uninit$00
}

// skipStateInit skips StateInit (block.tlb:141-143): split_depth:(Maybe (## 5))
// special:(Maybe TickTock) code:(Maybe ^Cell) data:(Maybe ^Cell) plus the
// library collection (1 presence bit + optional root ref).
func skipStateInit(s *cell.Slice) error {
	hasSplitDepth, err := s.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load split depth flag: %w", err)
	}
	if hasSplitDepth {
		if _, err = s.LoadUInt(5); err != nil {
			return fmt.Errorf("failed to skip split depth: %w", err)
		}
	}
	hasSpecial, err := s.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load tick tock flag: %w", err)
	}
	if hasSpecial {
		if _, err = s.LoadUInt(2); err != nil {
			return fmt.Errorf("failed to skip tick tock: %w", err)
		}
	}
	for i, name := range []string{"code", "data", "library"} {
		has, err := s.LoadBoolBit()
		if err != nil {
			return fmt.Errorf("failed to load %s flag: %w", name, err)
		}
		if has {
			if _, err = s.LoadRefCell(); err != nil {
				return fmt.Errorf("failed to skip %s ref %d: %w", name, i, err)
			}
		}
	}
	return nil
}

// ===========================================================================
// AugShardAccountBlocks: HashmapAugE 256 AccountBlock CurrencyCollection
// ===========================================================================

// AugShardAccountBlocks implements the augmentation of ShardAccountBlocks
// (block.tlb:341: _ (HashmapAugE 256 AccountBlock CurrencyCollection)).
//
// Leaf rule (Aug_ShardAccountBlocks::eval_leaf, block-parse.cpp:1634-1637 ->
// AccountBlock::get_total_fees, block-parse.cpp:1628-1632): skip acc_trans#5
// and account_addr, then extract the root extra of the inner
// (HashmapAug 64 ^Transaction CurrencyCollection) dictionary and re-store it
// canonically; i.e. the extra of an AccountBlock is the total fees of all its
// transactions.
//
// Fork rule: CurrencyCollection::add_values (block-parse.cpp:619-621).
// Empty value: CurrencyCollection null (5 zero bits).
type AugShardAccountBlocks struct{}

func (AugShardAccountBlocks) SkipExtra(loader *cell.Slice) error {
	return skipCurrencyCollectionBoundary(loader)
}

func (AugShardAccountBlocks) EmptyExtra() (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := storeEmptyCurrencyCollection(b); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

func (AugShardAccountBlocks) LeafExtra(value *cell.Slice) (*cell.Cell, error) {
	v := value.Copy()
	// acc_trans#5 account_addr:bits256, advanced blindly like C++
	// get_total_fees (cs.advance(4 + 256), block-parse.cpp:1629).
	if _, err := v.LoadUInt(4); err != nil {
		return nil, fmt.Errorf("failed to skip account block tag: %w", err)
	}
	if _, err := v.LoadSlice(256); err != nil {
		return nil, fmt.Errorf("failed to skip account block address: %w", err)
	}

	// transactions:(HashmapAug 64 ^Transaction CurrencyCollection); extract the
	// root node extra (HashmapAug::extract_extra, block-parse.cpp:1100-1103).
	dict, err := v.ToAugDictWithValueAndAugmentation(64, AugAccountTransactions{}, skipAugRefValue)
	if err != nil {
		return nil, fmt.Errorf("failed to load account transactions dict: %w", err)
	}
	rootExtra, err := dict.LoadRootExtra()
	if err != nil {
		return nil, fmt.Errorf("failed to extract account transactions root extra: %w", err)
	}

	// total_fees.fetch + store canonically re-encodes the CurrencyCollection
	// (block-parse.cpp:1631-1636).
	return canonicalCurrencyCollectionFromSlice(rootExtra)
}

func (AugShardAccountBlocks) CombineExtra(leftExtra, rightExtra *cell.Slice) (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := addCurrencyCollectionSlices(b, leftExtra, rightExtra); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

// canonicalCurrencyCollectionFromSlice re-encodes a CurrencyCollection read
// from the slice: canonical grams plus the original extra dictionary root,
// which is what C++ block::CurrencyCollection fetch+store does
// (block.cpp:1289-1299).
func canonicalCurrencyCollectionFromSlice(s *cell.Slice) (*cell.Cell, error) {
	grams, err := s.LoadBigCoins()
	if err != nil {
		return nil, fmt.Errorf("failed to load grams: %w", err)
	}
	extra, err := loadRawExtraDict(s)
	if err != nil {
		return nil, fmt.Errorf("failed to load extra currencies: %w", err)
	}

	b := cell.BeginCell()
	if err = storeCanonicalGrams(b, grams); err != nil {
		return nil, err
	}
	if err = extra.appendTo(b); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

// ===========================================================================
// AugAccountTransactions: HashmapAug 64 ^Transaction CurrencyCollection
// ===========================================================================

// AugAccountTransactions implements the augmentation of the transaction
// dictionary inside an AccountBlock (block.tlb:337-339).
//
// Leaf rule (Aug_AccountTransactions::eval_leaf, block-parse.cpp:1599-1604 ->
// Transaction::get_total_fees, block-parse.cpp:1583-1594): the extra of a
// ^Transaction leaf is the transaction's total_fees:CurrencyCollection,
// canonically re-encoded.
//
// Fork rule: CurrencyCollection::add_values (block-parse.cpp:619-621).
// Empty value: CurrencyCollection null (5 zero bits).
type AugAccountTransactions struct{}

func (AugAccountTransactions) SkipExtra(loader *cell.Slice) error {
	return skipCurrencyCollectionBoundary(loader)
}

func (AugAccountTransactions) EmptyExtra() (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := storeEmptyCurrencyCollection(b); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

func (AugAccountTransactions) LeafExtra(value *cell.Slice) (*cell.Cell, error) {
	v := value.Copy()
	tx, err := v.PeekRefCell() // value is ^Transaction, prefetched like C++ (block-parse.cpp:1600)
	if err != nil {
		return nil, fmt.Errorf("failed to load transaction ref: %w", err)
	}

	s, err := tx.BeginParse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction: %w", err)
	}

	// Transaction::get_total_fees (block-parse.cpp:1583-1594).
	tag, err := s.LoadUInt(4)
	if err != nil {
		return nil, fmt.Errorf("failed to load transaction tag: %w", err)
	}
	if tag != 0b0111 {
		return nil, fmt.Errorf("invalid transaction tag %b", tag)
	}
	// account_addr:bits256 lt:uint64 prev_trans_hash:bits256 prev_trans_lt:uint64
	// now:uint32 outmsg_cnt:uint15
	if _, err = s.LoadSlice(256 + 64 + 256 + 64 + 32 + 15); err != nil {
		return nil, fmt.Errorf("failed to skip transaction header: %w", err)
	}
	// orig_status:AccountStatus end_status:AccountStatus (2 bits each)
	if _, err = s.LoadUInt(4); err != nil {
		return nil, fmt.Errorf("failed to skip account statuses: %w", err)
	}
	// ^[ in_msg:(Maybe ^(Message Any)) out_msgs:(HashmapE 15 ^(Message Any)) ]
	if _, err = s.LoadRefCell(); err != nil {
		return nil, fmt.Errorf("failed to skip transaction io ref: %w", err)
	}

	return canonicalCurrencyCollectionFromSlice(s)
}

func (AugAccountTransactions) CombineExtra(leftExtra, rightExtra *cell.Slice) (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := addCurrencyCollectionSlices(b, leftExtra, rightExtra); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

// ===========================================================================
// AugInMsgDescr: HashmapAugE 256 InMsg ImportFees
// ===========================================================================

// AugInMsgDescr implements the augmentation of InMsgDescr
// (block.tlb:188: _ (HashmapAugE 256 InMsg ImportFees)).
//
// Leaf rule (Aug_InMsgDescr::eval_leaf, block-parse.h:841-848 ->
// InMsg::get_import_fees, block-parse.cpp:1741-1835): C++ DERIVES the
// ImportFees from the InMsg itself (message headers, envelope fees and the fee
// fields of the InMsg record) rather than trusting a stored extra, so this
// implementation performs the same derivation; callers only Set the InMsg
// value and the extra is computed. The per-variant rules are documented inline.
//
// Fork rule: ImportFees::add_values (block-parse.cpp:1650-1652), i.e.
// fees_collected added as Grams and value_imported added as CurrencyCollection.
// Empty value: ImportFees::null_value (block-parse.h:797-799), 4+4+1 zero bits.
type AugInMsgDescr struct{}

func (AugInMsgDescr) SkipExtra(loader *cell.Slice) error {
	if _, err := loader.LoadBigCoins(); err != nil { // fees_collected:Grams
		return err
	}
	return skipCurrencyCollectionBoundary(loader) // value_imported:CurrencyCollection
}

func (AugInMsgDescr) EmptyExtra() (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := b.StoreUInt(0, 4); err != nil { // fees_collected: zero Grams
		return nil, err
	}
	if err := storeEmptyCurrencyCollection(b); err != nil { // value_imported: zero
		return nil, err
	}
	return b.EndCell(), nil
}

func (AugInMsgDescr) LeafExtra(value *cell.Slice) (*cell.Cell, error) {
	v := value.Copy()
	tag, err := v.LoadUInt(3)
	if err != nil {
		return nil, fmt.Errorf("failed to load in msg tag: %w", err)
	}
	if tag == 0b001 {
		// current upstream 5-bit variants msg_import_deferred_fin$00100 and
		// msg_import_deferred_tr$00101 (dispatch queue feature; not present in
		// the local ton checkout's block.tlb). Their import fees derivation is
		// validated bit-exactly against mainnet InMsgDescr data in tests.
		rest, err := v.LoadUInt(2)
		if err != nil {
			return nil, fmt.Errorf("failed to load deferred in msg tag: %w", err)
		}
		return augInMsgDescrDeferredLeaf(v, rest)
	}

	b := cell.BeginCell()
	switch tag {
	case 0b000: // msg_import_ext: no value and no import fees (block-parse.cpp:1743-1744)
		if err = b.StoreUInt(0, 4); err != nil {
			return nil, err
		}
		if err = storeEmptyCurrencyCollection(b); err != nil {
			return nil, err
		}
		return b.EndCell(), nil

	case 0b010: // msg_import_ihr (block-parse.cpp:1745-1759)
		// msg:^(Message Any) transaction:^Transaction ihr_fee:Grams proof_created:^Cell
		if v.RefsNum() < 3 {
			return nil, fmt.Errorf("msg_import_ihr must have 3 refs")
		}
		msg, err := v.LoadRefCell()
		if err != nil {
			return nil, err
		}
		info, err := parseIntMsgInfoView(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse imported message: %w", err)
		}
		if _, err = v.LoadRefCell(); err != nil { // transaction
			return nil, err
		}
		ihrFee, err := v.LoadBigCoins()
		if err != nil {
			return nil, fmt.Errorf("failed to load ihr fee: %w", err)
		}
		if _, err = v.LoadRefCell(); err != nil { // proof_created
			return nil, err
		}
		if ihrFee.Cmp(info.ihrFee.val) != 0 {
			return nil, fmt.Errorf("in msg ihr fee %s does not match message ihr fee %s", ihrFee, info.ihrFee.val)
		}
		// fees_collected := ihr_fee (original bits)
		if err = info.ihrFee.appendTo(b); err != nil {
			return nil, err
		}
		// value_imported := ihr_fee + msg.value (canonical sum, message extra dict reused)
		if err = storeCanonicalGrams(b, new(big.Int).Add(info.ihrFee.val, info.valueGrams)); err != nil {
			return nil, err
		}
		if err = info.valueExtra.appendTo(b); err != nil {
			return nil, err
		}
		return b.EndCell(), nil

	case 0b011: // msg_import_imm (block-parse.cpp:1760-1766)
		// in_msg:^MsgEnvelope transaction:^Transaction fwd_fee:Grams
		if v.RefsNum() < 2 {
			return nil, fmt.Errorf("msg_import_imm must have 2 refs")
		}
		if _, err = v.LoadRefCell(); err != nil {
			return nil, err
		}
		if _, err = v.LoadRefCell(); err != nil {
			return nil, err
		}
		fwdFee, err := loadRawGrams(v)
		if err != nil {
			return nil, fmt.Errorf("failed to load fwd fee: %w", err)
		}
		// fees_collected := fwd_fee (original bits); value_imported := 0
		if err = fwdFee.appendTo(b); err != nil {
			return nil, err
		}
		if err = storeEmptyCurrencyCollection(b); err != nil {
			return nil, err
		}
		return b.EndCell(), nil

	case 0b100: // msg_import_fin (block-parse.cpp:1767-1786)
		// in_msg:^MsgEnvelope transaction:^Transaction fwd_fee:Grams
		if v.RefsNum() < 2 {
			return nil, fmt.Errorf("msg_import_fin must have 2 refs")
		}
		envCell, err := v.LoadRefCell()
		if err != nil {
			return nil, err
		}
		env, err := parseMsgEnvelopeView(envCell)
		if err != nil {
			return nil, fmt.Errorf("failed to parse in msg envelope: %w", err)
		}
		if _, err = v.LoadRefCell(); err != nil { // transaction
			return nil, err
		}
		fwdFee, err := v.LoadBigCoins()
		if err != nil {
			return nil, fmt.Errorf("failed to load fwd fee: %w", err)
		}
		if fwdFee.Cmp(env.fwdFeeRemaining.val) != 0 {
			return nil, fmt.Errorf("in msg fwd fee %s does not match envelope remaining fee %s",
				fwdFee, env.fwdFeeRemaining.val)
		}
		info, err := parseIntMsgInfoView(env.msg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse enveloped message: %w", err)
		}
		// fees_collected := fwd_fee_remaining (original bits)
		if err = env.fwdFeeRemaining.appendTo(b); err != nil {
			return nil, err
		}
		// value_imported := msg.value + msg.ihr_fee + fwd_fee_remaining
		sum := new(big.Int).Add(info.valueGrams, info.ihrFee.val)
		sum.Add(sum, env.fwdFeeRemaining.val)
		if err = storeCanonicalGrams(b, sum); err != nil {
			return nil, err
		}
		if err = info.valueExtra.appendTo(b); err != nil {
			return nil, err
		}
		return b.EndCell(), nil

	case 0b101: // msg_import_tr (block-parse.cpp:1787-1807)
		// in_msg:^MsgEnvelope out_msg:^MsgEnvelope transit_fee:Grams
		if v.RefsNum() < 2 {
			return nil, fmt.Errorf("msg_import_tr must have 2 refs")
		}
		envCell, err := v.LoadRefCell()
		if err != nil {
			return nil, err
		}
		env, err := parseMsgEnvelopeView(envCell)
		if err != nil {
			return nil, fmt.Errorf("failed to parse in msg envelope: %w", err)
		}
		if _, err = v.LoadRefCell(); err != nil { // out_msg
			return nil, err
		}
		transitFee, err := v.LoadBigCoins()
		if err != nil {
			return nil, fmt.Errorf("failed to load transit fee: %w", err)
		}
		if transitFee.Cmp(env.fwdFeeRemaining.val) > 0 {
			return nil, fmt.Errorf("transit fee %s is above envelope remaining fee %s",
				transitFee, env.fwdFeeRemaining.val)
		}
		info, err := parseIntMsgInfoView(env.msg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse enveloped message: %w", err)
		}
		// fees_collected := transit_fee (canonical store, block-parse.cpp:1800)
		if err = storeCanonicalGrams(b, transitFee); err != nil {
			return nil, err
		}
		// value_imported := msg.value + msg.ihr_fee + fwd_fee_remaining
		sum := new(big.Int).Add(info.valueGrams, info.ihrFee.val)
		sum.Add(sum, env.fwdFeeRemaining.val)
		if err = storeCanonicalGrams(b, sum); err != nil {
			return nil, err
		}
		if err = info.valueExtra.appendTo(b); err != nil {
			return nil, err
		}
		return b.EndCell(), nil

	case 0b110, 0b111: // msg_discard_fin / msg_discard_tr (block-parse.cpp:1808-1827)
		// in_msg:^MsgEnvelope transaction_id:uint64 fwd_fee:Grams [proof_delivered:^Cell]
		wantRefs := 1
		if tag == 0b111 {
			wantRefs = 2
		}
		if v.RefsNum() < wantRefs {
			return nil, fmt.Errorf("msg_discard must have %d refs", wantRefs)
		}
		if _, err = v.LoadRefCell(); err != nil { // in_msg
			return nil, err
		}
		if _, err = v.LoadUInt(64); err != nil { // transaction_id
			return nil, err
		}
		fwdFee, err := loadRawGrams(v)
		if err != nil {
			return nil, fmt.Errorf("failed to load fwd fee: %w", err)
		}
		if tag == 0b111 {
			if _, err = v.LoadRefCell(); err != nil { // proof_delivered
				return nil, err
			}
		}
		// fees_collected := fwd_fee; value_imported := fwd_fee (original bits, empty extra)
		if err = fwdFee.appendTo(b); err != nil {
			return nil, err
		}
		if err = fwdFee.appendTo(b); err != nil {
			return nil, err
		}
		if err = b.StoreBoolBit(false); err != nil {
			return nil, err
		}
		return b.EndCell(), nil

	default:
		return nil, fmt.Errorf("unknown in msg tag %b", tag)
	}
}

// augInMsgDescrDeferredLeaf computes ImportFees for the deferred in-message
// variants (rest is the low 2 bits of the 5-bit tag):
//
//	msg_import_deferred_fin$00100 in_msg:^MsgEnvelope transaction:^Transaction
//	    fwd_fee:Grams = InMsg;
//	msg_import_deferred_tr$00101 in_msg:^MsgEnvelope out_msg:^MsgEnvelope = InMsg;
//
// deferred_fin follows the msg_import_fin rule (fees_collected :=
// fwd_fee_remaining, which must equal the stored fwd_fee; value_imported :=
// msg.value + msg.ihr_fee + fwd_fee_remaining); deferred_tr collects no fees
// (fees_collected := 0) and imports msg.value + msg.ihr_fee +
// fwd_fee_remaining. Both rules are validated bit-exactly against real mainnet
// InMsgDescr extras in tests, since the local ton checkout predates them.
func augInMsgDescrDeferredLeaf(v *cell.Slice, rest uint64) (*cell.Cell, error) {
	if rest > 1 {
		return nil, fmt.Errorf("unknown deferred in msg tag %b", 0b00100|rest)
	}

	if v.RefsNum() < 2 {
		return nil, fmt.Errorf("deferred in msg must have 2 refs")
	}
	envCell, err := v.LoadRefCell()
	if err != nil {
		return nil, err
	}
	env, err := parseMsgEnvelopeView(envCell)
	if err != nil {
		return nil, fmt.Errorf("failed to parse in msg envelope: %w", err)
	}
	if _, err = v.LoadRefCell(); err != nil { // transaction / out_msg
		return nil, err
	}

	info, err := parseIntMsgInfoView(env.msg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse enveloped message: %w", err)
	}

	b := cell.BeginCell()
	if rest == 0 { // msg_import_deferred_fin
		fwdFee, err := v.LoadBigCoins()
		if err != nil {
			return nil, fmt.Errorf("failed to load fwd fee: %w", err)
		}
		if fwdFee.Cmp(env.fwdFeeRemaining.val) != 0 {
			return nil, fmt.Errorf("in msg fwd fee %s does not match envelope remaining fee %s",
				fwdFee, env.fwdFeeRemaining.val)
		}
		// fees_collected := fwd_fee_remaining (original bits)
		if err = env.fwdFeeRemaining.appendTo(b); err != nil {
			return nil, err
		}
	} else { // msg_import_deferred_tr
		// fees_collected := 0
		if err = b.StoreUInt(0, 4); err != nil {
			return nil, err
		}
	}

	// value_imported := msg.value + msg.ihr_fee + fwd_fee_remaining
	sum := new(big.Int).Add(info.valueGrams, info.ihrFee.val)
	sum.Add(sum, env.fwdFeeRemaining.val)
	if err = storeCanonicalGrams(b, sum); err != nil {
		return nil, err
	}
	if err = info.valueExtra.appendTo(b); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

func (AugInMsgDescr) CombineExtra(leftExtra, rightExtra *cell.Slice) (*cell.Cell, error) {
	// ImportFees::add_values (block-parse.cpp:1650-1652).
	lg, err := leftExtra.LoadBigCoins()
	if err != nil {
		return nil, fmt.Errorf("failed to load left fees collected: %w", err)
	}
	rg, err := rightExtra.LoadBigCoins()
	if err != nil {
		return nil, fmt.Errorf("failed to load right fees collected: %w", err)
	}

	b := cell.BeginCell()
	if err = storeCanonicalGrams(b, new(big.Int).Add(lg, rg)); err != nil {
		return nil, err
	}
	if err = addCurrencyCollectionSlices(b, leftExtra, rightExtra); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

// ===========================================================================
// AugOutMsgDescr: HashmapAugE 256 OutMsg CurrencyCollection
// ===========================================================================

// AugOutMsgDescr implements the augmentation of OutMsgDescr
// (block.tlb:212: _ (HashmapAugE 256 OutMsg CurrencyCollection)).
//
// Leaf rule (Aug_OutMsgDescr::eval_leaf, block-parse.h:864-871 ->
// OutMsg::get_export_value, block-parse.cpp:1918-1955): only queued exports
// (msg_export_new, msg_export_tr, msg_export_tr_req) carry a value equal to
// msg.value + msg.ihr_fee + envelope fwd_fee_remaining; all other variants,
// including every dequeue record, evaluate to the zero CurrencyCollection.
//
// Fork rule: CurrencyCollection::add_values (block-parse.cpp:619-621).
// Empty value: CurrencyCollection null (5 zero bits).
type AugOutMsgDescr struct{}

func (AugOutMsgDescr) SkipExtra(loader *cell.Slice) error {
	return skipCurrencyCollectionBoundary(loader)
}

func (AugOutMsgDescr) EmptyExtra() (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := storeEmptyCurrencyCollection(b); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

func (AugOutMsgDescr) LeafExtra(value *cell.Slice) (*cell.Cell, error) {
	v := value.Copy()

	tag3, err := v.Copy().LoadUInt(3)
	if err != nil {
		return nil, fmt.Errorf("failed to load out msg tag: %w", err)
	}
	tag := tag3
	tagBits := uint(3)
	switch tag3 {
	case 0b110: // msg_export_deq$1100 / msg_export_deq_short$1101
		tagBits = 4
	case 0b101:
		// current upstream 5-bit variants msg_export_new_defer$10100 and
		// msg_export_deferred_tr$10101 (dispatch queue feature; not present in
		// the local ton checkout's block.tlb).
		tagBits = 5
	}
	if tagBits != 3 {
		tag, err = v.Copy().LoadUInt(tagBits)
		if err != nil {
			return nil, fmt.Errorf("failed to load out msg long tag: %w", err)
		}
	}

	zero := func(needBits uint, needRefs int) (*cell.Cell, error) {
		if v.BitsLeft() < needBits || v.RefsNum() < needRefs {
			return nil, fmt.Errorf("truncated out msg with tag %b", tag)
		}
		b := cell.BeginCell()
		if err := storeEmptyCurrencyCollection(b); err != nil {
			return nil, err
		}
		return b.EndCell(), nil
	}

	exported := func() (*cell.Cell, error) {
		if _, err = v.LoadUInt(tagBits); err != nil {
			return nil, err
		}
		if v.RefsNum() < 2 {
			return nil, fmt.Errorf("out msg with tag %b must have 2 refs", tag)
		}
		envCell, err := v.LoadRefCell()
		if err != nil {
			return nil, err
		}
		if _, err = v.LoadRefCell(); err != nil { // transaction / imported
			return nil, err
		}
		env, err := parseMsgEnvelopeView(envCell)
		if err != nil {
			return nil, fmt.Errorf("failed to parse out msg envelope: %w", err)
		}
		info, err := parseIntMsgInfoView(env.msg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse enveloped message: %w", err)
		}

		// exported value = msg.value + msg.ihr_fee + fwd_fee_remaining
		b := cell.BeginCell()
		sum := new(big.Int).Add(info.valueGrams, info.ihrFee.val)
		sum.Add(sum, env.fwdFeeRemaining.val)
		if err = storeCanonicalGrams(b, sum); err != nil {
			return nil, err
		}
		if err = info.valueExtra.appendTo(b); err != nil {
			return nil, err
		}
		return b.EndCell(), nil
	}

	switch tag {
	case 0b000: // msg_export_ext (block-parse.cpp:1920-1924)
		return zero(3, 2)
	case 0b010: // msg_export_imm (block-parse.cpp:1925-1926)
		return zero(3, 3)
	case 0b100: // msg_export_deq_imm (block-parse.cpp:1927-1928)
		return zero(3, 2)
	case 0b1100: // msg_export_deq (block-parse.cpp:1929-1930)
		return zero(4+63, 1)
	case 0b1101: // msg_export_deq_short (block-parse.cpp:1931-1932)
		return zero(4+256+32+64+64, 0)
	case 0b001, 0b011, 0b111: // msg_export_new / msg_export_tr / msg_export_tr_req (block-parse.cpp:1933-1950)
		return exported()
	case 0b10100, 0b10101: // msg_export_new_defer / msg_export_deferred_tr (upstream), same value rule
		return exported()
	default:
		return nil, fmt.Errorf("unknown out msg tag %b", tag)
	}
}

func (AugOutMsgDescr) CombineExtra(leftExtra, rightExtra *cell.Slice) (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := addCurrencyCollectionSlices(b, leftExtra, rightExtra); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

// ===========================================================================
// AugOutMsgQueue: HashmapAugE 352 EnqueuedMsg uint64
// ===========================================================================

// AugOutMsgQueue implements the augmentation of OutMsgQueue
// (block.tlb:214: _ (HashmapAugE 352 EnqueuedMsg uint64)).
//
// Leaf rule (Aug_OutMsgQueue::eval_leaf, block-parse.cpp:2004-2009): the
// uint64 extra of an EnqueuedMsg is derived from the message envelope
// referenced by the value (the enqueued_lt bits are NOT used):
//   - the local (pre msg_envelope_v2) checkout takes the created_lt of the
//     message inside the envelope (MsgEnvelope::get_created_lt,
//     block-parse.cpp:858-864 -> CommonMsgInfo::get_created_lt, :714-737);
//   - current upstream (MsgEnvelope::get_emitted_lt) prefers the explicit
//     emitted_lt of a msg_envelope_v2#5 when present and falls back to the
//     message created_lt otherwise; this implementation follows the upstream
//     rule since mainnet queues contain v2 envelopes.
//
// Fork rule (Aug_OutMsgQueue::eval_fork, block-parse.cpp:1994-1998):
// min(left, right). Empty value (eval_empty, :2000-2002): 0.
type AugOutMsgQueue struct{}

func (AugOutMsgQueue) SkipExtra(loader *cell.Slice) error {
	return skipUint64Boundary(loader)
}

func (AugOutMsgQueue) EmptyExtra() (*cell.Cell, error) {
	return cell.BeginCell().MustStoreUInt(0, 64).EndCell(), nil
}

func (AugOutMsgQueue) LeafExtra(value *cell.Slice) (*cell.Cell, error) {
	v := value.Copy()
	// EnqueuedMsg: enqueued_lt:uint64 out_msg:^MsgEnvelope; C++ fetches the ref
	// without reading enqueued_lt (cs.fetch_ref_to, block-parse.cpp:2006).
	envCell, err := v.PeekRefCell()
	if err != nil {
		return nil, fmt.Errorf("failed to load enqueued message envelope: %w", err)
	}

	env, err := parseMsgEnvelopeView(envCell)
	if err != nil {
		return nil, fmt.Errorf("failed to parse enqueued message envelope: %w", err)
	}

	var lt uint64
	if env.emittedLT != nil {
		lt = *env.emittedLT
	} else {
		lt, err = messageCreatedLT(env.msg)
		if err != nil {
			return nil, fmt.Errorf("failed to read message created lt: %w", err)
		}
	}
	return cell.BeginCell().MustStoreUInt(lt, 64).EndCell(), nil
}

func (AugOutMsgQueue) CombineExtra(leftExtra, rightExtra *cell.Slice) (*cell.Cell, error) {
	l, err := leftExtra.LoadUInt(64)
	if err != nil {
		return nil, fmt.Errorf("failed to load left lt: %w", err)
	}
	r, err := rightExtra.LoadUInt(64)
	if err != nil {
		return nil, fmt.Errorf("failed to load right lt: %w", err)
	}
	if r < l {
		l = r
	}
	return cell.BeginCell().MustStoreUInt(l, 64).EndCell(), nil
}

// ===========================================================================
// AugShardFees: HashmapAugE 96 ShardFeeCreated ShardFeeCreated
// ===========================================================================

// AugShardFees implements the augmentation of ShardFees
// (block.tlb:449: _ (HashmapAugE 96 ShardFeeCreated ShardFeeCreated)).
//
// Leaf rule (Aug_ShardFees::eval_leaf, block-parse.cpp:2289-2291): the extra
// is a verbatim copy of the leaf value, which must parse as ShardFeeCreated
// with nothing left over (cs.empty_ext()).
//
// Fork rule (ShardFeeCreated::add_values, block-parse.cpp:2282-2284):
// component-wise CurrencyCollection addition of fees and create.
// Empty value (ShardFeeCreated::null_value, block-parse.cpp:2278-2280):
// two zero CurrencyCollections (10 zero bits).
type AugShardFees struct{}

func (AugShardFees) SkipExtra(loader *cell.Slice) error {
	return skipShardFeeCreatedBoundary(loader)
}

func (AugShardFees) EmptyExtra() (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := storeEmptyCurrencyCollection(b); err != nil {
		return nil, err
	}
	if err := storeEmptyCurrencyCollection(b); err != nil {
		return nil, err
	}
	return b.EndCell(), nil
}

func (AugShardFees) LeafExtra(value *cell.Slice) (*cell.Cell, error) {
	check := value.Copy()
	if err := skipShardFeeCreatedBoundary(check); err != nil {
		return nil, fmt.Errorf("shard fee value is not a valid ShardFeeCreated: %w", err)
	}
	if check.BitsLeft() != 0 || check.RefsNum() != 0 {
		return nil, fmt.Errorf("shard fee value has %d trailing bits and %d trailing refs",
			check.BitsLeft(), check.RefsNum())
	}
	return value.Copy().ToCell()
}

func (AugShardFees) CombineExtra(leftExtra, rightExtra *cell.Slice) (*cell.Cell, error) {
	b := cell.BeginCell()
	if err := addCurrencyCollectionSlices(b, leftExtra, rightExtra); err != nil { // fees
		return nil, err
	}
	if err := addCurrencyCollectionSlices(b, leftExtra, rightExtra); err != nil { // create
		return nil, err
	}
	return b.EndCell(), nil
}

// NOTE: a writable augmentation for DispatchQueue (HashmapAugE 256
// AccountDispatchQueue uint64) is intentionally NOT provided: the local
// authoritative TON checkout (crypto/block/block.tlb) predates the dispatch
// queue format and contains neither the TLB schema nor Aug_DispatchQueue, so
// there is no source to derive exact semantics from. The read-only parser in
// out_msg_queue.go remains available.

// ===========================================================================
// wrapper types and writable constructors
// ===========================================================================

// InMsgDescrAugDict wraps InMsgDescr (HashmapAugE 256 InMsg ImportFees).
type InMsgDescrAugDict struct {
	*cell.AugmentedDictionary
}

// OutMsgDescrAugDict wraps OutMsgDescr (HashmapAugE 256 OutMsg CurrencyCollection).
type OutMsgDescrAugDict struct {
	*cell.AugmentedDictionary
}

func (d *InMsgDescrAugDict) LoadFromCell(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(256, AugInMsgDescr{}, false)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *InMsgDescrAugDict) LoadFromCellAsProof(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(256, AugInMsgDescr{}, true)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *InMsgDescrAugDict) ToCell() (*cell.Cell, error) {
	return augDictToCell(d)
}

func (d *InMsgDescrAugDict) getAugmentedDictionary() *cell.AugmentedDictionary {
	if d == nil {
		return nil
	}
	return d.AugmentedDictionary
}

func (d *OutMsgDescrAugDict) LoadFromCell(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(256, AugOutMsgDescr{}, false)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *OutMsgDescrAugDict) LoadFromCellAsProof(loader *cell.Slice) error {
	dict, err := loader.LoadAugDict(256, AugOutMsgDescr{}, true)
	if err != nil {
		return err
	}
	d.AugmentedDictionary = dict
	return nil
}

func (d *OutMsgDescrAugDict) ToCell() (*cell.Cell, error) {
	return augDictToCell(d)
}

func (d *OutMsgDescrAugDict) getAugmentedDictionary() *cell.AugmentedDictionary {
	if d == nil {
		return nil
	}
	return d.AugmentedDictionary
}

// NewShardAccountsAugDict returns an empty writable ShardAccounts dictionary.
func NewShardAccountsAugDict() (*ShardAccountsAugDict, error) {
	dict, err := cell.NewAugDict(256, AugShardAccounts{})
	if err != nil {
		return nil, err
	}
	return &ShardAccountsAugDict{dict}, nil
}

// NewShardAccountBlocksAugDict returns an empty writable ShardAccountBlocks dictionary.
func NewShardAccountBlocksAugDict() (*ShardAccountBlocksAugDict, error) {
	dict, err := cell.NewAugDict(256, AugShardAccountBlocks{})
	if err != nil {
		return nil, err
	}
	return &ShardAccountBlocksAugDict{dict}, nil
}

// NewAccountTransactionsAugDict returns an empty writable transaction
// dictionary of an AccountBlock. Note that the serialized form inside an
// AccountBlock is an inline HashmapAug 64 which cannot be empty; ToCell of the
// returned wrapper produces the HashmapAugE-style wrapped form, use
// InlineCell to embed the dictionary into an AccountBlock.
func NewAccountTransactionsAugDict() (*AccountTransactionsAugDict, error) {
	dict, err := cell.NewAugDict(64, AugAccountTransactions{})
	if err != nil {
		return nil, err
	}
	return &AccountTransactionsAugDict{AugmentedDictionary: dict, wrapped: true}, nil
}

// NewInMsgDescrAugDict returns an empty writable InMsgDescr dictionary.
func NewInMsgDescrAugDict() (*InMsgDescrAugDict, error) {
	dict, err := cell.NewAugDict(256, AugInMsgDescr{})
	if err != nil {
		return nil, err
	}
	return &InMsgDescrAugDict{dict}, nil
}

// NewOutMsgDescrAugDict returns an empty writable OutMsgDescr dictionary.
func NewOutMsgDescrAugDict() (*OutMsgDescrAugDict, error) {
	dict, err := cell.NewAugDict(256, AugOutMsgDescr{})
	if err != nil {
		return nil, err
	}
	return &OutMsgDescrAugDict{dict}, nil
}

// NewOutMsgQueueAugDict returns an empty writable OutMsgQueue dictionary.
func NewOutMsgQueueAugDict() (*OutMsgQueueAugDict, error) {
	dict, err := cell.NewAugDict(352, AugOutMsgQueue{})
	if err != nil {
		return nil, err
	}
	return &OutMsgQueueAugDict{dict}, nil
}

// NewShardFeesAugDict returns an empty writable ShardFees dictionary.
func NewShardFeesAugDict() (*ShardFeesAugDict, error) {
	dict, err := cell.NewAugDict(96, AugShardFees{})
	if err != nil {
		return nil, err
	}
	return &ShardFeesAugDict{dict}, nil
}
