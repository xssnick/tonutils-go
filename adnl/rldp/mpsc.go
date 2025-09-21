package rldp

import (
	"runtime"
	"sync/atomic"
)

type mpscSlot struct {
	seq atomic.Uint64
	ptr atomic.Pointer[MessagePart]
}

type MPSCRing struct {
	mask uint64
	head atomic.Uint64
	tail atomic.Uint64
	buf  []mpscSlot
}

func NewMPSCRing(pow2 int) *MPSCRing {
	if pow2 <= 0 || pow2&(pow2-1) != 0 {
		panic("size must be power-of-two")
	}

	q := &MPSCRing{mask: uint64(pow2 - 1), buf: make([]mpscSlot, pow2)}
	for i := range q.buf {
		q.buf[i].seq.Store(uint64(i))
	}
	return q
}

func (q *MPSCRing) Enqueue(m *MessagePart) {
	capu := q.mask + 1
	for {
		pos := q.head.Load()
		slot := &q.buf[pos&q.mask]
		seq := slot.seq.Load()
		diff := int64(seq) - int64(pos)

		if diff == 0 {
			if q.head.CompareAndSwap(pos, pos+1) {
				slot.ptr.Store(m)       // payload
				slot.seq.Store(pos + 1) // publish
				return
			}
			runtime.Gosched()
			continue
		}

		if diff < 0 {
			needTail := (pos + 1) - capu
			for {
				t := q.tail.Load()
				if t >= needTail {
					break
				}
				if q.tail.CompareAndSwap(t, t+1) {
					old := &q.buf[t&q.mask]
					// ВАЖНО: только seq, ptr НЕ трогаем
					old.seq.Store(t + capu)
					continue
				}
			}
		}

		runtime.Gosched()
	}
}

func (q *MPSCRing) Dequeue() (*MessagePart, bool) {
	pos := q.tail.Load()
	slot := &q.buf[pos&q.mask]

	if slot.seq.Load() == pos+1 {
		p := slot.ptr.Load()
		if p == nil {
			return nil, false
		}

		m := slot.ptr.Swap(nil)
		if m == nil {
			return nil, false
		}

		slot.seq.Store(pos + (q.mask + 1))
		_ = q.tail.CompareAndSwap(pos, pos+1)
		return m, true
	}
	return nil, false
}
