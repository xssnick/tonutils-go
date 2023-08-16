package ton

import (
	"crypto/sha512"
	"encoding/binary"
	"math/big"
)

type validatorSetDescription struct {
	seed      [32]byte
	shard     uint64
	workchain int32
	ccSeqno   uint32

	hash []byte
}

type ValidatorSetPRNG struct {
	desc  validatorSetDescription
	pos   int
	limit int
}

func NewValidatorSetPRNG(shard int64, workchain int32, catchainSeqno uint32, seed []byte) *ValidatorSetPRNG {
	if len(seed) != 0 && len(seed) != 32 {
		panic("invalid seed len")
	}

	desc := validatorSetDescription{
		shard:     uint64(shard),
		workchain: workchain,
		ccSeqno:   catchainSeqno,
	}

	if len(seed) != 0 {
		copy(desc.seed[:], seed)
	}

	return &ValidatorSetPRNG{desc: desc}
}

func (r *ValidatorSetPRNG) NextUint64() uint64 {
	if r.pos < r.limit {
		pos := r.pos
		r.pos += 1
		return binary.BigEndian.Uint64(r.desc.hash[pos*8:])
	}
	r.desc.calcHash()
	r.desc.incSeed()
	r.pos = 1
	r.limit = 8
	return binary.BigEndian.Uint64(r.desc.hash[0:])
}

func (r *ValidatorSetPRNG) NextRanged(rg uint64) uint64 {
	ff := r.NextUint64()
	y := new(big.Int).SetUint64(ff)
	x := new(big.Int).SetUint64(rg)
	z := x.Mul(x, y)
	return z.Rsh(z, 64).Uint64()
}

func (v *validatorSetDescription) calcHash() {
	h := sha512.New()
	h.Write(v.seed[:])

	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf, v.shard)
	binary.BigEndian.PutUint32(buf[8:], uint32(v.workchain))
	binary.BigEndian.PutUint32(buf[12:], v.ccSeqno)

	h.Write(buf)
	v.hash = h.Sum(nil)
}

func (v *validatorSetDescription) incSeed() {
	for i := 31; i >= 0; i-- {
		v.seed[i]++
		if v.seed[i] != 0 {
			break
		}
	}
}
