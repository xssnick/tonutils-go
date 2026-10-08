package tlb

import (
	"errors"
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func loadProcessedUptoRecordsOwned(dict *cell.Dictionary, ownerShard uint64) ([]ProcessedUptoRecord, error) {
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

func TestLoadProcessedUptoRecordsBorrowedParity(t *testing.T) {
	valid := func() *cell.Dictionary {
		records := []ProcessedUptoRecord{
			{ShardPrefix: testShardAll, MCSeqno: 90, LastMsgLT: 900, LastMsgHash: [32]byte{0x90}},
			{ShardPrefix: testShardAll, MCSeqno: 3, LastMsgLT: 30, LastMsgHash: [32]byte{0x03}},
			{ShardPrefix: testShardAll, MCSeqno: 41, LastMsgLT: 410, LastMsgHash: [32]byte{0x41}},
		}
		dict, err := ProcessedUptoDict(records)
		if err != nil {
			t.Fatal(err)
		}
		return dict
	}
	withForkPayload := func() *cell.Dictionary {
		root := valid().AsCell().ToBuilder().MustStoreUInt(1, 1).EndCell()
		return root.AsDict(processedUptoKeyBits)
	}
	withValue := func(shard uint64, mcSeqno uint32, value *cell.Cell) *cell.Dictionary {
		dict := cell.NewDict(processedUptoKeyBits)
		key := cell.BeginCell().MustStoreUInt(shard, 64).MustStoreUInt(uint64(mcSeqno), 32).EndCell()
		if err := dict.Set(key, value); err != nil {
			t.Fatal(err)
		}
		return dict
	}
	withLateTraversalError := func() *cell.Dictionary {
		dict := cell.NewDict(processedUptoKeyBits)
		value := cell.BeginCell().MustStoreUInt(1, 64).MustStoreSlice(make([]byte, 32), 256).EndCell()
		for _, key := range []struct {
			shard   uint64
			mcSeqno uint32
		}{
			{shard: 0, mcSeqno: 1},
			{shard: testShardAll, mcSeqno: 2},
		} {
			keyCell := cell.BeginCell().MustStoreUInt(key.shard, 64).MustStoreUInt(uint64(key.mcSeqno), 32).EndCell()
			if err := dict.Set(keyCell, value); err != nil {
				t.Fatal(err)
			}
		}

		pendingErr := errors.New("late processed info traversal error")
		loads := 0
		var trace *cell.Trace
		trace = cell.NewTrace(cell.TraceHooks{
			OnLoad: func(*cell.Cell) {
				loads++
			},
			OnChild: func(int) *cell.Trace {
				return trace
			},
			PendingError: func() error {
				if loads >= 3 {
					return pendingErr
				}
				return nil
			},
		})
		return dict.AsCell().WithoutTrace().AsDictWithTrace(processedUptoKeyBits, trace)
	}

	tests := []struct {
		name  string
		dict  func() *cell.Dictionary
		owner uint64
	}{
		{
			name:  "nil dictionary",
			dict:  func() *cell.Dictionary { return nil },
			owner: testShardAll,
		},
		{
			name:  "wrong key size",
			dict:  func() *cell.Dictionary { return cell.NewDict(processedUptoKeyBits - 1) },
			owner: testShardAll,
		},
		{
			name: "zero owner shard",
			dict: func() *cell.Dictionary { return cell.NewDict(processedUptoKeyBits) },
		},
		{
			name:  "empty",
			dict:  func() *cell.Dictionary { return cell.NewDict(processedUptoKeyBits) },
			owner: testShardAll,
		},
		{
			name:  "valid insertion order differs from key order",
			dict:  valid,
			owner: testShardAll,
		},
		{
			name:  "legacy fork payload",
			dict:  withForkPayload,
			owner: testShardAll,
		},
		{
			name: "zero record shard",
			dict: func() *cell.Dictionary {
				value := cell.BeginCell().MustStoreUInt(1, 64).MustStoreSlice(make([]byte, 32), 256).EndCell()
				return withValue(0, 1, value)
			},
			owner: testShardAll,
		},
		{
			name: "record outside owner shard",
			dict: func() *cell.Dictionary {
				value := cell.BeginCell().MustStoreUInt(1, 64).MustStoreSlice(make([]byte, 32), 256).EndCell()
				return withValue(0xc000000000000000, 1, value)
			},
			owner: 0x4000000000000000,
		},
		{
			name: "short value",
			dict: func() *cell.Dictionary {
				value := cell.BeginCell().MustStoreUInt(1, 64).MustStoreSlice(make([]byte, 32), 255).EndCell()
				return withValue(testShardAll, 1, value)
			},
			owner: testShardAll,
		},
		{
			name: "value with reference",
			dict: func() *cell.Dictionary {
				value := cell.BeginCell().MustStoreUInt(1, 64).MustStoreSlice(make([]byte, 32), 256).
					MustStoreRef(cell.BeginCell().EndCell()).EndCell()
				return withValue(testShardAll, 1, value)
			},
			owner: testShardAll,
		},
		{
			name:  "late traversal error takes priority over an earlier record error",
			dict:  withLateTraversalError,
			owner: testShardAll,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want, wantErr := loadProcessedUptoRecordsOwned(test.dict(), test.owner)
			got, gotErr := LoadProcessedUptoRecords(test.dict(), test.owner)
			if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
				t.Fatalf("error = %v, want %v", gotErr, wantErr)
			}
			if len(got) != len(want) {
				t.Fatalf("loaded %d records, want %d", len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("record %d = %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	}
}

var processedUptoRecordsSink []ProcessedUptoRecord

func BenchmarkLoadProcessedUptoRecords(b *testing.B) {
	const recordsCount = 64

	records := make([]ProcessedUptoRecord, recordsCount)
	for i := range records {
		records[i] = ProcessedUptoRecord{
			ShardPrefix: testShardAll,
			MCSeqno:     uint32(i),
			LastMsgLT:   uint64(i + 1),
			LastMsgHash: [32]byte{byte(i), byte(i >> 8)},
		}
	}
	dict, err := ProcessedUptoDict(records)
	if err != nil {
		b.Fatal(err)
	}

	bench := func(b *testing.B, load func(*cell.Dictionary, uint64) ([]ProcessedUptoRecord, error)) {
		b.ReportAllocs()
		for b.Loop() {
			loaded, err := load(dict, testShardAll)
			if err != nil {
				b.Fatal(err)
			}
			processedUptoRecordsSink = loaded
		}
	}
	b.Run("LoadAll", func(b *testing.B) {
		bench(b, loadProcessedUptoRecordsOwned)
	})
	b.Run("BorrowedIterator", func(b *testing.B) {
		bench(b, LoadProcessedUptoRecords)
	})
}
