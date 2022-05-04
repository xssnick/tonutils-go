package address

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"github.com/sigurn/crc16"
)

type Address struct {
	flags     byte
	workchain byte
	data      []byte
}

func MustParseAddr(addr string) *Address {
	a, err := ParseAddr(addr)
	if err != nil {
		panic(err)
	}
	return a
}

func ParseAddr(addr string) (*Address, error) {
	data, err := base64.URLEncoding.DecodeString(addr)
	if err != nil {
		return nil, err
	}

	// TODO: all types of addrs
	// TODO: flags parse

	a := &Address{
		flags:     data[0],
		workchain: data[1],
		data:      data[2 : len(data)-2],
	}

	checksum := data[len(data)-2:]
	if crc16.Checksum(data[:len(data)-2], crc16.MakeTable(crc16.CRC16_XMODEM)) != binary.BigEndian.Uint16(checksum) {
		return nil, errors.New("invalid address")
	}

	return a, nil
}

func (a *Address) Workchain() byte {
	return a.workchain
}

func (a *Address) Data() []byte {
	return a.data
}
