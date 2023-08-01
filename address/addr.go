package address

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/sigurn/crc16"
)

type AddrType int

const (
	NoneAddress AddrType = 0
	ExtAddress  AddrType = 1
	StdAddress  AddrType = 2
	VarAddress  AddrType = 3
)

const MasterchainID int32 = -1

type Address struct {
	flags     flags
	addrType  AddrType
	workchain int32
	bitsLen   uint
	data      []byte
}

type flags struct {
	bounceable bool
	testnet    bool
}

func NewAddress(flags byte, workchain byte, data []byte) *Address {
	return &Address{
		flags:     parseFlags(flags),
		addrType:  StdAddress,
		workchain: int32(int8(workchain)),
		bitsLen:   256,
		data:      data,
	}
}

func NewAddressVar(flags byte, workchain int32, bitsLen uint, data []byte) *Address {
	return &Address{
		flags:     parseFlags(flags),
		addrType:  VarAddress,
		workchain: workchain,
		bitsLen:   bitsLen,
		data:      data,
	}
}

func NewAddressExt(flags byte, bitsLen uint, data []byte) *Address {
	return &Address{
		flags:     parseFlags(flags),
		addrType:  ExtAddress,
		workchain: 0,
		bitsLen:   bitsLen,
		data:      data,
	}
}

func NewAddressNone() *Address {
	return &Address{
		addrType: NoneAddress,
	}
}

func (a *Address) IsAddrNone() bool {
	return a.addrType == NoneAddress
}

func (a *Address) Type() AddrType {
	return a.addrType
}

func (a *Address) BitsLen() uint {
	return a.bitsLen
}

var crcTable = crc16.MakeTable(crc16.CRC16_XMODEM)

func (a *Address) String() string {
	switch a.addrType {
	case NoneAddress:
		return "NONE"
	case StdAddress:
		var address [36]byte
		copy(address[0:34], a.prepareChecksumData())
		binary.BigEndian.PutUint16(address[34:], crc16.Checksum(address[:34], crcTable))
		return base64.RawURLEncoding.EncodeToString(address[:])
	case ExtAddress:
		// TODO support readable serialization
		return "EXT_ADDRESS"
	case VarAddress:
		return "VAR_ADDRESS"
	default:
		return "NOT_SUPPORTED"
	}
}

func (a *Address) StringToBytes(dst []byte, addr []byte) {
	switch a.addrType {
	case NoneAddress:
		copy(dst, []byte("NONE"))
		return
	case StdAddress:
		copy(addr[0:34], a.prepareChecksumData())
		binary.BigEndian.PutUint16(addr[34:], crc16.Checksum(addr[:34], crcTable))
		base64.RawURLEncoding.Encode(dst, addr[:])
		return
	case ExtAddress:
		copy(dst, []byte("EXT_ADDRESS"))
		return
	case VarAddress:
		copy(dst, []byte("VAR_ADDRESS"))
		return
	default:
		copy(dst, []byte("NOT_SUPPORTED"))
		return
	}
}

func (a *Address) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", a.String())), nil
}

func MustParseAddr(addr string) *Address {
	a, err := ParseAddr(addr)
	if err != nil {
		panic(err)
	}
	return a
}

func (a *Address) FlagsToByte() (flags byte) {
	// TODO check this magic...
	flags = 0b00010001
	if !a.flags.bounceable {
		setBit(&flags, 6)
	}
	if a.flags.testnet {
		setBit(&flags, 7)
	}
	return flags
}

func parseFlags(data byte) flags {
	return flags{
		bounceable: !hasBit(data, 6),
		testnet:    hasBit(data, 7),
	}
}

func ParseAddr(addr string) (*Address, error) {
	data, err := base64.RawURLEncoding.DecodeString(addr)
	if err != nil {
		return nil, err
	}

	if len(data) != 36 {
		return nil, errors.New("incorrect address data")
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
	data[1] = byte(a.workchain)
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

func (a *Address) Workchain() int32 {
	return a.workchain
}

func (a *Address) Data() []byte {
	return a.data
}
