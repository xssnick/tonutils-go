//go:build !linux

package adnl

import "net"

func newPacketBatchConn(net.PacketConn) packetBatchConn {
	return nil
}
