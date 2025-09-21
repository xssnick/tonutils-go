package rldp

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// helper: создаём MessagePart с меткой
func mp(id uint32) *MessagePart {
	return &MessagePart{Seqno: id}
}

func TestNewMPSCRing_PanicOnNonPowerOfTwo(t *testing.T) {
	cases := []int{-1, 0, 3, 6, 7, 10}
	for _, sz := range cases {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for size=%d", sz)
				}
			}()
			_ = NewMPSCRing(sz)
		}()
	}
}

func TestNewMPSCRing_OkOnPowerOfTwo(t *testing.T) {
	cases := []int{1, 2, 4, 8, 16, 1024}
	for _, sz := range cases {
		q := NewMPSCRing(sz)
		if q == nil || int(q.mask+1) != sz {
			t.Fatalf("bad ring for size=%d", sz)
		}
		// проверим базовую пустоту
		if _, ok := q.Dequeue(); ok {
			t.Fatalf("queue should be empty just after init size=%d", sz)
		}
	}
}

func TestMPSCRing_FIFO_NoDrop(t *testing.T) {
	q := NewMPSCRing(8)

	// положим 8, снимем 8 — порядок должен сохраниться
	for i := uint32(0); i < 8; i++ {
		q.Enqueue(mp(i))
	}
	for i := uint32(0); i < 8; i++ {
		m, ok := q.Dequeue()
		if !ok {
			t.Fatalf("expected ok=true at i=%d", i)
		}
		if m == nil {
			t.Fatalf("nil message at i=%d", i)
		}
		if m.Seqno != i {
			t.Fatalf("want seq=%d, got=%d", i, m.Seqno)
		}
	}
	// должна опустеть
	if _, ok := q.Dequeue(); ok {
		t.Fatalf("queue must be empty")
	}
}

func TestMPSCRing_WrapAround(t *testing.T) {
	q := NewMPSCRing(4)

	// заполним
	for i := uint32(0); i < 4; i++ {
		q.Enqueue(mp(i))
	}
	// снимем два
	for i := uint32(0); i < 2; i++ {
		m, ok := q.Dequeue()
		if !ok || m == nil || m.Seqno != i {
			t.Fatalf("wrap step1 want %d", i)
		}
	}
	// доложим ещё два (должно пойти в начало буфера, wrap)
	q.Enqueue(mp(4))
	q.Enqueue(mp(5))

	// теперь должны идти 2,3,4,5
	want := []uint32{2, 3, 4, 5}
	for idx, w := range want {
		m, ok := q.Dequeue()
		if !ok || m == nil || m.Seqno != w {
			t.Fatalf("wrap step2[%d] want=%d, got=%v ok=%v", idx, w, m, ok)
		}
	}
	if _, ok := q.Dequeue(); ok {
		t.Fatalf("queue must be empty after wrap test")
	}
}

func TestMPSCRing_OverwriteOldest(t *testing.T) {
	q := NewMPSCRing(4)

	// кладём 6 без чтения — должны остаться последние 4: 2,3,4,5
	for i := uint32(0); i < 6; i++ {
		q.Enqueue(mp(i))
	}

	want := []uint32{2, 3, 4, 5}
	for i, w := range want {
		m, ok := q.Dequeue()
		if !ok {
			t.Fatalf("expected ok at i=%d", i)
		}
		if m == nil {
			t.Fatalf("nil at i=%d", i)
		}
		if m.Seqno != w {
			t.Fatalf("want=%d got=%d at i=%d", w, m.Seqno, i)
		}
	}
	// пусто
	if _, ok := q.Dequeue(); ok {
		t.Fatalf("queue must be empty")
	}
}

