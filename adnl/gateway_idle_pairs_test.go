package adnl

import (
	"testing"
	"time"
)

func TestSelectIdlePeerPairVictimsKeepsNewestUpToLimit(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	peers := make([]*peerConn, 5)
	idle := make([]idlePeerPair, 5)
	for i := range peers {
		peers[i] = &peerConn{clientId: string(rune('a' + i))}
		// Deliberately unsorted input: 2, 0, 4, 1, 3 minutes of age.
		age := []int{2, 0, 4, 1, 3}[i]
		idle[i] = idlePeerPair{peer: peers[i], lastActive: base.Add(-time.Duration(age) * time.Minute)}
	}

	victims := selectIdlePeerPairVictims(idle, 3)
	if len(victims) != 2 {
		t.Fatalf("expected 2 victims above the limit, got %d", len(victims))
	}
	// The two oldest by lastActive are ages 4 (index 2) and 3 (index 4).
	if victims[0] != peers[2] || victims[1] != peers[4] {
		t.Fatalf("victims must be the oldest idle pairs, oldest first")
	}
}

func TestSelectIdlePeerPairVictimsUnderLimit(t *testing.T) {
	idle := []idlePeerPair{
		{peer: &peerConn{}, lastActive: time.Unix(1, 0)},
		{peer: &peerConn{}, lastActive: time.Unix(2, 0)},
	}
	if victims := selectIdlePeerPairVictims(idle, 2); victims != nil {
		t.Fatalf("no victims expected at the limit, got %d", len(victims))
	}
	if victims := selectIdlePeerPairVictims(nil, 0); victims != nil {
		t.Fatalf("no victims expected for empty input")
	}
}

func TestCollectIdlePeerPairsSkipsSmallPools(t *testing.T) {
	g := &Gateway{peers: map[string]*peerConn{}}

	old := MaxIdlePeerPairs
	MaxIdlePeerPairs = 3
	defer func() { MaxIdlePeerPairs = old }()

	// Under the cap nothing is even inspected: Stats() would panic on the nil
	// client, proving the fast path returns before touching peers.
	snapshot := []*peerConn{{clientId: "a"}, {clientId: "b"}, {clientId: "c"}}
	if victims := g.collectIdlePeerPairs(snapshot, time.Now()); victims != nil {
		t.Fatalf("pool within cap must not produce victims, got %d", len(victims))
	}
}
