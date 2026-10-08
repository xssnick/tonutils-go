package tlb

import (
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

const testShardAll = uint64(1) << 63

func TestProcessedUptoRecordsRoundTrip(t *testing.T) {
	records := []ProcessedUptoRecord{
		{ShardPrefix: testShardAll, MCSeqno: 7, LastMsgLT: 1_000_100, LastMsgHash: [32]byte{0x11}},
		{ShardPrefix: 0x4000000000000000, MCSeqno: 9, LastMsgLT: 2_000_200, LastMsgHash: [32]byte{0x22}},
		{ShardPrefix: 0xc000000000000000, MCSeqno: 3, LastMsgLT: 500, LastMsgHash: [32]byte{0x33}},
	}

	dict, err := ProcessedUptoDict(records)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProcessedUptoRecords(dict, testShardAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != len(records) {
		t.Fatalf("loaded %d records, want %d", len(loaded), len(records))
	}
	byKey := map[[2]uint64]ProcessedUptoRecord{}
	for _, rec := range loaded {
		byKey[[2]uint64{rec.ShardPrefix, uint64(rec.MCSeqno)}] = rec
	}
	for _, want := range records {
		got, ok := byKey[[2]uint64{want.ShardPrefix, uint64(want.MCSeqno)}]
		if !ok || got != want {
			t.Fatalf("record %+v round-tripped as %+v", want, got)
		}
	}

	// A rebuilt dictionary must be canonical: bit-identical to the source.
	rebuilt, err := ProcessedUptoDict(loaded)
	if err != nil {
		t.Fatal(err)
	}
	source, err := dict.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	mirrored, err := rebuilt.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	if source.HashKey() != mirrored.HashKey() {
		t.Fatal("rebuilt processed info differs from its source")
	}
}

func TestSetProcessedUptoRecordReplacesValue(t *testing.T) {
	dict := cell.NewDict(96)
	rec := ProcessedUptoRecord{ShardPrefix: testShardAll, MCSeqno: 5, LastMsgLT: 100, LastMsgHash: [32]byte{1}}
	if err := SetProcessedUptoRecord(dict, rec); err != nil {
		t.Fatal(err)
	}
	rec.LastMsgLT = 200
	rec.LastMsgHash = [32]byte{2}
	if err := SetProcessedUptoRecord(dict, rec); err != nil {
		t.Fatal(err)
	}

	records, err := LoadProcessedUptoRecords(dict, testShardAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0] != rec {
		t.Fatalf("records = %+v, want the replaced bound", records)
	}
}

func TestLoadProcessedUptoRecordsRejectsMalformedEntries(t *testing.T) {
	tests := []struct {
		name  string
		dict  func() *cell.Dictionary
		owner uint64
		wants string
	}{
		{
			name:  "nil dictionary",
			dict:  func() *cell.Dictionary { return nil },
			wants: "nil",
		},
		{
			name:  "wrong key size",
			dict:  func() *cell.Dictionary { return cell.NewDict(95) },
			wants: "key size",
		},
		{
			name: "zero owner shard",
			dict: func() *cell.Dictionary { return cell.NewDict(96) },
			// the zero default of the owner field stays in place
			wants: "owner has a zero shard",
		},
		{
			name: "shard outside the owner",
			dict: func() *cell.Dictionary {
				dict := cell.NewDict(96)
				key := cell.BeginCell().MustStoreUInt(0xc000000000000000, 64).MustStoreUInt(1, 32).EndCell()
				value := cell.BeginCell().MustStoreUInt(1, 64).MustStoreSlice(make([]byte, 32), 256).EndCell()
				if err := dict.Set(key, value); err != nil {
					t.Fatal(err)
				}
				return dict
			},
			owner: 0x4000000000000000,
			wants: "outside the owner shard",
		},
		{
			name: "zero shard",
			dict: func() *cell.Dictionary {
				dict := cell.NewDict(96)
				key := cell.BeginCell().MustStoreUInt(0, 64).MustStoreUInt(1, 32).EndCell()
				value := cell.BeginCell().MustStoreUInt(1, 64).MustStoreSlice(make([]byte, 32), 256).EndCell()
				if err := dict.Set(key, value); err != nil {
					t.Fatal(err)
				}
				return dict
			},
			owner: testShardAll,
			wants: "zero shard",
		},
		{
			name: "short value",
			dict: func() *cell.Dictionary {
				dict := cell.NewDict(96)
				key := cell.BeginCell().MustStoreUInt(testShardAll, 64).MustStoreUInt(1, 32).EndCell()
				value := cell.BeginCell().MustStoreUInt(1, 64).MustStoreSlice(make([]byte, 32), 255).EndCell()
				if err := dict.Set(key, value); err != nil {
					t.Fatal(err)
				}
				return dict
			},
			owner: testShardAll,
			wants: "malformed",
		},
		{
			name: "value with reference",
			dict: func() *cell.Dictionary {
				dict := cell.NewDict(96)
				key := cell.BeginCell().MustStoreUInt(testShardAll, 64).MustStoreUInt(1, 32).EndCell()
				value := cell.BeginCell().MustStoreUInt(1, 64).MustStoreSlice(make([]byte, 32), 256).
					MustStoreRef(cell.BeginCell().EndCell()).EndCell()
				if err := dict.Set(key, value); err != nil {
					t.Fatal(err)
				}
				return dict
			},
			owner: testShardAll,
			wants: "malformed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadProcessedUptoRecords(test.dict(), test.owner)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), test.wants) {
				t.Fatalf("error %q does not mention %q", err, test.wants)
			}
		})
	}
}

