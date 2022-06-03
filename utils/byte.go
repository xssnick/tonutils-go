package utils

import (
	"encoding/binary"
)

func TLBytes(buf []byte) []byte {
	var data []byte

	// store buf length
	if len(buf) >= 0xFE {
		ln := make([]byte, 4)
		binary.LittleEndian.PutUint32(ln, uint32(len(buf)<<8)|0xFE)
		data = append(data, ln...)
	} else {
		data = append(data, byte(len(buf)))
	}

	data = append(data, buf...)

	// adjust actual length to fit % 4 = 0
	if round := len(data) % 4; round != 0 {
		data = append(data, make([]byte, 4-round)...)
	}

	return data
}
