package rldp

import (
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

func inboundTransferCount(addr string) int {
	ip := netip.MustParseAddr(addr)
	inboundTransfers.Lock()
	defer inboundTransfers.Unlock()
	return inboundTransfers.byIP[ip]
}

func TestRLDPCodecFailureReleasesAdmission(t *testing.T) {
	client := newAdmissionClient(t, "192.0.2.111:1000")
	for i := uint64(0); i < 1600; i++ {
		part := admissionPart(i)
		part.TotalSize = 56404
		part.FecType = FECRaptorQ{DataSize: 56404, SymbolSize: 1, SymbolsCount: 56404}
		part.Data = []byte{1}
		if err := client.handleMessagePart(part, true); err == nil {
			t.Fatal("codec failure expected")
		}
		if n := inboundTransferCount("192.0.2.111"); n != 0 {
			t.Fatalf("codec failure retained %d slots", n)
		}
	}
	if len(client.recvStreams) != 0 {
		t.Fatal("codec failures retained streams")
	}
	if err := client.handleMessagePart(admissionPart(2000), true); err != nil {
		t.Fatal(err)
	}
}

func TestRLDPCompletedTransferReleasesAdmissionOnExpiry(t *testing.T) {
	client := newAdmissionClient(t, "192.0.2.112:1000")
	data, err := tl.Serialize(Message{ID: make([]byte, 32)}, true)
	if err != nil {
		t.Fatal(err)
	}
	part := admissionPart(1)
	part.TotalSize = uint64(len(data))
	part.FecType = FECRoundRobin{DataSize: uint32(len(data)), SymbolSize: 512, SymbolsCount: 1}
	copy(part.Data, data)
	if err := client.handleMessagePart(part, true); err != nil {
		t.Fatal(err)
	}
	stream := client.recvStreams[[32]byte(part.TransferID)]
	if stream.finishedAt == nil {
		t.Fatal("transfer did not complete")
	}
	if n := inboundTransferCount("192.0.2.112"); n != 1 {
		t.Fatalf("completed retained stream count %d", n)
	}
	client.cleanupRecvStreams(time.Now().Add(_recvStreamFinishedTimeout + time.Second))
	if n := inboundTransferCount("192.0.2.112"); n != 0 {
		t.Fatalf("expired completed stream retained %d", n)
	}
	client.retireInboundStream(stream, inboundStreamCanceled)
	client.closeState()
	client.closeState()
	if n := inboundTransferCount("192.0.2.112"); n != 0 {
		t.Fatalf("double retirement corrupted quota %d", n)
	}
}

func TestRLDPCancellationReleasesAdmission(t *testing.T) {
	client := newAdmissionClient(t, "192.0.2.113:1000")
	part := admissionPart(1)
	id := [32]byte(part.TransferID)
	request := &activeRequest{id: "request", expectedTransferID: id, maxAnswerSize: 1024, deadline: time.Now().Add(time.Minute).UnixMilli()}
	client.activeRequests[request.id] = request
	client.expectedTransfers[id] = request
	if err := client.handleMessagePart(part, true); err != nil {
		t.Fatal(err)
	}
	client.cancelActiveRequest(request)
	if n := inboundTransferCount("192.0.2.113"); n != 0 {
		t.Fatalf("cancellation retained %d", n)
	}
	if err := client.handleMessagePart(part, true); err != nil {
		t.Fatal(err)
	}
	if n := inboundTransferCount("192.0.2.113"); n != 0 {
		t.Fatalf("late canceled part reserved %d", n)
	}
}

func TestRLDPConcurrentRetirementReleasesAdmissionOnce(t *testing.T) {
	client := newAdmissionClient(t, "192.0.2.114:1000")
	part := admissionPart(1)
	if err := client.handleMessagePart(part, true); err != nil {
		t.Fatal(err)
	}
	stream := client.recvStreams[[32]byte(part.TransferID)]
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() { client.retireInboundStream(stream, inboundStreamCanceled) })
	}
	wg.Go(client.closeState)
	wg.Wait()
	if n := inboundTransferCount("192.0.2.114"); n != 0 {
		t.Fatalf("concurrent retirement corrupted quota %d", n)
	}
	if err := client.handleMessagePart(admissionPart(2), true); err != nil {
		t.Fatal(err)
	}
	if n := inboundTransferCount("192.0.2.114"); n != 0 {
		t.Fatalf("closed client reserved %d", n)
	}
}
