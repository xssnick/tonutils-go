package ton

import (
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestCheckShardInMasterProofForAccountRejectsWrongShard(t *testing.T) {
	addr := address.NewAddress(0, 0, make([]byte, 32))
	rightShard := uint64(0xC000000000000000)
	if tlb.ShardID(rightShard).ContainsAddress(addr) {
		t.Fatal("test setup should use a shard outside of the account address")
	}

	shard := &BlockIDExt{
		Workchain: 0,
		Shard:     int64(rightShard),
		SeqNo:     1,
		RootHash:  make([]byte, 32),
		FileHash:  make([]byte, 32),
	}
	err := CheckShardInMasterProofForAccount(&BlockIDExt{}, nil, shard, addr)
	if err == nil || !strings.Contains(err.Error(), "account address is not in shard") {
		t.Fatalf("expected shard/address binding error, got %v", err)
	}
}

func TestShardFromBinTreeKeyUsesTreePath(t *testing.T) {
	tests := []struct {
		name string
		key  *cell.Cell
		want uint64
	}{
		{
			name: "root",
			key:  cell.BeginCell().EndCell(),
			want: 0x8000000000000000,
		},
		{
			name: "left",
			key:  cell.BeginCell().MustStoreUInt(0, 1).EndCell(),
			want: 0x4000000000000000,
		},
		{
			name: "right",
			key:  cell.BeginCell().MustStoreUInt(1, 1).EndCell(),
			want: 0xC000000000000000,
		},
		{
			name: "right-left",
			key:  cell.BeginCell().MustStoreUInt(0b10, 2).EndCell(),
			want: 0xA000000000000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := shardFromBinTreeKey(tt.key)
			if err != nil {
				t.Fatal(err)
			}
			if uint64(got) != tt.want {
				t.Fatalf("unexpected shard id: got=%016x want=%016x", uint64(got), tt.want)
			}
		})
	}
}
