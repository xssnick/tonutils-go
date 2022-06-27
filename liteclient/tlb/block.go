package tlb

import (
	"encoding/binary"
	"errors"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type BlockInfo struct {
	Workchain int32
	Shard     uint64
	SeqNo     uint32
	RootHash  []byte
	FileHash  []byte
}

func (b *BlockInfo) Load(data []byte) ([]byte, error) {
	if len(data) < 32+32+4+8+4 {
		return nil, errors.New("not enough length")
	}

	b.Workchain = int32(binary.LittleEndian.Uint32(data))
	data = data[4:]

	b.Shard = binary.LittleEndian.Uint64(data)
	data = data[8:]

	b.SeqNo = binary.LittleEndian.Uint32(data)
	data = data[4:]

	b.RootHash = data[:32]
	data = data[32:]

	b.FileHash = data[:32]
	data = data[32:]

	return data, nil
}

func (b *BlockInfo) Serialize() []byte {
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data, uint32(b.Workchain))
	binary.LittleEndian.PutUint64(data[4:], b.Shard)
	binary.LittleEndian.PutUint32(data[12:], b.SeqNo)

	data = append(data, b.RootHash...)
	data = append(data, b.FileHash...)

	return data
}

type StateUpdate struct {
	Old ShardState `tlb:"^"`
	New ShardState `tlb:"^"`
}

type McBlockExtra struct {
	_           Magic            `tlb:"#cca5"`
	KeyBlock    uint8            `tlb:"## 1"`
	ShardHashes *cell.Dictionary `tlb:"maybe ^dict 32"`
	ShardFees   *cell.Dictionary `tlb:"maybe ^dict 96"`
}

type BlockExtra struct {
	InMsgDesc          *cell.Cell    `tlb:"^"`
	OutMsgDesc         *cell.Cell    `tlb:"^"`
	ShardAccountBlocks *cell.Cell    `tlb:"^"`
	RandSeed           []byte        `tlb:"bits 256"`
	CreatedBy          []byte        `tlb:"bits 256"`
	Custom             *McBlockExtra `tlb:"maybe ^"`
}

type Block struct {
	_           Magic       `tlb:"#11ef55aa"`
	GlobalID    int32       `tlb:"## 32"`
	BlockInfo   *cell.Cell  `tlb:"^"`
	ValueFlow   *cell.Cell  `tlb:"^"`
	StateUpdate StateUpdate `tlb:"^"`
	Extra       *BlockExtra `tlb:"^"`
}
