package address

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/sigurn/crc16"
	"github.com/xssnick/tonutils-go/utils"
)

type Address struct {
	flags     AddressFlags
	workchain byte
	data      []byte
}

type AddressFlags struct {
	bounceable bool
	testnet    bool
}

func NewAddress(flags byte, workchain byte, data []byte) *Address {
	// TODO: all types of addrs
	// TODO: flags parse
	return &Address{
		flags:     ParseFlags(flags),
		workchain: workchain,
		data:      data,
	}
}

func (a *Address) String() string {
	var address [36]byte
	copy(address[0:34], a.prepareChecksumData())
	binary.BigEndian.PutUint16(address[34:], crc16.Checksum(address[:34], crc16.MakeTable(crc16.CRC16_XMODEM)))
	return base64.RawURLEncoding.EncodeToString(address[:])
}

func MustParseAddr(addr string) *Address {
	a, err := ParseAddr(addr)
	if err != nil {
		panic(err)
	}
	return a
}

func (a *Address) FlagsToByte() (flags byte) {
	//TODO check this magic...
	flags = 0b00010001
	if a.flags.bounceable {
		utils.SetBit(&flags, 6)
	}
	if a.flags.testnet {
		utils.SetBit(&flags, 7)
	}
	return flags
}

func ParseFlags(data byte) AddressFlags {
	return AddressFlags{
		bounceable: utils.HasBit(data, 6),
		testnet:    utils.HasBit(data, 7),
	}
}

func ParseAddr(addr string) (*Address, error) {
	data, err := base64.URLEncoding.DecodeString(addr)
	if err != nil {
		return nil, err
	}

	checksum := data[len(data)-2:]
	if crc16.Checksum(data[:len(data)-2], crc16.MakeTable(crc16.CRC16_XMODEM)) != binary.BigEndian.Uint16(checksum) {
		return nil, errors.New("invalid address")
	}

	a := NewAddress(data[0], data[1], data[2:len(data)-2])
	return a, nil
}

func (a *Address) Checksum() uint16 {
	return crc16.Checksum(a.prepareChecksumData(), crc16.MakeTable(crc16.CRC16_XMODEM))
}

func (a *Address) prepareChecksumData() []byte {
	var data [34]byte
	data[0] = a.FlagsToByte()
	data[1] = a.workchain
	copy(data[2:34], a.data)
	return data[:]
}

func (a *Address) Dump() string {
	return fmt.Sprintf("human-readable address: %s isBounceable: %t, isTestnetOnly: %t, data.len: %d", a, a.IsBounceable(), a.IsTestnetOnly(), len(a.data))
}

func (a *Address) SetBounce(bouncable bool) {
	a.flags.bounceable = bouncable
}

func (a *Address) IsBounceable() bool {
	return a.flags.bounceable
}

func (a *Address) SetTestnetOnly(testnetOnly bool) {
	a.flags.testnet = testnetOnly
}

func (a *Address) IsTestnetOnly() bool {
	return a.flags.testnet
}

func (a *Address) Workchain() byte {
	return a.workchain
}

func (a *Address) Data() []byte {
	return a.data
}
