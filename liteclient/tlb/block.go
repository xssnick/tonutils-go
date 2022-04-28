package tlb

import (
	"encoding/binary"
	"errors"
)

type BlockInfo struct {
	Workchain int32
	Shard     int64
	SeqNo     int32
	RootHash  []byte
	FileHash  []byte
}

func (b *BlockInfo) Load(data []byte) ([]byte, error) {
	if len(data) < 32+32+4+8+4 {
		return nil, errors.New("not enough length")
	}

	b.Workchain = int32(binary.LittleEndian.Uint32(data))
	data = data[4:]

	b.Shard = int64(binary.LittleEndian.Uint64(data))
	data = data[8:]

	b.SeqNo = int32(binary.LittleEndian.Uint32(data))
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
	binary.LittleEndian.PutUint64(data[4:], uint64(b.Shard))
	binary.LittleEndian.PutUint32(data[12:], uint32(b.SeqNo))

	data = append(data, b.RootHash...)
	data = append(data, b.FileHash...)

	return data
}
