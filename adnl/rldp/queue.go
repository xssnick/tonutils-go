package rldp

type Queue struct {
	ch chan *MessagePart
}

func NewQueue(sz int) *Queue {
	return &Queue{
		ch: make(chan *MessagePart, sz),
	}
}

func (q *Queue) Enqueue(m *MessagePart) {
	for {
		select {
		case q.ch <- m:
			// written
			return
		default:
		}

		select {
		case <-q.ch:
		// not written, drop oldest
		default:
		}
	}
}

func (q *Queue) Dequeue() (*MessagePart, bool) {
	select {
	case m := <-q.ch:
		return m, true
	default:
		return nil, false
	}
}

func (q *Queue) Drain() {
	for {
		select {
		case <-q.ch:
		default:
			return
		}
	}
}
