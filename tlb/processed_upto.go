package tlb

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// ProcessedInfo (block.tlb:224-226):
//
//	processed_upto$_ last_msg_lt:uint64 last_msg_hash:bits256 = ProcessedUpto;
//	_ (HashmapE 96 ProcessedUpto) = ProcessedInfo;
//
// The 96-bit key is shard:uint64 ++ mc_seqno:uint32. The semantics mirror
// C++ block::MsgProcessedUpto / block::MsgProcessedUptoCollection
// (crypto/block/block.cpp).

const (
	processedUptoKeyBits   = 96
	processedUptoValueBits = 64 + 256
)

// ProcessedUptoRecord is one ProcessedInfo entry: as of masterchain seqno
// MCSeqno, every inbound message routed into ShardPrefix with
// (lt, hash) <= (LastMsgLT, LastMsgHash) has been processed.
type ProcessedUptoRecord struct {
	ShardPrefix uint64
	MCSeqno     uint32
	LastMsgLT   uint64
	LastMsgHash [32]byte
}

// ProcessedMsgDescr describes one queued inbound message the way C++
// block::EnqueuedMsgDescr does. LT is the canonical import-order lt (the
// out-queue augmentation value: MsgEnvelope.emitted_lt for v2 envelopes,
// Message.created_lt otherwise); EnqueuedLT is EnqueuedMsg.enqueued_lt and
// may exceed LT for transit entries.
type ProcessedMsgDescr struct {
	CurWorkchain  int32
	CurPrefix     uint64
	NextWorkchain int32
	NextPrefix    uint64
	LT            uint64
	EnqueuedLT    uint64
	Hash          [32]byte
}

// ShardEndLTFunc resolves the end lt of the shard containing
// (workchain, prefix) as of the masterchain state at mcSeqno — the C++
// compute_shard_end_lt binding (ShardConfig::get_shard_end_lt).
type ShardEndLTFunc func(mcSeqno uint32, workchain int32, prefix uint64) uint64

// LoadProcessedUptoRecords parses every ProcessedInfo record of a state
// dictionary owned by ownerShard, validating the exact C++ unpack shape: a
// 96-bit key, a reference-free 320-bit value and a non-zero record shard
// contained in the owner shard.
func LoadProcessedUptoRecords(dict *cell.Dictionary, ownerShard uint64) ([]ProcessedUptoRecord, error) {
	if dict == nil {
		return nil, fmt.Errorf("processed info dictionary is nil")
	}
	if dict.GetKeySize() != processedUptoKeyBits {
		return nil, fmt.Errorf("processed info key size is %d, want %d", dict.GetKeySize(), processedUptoKeyBits)
	}
	if ownerShard == 0 {
		return nil, fmt.Errorf("processed info owner has a zero shard")
	}
	items, err := dict.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to load processed info: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}

	records := make([]ProcessedUptoRecord, len(items))
	for i := range items {
		rec := &records[i]
		// LoadAll yields keys of exactly 96 bits, so the key reads cannot fail.
		rec.ShardPrefix = items[i].Key.MustLoadUInt(64)
		if rec.ShardPrefix == 0 {
			return nil, fmt.Errorf("processed info record %d has a zero shard", i)
		}
		if !shardContainsPrefix(ownerShard, rec.ShardPrefix) {
			return nil, fmt.Errorf("processed info record %d shard %016x is outside the owner shard %016x",
				i, rec.ShardPrefix, ownerShard)
		}
		rec.MCSeqno = uint32(items[i].Key.MustLoadUInt(32))

		value := items[i].Value
		if value.BitsLeft() != processedUptoValueBits || value.RefsNum() != 0 {
			return nil, fmt.Errorf("malformed processed info value %d", i)
		}
		rec.LastMsgLT = value.MustLoadUInt(64)
		copy(rec.LastMsgHash[:], value.MustLoadSlice(256))
	}
	return records, nil
}

// SetProcessedUptoRecord writes or replaces one record in a writable
// ProcessedInfo dictionary.
func SetProcessedUptoRecord(dict *cell.Dictionary, rec ProcessedUptoRecord) error {
	if dict == nil {
		return fmt.Errorf("processed info dictionary is nil")
	}
	if dict.GetKeySize() != processedUptoKeyBits {
		return fmt.Errorf("processed info key size is %d, want %d", dict.GetKeySize(), processedUptoKeyBits)
	}
	if rec.ShardPrefix == 0 {
		return fmt.Errorf("processed info record has a zero shard")
	}
	key := cell.BeginCell().
		MustStoreUInt(rec.ShardPrefix, 64).
		MustStoreUInt(uint64(rec.MCSeqno), 32).
		EndCell()
	value := cell.BeginCell().
		MustStoreUInt(rec.LastMsgLT, 64).
		MustStoreSlice(rec.LastMsgHash[:], 256)
	return dict.SetBuilder(key, value)
}

// ProcessedUptoDict serializes records into a fresh canonical ProcessedInfo
// dictionary, mirroring C++ MsgProcessedUptoCollection::pack (without its
// implicit compactify).
func ProcessedUptoDict(records []ProcessedUptoRecord) (*cell.Dictionary, error) {
	dict := cell.NewDict(processedUptoKeyBits)
	for i := range records {
		if err := SetProcessedUptoRecord(dict, records[i]); err != nil {
			return nil, fmt.Errorf("failed to store processed info record %d: %w", i, err)
		}
	}
	return dict, nil
}

