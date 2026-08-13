// Package tlb implements writable augmentation semantics for block and state
// dictionaries. Unless noted otherwise, fork extras use the extra type's
// addition rule and empty dictionaries use its null encoding.
package tlb

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

var (
	_ cell.Augmentation = AugShardAccounts{}
	_ cell.Augmentation = AugShardAccountBlocks{}
	_ cell.Augmentation = AugAccountTransactions{}
	_ cell.Augmentation = AugInMsgDescr{}
	_ cell.Augmentation = AugOutMsgDescr{}
	_ cell.Augmentation = AugOutMsgQueue{}
	_ cell.Augmentation = AugDispatchQueue{}
	_ cell.Augmentation = AugShardFees{}
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

// rawGrams captures a Grams (VarUInteger 16) field bit-exactly: a 4-bit byte
// length plus the value bytes. Numeric decoding stays lazy for raw-copy paths.
// Chain-validated values cannot contain a leading zero byte, so their raw and
// canonical encodings coincide.
//
// The value lives in a fixed buffer — fifteen bytes is the most VarUInteger 16
// can carry — so loading one costs no heap. The struct travels by value; the
// Augmentation contract forbids retaining borrowed slices anyway, and a copy of
// the bytes is what makes that safe.
type rawGrams struct {
	ln   uint64
	data [15]byte
}

func loadRawGrams(loader *cell.Slice) (rawGrams, error) {
	ln, err := loader.LoadUInt(4)
	if err != nil {
		return rawGrams{}, fmt.Errorf("failed to load grams length: %w", err)
	}
	if ln == 0 {
		return rawGrams{}, nil
	}
	var g rawGrams
	g.ln = ln
	if err = loader.LoadSliceInto(g.data[:ln], uint(ln)*8); err != nil {
		return rawGrams{}, fmt.Errorf("failed to load grams value: %w", err)
	}
	return g, nil
}

func (g rawGrams) appendTo(b *cell.Builder) error {
	if err := b.StoreUInt(g.ln, 4); err != nil {
		return err
	}
	if g.ln == 0 {
		return nil
	}
	return b.StoreSlice(g.data[:g.ln], uint(g.ln)*8)
}

func (g rawGrams) integer() (*big.Int, error) {
	if g.ln > 0 && g.data[0] == 0 {
		return nil, fmt.Errorf("grams value has a leading zero byte")
	}
	return new(big.Int).SetBytes(g.data[:g.ln]), nil
}

// value128 is integer() without the big.Int: the same canonical-form check, the
// value as a fixed 128-bit pair.
func (g rawGrams) value128() (gramsU128, error) {
	if g.ln > 0 && g.data[0] == 0 {
		return gramsU128{}, fmt.Errorf("grams value has a leading zero byte")
	}
	var v gramsU128
	for i := uint64(0); i < g.ln; i++ {
		v.hi = v.hi<<8 | v.lo>>56
		v.lo = v.lo<<8 | uint64(g.data[i])
	}
	return v, nil
}

func loadCanonicalGrams(loader *cell.Slice) (*big.Int, error) {
	grams, err := loadRawGrams(loader)
	if err != nil {
		return nil, err
	}
	return grams.integer()
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

// storeCanonicalGrams stores a Grams value using the minimal byte length
// required by VarUInteger 16. Negative values are rejected by StoreBigVarUInt.
func storeCanonicalGrams(b *cell.Builder, v *big.Int) error {
	return b.StoreBigVarUInt(v, 16)
}

// addCurrencyCollectionSlices consumes one CurrencyCollection from each slice
// and appends their sum. The grams sum is stored canonically; extra currency
// dictionaries are merged by key with VarUIntegerPos addition, while subtrees
// unique to one side are reused as-is.
func addCurrencyCollectionSlices(b *cell.Builder, left, right *cell.Slice) error {
	if err := addGramsSlices(b, left, right); err != nil {
		return err
	}

	// Nearly every amount that exists carries no extra currencies, and merging
	// two absent dictionaries yields one zero bit — exactly what StoreDict
	// writes for an empty one. Taking that shortcut avoids building two
	// Dictionary values only to throw them away.
	absent, err := bothExtraDictsAbsent(left, right)
	if err != nil {
		return err
	}
	if absent {
		if _, err = left.LoadBoolBit(); err != nil {
			return fmt.Errorf("failed to load left extra currencies: %w", err)
		}
		if _, err = right.LoadBoolBit(); err != nil {
			return fmt.Errorf("failed to load right extra currencies: %w", err)
		}
		return b.StoreUInt(0, 1)
	}

	ld, err := left.LoadDict(32)
	if err != nil {
		return fmt.Errorf("failed to load left extra currencies: %w", err)
	}
	rd, err := right.LoadDict(32)
	if err != nil {
		return fmt.Errorf("failed to load right extra currencies: %w", err)
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
// zero grams (4 zero bits) plus an empty extra dictionary (1 zero bit).
func storeEmptyCurrencyCollection(b *cell.Builder) error {
	return b.StoreUInt(0, 5)
}

// ===========================================================================
// message helpers shared by InMsgDescr / OutMsgDescr / OutMsgQueue leaves
// ===========================================================================

// skipMaybeAnycast skips Maybe Anycast and returns the anycast rewrite depth
// (0 when absent). The depth:(#<= 30) field is a 5-bit integer followed by that
// many rewrite-prefix bits.
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
	if depth == 0 || depth > 30 {
		return 0, fmt.Errorf("anycast depth %d is outside 1..30", depth)
	}
	if err = s.SkipBits(uint(depth)); err != nil {
		return 0, fmt.Errorf("failed to skip anycast rewrite prefix: %w", err)
	}
	return depth, nil
}

// skipMsgAddressIntGetDepth skips MsgAddressInt and returns its anycast depth.
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
		if err = s.SkipBits(8 + 256); err != nil {
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
		if err = s.SkipBits(32 + uint(addrLen)); err != nil {
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

// skipMsgAddressExt skips either MsgAddressExt encoding:
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
		if err = s.SkipBits(uint(ln)); err != nil {
			return fmt.Errorf("failed to skip external address: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("invalid external address tag %b", tag)
	}
}

// messageExtraFlagsVersion is the global version since which ihr_fee no longer
// contributes to the imported/exported value of a message.
const messageExtraFlagsVersion = uint32(12)

// intMsgInfoView is the subset of int_msg_info$0 needed by augmentation leaves.
type intMsgInfoView struct {
	valueGrams rawGrams     // value:CurrencyCollection grams part
	valueExtra rawExtraDict // value:CurrencyCollection extra part, raw
	ihrFee     rawGrams
}

func (v intMsgInfoView) valueWithRemainingFee(remaining gramsU128, globalVersion uint32) (gramsU128, error) {
	value, err := v.valueGrams.value128()
	if err != nil {
		return gramsU128{}, fmt.Errorf("message value: %w", err)
	}

	if globalVersion < messageExtraFlagsVersion {
		ihrFee, err := v.ihrFee.value128()
		if err != nil {
			return gramsU128{}, fmt.Errorf("message ihr fee: %w", err)
		}
		if value, err = value.add(ihrFee); err != nil {
			return gramsU128{}, err
		}
	}
	return value.add(remaining)
}

func parseIntMsgInfoView(msg *cell.Cell) (intMsgInfoView, error) {
	// Nothing the view carries points back at the slice — rawGrams is a fixed
	// array and rawExtraDict keeps a cell — so the loader stays local.
	var sv cell.Slice
	if err := msg.BeginParseInto(&sv); err != nil {
		return intMsgInfoView{}, fmt.Errorf("failed to parse message: %w", err)
	}
	s := &sv

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
	if _, err = loadMsgCreatedLTAndAt(s); err != nil {
		return intMsgInfoView{}, err
	}

	return intMsgInfoView{
		valueGrams: valueGrams,
		valueExtra: valueExtra,
		ihrFee:     ihrFee,
	}, nil
}

// loadMsgCreatedLTAndAt consumes the created_lt:uint64 created_at:uint32 tail
// shared by int_msg_info$0 and ext_out_msg_info$11.
func loadMsgCreatedLTAndAt(s *cell.Slice) (uint64, error) {
	createdLT, err := s.LoadUInt(64)
	if err != nil {
		return 0, fmt.Errorf("failed to load created lt: %w", err)
	}
	if _, err = s.LoadUInt(32); err != nil {
		return 0, fmt.Errorf("failed to load created at: %w", err)
	}
	return createdLT, nil
}

// messageCreatedLT reads created_lt from int_msg_info$0 or
// ext_out_msg_info$11. External inbound messages do not contain this field.
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
		if err = skipGrams(s); err != nil { // ihr_fee
			return 0, fmt.Errorf("failed to skip ihr fee: %w", err)
		}
		if err = skipGrams(s); err != nil { // fwd_fee
			return 0, fmt.Errorf("failed to skip fwd fee: %w", err)
		}
		return loadMsgCreatedLTAndAt(s)
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
	return loadMsgCreatedLTAndAt(s)
}

// msgEnvelopeView is the subset of MsgEnvelope needed by augmentation leaves.
// It accepts msg_envelope#4 and msg_envelope_v2#5; v2 may append emitted_lt and
// metadata after the message reference.
type msgEnvelopeView struct {
	fwdFeeRemaining rawGrams
	fwdFeeValue     gramsU128
	msg             *cell.Cell
	emittedLT       uint64
	hasEmittedLT    bool
	v2              bool
}

func parseMsgEnvelopePrefix(env *cell.Cell) (msgEnvelopeView, *cell.Slice, error) {
	s, err := env.BeginParse()
	if err != nil {
		return msgEnvelopeView{}, nil, fmt.Errorf("failed to parse message envelope: %w", err)
	}

	tag, err := s.LoadUInt(4)
	if err != nil {
		return msgEnvelopeView{}, nil, fmt.Errorf("failed to load message envelope tag: %w", err)
	}
	if tag != 4 && tag != 5 {
		return msgEnvelopeView{}, nil, fmt.Errorf("unsupported message envelope tag %d", tag)
	}

	if err = skipIntermediateAddress(s); err != nil {
		return msgEnvelopeView{}, nil, fmt.Errorf("failed to skip current intermediate address: %w", err)
	}
	if err = skipIntermediateAddress(s); err != nil {
		return msgEnvelopeView{}, nil, fmt.Errorf("failed to skip next intermediate address: %w", err)
	}

	fwdFeeRemaining, err := loadRawGrams(s)
	if err != nil {
		return msgEnvelopeView{}, nil, fmt.Errorf("failed to load remaining forward fee: %w", err)
	}

	msg, err := s.LoadRefCell()
	if err != nil {
		return msgEnvelopeView{}, nil, fmt.Errorf("failed to load message ref: %w", err)
	}

	return msgEnvelopeView{fwdFeeRemaining: fwdFeeRemaining, msg: msg, v2: tag == 5}, s, nil
}

func loadMsgEnvelopeEmittedLT(view *msgEnvelopeView, s *cell.Slice) error {
	hasEmittedLT, err := s.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load emitted lt flag: %w", err)
	}
	if hasEmittedLT {
		emittedLT, err := s.LoadUInt(64)
		if err != nil {
			return fmt.Errorf("failed to load emitted lt: %w", err)
		}
		view.emittedLT = emittedLT
		view.hasEmittedLT = true
	}
	return nil
}

func skipMsgMetadata(s *cell.Slice) error {
	tag, err := s.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load metadata tag: %w", err)
	}
	if tag != 0 {
		return fmt.Errorf("unsupported metadata tag %d", tag)
	}
	if _, err = s.LoadUInt(32); err != nil {
		return fmt.Errorf("failed to load metadata depth: %w", err)
	}
	if err = skipMsgAddressInt(s); err != nil {
		return fmt.Errorf("failed to skip metadata initiator: %w", err)
	}
	if _, err = s.LoadUInt(64); err != nil {
		return fmt.Errorf("failed to load metadata initiator lt: %w", err)
	}
	return nil
}

func parseMsgEnvelopeView(env *cell.Cell) (msgEnvelopeView, error) {
	view, s, err := parseMsgEnvelopePrefix(env)
	if err != nil {
		return view, err
	}
	view.fwdFeeValue, err = view.fwdFeeRemaining.value128()
	if err != nil {
		return msgEnvelopeView{}, fmt.Errorf("remaining forward fee: %w", err)
	}
	if !view.v2 {
		return view, nil
	}

	if err = loadMsgEnvelopeEmittedLT(&view, s); err != nil {
		return msgEnvelopeView{}, err
	}
	hasMetadata, err := s.LoadBoolBit()
	if err != nil {
		return msgEnvelopeView{}, fmt.Errorf("failed to load metadata flag: %w", err)
	}
	if hasMetadata {
		if err = skipMsgMetadata(s); err != nil {
			return msgEnvelopeView{}, fmt.Errorf("failed to load metadata: %w", err)
		}
	}
	return view, nil
}

func parseMsgEnvelopeEmissionView(env *cell.Cell) (msgEnvelopeView, error) {
	view, s, err := parseMsgEnvelopePrefix(env)
	if err != nil || !view.v2 {
		return view, err
	}
	if err = loadMsgEnvelopeEmittedLT(&view, s); err != nil {
		return msgEnvelopeView{}, err
	}
	return view, nil
}

// ===========================================================================
// AugShardAccounts: HashmapAugE 256 ShardAccount DepthBalanceInfo
// ===========================================================================

// AugShardAccounts implements the augmentation of ShardAccounts
// (crypto/block/block.tlb:263: _ (HashmapAugE 256 ShardAccount DepthBalanceInfo)).
//
// Leaf rule: the extra of a ShardAccount leaf is
// depth_balance$_ split_depth:(#<= 30)
// balance:CurrencyCollection where split_depth is the anycast rewrite depth of
// the account address (0 without anycast) and balance is the account balance
// copied bit-exactly; account_none yields the DepthBalanceInfo null value.
//
// Fork rule: split_depth = max(left, right), balance = left + right.
//
// Empty value: zero split_depth and zero balance (10 zero bits).
type AugShardAccounts struct{}

func (AugShardAccounts) SkipExtra(loader *cell.Slice) error {
	return skipDepthBalanceInfoBoundary(loader)
}

func (AugShardAccounts) EmptyExtra(dst *cell.Builder) error {
	if err := dst.StoreUInt(0, 5); err != nil { // split_depth:(#<= 30)
		return err
	}
	if err := storeEmptyCurrencyCollection(dst); err != nil {
		return err
	}
	return nil
}

func (AugShardAccounts) LeafExtra(value *cell.Slice, dst *cell.Builder) error {
	var v cell.Slice
	value.CopyInto(&v)
	// ShardAccount: account_descr$_ account:^Account last_trans_hash:bits256
	// last_trans_lt:uint64. The account ref is read without consuming inline bits.
	// Parsing the ref straight into a local slice keeps the child trace on the
	// slice: peeking the cell first would copy it only to carry that trace, and
	// parsing it would allocate a second time.
	var sv cell.Slice
	if err := v.PreloadRefInto(&sv); err != nil {
		return fmt.Errorf("failed to load account ref of shard account: %w", err)
	}
	s := &sv

	isAccount, err := s.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load account tag: %w", err)
	}

	if !isAccount {
		// account_none$0 maps to the DepthBalanceInfo null value.
		if err = dst.StoreUInt(0, 5); err != nil {
			return err
		}
		if err = storeEmptyCurrencyCollection(dst); err != nil {
			return err
		}
		return nil
	}

	// account$1 addr:MsgAddressInt storage_stat:StorageInfo storage:AccountStorage
	depth, err := skipMsgAddressIntGetDepth(s)
	if err != nil {
		return fmt.Errorf("failed to read account address: %w", err)
	}
	if err = dst.StoreUInt(depth, 5); err != nil { // split_depth:(#<= 30)
		return err
	}

	// storage_stat:StorageInfo contains StorageUsed cells/bits as VarUInteger 7,
	// StorageExtraInfo, last_paid, and an optional due_payment.
	if err = skipVarUInt(s, 7); err != nil {
		return fmt.Errorf("failed to skip storage cells used: %w", err)
	}
	if err = skipVarUInt(s, 7); err != nil {
		return fmt.Errorf("failed to skip storage bits used: %w", err)
	}
	storageExtraTag, err := s.LoadUInt(3)
	if err != nil {
		return fmt.Errorf("failed to load storage extra tag: %w", err)
	}
	switch storageExtraTag {
	case 0b000: // storage_extra_none$000
	case 0b001: // storage_extra_info$001 dict_hash:uint256
		if err = s.SkipBits(256); err != nil {
			return fmt.Errorf("failed to skip storage extra dict hash: %w", err)
		}
	default:
		return fmt.Errorf("unknown storage extra tag %b", storageExtraTag)
	}
	if _, err = s.LoadUInt(32); err != nil { // last_paid
		return fmt.Errorf("failed to skip storage last paid: %w", err)
	}
	hasDue, err := s.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load due payment flag: %w", err)
	}
	if hasDue {
		if err = skipGrams(s); err != nil {
			return fmt.Errorf("failed to skip due payment: %w", err)
		}
	}

	// storage:AccountStorage = last_trans_lt:uint64 balance:CurrencyCollection
	// state:AccountState; the balance is copied bit-exactly.
	if _, err = s.LoadUInt(64); err != nil {
		return fmt.Errorf("failed to skip last transaction lt: %w", err)
	}
	balanceGrams, err := loadRawGrams(s)
	if err != nil {
		return fmt.Errorf("failed to load account balance grams: %w", err)
	}
	balanceExtra, err := loadRawExtraDict(s)
	if err != nil {
		return fmt.Errorf("failed to load account balance extra: %w", err)
	}
	if err = balanceGrams.appendTo(dst); err != nil {
		return err
	}
	if err = balanceExtra.appendTo(dst); err != nil {
		return err
	}

	// State is skipped after the balance so the remaining structure is validated.
	if err = skipAccountState(s); err != nil {
		return fmt.Errorf("failed to skip account state: %w", err)
	}

	return nil
}

