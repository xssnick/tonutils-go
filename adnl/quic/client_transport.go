package quic

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"

	quicgo "github.com/xssnick/quic-go-ton"
)

// clientTransport is the single client-only UDP transport owned by a Gateway
// that dialed before entering server mode.
type clientTransport struct {
	pc        net.PacketConn
	tr        *quicgo.Transport
	quicConf  *quicgo.Config
	maxObject int64
}

func newClientTransport(limits Limits) (*clientTransport, error) {
	pc, err := net.ListenPacket("udp", ":0")
	if err != nil {
		return nil, fmt.Errorf("quic: open client UDP socket: %w", err)
	}

	return &clientTransport{
		pc:        pc,
		tr:        &quicgo.Transport{Conn: pc},
		quicConf:  defaultQUICConfig(limits),
		maxObject: limits.MaxObjectSize,
	}, nil
}

func (t *clientTransport) dialIdentity(ctx context.Context, addr string, local Identity, expectedPeer ed25519.PublicKey) (*Client, error) {
	remoteAddr, err := parseDialEndpoint(addr)
	if err != nil {
		return nil, err
	}
	tlsConf, expectedPeerID, err := clientTLSConfig(local, expectedPeer)
	if err != nil {
		return nil, err
	}

	conn, err := t.tr.Dial(ctx, remoteAddr, tlsConf, t.quicConf)
	if err != nil {
		return nil, fmt.Errorf("quic: dial %s: %w", addr, err)
	}

	return authenticatedClient(conn, expectedPeerID, t.maxObject)
}

func (t *clientTransport) close() error {
	trErr := t.tr.Close()
	pcErr := t.pc.Close()
	if trErr != nil {
		return trErr
	}
	return pcErr
}