// Contains reports the C++ MsgProcessedUpto::contains domination: a
// same-or-wider shard with a same-or-newer masterchain seqno and a
// same-or-later (lt, hash) bound.
func (r ProcessedUptoRecord) Contains(other ProcessedUptoRecord) bool {
	return ShardID(r.ShardPrefix).IsAncestor(ShardID(other.ShardPrefix)) &&
		r.MCSeqno >= other.MCSeqno &&
		(r.LastMsgLT > other.LastMsgLT ||
			(r.LastMsgLT == other.LastMsgLT && bytes.Compare(r.LastMsgHash[:], other.LastMsgHash[:]) >= 0))
}

// alreadyProcessed mirrors C++ MsgProcessedUpto::already_processed. The
// resolver is required for any message generated outside the record's shard
// (a neighbor shard of the same workchain or another workchain); reaching
// that branch without one is an error, matching the C++ can_check_processed
// contract instead of silently under-reporting.
func (r *ProcessedUptoRecord) alreadyProcessed(msg *ProcessedMsgDescr, shardEndLT ShardEndLTFunc) (bool, error) {
	if msg.LT > r.LastMsgLT {
		return false, nil
	}
	if !shardContainsPrefix(r.ShardPrefix, msg.NextPrefix) {
		return false, nil
	}
	if msg.LT == r.LastMsgLT && bytes.Compare(r.LastMsgHash[:], msg.Hash[:]) < 0 {
		return false, nil
	}
	if msg.CurWorkchain == msg.NextWorkchain && shardContainsPrefix(r.ShardPrefix, msg.CurPrefix) {
		// Messages generated in the same shardchain are processed strictly in
		// the (lt, hash) order, so the bound alone covers them.
		return true, nil
	}
	if shardEndLT == nil {
		return false, fmt.Errorf("no shard end lt resolver to check a message generated outside shard %016x", r.ShardPrefix)
	}
	return msg.EnqueuedLT < shardEndLT(r.MCSeqno, msg.CurWorkchain, msg.CurPrefix), nil
}

// AlreadyProcessed mirrors C++ MsgProcessedUptoCollection::already_processed
// for a collection owned by (ownerWorkchain, ownerShard): whether any record
// covers the message. It fails when a record needs the shardEndLT resolver
// and none was supplied.
func AlreadyProcessed(
	records []ProcessedUptoRecord,
	ownerWorkchain int32,
	ownerShard uint64,
	msg *ProcessedMsgDescr,
	shardEndLT ShardEndLTFunc,
) (bool, error) {
	if msg.NextWorkchain != ownerWorkchain || !shardContainsPrefix(ownerShard, msg.NextPrefix) {
		return false, nil
	}
	for i := range records {
		processed, err := records[i].alreadyProcessed(msg, shardEndLT)
		if err != nil {
			return false, err
		}
		if processed {
			return true, nil
		}
	}
	return false, nil
}

// InsertProcessedUpto mirrors C++ MsgProcessedUptoCollection::insert: a bound
// already covered by an existing record is dropped, otherwise a record for
// the owner shard is appended. The lt must be non-zero.
func InsertProcessedUpto(
	records []ProcessedUptoRecord,
	ownerShard uint64,
	mcSeqno uint32,
	lastLT uint64,
	lastHash [32]byte,
) ([]ProcessedUptoRecord, error) {
	if lastLT == 0 {
		return nil, fmt.Errorf("processed info bound has a zero lt")
	}
	inserted := ProcessedUptoRecord{
		ShardPrefix: ownerShard,
		MCSeqno:     mcSeqno,
		LastMsgLT:   lastLT,
		LastMsgHash: lastHash,
	}
	for i := range records {
		if records[i].Contains(inserted) {
			return records, nil
		}
	}
	return append(records, inserted), nil
}

// CompactifyProcessedUpto mirrors C++ MsgProcessedUptoCollection::compactify:
// records are ordered by (shard, mc_seqno) and every record dominated by
// another surviving record is dropped in place.
func CompactifyProcessedUpto(records []ProcessedUptoRecord) []ProcessedUptoRecord {
	sort.Slice(records, func(i, j int) bool {
		return records[i].ShardPrefix < records[j].ShardPrefix ||
			(records[i].ShardPrefix == records[j].ShardPrefix && records[i].MCSeqno < records[j].MCSeqno)
	})
	if len(records) < 2 {
		return records
	}

	var markBuf [8]bool
	mark := markBuf[:]
	if len(records) > len(mark) {
		mark = make([]bool, len(records))
	} else {
		mark = mark[:len(records)]
	}
	dropped := 0
	for i := range records {
		for j := range records {
			if j != i && !mark[j] && records[j].Contains(records[i]) {
				mark[i] = true
				dropped++
				break
			}
		}
	}
	if dropped == 0 {
		return records
	}
	kept := records[:0]
	for i := range records {
		if !mark[i] {
			kept = append(kept, records[i])
		}
	}
	return kept
}

// shardContainsPrefix is C++ ton::shard_contains(ShardId, AccountIdPrefix):
// whether the marked shard prefix covers the 64-bit account prefix.
func shardContainsPrefix(shard, prefix uint64) bool {
	return (shard^prefix)&(bitsNegate64(lowerBit64(shard))<<1) == 0
}
