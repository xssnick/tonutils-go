package quic

import (
	"errors"
	"fmt"
	"net"
	"net/netip"

	adnladdr "github.com/xssnick/tonutils-go/adnl/address"
)

const nodePortOffset = 1000

var ErrNoPeerEndpoint = errors.New("quic: peer has no IP endpoint")

func parseDialEndpoint(endpoint string) (*net.UDPAddr, error) {
	addr, err := netip.ParseAddrPort(endpoint)
	if err != nil {
		return nil, fmt.Errorf("quic: parse numeric endpoint %q: %w", endpoint, err)
	}
	return net.UDPAddrFromAddrPort(addr), nil
}

// PeerEndpoint selects an endpoint from a decoded adnl.addressList exactly as
// cppnode's QuicSender does: the first explicit adnl.address.quic wins;
// otherwise the first usable ADNL UDP endpoint is shifted by 1000 modulo 65536.
func PeerEndpoint(addresses []adnladdr.Address) (netip.AddrPort, error) {
	var fallback netip.AddrPort
	for _, raw := range addresses {
		var (
			ipBytes []byte
			port    uint16
			ipv6    bool
		)
		switch addr := raw.(type) {
		case adnladdr.QUIC:
			ip, ok := netip.AddrFromSlice(addr.IP)
			if !ok {
				return netip.AddrPort{}, errors.New("quic: malformed explicit IPv4 address")
			}

			ip = ip.Unmap()
			if !ip.Is4() {
				return netip.AddrPort{}, errors.New("quic: malformed explicit IPv4 address")
			}
			return netip.AddrPortFrom(ip, uint16(addr.Port)), nil
		case adnladdr.UDP:
			ipBytes, port = addr.IP, uint16(addr.Port)
		case adnladdr.UDP6:
			ipBytes, port, ipv6 = addr.IP, uint16(addr.Port), true
		default:
			continue
		}
		if fallback.IsValid() || port == 0 {
			continue
		}

		ip, ok := netip.AddrFromSlice(ipBytes)
		if !ok {
			continue
		}

		if ipv6 {
			if !ip.Is6() {
				continue
			}
		} else {
			ip = ip.Unmap()
			if !ip.Is4() {
				continue
			}
		}
		fallback = netip.AddrPortFrom(ip, port+nodePortOffset)
	}
	if fallback.IsValid() {
		return fallback, nil
	}
	return netip.AddrPort{}, ErrNoPeerEndpoint
}
