package liteclient

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

type testNoopStream struct{}

func (testNoopStream) XORKeyStream(dst, src []byte) {
	copy(dst, src)
}

func TestServerQueryQueueWaitsForSlot(t *testing.T) {
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

	third := serverQueueTestQuery(client)
	select {
	case err = <-third:
		t.Fatalf("third query completed before queue slot was released: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err = <-first; err != nil {
		t.Fatalf("first query err: %v", err)
	}
	if err = <-second; err != nil {
		t.Fatalf("second query err: %v", err)
	}
	if err = <-third; err != nil {
		t.Fatalf("third query err: %v", err)
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
		sendQueue: make(chan packetBuffer, 1),
	}
	defer sc.releaseQueuedPackets()

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

func TestServerClientSendQueueSize(t *testing.T) {
	if ServerClientSendQueueSize != 100 {
		t.Fatalf("send queue size is %d, want 100", ServerClientSendQueueSize)
	}
}

func TestServerClientSendQueueSizeOverride(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	prev := ServerClientSendQueueSize
	ServerClientSendQueueSize = 7
	defer func() {
		ServerClientSendQueueSize = prev
	}()

	s := NewServer([]ed25519.PrivateKey{key})
	gotQueueSize := make(chan int, 1)
	s.SetConnectionHook(func(client *ServerClient) error {
		gotQueueSize <- cap(client.sendQueue)
		return nil
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

	select {
	case got := <-gotQueueSize:
		if got != 7 {
			t.Fatalf("send queue size is %d, want 7", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connection hook was not called")
	}
}

func TestServerQueryWorkersDefault(t *testing.T) {
	want := runtime.GOMAXPROCS(0) * 4
	if ServerQueryWorkers != want {
		t.Fatalf("query workers is %d, want %d", ServerQueryWorkers, want)
	}
}

func TestServerQueryWorkersOverride(t *testing.T) {
	prev := ServerQueryWorkers
	ServerQueryWorkers = 3
	defer func() {
		ServerQueryWorkers = prev
	}()

	s := NewServer(nil)
	s.startQueryWorkers()
	defer close(s.queryQueue)

	if cap(s.queryQueue) != 3*serverQueryQueuePerWorker {
		t.Fatalf("query queue cap is %d, want %d", cap(s.queryQueue), 3*serverQueryQueuePerWorker)
	}
}

func TestServerClientWriteBatchWritesPacketsInOrder(t *testing.T) {
	reader, writer := net.Pipe()
	defer reader.Close()
	defer writer.Close()

	packet1, err := buildPacketSerialized(TCPPong{RandomID: 1})
	if err != nil {
		t.Fatal(err)
	}
	packet2, err := buildPacketSerialized(TCPPong{RandomID: 2})
	if err != nil {
		packet1.release()
		t.Fatal(err)
	}

	sc := &ServerClient{
		conn:   writer,
		wCrypt: testNoopStream{},
	}

	writeErr := make(chan error, 1)
	go func() {
		writeErr <- sc.writeBatch([]packetBuffer{packet1, packet2})
	}()

	if got := readTestPong(t, reader); got != 1 {
		t.Fatalf("first pong id is %d, want 1", got)
	}
	if got := readTestPong(t, reader); got != 2 {
		t.Fatalf("second pong id is %d, want 2", got)
	}

	if err = <-writeErr; err != nil {
		t.Fatal(err)
	}
}

func readTestPong(t *testing.T, conn net.Conn) int64 {
	t.Helper()

	sz, err := readSize(conn, testNoopStream{})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := readData(conn, testNoopStream{}, sz)
	if err != nil {
		t.Fatal(err)
	}
	data := packet

	checksum := data[len(data)-32:]
	data = data[:len(data)-32]

	if err = validatePacket(data, checksum); err != nil {
		releasePacketBuffer(packet)
		t.Fatal(err)
	}

	data = data[32:]

	var msg tl.Serializable
	if _, err = tl.Parse(&msg, data, true); err != nil {
		releasePacketBuffer(packet)
		t.Fatal(err)
	}
	releasePacketBuffer(packet)

	pong, ok := msg.(TCPPong)
	if !ok {
		t.Fatalf("unexpected message type %T", msg)
	}
	return pong.RandomID
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
