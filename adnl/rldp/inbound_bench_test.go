package rldp

import "testing"

func benchmarkInboundPart() *MessagePart {
	return &MessagePart{
		TransferID: make([]byte, 32),
		FecType:    FECRoundRobin{DataSize: 1024, SymbolSize: 512, SymbolsCount: 2},
		TotalSize:  1024,
		Data:       make([]byte, 512),
	}
}

func BenchmarkRLDPInboundSymbol(b *testing.B) {
	client := newInertClient()
	b.Cleanup(client.closeState)
	part := benchmarkInboundPart()
	if err := client.handleMessagePart(part, true); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := client.handleMessagePart(part, true); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRLDPInboundTransfer(b *testing.B) {
	client := newInertClient()
	b.Cleanup(client.closeState)
	part := benchmarkInboundPart()
	id := [32]byte(part.TransferID)
	b.ReportAllocs()
	for b.Loop() {
		if err := client.handleMessagePart(part, true); err != nil {
			b.Fatal(err)
		}
		client.mx.Lock()
		stream := client.recvStreams[id]
		delete(client.recvStreams, id)
		client.mx.Unlock()
		client.retireInboundStream(stream, inboundStreamExpired)
	}
}
