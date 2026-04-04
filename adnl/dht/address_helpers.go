package dht

import (
	"fmt"

	"github.com/xssnick/tonutils-go/adnl/address"
)

func firstDialAddress(addrs []address.Address) (address.Address, string, error) {
	for _, addr := range addrs {
		if addr == nil {
			continue
		}

		s, err := address.DialString(addr)
		if err == nil {
			return addr, s, nil
		}
	}

	return nil, "", fmt.Errorf("no dialable addresses")
}

func firstDialAddressFromList(list *address.List) (address.Address, string, error) {
	if list == nil {
		return nil, "", fmt.Errorf("address list is nil")
	}
	return firstDialAddress(list.Addresses)
}

func describeNodeAddress(list *address.List) string {
	_, addr, err := firstDialAddressFromList(list)
	if err != nil {
		return "<unknown>"
	}
	return addr
}