func (AugShardAccounts) CombineExtra(leftExtra, rightExtra *cell.Slice, dst *cell.Builder) error {
	d1, err := leftExtra.LoadUInt(5)
	if err != nil {
		return fmt.Errorf("failed to load left split depth: %w", err)
	}
	d2, err := rightExtra.LoadUInt(5)
	if err != nil {
		return fmt.Errorf("failed to load right split depth: %w", err)
	}
	if d1 > 30 || d2 > 30 {
		return fmt.Errorf("split depth above 30")
	}

	depth := d1
	if d2 > depth {
		depth = d2
	}
	if err = dst.StoreUInt(depth, 5); err != nil {
		return err
	}
	if err = addCurrencyCollectionSlices(dst, leftExtra, rightExtra); err != nil {
		return err
	}
	return nil
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
		if err = s.SkipBits(256); err != nil {
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
// Leaf rule: skip acc_trans#5 and account_addr, then extract the root extra of
// the inner
// (HashmapAug 64 ^Transaction CurrencyCollection) dictionary and re-store it
// canonically; i.e. the extra of an AccountBlock is the total fees of all its
// transactions.
//
// Fork rule: add the two CurrencyCollections component-wise.
// Empty value: CurrencyCollection null (5 zero bits).
type AugShardAccountBlocks struct{}

func (AugShardAccountBlocks) SkipExtra(loader *cell.Slice) error {
	return skipCurrencyCollectionBoundary(loader)
}

func (AugShardAccountBlocks) EmptyExtra(dst *cell.Builder) error {
	if err := storeEmptyCurrencyCollection(dst); err != nil {
		return err
	}
	return nil
}

func (AugShardAccountBlocks) LeafExtra(value *cell.Slice, dst *cell.Builder) error {
	var v cell.Slice
	value.CopyInto(&v)
	// acc_trans#5 and account_addr:bits256 precede the transactions dictionary.
	if _, err := v.LoadUInt(4); err != nil {
		return fmt.Errorf("failed to skip account block tag: %w", err)
	}
	if err := v.SkipBits(256); err != nil {
		return fmt.Errorf("failed to skip account block address: %w", err)
	}

	// transactions:(HashmapAug 64 ^Transaction CurrencyCollection); extract its
	// root-node extra.
	dict, err := v.ToAugDictWithValueAndAugmentation(64, AugAccountTransactions{}, skipAugRefValue)
	if err != nil {
		return fmt.Errorf("failed to load account transactions dict: %w", err)
	}
	rootExtra, err := dict.LoadRootExtra()
	if err != nil {
		return fmt.Errorf("failed to extract account transactions root extra: %w", err)
	}

	// Re-encode the extracted CurrencyCollection canonically.
	return storeCanonicalCurrencyCollectionFromSlice(rootExtra, dst)
}

func (AugShardAccountBlocks) CombineExtra(leftExtra, rightExtra *cell.Slice, dst *cell.Builder) error {
	if err := addCurrencyCollectionSlices(dst, leftExtra, rightExtra); err != nil {
		return err
	}
	return nil
}

// storeCanonicalCurrencyCollectionFromSlice re-encodes a CurrencyCollection read
// from the slice: canonical grams plus the original extra dictionary root.
func storeCanonicalCurrencyCollectionFromSlice(s *cell.Slice, dst *cell.Builder) error {
	grams, err := loadGramsU128(s)
	if err != nil {
		return fmt.Errorf("failed to load grams: %w", err)
	}
	extra, err := loadRawExtraDict(s)
	if err != nil {
		return fmt.Errorf("failed to load extra currencies: %w", err)
	}

	if err = grams.storeTo(dst); err != nil {
		return err
	}
	if err = extra.appendTo(dst); err != nil {
		return err
	}
	return nil
}

// ===========================================================================
// AugAccountTransactions: HashmapAug 64 ^Transaction CurrencyCollection
// ===========================================================================

// AugAccountTransactions implements the augmentation of the transaction
// dictionary inside an AccountBlock (block.tlb:337-339).
//
// Leaf rule: the extra of a ^Transaction leaf is the transaction's
// total_fees:CurrencyCollection, canonically re-encoded.
//
// Fork rule: add the two CurrencyCollections component-wise.
// Empty value: CurrencyCollection null (5 zero bits).
type AugAccountTransactions struct{}

func (AugAccountTransactions) SkipExtra(loader *cell.Slice) error {
	return skipCurrencyCollectionBoundary(loader)
}

func (AugAccountTransactions) EmptyExtra(dst *cell.Builder) error {
	if err := storeEmptyCurrencyCollection(dst); err != nil {
		return err
	}
	return nil
}

func (AugAccountTransactions) LeafExtra(value *cell.Slice, dst *cell.Builder) error {
	var v cell.Slice
	value.CopyInto(&v)
	// value is ^Transaction; see AugShardAccounts.LeafExtra on why the ref is
	// parsed straight into a local slice rather than peeked as a cell first.
	var sv cell.Slice
	if err := v.PreloadRefInto(&sv); err != nil {
		return fmt.Errorf("failed to load transaction ref: %w", err)
	}
	s := &sv

	// Validate the constructor before locating total_fees after the header.
	tag, err := s.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load transaction tag: %w", err)
	}
	if tag != 0b0111 {
		return fmt.Errorf("invalid transaction tag %b", tag)
	}
	// account_addr:bits256 lt:uint64 prev_trans_hash:bits256 prev_trans_lt:uint64
	// now:uint32 outmsg_cnt:uint15
	if err = s.SkipBits(256 + 64 + 256 + 64 + 32 + 15); err != nil {
		return fmt.Errorf("failed to skip transaction header: %w", err)
	}
	// orig_status:AccountStatus end_status:AccountStatus (2 bits each)
	if _, err = s.LoadUInt(4); err != nil {
		return fmt.Errorf("failed to skip account statuses: %w", err)
	}
	// ^[ in_msg:(Maybe ^(Message Any)) out_msgs:(HashmapE 15 ^(Message Any)) ]
	if _, err = s.LoadRefCell(); err != nil {
		return fmt.Errorf("failed to skip transaction io ref: %w", err)
	}

	return storeCanonicalCurrencyCollectionFromSlice(s, dst)
}

func (AugAccountTransactions) CombineExtra(leftExtra, rightExtra *cell.Slice, dst *cell.Builder) error {
	if err := addCurrencyCollectionSlices(dst, leftExtra, rightExtra); err != nil {
		return err
	}
	return nil
}

// ===========================================================================
// AugInMsgDescr: HashmapAugE 256 InMsg ImportFees
// ===========================================================================

// AugInMsgDescr implements the augmentation of InMsgDescr
// (block.tlb:188: _ (HashmapAugE 256 InMsg ImportFees)).
//
// Leaf rule: derive ImportFees from the InMsg message headers, envelope fees,
// and record fee fields. Callers set only the InMsg value; the dictionary
// computes the extra according to the variant-specific rules below.
//
// Fork rule: add fees_collected as Grams and value_imported as a
// CurrencyCollection. Empty value: 4+4+1 zero bits.
type AugInMsgDescr struct {
	GlobalVersion uint32
}

func (AugInMsgDescr) SkipExtra(loader *cell.Slice) error {
	if err := skipGrams(loader); err != nil { // fees_collected:Grams
		return err
	}
	return skipCurrencyCollectionBoundary(loader) // value_imported:CurrencyCollection
}

func (AugInMsgDescr) EmptyExtra(dst *cell.Builder) error {
	if err := dst.StoreUInt(0, 4); err != nil { // fees_collected: zero Grams
		return err
	}
	if err := storeEmptyCurrencyCollection(dst); err != nil { // value_imported: zero
		return err
	}
	return nil
}

func (a AugInMsgDescr) LeafExtra(value *cell.Slice, dst *cell.Builder) error {
	var v cell.Slice
	value.CopyInto(&v)
	tag, err := v.LoadUInt(3)
	if err != nil {
		return fmt.Errorf("failed to load in msg tag: %w", err)
	}
	if tag == 0b001 {
		// Dispatch-queue variants msg_import_deferred_fin$00100 and
		// msg_import_deferred_tr$00101 use five-bit tags.
		rest, err := v.LoadUInt(2)
		if err != nil {
			return fmt.Errorf("failed to load deferred in msg tag: %w", err)
		}
		return augInMsgDescrDeferredLeaf(&v, rest, a.GlobalVersion, dst)
	}

	switch tag {
	case 0b000: // msg_import_ext: no value and no import fees
		if err = dst.StoreUInt(0, 4); err != nil {
			return err
		}
		if err = storeEmptyCurrencyCollection(dst); err != nil {
			return err
		}
		return nil

	case 0b010: // msg_import_ihr
		return errors.New("msg_import_ihr is unsupported")

	case 0b011: // msg_import_imm
		// in_msg:^MsgEnvelope transaction:^Transaction fwd_fee:Grams
		if v.RefsNum() < 2 {
			return fmt.Errorf("msg_import_imm must have 2 refs")
		}
		if _, err = v.LoadRefCell(); err != nil {
			return err
		}
		if _, err = v.LoadRefCell(); err != nil {
			return err
		}
		fwdFee, err := loadRawGrams(&v)
		if err != nil {
			return fmt.Errorf("failed to load fwd fee: %w", err)
		}
		// fees_collected := fwd_fee (original bits); value_imported := 0
		if err = fwdFee.appendTo(dst); err != nil {
			return err
		}
		if err = storeEmptyCurrencyCollection(dst); err != nil {
			return err
		}
		return nil

	case 0b100: // msg_import_fin
		// in_msg:^MsgEnvelope transaction:^Transaction fwd_fee:Grams
		if v.RefsNum() < 2 {
			return fmt.Errorf("msg_import_fin must have 2 refs")
		}
		envCell, err := v.LoadRefCell()
		if err != nil {
			return err
		}
		env, err := parseMsgEnvelopeView(envCell)
		if err != nil {
			return fmt.Errorf("failed to parse in msg envelope: %w", err)
		}
		if _, err = v.LoadRefCell(); err != nil { // transaction
			return err
		}
		fwdFee, err := loadGramsU128(&v)
		if err != nil {
			return fmt.Errorf("failed to load fwd fee: %w", err)
		}
		if fwdFee != env.fwdFeeValue {
			return fmt.Errorf("in msg fwd fee %s does not match envelope remaining fee %s",
				fwdFee, env.fwdFeeValue)
		}
		info, err := parseIntMsgInfoView(env.msg)
		if err != nil {
			return fmt.Errorf("failed to parse enveloped message: %w", err)
		}
		// fees_collected := fwd_fee_remaining (original bits)
		if err = env.fwdFeeRemaining.appendTo(dst); err != nil {
			return err
		}
		// value_imported := msg.value + pre-v12 ihr_fee + fwd_fee_remaining
		sum, err := info.valueWithRemainingFee(env.fwdFeeValue, a.GlobalVersion)
		if err != nil {
			return err
		}
		if err = sum.storeTo(dst); err != nil {
			return err
		}
		if err = info.valueExtra.appendTo(dst); err != nil {
			return err
		}
		return nil

	case 0b101: // msg_import_tr
		// in_msg:^MsgEnvelope out_msg:^MsgEnvelope transit_fee:Grams
		if v.RefsNum() < 2 {
			return fmt.Errorf("msg_import_tr must have 2 refs")
		}
		envCell, err := v.LoadRefCell()
		if err != nil {
			return err
		}
		env, err := parseMsgEnvelopeView(envCell)
		if err != nil {
			return fmt.Errorf("failed to parse in msg envelope: %w", err)
		}
		if _, err = v.LoadRefCell(); err != nil { // out_msg
			return err
		}
		transitFee, err := loadGramsU128(&v)
		if err != nil {
			return fmt.Errorf("failed to load transit fee: %w", err)
		}
		if transitFee.greater(env.fwdFeeValue) {
			return fmt.Errorf("transit fee %s is above envelope remaining fee %s",
				transitFee, env.fwdFeeValue)
		}
		info, err := parseIntMsgInfoView(env.msg)
		if err != nil {
			return fmt.Errorf("failed to parse enveloped message: %w", err)
		}
		// fees_collected := transit_fee (canonical encoding)
		if err = transitFee.storeTo(dst); err != nil {
			return err
		}
		// value_imported := msg.value + pre-v12 ihr_fee + fwd_fee_remaining
		sum, err := info.valueWithRemainingFee(env.fwdFeeValue, a.GlobalVersion)
		if err != nil {
			return err
		}
		if err = sum.storeTo(dst); err != nil {
			return err
		}
		if err = info.valueExtra.appendTo(dst); err != nil {
			return err
		}
		return nil

	case 0b110, 0b111: // msg_discard_fin / msg_discard_tr
		// in_msg:^MsgEnvelope transaction_id:uint64 fwd_fee:Grams [proof_delivered:^Cell]
		wantRefs := 1
		if tag == 0b111 {
			wantRefs = 2
		}
		if v.RefsNum() < wantRefs {
			return fmt.Errorf("msg_discard must have %d refs", wantRefs)
		}
		if _, err = v.LoadRefCell(); err != nil { // in_msg
			return err
		}
		if _, err = v.LoadUInt(64); err != nil { // transaction_id
			return err
		}
		fwdFee, err := loadRawGrams(&v)
		if err != nil {
			return fmt.Errorf("failed to load fwd fee: %w", err)
		}
		if tag == 0b111 {
			if _, err = v.LoadRefCell(); err != nil { // proof_delivered
				return err
			}
		}
		// fees_collected := fwd_fee; value_imported := fwd_fee (original bits, empty extra)
		if err = fwdFee.appendTo(dst); err != nil {
			return err
		}
		if err = fwdFee.appendTo(dst); err != nil {
			return err
		}
		if err = dst.StoreBoolBit(false); err != nil {
			return err
		}
		return nil

	default:
		return fmt.Errorf("unknown in msg tag %b", tag)
	}
}

// augInMsgDescrDeferredLeaf computes ImportFees for the deferred in-message
// variants (rest is the low 2 bits of the 5-bit tag):
//
//	msg_import_deferred_fin$00100 in_msg:^MsgEnvelope transaction:^Transaction
//	    fwd_fee:Grams = InMsg;
//	msg_import_deferred_tr$00101 in_msg:^MsgEnvelope out_msg:^MsgEnvelope = InMsg;
//
// deferred_fin collects fwd_fee_remaining, which must equal the stored fwd_fee;
// deferred_tr collects no fees. Both import msg.value plus fwd_fee_remaining,
// and global versions before 12 also include msg.ihr_fee.
func augInMsgDescrDeferredLeaf(v *cell.Slice, rest uint64, globalVersion uint32, dst *cell.Builder) error {
	if rest > 1 {
		return fmt.Errorf("unknown deferred in msg tag %b", 0b00100|rest)
	}

	if v.RefsNum() < 2 {
		return fmt.Errorf("deferred in msg must have 2 refs")
	}
	envCell, err := v.LoadRefCell()
	if err != nil {
		return err
	}
	env, err := parseMsgEnvelopeView(envCell)
	if err != nil {
		return fmt.Errorf("failed to parse in msg envelope: %w", err)
	}
	if _, err = v.LoadRefCell(); err != nil { // transaction / out_msg
		return err
	}

	info, err := parseIntMsgInfoView(env.msg)
	if err != nil {
		return fmt.Errorf("failed to parse enveloped message: %w", err)
	}

	if rest == 0 { // msg_import_deferred_fin
		fwdFee, err := loadGramsU128(v)
		if err != nil {
			return fmt.Errorf("failed to load fwd fee: %w", err)
		}
		if fwdFee != env.fwdFeeValue {
			return fmt.Errorf("in msg fwd fee %s does not match envelope remaining fee %s",
				fwdFee, env.fwdFeeValue)
		}
		// fees_collected := fwd_fee_remaining (original bits)
		if err = env.fwdFeeRemaining.appendTo(dst); err != nil {
			return err
		}
	} else { // msg_import_deferred_tr
		// fees_collected := 0
		if err = dst.StoreUInt(0, 4); err != nil {
			return err
		}
	}

	// value_imported := msg.value + pre-v12 ihr_fee + fwd_fee_remaining
	sum, err := info.valueWithRemainingFee(env.fwdFeeValue, globalVersion)
	if err != nil {
		return err
	}
	if err = sum.storeTo(dst); err != nil {
		return err
	}
	if err = info.valueExtra.appendTo(dst); err != nil {
		return err
	}
	return nil
}

func (AugInMsgDescr) CombineExtra(leftExtra, rightExtra *cell.Slice, dst *cell.Builder) error {
	if err := addGramsSlices(dst, leftExtra, rightExtra); err != nil {
		return fmt.Errorf("failed to combine fees collected: %w", err)
	}
	return addCurrencyCollectionSlices(dst, leftExtra, rightExtra)
}

// ===========================================================================
// AugOutMsgDescr: HashmapAugE 256 OutMsg CurrencyCollection
// ===========================================================================

// AugOutMsgDescr implements the augmentation of OutMsgDescr
// (block.tlb:212: _ (HashmapAugE 256 OutMsg CurrencyCollection)).
//
// Leaf rule: queued new, transit, transit-requested and deferred export
// variants carry msg.value plus envelope fwd_fee_remaining. Global versions
// before 12 also include msg.ihr_fee. All other variants, including every
// dequeue record, evaluate to the zero CurrencyCollection.
//
// Fork rule: add the two CurrencyCollections component-wise.
// Empty value: CurrencyCollection null (5 zero bits).
type AugOutMsgDescr struct {
	GlobalVersion uint32
}

func (AugOutMsgDescr) SkipExtra(loader *cell.Slice) error {
	return skipCurrencyCollectionBoundary(loader)
}

func (AugOutMsgDescr) EmptyExtra(dst *cell.Builder) error {
	if err := storeEmptyCurrencyCollection(dst); err != nil {
		return err
	}
	return nil
}

func (a AugOutMsgDescr) LeafExtra(value *cell.Slice, dst *cell.Builder) error {
	var v, tagView cell.Slice
	value.CopyInto(&v)
	v.CopyInto(&tagView)

	tag3, err := tagView.LoadUInt(3)
	if err != nil {
		return fmt.Errorf("failed to load out msg tag: %w", err)
	}
	tag := tag3
	tagBits := uint(3)
	switch tag3 {
	case 0b110: // msg_export_deq$1100 / msg_export_deq_short$1101
		tagBits = 4
	case 0b101:
		// Dispatch-queue variants msg_export_new_defer$10100 and
		// msg_export_deferred_tr$10101 use five-bit tags.
		tagBits = 5
	}
	if tagBits != 3 {
		v.CopyInto(&tagView)
		tag, err = tagView.LoadUInt(tagBits)
		if err != nil {
			return fmt.Errorf("failed to load out msg long tag: %w", err)
		}
	}

	zero := func(needBits uint, needRefs int) error {
		if v.BitsLeft() < needBits || v.RefsNum() < needRefs {
			return fmt.Errorf("truncated out msg with tag %b", tag)
		}
		if err := storeEmptyCurrencyCollection(dst); err != nil {
			return err
		}
		return nil
	}

	exported := func() error {
		if _, err = v.LoadUInt(tagBits); err != nil {
			return err
		}
		if v.RefsNum() < 2 {
			return fmt.Errorf("out msg with tag %b must have 2 refs", tag)
		}
		envCell, err := v.LoadRefCell()
		if err != nil {
			return err
		}
		if _, err = v.LoadRefCell(); err != nil { // transaction / imported
			return err
		}
		env, err := parseMsgEnvelopeView(envCell)
		if err != nil {
			return fmt.Errorf("failed to parse out msg envelope: %w", err)
		}
		info, err := parseIntMsgInfoView(env.msg)
		if err != nil {
			return fmt.Errorf("failed to parse enveloped message: %w", err)
		}

		// exported value = msg.value + pre-v12 ihr_fee + fwd_fee_remaining
		sum, err := info.valueWithRemainingFee(env.fwdFeeValue, a.GlobalVersion)
		if err != nil {
			return err
		}
		if err = sum.storeTo(dst); err != nil {
			return err
		}
		if err = info.valueExtra.appendTo(dst); err != nil {
			return err
		}
		return nil
	}

	switch tag {
	case 0b000: // msg_export_ext
		return zero(3, 2)
	case 0b010: // msg_export_imm
		return zero(3, 3)
	case 0b100: // msg_export_deq_imm
		return zero(3, 2)
	case 0b1100: // msg_export_deq
		return zero(4+63, 1)
	case 0b1101: // msg_export_deq_short
		return zero(4+256+32+64+64, 0)
	case 0b001, 0b011, 0b111: // msg_export_new / msg_export_tr / msg_export_tr_req
		return exported()
	case 0b10100, 0b10101: // msg_export_new_defer / msg_export_deferred_tr, same value rule
		return exported()
	default:
		return fmt.Errorf("unknown out msg tag %b", tag)
	}
}

func (AugOutMsgDescr) CombineExtra(leftExtra, rightExtra *cell.Slice, dst *cell.Builder) error {
	if err := addCurrencyCollectionSlices(dst, leftExtra, rightExtra); err != nil {
		return err
	}
	return nil
}

// ===========================================================================
// AugOutMsgQueue: HashmapAugE 352 EnqueuedMsg uint64
// ===========================================================================

// AugOutMsgQueue implements the augmentation of OutMsgQueue
// (block.tlb:214: _ (HashmapAugE 352 EnqueuedMsg uint64)).
//
// Leaf rule: derive the uint64 extra from the referenced message envelope,
// ignoring EnqueuedMsg.enqueued_lt. An explicit emitted_lt in
// msg_envelope_v2#5 takes precedence; otherwise use the enclosed message's
// created_lt.
//
// Fork rule: min(left, right). Empty value: 0.
type AugOutMsgQueue struct{}

func (AugOutMsgQueue) SkipExtra(loader *cell.Slice) error {
	return skipUint64Boundary(loader)
}

func (AugOutMsgQueue) EmptyExtra(dst *cell.Builder) error {
	return dst.StoreUInt(0, 64)
}

func (AugOutMsgQueue) LeafExtra(value *cell.Slice, dst *cell.Builder) error {
	var v cell.Slice
	value.CopyInto(&v)
	// EnqueuedMsg stores enqueued_lt inline and MsgEnvelope by reference; only the
	// reference contributes to the leaf extra.
	envCell, err := v.PeekRefCell()
	if err != nil {
		return fmt.Errorf("failed to load enqueued message envelope: %w", err)
	}

	env, err := parseMsgEnvelopeEmissionView(envCell)
	if err != nil {
		return fmt.Errorf("failed to parse enqueued message envelope: %w", err)
	}

	var lt uint64
	if env.hasEmittedLT {
		lt = env.emittedLT
	} else {
		lt, err = messageCreatedLT(env.msg)
		if err != nil {
			return fmt.Errorf("failed to read message created lt: %w", err)
		}
	}
	return dst.StoreUInt(lt, 64)
}

func (AugOutMsgQueue) CombineExtra(leftExtra, rightExtra *cell.Slice, dst *cell.Builder) error {
	l, err := leftExtra.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load left lt: %w", err)
	}
	r, err := rightExtra.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load right lt: %w", err)
	}
	if r < l {
		l = r
	}
	return dst.StoreUInt(l, 64)
}

