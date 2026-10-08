//go:build linux

package adnl

import (
	"net"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
)

type udpBatchSocket interface {
	ReadBatch([]ipv4.Message, int) (int, error)
	WriteBatch([]ipv4.Message, int) (int, error)
}

type udpPacketBatchConn struct {
	conn udpBatchSocket

	rx        [udpBatchSize]ipv4.Message
	rxBuffers [udpBatchSize][1][]byte
	tx        [udpBatchSize]ipv4.Message
	txBuffers [udpBatchSize][1][]byte
}

func newPacketBatchConn(conn net.PacketConn) packetBatchConn {
	if conn, ok := conn.(*SyncConn); ok {
		return conn.batch
	}

	udp, ok := conn.(*net.UDPConn)
	if !ok {
		// Custom PacketConns may wrap or transform datagrams, so their ReadFrom
		// and WriteTo methods must stay on the ordinary PacketConn path.
		return nil
	}

	b := &udpPacketBatchConn{}
	if udp.LocalAddr().(*net.UDPAddr).IP.To4() != nil {
		b.conn = ipv4.NewPacketConn(udp)
	} else {
		b.conn = ipv6.NewPacketConn(udp)
	}
	for i := range b.rx {
		b.rx[i].Buffers = b.rxBuffers[i][:]
		b.tx[i].Buffers = b.txBuffers[i][:]
	}
	return b
}

func (b *udpPacketBatchConn) readBatch(packets []*UDPPacket) (int, error) {
	for i, p := range packets {
		b.rxBuffers[i][0] = p.data
		b.rx[i].N = 0
		b.rx[i].Flags = 0
	}

	// Wait for the first datagram only, then receive the packets already in
	// the socket queue. A sparse flow never waits for a full batch.
	n, err := b.conn.ReadBatch(b.rx[:len(packets)], unix.MSG_WAITFORONE)
	// x/net may return the Linux syscall's -1 count on an error.
	if n < 0 {
		n = 0
	}
	for i := range n {
		p := packets[i]
		p.from = b.rx[i].Addr
		p.n = b.rx[i].N
		if b.rx[i].Flags&unix.MSG_TRUNC != 0 {
			p.n = 0
		}
	}
	for i := range packets {
		b.rxBuffers[i][0] = nil
		b.rx[i].Addr = nil
	}
	return n, err
}

func (b *udpPacketBatchConn) writeBatch(packets []syncPacket) (int, error) {
	for i, p := range packets {
		b.txBuffers[i][0] = p.buf
		b.tx[i].Addr = p.addr
	}

	n, err := b.conn.WriteBatch(b.tx[:len(packets)], 0)
	if n < 0 {
		n = 0
	}
	for i := range packets {
		b.txBuffers[i][0] = nil
		b.tx[i].Addr = nil
	}
	return n, err
}
