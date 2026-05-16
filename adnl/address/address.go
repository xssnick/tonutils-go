package address

import (
	"fmt"
	"net"
	"strconv"

	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(UDP{}, "adnl.address.udp ip:int port:int = adnl.Address")
	tl.Register(UDP6{}, "adnl.address.udp6 ip:int128 port:int = adnl.Address")
	tl.Register(List{}, "adnl.addressList addrs:(vector adnl.Address) version:int reinit_date:int priority:int expire_at:int = adnl.AddressList")
}

type Address interface {
}

type UDP struct {
	IP   net.IP `tl:"int"`
	Port int32  `tl:"int"`
}

type UDP6 struct {
	IP   net.IP `tl:"int128"`
	Port int32  `tl:"int"`
}

func IPValue(addr Address) net.IP {
	switch a := addr.(type) {
	case nil:
		return nil
	case UDP:
		return net.IP(a.IP)
	case *UDP:
		if a == nil {
			return nil
		}
		return net.IP(a.IP)
	case UDP6:
		return net.IP(a.IP)
	case *UDP6:
		if a == nil {
			return nil
		}
		return net.IP(a.IP)
	default:
		return nil
	}
}

func PortValue(addr Address) int32 {
	switch a := addr.(type) {
	case nil:
		return 0
	case UDP:
		return a.Port
	case *UDP:
		if a == nil {
			return 0
		}
		return a.Port
	case UDP6:
		return a.Port
	case *UDP6:
		if a == nil {
			return 0
		}
		return a.Port
	default:
		return 0
	}
}

type List struct {
	Addresses  []Address `tl:"vector struct boxed [adnl.address.udp,adnl.address.udp6]"`
	Version    int32     `tl:"int"`
	ReinitDate int32     `tl:"int"`
	Priority   int32     `tl:"int"`
	ExpireAt   int32     `tl:"int"`
}

func NewAddress(ip net.IP, port int32) (Address, error) {
	if ip == nil {
		return nil, fmt.Errorf("ip is nil")
	}

	if v4 := ip.To4(); v4 != nil {
		return &UDP{
			IP:   append(net.IP(nil), v4...),
			Port: port,
		}, nil
	}

	if v6 := ip.To16(); v6 != nil {
		return &UDP6{
			IP:   append(net.IP(nil), v6...),
			Port: port,
		}, nil
	}

	return nil, fmt.Errorf("invalid ip")
}

func Clone(addr Address) Address {
	switch a := addr.(type) {
	case nil:
		return nil
	case UDP:
		return &UDP{
			IP:   append(net.IP(nil), a.IP...),
			Port: a.Port,
		}
	case *UDP:
		if a == nil {
			return nil
		}
		return &UDP{
			IP:   append(net.IP(nil), a.IP...),
			Port: a.Port,
		}
	case UDP6:
		return &UDP6{
			IP:   append(net.IP(nil), a.IP...),
			Port: a.Port,
		}
	case *UDP6:
		if a == nil {
			return nil
		}
		return &UDP6{
			IP:   append(net.IP(nil), a.IP...),
			Port: a.Port,
		}
	default:
		return nil
	}
}

func CloneList(list *List) *List {
	if list == nil {
		return nil
	}

	cp := &List{
		Version:    list.Version,
		ReinitDate: list.ReinitDate,
		Priority:   list.Priority,
		ExpireAt:   list.ExpireAt,
	}

	if len(list.Addresses) == 0 {
		return cp
	}

	cp.Addresses = make([]Address, 0, len(list.Addresses))
	for _, addr := range list.Addresses {
		cp.Addresses = append(cp.Addresses, Clone(addr))
	}
	return cp
}

func DialString(addr Address) (string, error) {
	if addr == nil {
		return "", fmt.Errorf("address is nil")
	}

	ip := IPValue(addr)
	if ip == nil {
		return "", fmt.Errorf("address ip is nil")
	}

	return net.JoinHostPort(ip.String(), strconv.Itoa(int(PortValue(addr)))), nil
}

func IsZero(addr Address) bool {
	if addr == nil {
		return true
	}

	ip := IPValue(addr)
	return ip == nil || ip.IsUnspecified()
}