// ===========================================================================
// AugDispatchQueue: HashmapAugE 256 AccountDispatchQueue uint64
// ===========================================================================

// AugDispatchQueue implements the augmentation of DispatchQueue
// (block.tlb:255: _ (HashmapAugE 256 AccountDispatchQueue uint64)),
// mirroring C++ Aug_DispatchQueue (block-parse.cpp).
//
// Leaf rule: the minimal lt key of the account's messages dictionary, or
// 2^64-1 when the dictionary is absent. Only the messages maybe-ref of the
// AccountDispatchQueue value is read.
//
// Fork rule: min(left, right). Empty value: 0.
type AugDispatchQueue struct{}

func (AugDispatchQueue) SkipExtra(loader *cell.Slice) error {
	return skipUint64Boundary(loader)
}

func (AugDispatchQueue) EmptyExtra(dst *cell.Builder) error {
	return dst.StoreUInt(0, 64)
}

func (AugDispatchQueue) LeafExtra(value *cell.Slice, dst *cell.Builder) error {
	var v cell.Slice
	value.CopyInto(&v)
	hasMessages, err := v.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load account dispatch queue messages flag: %w", err)
	}

	minLT := ^uint64(0)
	if hasMessages {
		messages, err := v.LoadRefCell()
		if err != nil {
			return fmt.Errorf("failed to load account dispatch queue messages: %w", err)
		}
		key, _, err := messages.AsDict(64).LoadMinMax(false, false)
		if err != nil {
			return fmt.Errorf("failed to find minimal account dispatch queue lt: %w", err)
		}
		// LoadMinMax builds the key cell itself with exactly 64 bits.
		minLT = key.MustBeginParse().MustLoadUInt(64)
	}
	return dst.StoreUInt(minLT, 64)
}

