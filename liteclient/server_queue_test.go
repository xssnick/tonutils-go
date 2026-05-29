package liteclient

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

func TestServerQueryQueueReturnsBusy(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer([]ed25519.PrivateKey{key})
	s.queryWorkers = 1
	s.queryQueue = make(chan serverQueryTask, 1)

	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once

	s.SetQueryHandler(func(ctx context.Context, sc *ServerClient, query tl.Serializable) (tl.Serializable, error) {
		if _, ok := query.(GetMasterchainInf); !ok {
			return nil, fmt.Errorf("unexpected query type %T", query)
		}

		startedOnce.Do(func() {
			close(started)
		})

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
		}

		return testMasterchainInfo(), nil
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- s.listen(ln)
	}()
	defer func() {
		_ = s.Close()
		select {
		case err := <-listenErr:
			if err != nil {
				t.Errorf("listen err: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("server did not stop")
		}
	}()

	client := NewConnectionPool()
	defer client.Stop()

	connectCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = client.AddConnection(connectCtx, ln.Addr().String(), base64.StdEncoding.EncodeToString(pub))
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	first := serverQueueTestQuery(client)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first query was not started")
	}

	second := serverQueueTestQuery(client)
	waitServerQueueLen(t, s.queryQueue, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	var resp MasterchainInfo
	err = client.QueryLiteserver(ctx, GetMasterchainInf{}, &resp)
	cancel()
	busy, ok := err.(ServerBusy)
	if !ok {
		t.Fatalf("expected ServerBusy, got %T: %v", err, err)
	}
	if busy.Code != 429 || busy.Text != "server is busy" {
		t.Fatalf("unexpected busy response: %+v", busy)
	}

	close(release)
	if err = <-first; err != nil {
		t.Fatalf("first query err: %v", err)
	}
	if err = <-second; err != nil {
		t.Fatalf("second query err: %v", err)
	}
}

func serverQueueTestQuery(client *ConnectionPool) <-chan error {
	res := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var resp MasterchainInfo
		res <- client.QueryLiteserver(ctx, GetMasterchainInf{}, &resp)
	}()
	return res
}

func waitServerQueueLen(t *testing.T, queue chan serverQueryTask, want int) {
	t.Helper()

	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	for {
		if len(queue) == want {
			return
		}

		select {
		case <-deadline:
			t.Fatalf("queue len is %d, want %d", len(queue), want)
		case <-tick.C:
		}
	}
}

func TestServerClientEnqueueDropsWhenQueueFull(t *testing.T) {
	sc := &ServerClient{
		sendQueue: make(chan tl.Serializable, 1),
	}
	if !sc.enqueue(TCPPong{RandomID: 1}) {
		t.Fatal("first enqueue failed")
	}

	if sc.enqueue(TCPPong{RandomID: 2}) {
		t.Fatal("second enqueue succeeded")
	}
	if len(sc.sendQueue) != 1 {
		t.Fatalf("queue len is %d, want 1", len(sc.sendQueue))
	}
}

func TestServerClientStartQueryLimitsInflight(t *testing.T) {
	sc := &ServerClient{
		sendQueue: make(chan tl.Serializable, serverClientSendQueueSize),
	}

	for i := 0; i < serverClientSendQueueSize; i++ {
		if !sc.startQuery() {
			t.Fatalf("query %d was rejected", i)
		}
	}
	if sc.startQuery() {
		t.Fatal("query over limit was accepted")
	}

	sc.finishQuery()
	if !sc.startQuery() {
		t.Fatal("query after finish was rejected")
	}
}

func TestServerClientStartQueryRejectsFullSendQueue(t *testing.T) {
	sc := &ServerClient{
		sendQueue: make(chan tl.Serializable, 1),
	}
	sc.sendQueue <- TCPPong{RandomID: 1}

	if sc.startQuery() {
		t.Fatal("query was accepted with full send queue")
	}
}

func TestServerClientStartQueryLimitsCombinedPressure(t *testing.T) {
	sc := &ServerClient{
		sendQueue: make(chan tl.Serializable, serverClientSendQueueSize),
	}
	sc.sendQueue <- TCPPong{RandomID: 1}

	for i := 0; i < serverClientSendQueueSize-1; i++ {
		if !sc.startQuery() {
			t.Fatalf("query %d was rejected", i)
		}
	}
	if sc.startQuery() {
		t.Fatal("query over combined pressure limit was accepted")
	}
}

func testMasterchainInfo() MasterchainInfo {
	return MasterchainInfo{
		Last: &BlockIDExt{
			RootHash: make([]byte, 32),
			FileHash: make([]byte, 32),
		},
		StateRootHash: make([]byte, 32),
		Init: &ZeroStateIDExt{
			RootHash: make([]byte, 32),
			FileHash: make([]byte, 32),
		},
	}
}
