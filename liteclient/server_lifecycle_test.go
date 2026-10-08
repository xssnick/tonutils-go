package liteclient

import (
	"net"
	"sync"
	"testing"
	"testing/synctest"
)

type serverLifecycleListener struct {
	accepted  chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

func newServerLifecycleListener() *serverLifecycleListener {
	return &serverLifecycleListener{
		accepted: make(chan struct{}),
		closed:   make(chan struct{}),
	}
}

func (l *serverLifecycleListener) Accept() (net.Conn, error) {
	close(l.accepted)
	<-l.closed
	return nil, net.ErrClosed
}

func (l *serverLifecycleListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *serverLifecycleListener) Addr() net.Addr {
	return &net.TCPAddr{}
}

func TestServerRejectsDuplicateListener(t *testing.T) {
	tests := []struct {
		name  string
		serve func(*Server, net.Listener) error
	}{
		{name: "listen", serve: (*Server).listen},
		{name: "Serve", serve: (*Server).Serve},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := NewServer(nil)
				first := newServerLifecycleListener()
				second := newServerLifecycleListener()
				defer func() {
					_ = s.Close()
					_ = first.Close()
					_ = second.Close()
				}()

				firstDone := make(chan error, 1)
				go func() { firstDone <- s.Serve(first) }()
				synctest.Wait()

				select {
				case <-first.accepted:
				default:
					t.Fatal("first listener did not start accepting")
				}

				secondDone := make(chan error, 1)
				go func() { secondDone <- test.serve(s, second) }()
				synctest.Wait()

				select {
				case err := <-secondDone:
					if err == nil || err.Error() != "already started" {
						t.Fatalf("duplicate listener returned %v, want already started", err)
					}
				default:
					t.Fatal("duplicate listener was not rejected")
				}

				select {
				case <-second.closed:
					t.Fatal("rejected listener was closed")
				default:
				}

				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()

				select {
				case err := <-firstDone:
					if err != nil {
						t.Fatalf("Serve returned %v after Close", err)
					}
				default:
					t.Fatal("Serve did not stop after Close")
				}

				select {
				case <-second.closed:
					t.Fatal("Close closed the rejected listener")
				default:
				}
			})
		})
	}
}

func TestServerConcurrentServe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const callers = 8
		s := NewServer(nil)
		listeners := make([]*serverLifecycleListener, callers)
		defer func() {
			_ = s.Close()
			for _, listener := range listeners {
				_ = listener.Close()
			}
		}()

		start := make(chan struct{})
		done := make(chan error, callers)
		for i := range listeners {
			listener := newServerLifecycleListener()
			listeners[i] = listener
			go func() {
				<-start
				done <- s.Serve(listener)
			}()
		}
		close(start)
		synctest.Wait()

		var accepted int
		for _, listener := range listeners {
			select {
			case <-listener.accepted:
				accepted++
			default:
			}
		}
		if accepted != 1 {
			t.Fatalf("%d listeners started accepting, want 1", accepted)
		}

		for range callers - 1 {
			select {
			case err := <-done:
				if err == nil || err.Error() != "already started" {
					t.Fatalf("concurrent Serve returned %v, want already started", err)
				}
			default:
				t.Fatal("concurrent Serve did not reject a duplicate listener")
			}
		}

		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Serve returned %v after Close", err)
			}
		default:
			t.Fatal("Serve did not stop after Close")
		}
	})
}