func (AugDispatchQueue) CombineExtra(leftExtra, rightExtra *cell.Slice, dst *cell.Builder) error {
	l, err := leftExtra.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load left lt: %w", err)
	}
	r, err := rightExtra.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load right lt: %w", err)
	}
	if r < l {
		l = r
	}
	return dst.StoreUInt(l, 64)
}

// ===========================================================================
// AugShardFees: HashmapAugE 96 ShardFeeCreated ShardFeeCreated
// ===========================================================================

// AugShardFees implements the augmentation of ShardFees
// (block.tlb:449: _ (HashmapAugE 96 ShardFeeCreated ShardFeeCreated)).
//
// Leaf rule: the extra is a verbatim copy of the leaf value, which must parse as
// ShardFeeCreated with no trailing bits or references.
//
// Fork rule: add the fees and create CurrencyCollections component-wise.
// Empty value: two zero CurrencyCollections (10 zero bits).
type AugShardFees struct{}

func (AugShardFees) SkipExtra(loader *cell.Slice) error {
	return skipShardFeeCreatedBoundary(loader)
}

func (AugShardFees) EmptyExtra(dst *cell.Builder) error {
	if err := storeEmptyCurrencyCollection(dst); err != nil {
		return err
	}
	if err := storeEmptyCurrencyCollection(dst); err != nil {
		return err
	}
	return nil
}