func TestMPSCRing_MultiProducersSingleConsumer_NoDrop(t *testing.T) {
	const (
		producers   = 4
		perProducer = 10_000
	)
	// большая ёмкость, чтобы не было переполнений/overwrite
	q := NewMPSCRing(1 << 14) // 16384

	var produced int64
	var wg sync.WaitGroup
	wg.Add(producers)

	// consumer
	var consumed int64
	seen := make(map[uint64]bool, producers*perProducer)
	var seenMu sync.Mutex

	stop := make(chan struct{})
	go func() {
		defer close(stop)
		target := int64(producers * perProducer)
		for atomic.LoadInt64(&consumed) < target {
			if m, ok := q.Dequeue(); ok {
				if m == nil {
					t.Errorf("consumer got nil with ok=true")
					continue
				}
				atomic.AddInt64(&consumed, 1)
				// уникальный id из пары (p, i) зашиваем в Seqno:
				// Seqno = uint32(i), а уникальность по (p,i) проверим по ключу:
				key := uint64(m.Part)<<32 | uint64(m.Seqno) // используем Part как producer id
				seenMu.Lock()
				if seen[key] {
					t.Errorf("duplicate item key=%d", key)
				}
				seen[key] = true
				seenMu.Unlock()
			} else {
				// очередь пуста — уступим планировщику
				runtime.Gosched()
			}
		}
	}()

	// producers
	for p := 0; p < producers; p++ {
		pid := uint32(p)
		go func(pid uint32) {
			defer wg.Done()
			for i := uint32(0); i < perProducer; i++ {
				m := &MessagePart{Part: pid, Seqno: i}
				q.Enqueue(m)
				atomic.AddInt64(&produced, 1)
			}
		}(pid)
	}

	wg.Wait()

	// Ждём, пока consumer выест всё (с небольшим тайм-аутом на всякий)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&consumed) == int64(producers*perProducer) {
			break
		}
		runtime.Gosched()
	}
	if atomic.LoadInt64(&consumed) != int64(producers*perProducer) {
		t.Fatalf("consumed=%d, want=%d", consumed, producers*perProducer)
	}
	<-stop

	// финальная проверка уникальности количества
	if len(seen) != producers*perProducer {
		t.Fatalf("seen=%d, want=%d", len(seen), producers*perProducer)
	}
}

func TestMPSCRing_StressOverflow_NoNilOnOk(t *testing.T) {
	q := NewMPSCRing(256) // небольшая ёмкость, спровоцируем overwrite

	const producers = 4
	const perProducer = 20_000

	var wg sync.WaitGroup
	wg.Add(producers)

	// consumer — просто считает ok и nil-случаи
	var okCnt, nilOnOk int64
	done := make(chan struct{})

	go func() {
		defer close(done)
		stopAt := time.Now().Add(2 * time.Second) // ограничим время теста
		for time.Now().Before(stopAt) {
			if m, ok := q.Dequeue(); ok {
				atomic.AddInt64(&okCnt, 1)
				if m == nil {
					atomic.AddInt64(&nilOnOk, 1)
				}
			} else {
				runtime.Gosched()
			}
		}
	}()

	for p := 0; p < producers; p++ {
		pid := uint32(p)
		go func(pid uint32) {
			defer wg.Done()
			for i := uint32(0); i < perProducer; i++ {
				q.Enqueue(&MessagePart{Part: pid, Seqno: i})
				// дать шанс другим потокам
				if i%1024 == 0 {
					runtime.Gosched()
				}
			}
		}(pid)
	}

	wg.Wait()
	<-done

	if n := atomic.LoadInt64(&nilOnOk); n != 0 {
		t.Fatalf("got %d cases of (nil, ok=true) — violates publish order", n)
	}
	t.Logf("okDequeues=%d (overflow occurred, as expected)", okCnt)
}

func TestMPSCRing_EmptyDequeue(t *testing.T) {
	q := NewMPSCRing(8)
	if m, ok := q.Dequeue(); ok || m != nil {
		t.Fatalf("empty dequeue should return (nil,false), got (%v,%v)", m, ok)
	}
}

// Дополнительная проверка соответствия capacity и маски
func TestMPSCRing_InternalMask(t *testing.T) {
	for _, capPow2 := range []int{1, 2, 4, 8, 16, 64, 256, 1024} {
		q := NewMPSCRing(capPow2)
		gotCap := int(q.mask + 1)
		if gotCap != capPow2 {
			t.Fatalf("mask/cap mismatch: want=%d got=%d", capPow2, gotCap)
		}
	}
}
