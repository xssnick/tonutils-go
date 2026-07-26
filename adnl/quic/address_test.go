package quic

import (
	"errors"
	"net"
	"testing"

	adnladdr "github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/tl"
)

func TestPeerEndpointMatchesCppnodeSelection(t *testing.T) {
	tests := []struct {
		name      string
		addresses []adnladdr.Address
		want      string
		wantErr   error
	}{
		{
			name: "ADNL IPv4 plus offset",
			addresses: []adnladdr.Address{
				adnladdr.UDP{IP: net.IPv4(127, 0, 0, 1), Port: 30303},
			},
			want: "127.0.0.1:31303",
		},
		{
			name: "ADNL IPv6 plus offset",
			addresses: []adnladdr.Address{
				adnladdr.UDP6{IP: net.ParseIP("2001:db8::1"), Port: 30303},
			},
			want: "[2001:db8::1]:31303",
		},
		{
			name: "UDP6 keeps its wire address family",
			addresses: []adnladdr.Address{
				adnladdr.UDP6{IP: net.ParseIP("::ffff:192.0.2.1"), Port: 30303},
			},
			want: "[::ffff:192.0.2.1]:31303",
		},
		{
			name: "explicit QUIC wins over earlier ADNL",
			addresses: []adnladdr.Address{
				adnladdr.UDP{IP: net.IPv4(127, 0, 0, 1), Port: 30303},
				adnladdr.QUIC{IP: net.IPv4(192, 0, 2, 1), Port: 443},
			},
			want: "192.0.2.1:443",
		},
		{
			name: "ports use cppnode uint16 arithmetic",
			addresses: []adnladdr.Address{
				adnladdr.UDP{IP: net.IPv4(192, 0, 2, 1), Port: 65000},
			},
			want: "192.0.2.1:464",
		},
		{
			name: "derived port may wrap to zero",
			addresses: []adnladdr.Address{
				adnladdr.UDP{IP: net.IPv4(192, 0, 2, 1), Port: 64536},
			},
			want: "192.0.2.1:0",
		},
		{
			name: "explicit QUIC port zero is preserved",
			addresses: []adnladdr.Address{
				adnladdr.QUIC{IP: net.IPv4(192, 0, 2, 1), Port: 0},
			},
			want: "192.0.2.1:0",
		},
		{
			name: "zero ADNL port is skipped",
			addresses: []adnladdr.Address{
				adnladdr.UDP{IP: net.IPv4(192, 0, 2, 1), Port: 0},
			},
			wantErr: ErrNoPeerEndpoint,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := PeerEndpoint(test.addresses)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("PeerEndpoint error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != test.want {
				t.Fatalf("PeerEndpoint = %s, want %s", got, test.want)
			}
		})
	}
}

func TestPeerEndpointUsesDecodedAddressValueTypes(t *testing.T) {
	wire, err := tl.Serialize(adnladdr.List{
		Addresses: []adnladdr.Address{
			adnladdr.UDP{IP: net.IPv4(127, 0, 0, 1), Port: 30303},
			adnladdr.UDP6{IP: net.ParseIP("2001:db8::1"), Port: 30303},
			adnladdr.QUIC{IP: net.IPv4(192, 0, 2, 1), Port: 443},
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}

	var decoded adnladdr.List
	if rest, err := tl.ParseNoCopy(&decoded, wire, true); err != nil || len(rest) != 0 {
		t.Fatalf("decode address list: rest=%d err=%v", len(rest), err)
	}
	if _, ok := decoded.Addresses[0].(adnladdr.UDP); !ok {
		t.Fatalf("decoded UDP type = %T, want value", decoded.Addresses[0])
	}
	if _, ok := decoded.Addresses[1].(adnladdr.UDP6); !ok {
		t.Fatalf("decoded UDP6 type = %T, want value", decoded.Addresses[1])
	}
	if _, ok := decoded.Addresses[2].(adnladdr.QUIC); !ok {
		t.Fatalf("decoded QUIC type = %T, want value", decoded.Addresses[2])
	}

	endpoint, err := PeerEndpoint(decoded.Addresses)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.String() != "192.0.2.1:443" {
		t.Fatalf("PeerEndpoint = %s", endpoint)
	}
}

func TestParseDialEndpointAcceptsOnlyNumericAddresses(t *testing.T) {
	for _, endpoint := range []string{
		"192.0.2.1:30303",
		"[2001:db8::1]:30303",
	} {
		addr, err := parseDialEndpoint(endpoint)
		if err != nil {
			t.Fatalf("parse %q: %v", endpoint, err)
		}
		if addr.String() != endpoint {
			t.Fatalf("parse %q = %q", endpoint, addr.String())
		}
	}

	if _, err := parseDialEndpoint("example.com:30303"); err == nil {
		t.Fatal("hostname endpoint was accepted")
	}
}