func (AugShardFees) LeafExtra(value *cell.Slice, dst *cell.Builder) error {
	var check cell.Slice
	value.CopyInto(&check)
	if err := skipShardFeeCreatedBoundary(&check); err != nil {
		return fmt.Errorf("shard fee value is not a valid ShardFeeCreated: %w", err)
	}
	if check.BitsLeft() != 0 || check.RefsNum() != 0 {
		return fmt.Errorf("shard fee value has %d trailing bits and %d trailing refs",
			check.BitsLeft(), check.RefsNum())
	}
	value.ToBuilderInto(dst)
	return nil
}

func (AugShardFees) CombineExtra(leftExtra, rightExtra *cell.Slice, dst *cell.Builder) error {
	if err := addCurrencyCollectionSlices(dst, leftExtra, rightExtra); err != nil { // fees
		return err
	}
	if err := addCurrencyCollectionSlices(dst, leftExtra, rightExtra); err != nil { // create
		return err
	}
	return nil
}

// ===========================================================================
// wrapper types and constructors
// ===========================================================================

// InMsgDescrAugDict wraps InMsgDescr (HashmapAugE 256 InMsg ImportFees).
type InMsgDescrAugDict struct {
	*cell.AugmentedDictionary
}

// OutMsgDescrAugDict wraps OutMsgDescr (HashmapAugE 256 OutMsg CurrencyCollection).
type OutMsgDescrAugDict struct {
	*cell.AugmentedDictionary
}

