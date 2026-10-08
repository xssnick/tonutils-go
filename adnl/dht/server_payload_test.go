package dht

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/overlay"
	"github.com/xssnick/tonutils-go/tl"
)

func TestServerHandleQueryWirePrefix(t *testing.T) {
	sender, err := newCorrectNode(5, 6, 7, 8, 17004)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []tl.Serializable{
		Ping{ID: 123},
		FindNode{Key: bytes.Repeat([]byte{1}, 32), K: 3},
		FindValue{Key: bytes.Repeat([]byte{2}, 32), K: 3},
		SignedAddressListQuery{},
	} {
		t.Run(queryMethod(request), func(t *testing.T) {
			for _, prefixed := range []bool{false, true} {
				for _, noCopy := range []bool{false, true} {
					server := newTestServer(t)
					defer server.Close()
					peer := newMockPeerFromNode(t, sender, "5.6.7.8:17004")
					payload := request
					if prefixed {
						payload = []tl.Serializable{Query{Node: sender}, request}
					}
					wire, err := tl.Serialize(adnl.MessageQuery{ID: bytes.Repeat([]byte{3}, 32), Data: payload}, true)
					if err != nil {
						t.Fatal(err)
					}
					var decoded adnl.MessageQuery
					parse := tl.Parse
					if noCopy {
						parse = tl.ParseNoCopy
					}
					rest, err := parse(&decoded, wire, true)
					if err != nil || len(rest) != 0 {
						t.Fatalf("parse: rest=%x, err=%v", rest, err)
					}
					if err = server.handleQuery(peer, &decoded); err != nil {
						t.Fatalf("prefixed=%v noCopy=%v payload=%T: %v", prefixed, noCopy, decoded.Data, err)
					}
					if peer.answered == nil || !bytes.Equal(peer.answeredQueryID, decoded.ID) {
						t.Fatal("missing response to parsed query")
					}
					if prefixed && server.RoutingTableStats().ActiveNodes != 1 {
						t.Fatal("prefixed sender was not added to routing table")
					}
				}
			}
		})
	}
}

func TestServerUnsupportedQueryDescribesWireObjects(t *testing.T) {
	sender, err := newCorrectNode(5, 6, 7, 8, 17004)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		payload []tl.Serializable
		want    string
	}{
		{
			name:    "overlay query sent to DHT",
			payload: []tl.Serializable{overlay.Query{Overlay: bytes.Repeat([]byte{1}, 32)}, overlay.GetRandomPeers{}},
			want:    "len=2, types=[overlay.Query, overlay.GetRandomPeers]",
		},
		{
			name:    "duplicate prefix",
			payload: []tl.Serializable{Query{Node: sender}, Query{Node: sender}, Ping{ID: 123}},
			want:    "len=3, types=[dht.Query, dht.Query, dht.Ping]",
		},
		{
			name:    "extra function",
			payload: []tl.Serializable{Query{Node: sender}, Ping{ID: 123}, Ping{ID: 456}},
			want:    "len=3, types=[dht.Query, dht.Ping, dht.Ping]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newTestServer(t)
			defer server.Close()
			peer := newMockPeerFromNode(t, sender, "5.6.7.8:17004")
			wire, err := tl.Serialize(adnl.MessageQuery{Data: tc.payload}, true)
			if err != nil {
				t.Fatal(err)
			}
			var decoded adnl.MessageQuery
			if _, err = tl.Parse(&decoded, wire, true); err != nil {
				t.Fatal(err)
			}
			err = server.handleQuery(peer, &decoded)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if peer.answered != nil {
				t.Fatal("answered unsupported query")
			}
		})
	}
}

func TestDescribePayloadTypeBoundsSequence(t *testing.T) {
	want := "[]interface {} (len=1000, types=[dht.Ping, dht.Ping, dht.Ping, dht.Ping, dht.Ping, dht.Ping, dht.Ping, dht.Ping, ...])"
	items := make([]any, 1000)
	for i := range items {
		items[i] = Ping{ID: 123}
	}
	if got := describePayloadType(items); got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}
	if got := describePayloadType([]tl.Serializable{}); got != "[]tl.Serializable (len=0, types=[])" {
		t.Fatalf("empty sequence: %q", got)
	}
	if got := describePayloadType(Ping{ID: 123}); got != "dht.Ping" {
		t.Fatalf("single object: %q", got)
	}
}