func TestProcessedUptoRecordContains(t *testing.T) {
	base := ProcessedUptoRecord{ShardPrefix: testShardAll, MCSeqno: 10, LastMsgLT: 1_000, LastMsgHash: [32]byte{0x50}}
	tests := []struct {
		name  string
		other ProcessedUptoRecord
		want  bool
	}{
		{name: "itself", other: base, want: true},
		{
			name:  "narrower shard and older bound",
			other: ProcessedUptoRecord{ShardPrefix: 0x4000000000000000, MCSeqno: 9, LastMsgLT: 999, LastMsgHash: [32]byte{0xff}},
			want:  true,
		},
		{
			name:  "same lt lower hash",
			other: ProcessedUptoRecord{ShardPrefix: testShardAll, MCSeqno: 10, LastMsgLT: 1_000, LastMsgHash: [32]byte{0x4f}},
			want:  true,
		},
		{
			name:  "same lt higher hash",
			other: ProcessedUptoRecord{ShardPrefix: testShardAll, MCSeqno: 10, LastMsgLT: 1_000, LastMsgHash: [32]byte{0x51}},
			want:  false,
		},
		{
			name:  "newer masterchain seqno",
			other: ProcessedUptoRecord{ShardPrefix: testShardAll, MCSeqno: 11, LastMsgLT: 1, LastMsgHash: [32]byte{}},
			want:  false,
		},
		{
			name:  "later lt",
			other: ProcessedUptoRecord{ShardPrefix: testShardAll, MCSeqno: 10, LastMsgLT: 1_001, LastMsgHash: [32]byte{}},
			want:  false,
		},
		{
			name:  "wider shard",
			other: ProcessedUptoRecord{ShardPrefix: 0x4000000000000000, MCSeqno: 9, LastMsgLT: 1, LastMsgHash: [32]byte{}},
			want:  true,
		},
	}
	narrow := ProcessedUptoRecord{ShardPrefix: 0x4000000000000000, MCSeqno: 10, LastMsgLT: 1_000, LastMsgHash: [32]byte{0x50}}
	if narrow.Contains(base) {
		t.Fatal("a narrower shard must not contain its parent")
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := base.Contains(test.other); got != test.want {
				t.Fatalf("contains = %t, want %t", got, test.want)
			}
		})
	}
}

