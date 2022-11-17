package tl

import (
	"encoding/binary"
	"errors"
	"fmt"
)

func ToBytes(buf []byte) []byte {
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

func FromBytes(data []byte) (loaded []byte, buffer []byte, err error) {
	if len(data) == 0 {
		return nil, nil, errors.New("failed to load length, too short data")
	}

	offset := 1
	ln := int(data[0])
	if ln == 0xFE {
		ln = int(binary.LittleEndian.Uint32(data)) >> 8
		offset = 4
	}

	// bytes length should be dividable by 4, add additional offset to buffer if it is not
	bufSz := ln + offset
	if add := bufSz % 4; add != 0 {
		bufSz += 4 - add
	}

	// if its end, we don't need to align by 4
	if bufSz >= len(data) {
		if len(data) < offset+ln {
			return nil, nil, fmt.Errorf("failed to get payload with len %d, too short data", ln)
		}
		return data[offset : offset+ln], nil, nil
	}

	if len(data) < bufSz {
		return nil, nil, errors.New("failed to get payload, too short data")
	}
	return data[offset : offset+ln], data[bufSz:], nil
}
