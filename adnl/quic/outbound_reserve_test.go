package quic

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

// Inbound and outbound share one peer-path table. Without a reserve a remote
// party can fill it with connections it opens and leave the node unable to dial
// anyone — and since plumtree is QUIC-only, that silently costs the node its
// place in every broadcast tree it would otherwise rejoin.

func testGatewayWithPaths(t *testing.T, maxPaths, reserve int) *Gateway {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	limits := DefaultLimits()
	limits.MaxPeerPaths = maxPaths
	limits.OutboundPathReserve = reserve

	gw, err := NewGatewayWithLimits(limits, priv)
	if err != nil {
		t.Fatalf("new gateway: %v", err)
	}
	t.Cleanup(func() { _ = gw.Close() })
	return gw
}

func testPeerPath(t *testing.T, gw *Gateway, seed byte) (pathKey, ed25519.PublicKey) {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate peer key: %v", err)
	}
	var peer adnlID
	peer[0] = seed
	peer[1] = seed >> 1
	return pathKey{local: gw.identities.defaultIdentity().id, peer: peer}, pub
}

func TestInboundCannotExhaustOutboundPathReserve(t *testing.T) {
	const maxPaths, reserve = 10, 4
	gw := testGatewayWithPaths(t, maxPaths, reserve)

	// Flood with inbound-initiated paths.
	inbound := 0
	for i := 0; i < maxPaths*2; i++ {
		key, pub := testPeerPath(t, gw, byte(i))
		if _, _, err := gw.getOrCreatePeer(key, pub, true); err != nil {
			break
		}
		inbound++
	}

	if want := maxPaths - reserve; inbound != want {
		t.Fatalf("inbound occupied %d paths, want it bounded at %d", inbound, want)
	}

	// The reserve must still admit our own dials.
	outbound := 0
	for i := 0; i < maxPaths; i++ {
		key, pub := testPeerPath(t, gw, byte(200+i))
		if _, _, err := gw.getOrCreatePeer(key, pub, false); err != nil {
			break
		}
		outbound++
	}
	if outbound != reserve {
		t.Fatalf("outbound got %d paths out of a %d reserve", outbound, reserve)
	}
}

// An existing path must stay reusable in both directions no matter how full the
// table is: the limit applies to creation, never to traffic on a known peer.
func TestExistingPathIsReusableWhenTableIsFull(t *testing.T) {
	const maxPaths, reserve = 4, 2
	gw := testGatewayWithPaths(t, maxPaths, reserve)

	key, pub := testPeerPath(t, gw, 1)
	if _, created, err := gw.getOrCreatePeer(key, pub, true); err != nil || !created {
		t.Fatalf("first inbound path: created=%v err=%v", created, err)
	}
	for i := 0; i < maxPaths; i++ {
		other, otherPub := testPeerPath(t, gw, byte(50+i))
		_, _, _ = gw.getOrCreatePeer(other, otherPub, false)
	}

	if _, created, err := gw.getOrCreatePeer(key, pub, true); err != nil || created {
		t.Fatalf("known path must be reused even when full: created=%v err=%v", created, err)
	}
}

// Shrinking MaxPeerPaths alone must stay valid: the reserve clamps itself
// instead of turning the gateway into one that refuses all inbound.
func TestOutboundReserveClampsToHalfTheTable(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxPeerPaths = 8
	if got, want := limits.inboundPeerPathBudget(), 4; got != want {
		t.Fatalf("inbound budget = %d, want %d with the default reserve clamped", got, want)
	}

	limits.MaxPeerPaths = 1
	if got := limits.inboundPeerPathBudget(); got != 1 {
		t.Fatalf("a single-path table must still admit inbound, got %d", got)
	}

	limits.MaxPeerPaths = 1000
	limits.OutboundPathReserve = 100
	if got, want := limits.inboundPeerPathBudget(), 900; got != want {
		t.Fatalf("inbound budget = %d, want %d when the reserve fits", got, want)
	}
}
