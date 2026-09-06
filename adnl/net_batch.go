package adnl

import (
	"context"
	"errors"
	"net"
	"sync"
)

const udpBatchSize = 32

// One reader and one writer own the batch descriptors. Receive buffers are
// separate pooled packets: publishing one transfers it to the gateway until
// NetManager.Free, while the descriptor slot is refilled with another buffer.
type packetBatchConn interface {
	readBatch(packets []*UDPPacket) (int, error)
	writeBatch(packets []syncPacket) (int, error)
}

func readUDPPackets(ctx context.Context, conn net.PacketConn, batch packetBatchConn, pool *sync.Pool, deliver func(*UDPPacket) bool) {
	var slots [udpBatchSize]*UDPPacket
	count := 1
	if batch != nil {
		count = len(slots)
	}
	packets := slots[:count]

	defer func() {
		for _, p := range packets {
			if p != nil {
				p.from = nil
				p.n = 0
				pool.Put(p)
			}
		}
	}()

	for {
		for i, p := range packets {
			if p == nil {
				packets[i] = pool.Get().(*UDPPacket)
			}
		}

		var n int
		var err error
		if batch != nil {
			n, err = batch.readBatch(packets)
		} else {
			p := packets[0]
			p.n, p.from, err = conn.ReadFrom(p.data)
			if err == nil {
				n = 1
			}
		}

		for i, p := range packets[:n] {
			if p.n >= 64 && deliver(p) {
				packets[i] = nil
			}
		}

		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			if Logger != nil {
				Logger("failed to read packet:", err)
			}
		}
		if ctx.Err() != nil {
			return
		}
	}
}
