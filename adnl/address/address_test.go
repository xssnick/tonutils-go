package address

import (
	"net"
	"testing"

	"github.com/xssnick/tonutils-go/tl"
)

func TestListRoundtripMixedUDPAndUDP6(t *testing.T) {
	src := List{
		Addresses: []Address{
			&UDP{IP: net.IPv4(127, 0, 0, 1).To4(), Port: 30303},
			&UDP6{IP: net.ParseIP("2001:db8::1").To16(), Port: 40404},
		},
		Version:    10,
		ReinitDate: 11,
		Priority:   1,
		ExpireAt:   12,
	}

	data, err := tl.Serialize(src, true)
	if err != nil {
		t.Fatal(err)
	}

	var got List
	if _, err = tl.Parse(&got, data, true); err != nil {
		t.Fatal(err)
	}

	if len(got.Addresses) != 2 {
		t.Fatalf("unexpected addresses count: %d", len(got.Addresses))
	}

	if ip := IPValue(got.Addresses[0]); !ip.Equal(net.IPv4(127, 0, 0, 1).To4()) {
		t.Fatalf("unexpected first address ip: %v", ip)
	}

	if ip := IPValue(got.Addresses[1]); !ip.Equal(net.ParseIP("2001:db8::1")) {
		t.Fatalf("unexpected second address ip: %v", ip)
	}

	if got.Version != src.Version || got.ReinitDate != src.ReinitDate || got.Priority != src.Priority || got.ExpireAt != src.ExpireAt {
		t.Fatalf("list meta mismatch: %#v vs %#v", got, src)
	}
}

func TestNewAddress(t *testing.T) {
	v4, err := NewAddress(net.IPv4(1, 2, 3, 4), 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v4.(*UDP); !ok {
		t.Fatalf("unexpected v4 type: %T", v4)
	}

	v6, err := NewAddress(net.ParseIP("2001:db8::5"), 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v6.(*UDP6); !ok {
		t.Fatalf("unexpected v6 type: %T", v6)
	}
}

func TestDialStringIPv6(t *testing.T) {
	addr := &UDP6{IP: net.ParseIP("2001:db8::7"), Port: 1000}

	got, err := DialString(addr)
	if err != nil {
		t.Fatal(err)
	}

	if got != "[2001:db8::7]:1000" {
		t.Fatalf("unexpected dial string: %s", got)
	}
}
