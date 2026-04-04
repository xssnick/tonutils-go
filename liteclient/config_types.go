package liteclient

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"

	"github.com/xssnick/tonutils-go/adnl/address"
)

type GlobalConfig struct {
	Type        string             `json:"@type"`
	DHT         DHTConfig          `json:"dht"`
	Liteservers []LiteserverConfig `json:"liteservers"`
	Validator   ValidatorConfig    `json:"validator"`
}

type DHTConfig struct {
	Type        string   `json:"@type"`
	K           int      `json:"k"`
	A           int      `json:"a"`
	NetworkID   *int32   `json:"network_id,omitempty"`
	StaticNodes DHTNodes `json:"static_nodes"`
}

type DHTNodes struct {
	Type  string    `json:"@type"`
	Nodes []DHTNode `json:"nodes"`
}

type DHTNode struct {
	Type      string         `json:"@type"`
	ID        ServerID       `json:"id"`
	AddrList  DHTAddressList `json:"addr_list"`
	Version   int            `json:"version"`
	Signature string         `json:"signature"`
}

type LiteserverConfig struct {
	IP   int64    `json:"ip"`
	Port int      `json:"port"`
	ID   ServerID `json:"id"`
}

type DHTAddressList struct {
	Type       string       `json:"@type"`
	Addrs      []DHTAddress `json:"addrs"`
	Version    int          `json:"version"`
	ReinitDate int          `json:"reinit_date"`
	Priority   int          `json:"priority"`
	ExpireAt   int          `json:"expire_at"`
}

type DHTAddress struct {
	Type string `json:"@type"`
	IP   int    `json:"ip"`
	Port int    `json:"port"`

	IPv6 net.IP `json:"-"`
}

func (a *DHTAddress) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type string          `json:"@type"`
		IP   json.RawMessage `json:"ip"`
		Port int             `json:"port"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	a.Type = raw.Type
	a.Port = raw.Port
	a.IP = 0
	a.IPv6 = nil

	switch raw.Type {
	case "adnl.address.udp", "":
		if len(raw.IP) == 0 {
			return nil
		}
		var ip int
		if err := json.Unmarshal(raw.IP, &ip); err != nil {
			return err
		}
		a.IP = ip
		return nil
	case "adnl.address.udp6":
		ip, err := parseDHTIPv6(raw.IP)
		if err != nil {
			return err
		}
		a.IPv6 = ip
		return nil
	default:
		// keep unknown address types parseable for forward-compat
		return nil
	}
}

func parseDHTIPv6(data json.RawMessage) (net.IP, error) {
	if len(data) == 0 {
		return nil, nil
	}

	var ipStr string
	if err := json.Unmarshal(data, &ipStr); err == nil {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("invalid ipv6 address %q", ipStr)
		}
		ip = ip.To16()
		if ip == nil {
			return nil, fmt.Errorf("invalid ipv6 address %q", ipStr)
		}
		return ip, nil
	}

	var words []uint32
	if err := json.Unmarshal(data, &words); err == nil {
		switch len(words) {
		case 4:
			ip := make(net.IP, net.IPv6len)
			for i, word := range words {
				binary.BigEndian.PutUint32(ip[i*4:], word)
			}
			return ip, nil
		case 16:
			ip := make(net.IP, net.IPv6len)
			for i, word := range words {
				if word > 0xff {
					return nil, fmt.Errorf("invalid ipv6 byte %d", word)
				}
				ip[i] = byte(word)
			}
			return ip, nil
		}
	}

	return nil, fmt.Errorf("unsupported ipv6 encoding")
}

func (a DHTAddress) ToADNLAddress() (address.Address, error) {
	switch a.Type {
	case "adnl.address.udp", "":
		b := make(net.IP, net.IPv4len)
		binary.BigEndian.PutUint32(b, uint32(int32(a.IP)))
		return address.NewAddress(b, int32(a.Port))
	case "adnl.address.udp6":
		if len(a.IPv6) == 0 {
			return nil, fmt.Errorf("missing ipv6 address")
		}
		return address.NewAddress(a.IPv6, int32(a.Port))
	default:
		return nil, fmt.Errorf("unsupported address type %q", a.Type)
	}
}

type ServerID struct {
	Type string `json:"@type"`
	Key  string `json:"key"`
}

type ValidatorConfig struct {
	Type      string        `json:"@type"`
	ZeroState ConfigBlock   `json:"zero_state"`
	InitBlock ConfigBlock   `json:"init_block"`
	Hardforks []ConfigBlock `json:"hardforks"`
}

type ConfigBlock struct {
	Workchain int32  `json:"workchain"`
	Shard     int64  `json:"shard"`
	SeqNo     uint32 `json:"seqno"`
	RootHash  []byte `json:"root_hash"`
	FileHash  []byte `json:"file_hash"`
}
