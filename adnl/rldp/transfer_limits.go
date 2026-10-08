package rldp

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"
)

const maxInboundTransfersPerIP = 1500

// ErrTooManyInboundTransfers is returned when the remote IP already has 1500
// retained inbound transfers across all RLDP clients in this process.
var ErrTooManyInboundTransfers = errors.New("too many inbound RLDP transfers from remote IP")

// A peer can open several ADNL connections using different keys and ports.
// Share admission by IP so those connections cannot multiply the limit.
var inboundTransfers = struct {
	sync.Mutex
	byIP map[netip.Addr]int
}{byIP: make(map[netip.Addr]int)}

func acquireInboundTransfer(ip netip.Addr) error {
	inboundTransfers.Lock()
	defer inboundTransfers.Unlock()

	if inboundTransfers.byIP[ip] >= maxInboundTransfersPerIP {
		return fmt.Errorf("%w: %s", ErrTooManyInboundTransfers, ip)
	}

	inboundTransfers.byIP[ip]++
	return nil
}

func releaseInboundTransfer(ip netip.Addr) {
	inboundTransfers.Lock()
	defer inboundTransfers.Unlock()

	if inboundTransfers.byIP[ip] == 1 {
		delete(inboundTransfers.byIP, ip)
	} else {
		inboundTransfers.byIP[ip]--
	}
}
