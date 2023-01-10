package http

import (
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/sigurn/crc16"
	"strings"
)

var crc16table = crc16.MakeTable(crc16.CRC16_XMODEM)

func ParseADNLAddress(addr string) ([]byte, error) {
	if len(addr) != 55 {
		return nil, errors.New("wrong id length")
	}

	buf, err := base32.StdEncoding.DecodeString("F" + strings.ToUpper(addr))
	if err != nil {
		return nil, fmt.Errorf("failed to decode address: %w", err)
	}

	if buf[0] != 0x2d {
		return nil, errors.New("invalid first byte")
	}

	hash := binary.BigEndian.Uint16(buf[33:])
	calc := crc16.Checksum(buf[:33], crc16table)
	if hash != calc {
		return nil, errors.New("invalid address")
	}

	return buf[1:33], nil
}

func SerializeADNLAddress(addr []byte) (string, error) {
	a := append([]byte{0x2d}, addr...)

	crcBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(crcBytes, crc16.Checksum(a, crc16table))

	return strings.ToLower(base32.StdEncoding.EncodeToString(append(a, crcBytes...))[1:]), nil
}