// LoadInMsgDescrAugDict loads a writable InMsgDescr dictionary using the rules
// active at globalVersion.
func LoadInMsgDescrAugDict(loader *cell.Slice, globalVersion uint32) (*InMsgDescrAugDict, error) {
	dict, err := loader.LoadAugDict(256, AugInMsgDescr{GlobalVersion: globalVersion}, false)
	if err != nil {
		return nil, err
	}
	return &InMsgDescrAugDict{AugmentedDictionary: dict}, nil
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

// LoadOutMsgDescrAugDict loads a writable OutMsgDescr dictionary using the
// rules active at globalVersion.
func LoadOutMsgDescrAugDict(loader *cell.Slice, globalVersion uint32) (*OutMsgDescrAugDict, error) {
	dict, err := loader.LoadAugDict(256, AugOutMsgDescr{GlobalVersion: globalVersion}, false)
	if err != nil {
		return nil, err
	}
	return &OutMsgDescrAugDict{AugmentedDictionary: dict}, nil
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
	return &AccountTransactionsAugDict{AugmentedDictionary: dict}, nil
}

// NewInMsgDescrAugDict returns an empty writable InMsgDescr dictionary using
// the rules active at globalVersion.
func NewInMsgDescrAugDict(globalVersion uint32) (*InMsgDescrAugDict, error) {
	dict, err := cell.NewAugDict(256, AugInMsgDescr{GlobalVersion: globalVersion})
	if err != nil {
		return nil, err
	}
	return &InMsgDescrAugDict{AugmentedDictionary: dict}, nil
}

// NewOutMsgDescrAugDict returns an empty writable OutMsgDescr dictionary using
// the rules active at globalVersion.
func NewOutMsgDescrAugDict(globalVersion uint32) (*OutMsgDescrAugDict, error) {
	dict, err := cell.NewAugDict(256, AugOutMsgDescr{GlobalVersion: globalVersion})
	if err != nil {
		return nil, err
	}
	return &OutMsgDescrAugDict{AugmentedDictionary: dict}, nil
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