func TestAlreadyProcessed(t *testing.T) {
	records := []ProcessedUptoRecord{
		{ShardPrefix: testShardAll, MCSeqno: 10, LastMsgLT: 1_000, LastMsgHash: [32]byte{0x50}},
	}
	sameChain := func(lt uint64, hash byte) *ProcessedMsgDescr {
		return &ProcessedMsgDescr{
			CurWorkchain: 0, CurPrefix: 0x1122334455667788,
			NextWorkchain: 0, NextPrefix: 0x99aabbccddeeff00,
			LT: lt, EnqueuedLT: lt, Hash: [32]byte{hash},
		}
	}
	crossChain := func(lt, enqueuedLT uint64, hash byte) *ProcessedMsgDescr {
		return &ProcessedMsgDescr{
			CurWorkchain: -1, CurPrefix: testShardAll,
			NextWorkchain: 0, NextPrefix: 0x99aabbccddeeff00,
			LT: lt, EnqueuedLT: enqueuedLT, Hash: [32]byte{hash},
		}
	}
	endLT := func(mcSeqno uint32, workchain int32, prefix uint64) uint64 {
		if workchain != -1 || mcSeqno != 10 {
			t.Fatalf("unexpected shard end lt query: mc=%d wc=%d prefix=%x", mcSeqno, workchain, prefix)
		}
		return 900
	}

	tests := []struct {
		name    string
		msg     *ProcessedMsgDescr
		endLT   ShardEndLTFunc
		covered bool
		wantErr bool
	}{
		{name: "same chain below bound", msg: sameChain(999, 0xff), covered: true},
		{name: "same chain at bound lower hash", msg: sameChain(1_000, 0x4f), covered: true},
		{name: "same chain at bound exact hash", msg: sameChain(1_000, 0x50), covered: true},
		{name: "same chain at bound higher hash", msg: sameChain(1_000, 0x51), covered: false},
		{name: "same chain above bound", msg: sameChain(1_001, 0x00), covered: false},
		{name: "cross chain without resolver", msg: crossChain(999, 999, 0x00), wantErr: true},
		{name: "cross chain enqueued before source end lt", msg: crossChain(800, 800, 0x00), endLT: endLT, covered: true},
		{name: "cross chain enqueued after source end lt", msg: crossChain(950, 950, 0x00), endLT: endLT, covered: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := AlreadyProcessed(records, 0, testShardAll, test.msg, test.endLT)
			if test.wantErr {
				if err == nil {
					t.Fatal("a check requiring the resolver must fail without one")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.covered {
				t.Fatalf("already processed = %t, want %t", got, test.covered)
			}
		})
	}

	foreignHop := sameChain(1, 0)
	foreignHop.NextWorkchain = -1
	covered, err := AlreadyProcessed(records, 0, testShardAll, foreignHop, nil)
	if err != nil {
		t.Fatal(err)
	}
	if covered {
		t.Fatal("a next hop outside the owner workchain must not be covered")
	}
}

func TestInsertProcessedUpto(t *testing.T) {
	records := []ProcessedUptoRecord{
		{ShardPrefix: testShardAll, MCSeqno: 10, LastMsgLT: 1_000, LastMsgHash: [32]byte{0x50}},
	}

	if _, err := InsertProcessedUpto(records, testShardAll, 11, 0, [32]byte{}); err == nil {
		t.Fatal("a zero lt bound must be rejected")
	}

	covered, err := InsertProcessedUpto(records, testShardAll, 10, 999, [32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if len(covered) != 1 {
		t.Fatalf("covered insert produced %d records, want 1", len(covered))
	}

	extended, err := InsertProcessedUpto(records, testShardAll, 11, 2_000, [32]byte{0x60})
	if err != nil {
		t.Fatal(err)
	}
	if len(extended) != 2 || extended[1].MCSeqno != 11 || extended[1].LastMsgLT != 2_000 {
		t.Fatalf("extended records = %+v", extended)
	}
}

func TestCompactifyProcessedUpto(t *testing.T) {
	tests := []struct {
		name    string
		records []ProcessedUptoRecord
		want    []ProcessedUptoRecord
	}{
		{name: "empty"},
		{
			name: "single",
			records: []ProcessedUptoRecord{
				{ShardPrefix: testShardAll, MCSeqno: 1, LastMsgLT: 1},
			},
			want: []ProcessedUptoRecord{
				{ShardPrefix: testShardAll, MCSeqno: 1, LastMsgLT: 1},
			},
		},
		{
			name: "newer bound drops the older",
			records: []ProcessedUptoRecord{
				{ShardPrefix: testShardAll, MCSeqno: 11, LastMsgLT: 2_000, LastMsgHash: [32]byte{2}},
				{ShardPrefix: testShardAll, MCSeqno: 10, LastMsgLT: 1_000, LastMsgHash: [32]byte{1}},
			},
			want: []ProcessedUptoRecord{
				{ShardPrefix: testShardAll, MCSeqno: 11, LastMsgLT: 2_000, LastMsgHash: [32]byte{2}},
			},
		},
		{
			name: "wider shard dominates narrower",
			records: []ProcessedUptoRecord{
				{ShardPrefix: 0x4000000000000000, MCSeqno: 5, LastMsgLT: 500},
				{ShardPrefix: testShardAll, MCSeqno: 6, LastMsgLT: 600},
			},
			want: []ProcessedUptoRecord{
				{ShardPrefix: testShardAll, MCSeqno: 6, LastMsgLT: 600},
			},
		},
		{
			name: "incomparable records survive",
			records: []ProcessedUptoRecord{
				{ShardPrefix: testShardAll, MCSeqno: 11, LastMsgLT: 500},
				{ShardPrefix: testShardAll, MCSeqno: 10, LastMsgLT: 1_000},
			},
			want: []ProcessedUptoRecord{
				{ShardPrefix: testShardAll, MCSeqno: 10, LastMsgLT: 1_000},
				{ShardPrefix: testShardAll, MCSeqno: 11, LastMsgLT: 500},
			},
		},
		{
			name: "disjoint shards survive",
			records: []ProcessedUptoRecord{
				{ShardPrefix: 0xc000000000000000, MCSeqno: 5, LastMsgLT: 100},
				{ShardPrefix: 0x4000000000000000, MCSeqno: 5, LastMsgLT: 100},
			},
			want: []ProcessedUptoRecord{
				{ShardPrefix: 0x4000000000000000, MCSeqno: 5, LastMsgLT: 100},
				{ShardPrefix: 0xc000000000000000, MCSeqno: 5, LastMsgLT: 100},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CompactifyProcessedUpto(append([]ProcessedUptoRecord(nil), test.records...))
			if len(got) != len(test.want) {
				t.Fatalf("compactified records = %+v, want %+v", got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("compactified records = %+v, want %+v", got, test.want)
				}
			}
		})
	}
}

func TestShardContainsPrefix(t *testing.T) {
	tests := []struct {
		shard  uint64
		prefix uint64
		want   bool
	}{
		{shard: testShardAll, prefix: 0x0000000000000000, want: true},
		{shard: testShardAll, prefix: 0xffffffffffffffff, want: true},
		{shard: 0x4000000000000000, prefix: 0x3fffffffffffffff, want: true},
		{shard: 0x4000000000000000, prefix: 0x8000000000000000, want: false},
		{shard: 0xc000000000000000, prefix: 0x8000000000000000, want: true},
		{shard: 0xc000000000000000, prefix: 0x7fffffffffffffff, want: false},
	}
	for _, test := range tests {
		if got := shardContainsPrefix(test.shard, test.prefix); got != test.want {
			t.Fatalf("shardContainsPrefix(%x, %x) = %t, want %t", test.shard, test.prefix, got, test.want)
		}
	}
}
