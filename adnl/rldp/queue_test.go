package rldp

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mp(i uint32) *MessagePart { return &MessagePart{Seqno: i} }

func TestQueue_Basic(t *testing.T) {
	q := NewQueue(4)

	if _, ok := q.Dequeue(); ok {
		t.Fatalf("queue must be empty initially")
	}

	// put 3
	for i := uint32(0); i < 3; i++ {
		q.Enqueue(mp(i))
	}

	// get 3 in order
	for i := uint32(0); i < 3; i++ {
		m, ok := q.Dequeue()
		if !ok {
			t.Fatalf("expected ok for i=%d", i)
		}
		
		if m.Seqno != i {
			t.Fatalf("want=%d got=%d", i, m.Seqno)
		}
	}

	// empty again
	if _, ok := q.Dequeue(); ok {
		t.Fatalf("queue must be empty")
	}
}

func TestQueue_OverwriteOldest(t *testing.T) {
	q := NewQueue(4)

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
		if m == nil || m.Seqno != w {
			t.Fatalf("want=%d got=%v at i=%d", w, m, i)
		}
	}
	// пусто
	if _, ok := q.Dequeue(); ok {
		t.Fatalf("queue must be empty")
	}
}

func TestQueue_OverwriteInterleaved(t *testing.T) {
	q := NewQueue(2)

	// вместимость 2: заполним и начнем выталкивать
	q.Enqueue(mp(0))
	q.Enqueue(mp(1))
	q.Enqueue(mp(2)) // вытолкнет 0
	q.Enqueue(mp(3)) // вытолкнет 1

	// ожидаем 2,3
	m, ok := q.Dequeue()
	if !ok || m.Seqno != 2 {
		t.Fatalf("want=2 got=%v", m)
	}
	m, ok = q.Dequeue()
	if !ok || m.Seqno != 3 {
		t.Fatalf("want=3 got=%v", m)
	}
}

func TestQueue_DequeueEmpty(t *testing.T) {
	q := NewQueue(1)
	if m, ok := q.Dequeue(); ok || m != nil {
		t.Fatalf("should be empty")
	}
}

func TestQueue_Concurrent_Stress_NoNilOnOk(t *testing.T) {
	const (
		capacity    = 256
		producers   = 8
		perProducer = 50_000
	)

	q := NewQueue(capacity)

	var wg sync.WaitGroup
	wg.Add(producers)

	// consumer
	var okCnt, nilOnOk int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		stopAt := time.Now().Add(3 * time.Second)
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

	// producers
	for p := 0; p < producers; p++ {
		pid := uint32(p)
		go func(pid uint32) {
			defer wg.Done()
			for i := uint32(0); i < perProducer; i++ {
				q.Enqueue(&MessagePart{Part: pid, Seqno: i})
				if (i & 1023) == 0 {
					runtime.Gosched()
				}
			}
		}(pid)
	}

	wg.Wait()
	<-done

	if n := atomic.LoadInt64(&nilOnOk); n != 0 {
		t.Fatalf("got %d cases of (nil, ok=true) — must never happen", n)
	}
	if atomic.LoadInt64(&okCnt) == 0 {
		t.Fatalf("consumer should have received some items")
	}
}

func TestQueue_NoDeadlockOnFull(t *testing.T) {
	q := NewQueue(4)

	for i := uint32(0); i < 4; i++ {
		q.Enqueue(mp(i))
	}

	stop := make(chan struct{})
	go func() {
		defer close(stop)
		for i := uint32(4); i < 20_000; i++ {
			q.Enqueue(mp(i))
			if (i & 4095) == 0 {
				runtime.Gosched()
			}
		}
	}()

	timeout := time.After(500 * time.Millisecond)
	read := 0
loop:
	for {
		select {
		case <-timeout:
			break loop
		default:
			if _, ok := q.Dequeue(); ok {
				read++
			} else {
				runtime.Gosched()
			}
		}
	}

	<-stop
	if read == 0 {
		t.Fatalf("expected to read something without deadlock")
	}
}

func TestQueue_OrderAfterOverwriteWindow(t *testing.T) {
	q := NewQueue(8)

	for i := uint32(0); i < 20; i++ {
		q.Enqueue(mp(i))
	}

	for want := uint32(12); want < 20; want++ {
		m, ok := q.Dequeue()
		if !ok || m == nil || m.Seqno != want {
			t.Fatalf("want=%d got=%v (ok=%v)", want, m, ok)
		}
	}
	if _, ok := q.Dequeue(); ok {
		t.Fatalf("queue must be empty")
	}
}
