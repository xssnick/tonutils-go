package tl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

func ToBytes(buf []byte) []byte {
	var data = make([]byte, 0, ((len(buf)+4)/4+1)*4)

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

func ToBytesToBuffer(buf *bytes.Buffer, data []byte) {
	if len(data) == 0 {
		// fast path for empty slice
		buf.Write(make([]byte, 4))
		return
	}

	prevLen := buf.Len()

	// store buf length
	if len(data) >= 0xFE {
		ln := make([]byte, 4)
		binary.LittleEndian.PutUint32(ln, uint32(len(data)<<8)|0xFE)
		buf.Write(ln)
	} else {
		buf.WriteByte(byte(len(data)))
	}

	buf.Write(data)

	// adjust actual length to fit % 4 = 0
	if round := (buf.Len() - prevLen) % 4; round != 0 {
		for i := 0; i < 4-round; i++ {
			buf.WriteByte(0)
		}
	}
}

func RemapBufferAsSlice(buf *bytes.Buffer, from int) {
	serializedLen := buf.Len() - (from + 4)

	bufPtr := buf.Bytes()
	if serializedLen >= 0xFE {
		binary.LittleEndian.PutUint32(bufPtr[from:], uint32(serializedLen<<8)|0xFE)
	} else {
		bufPtr[from] = byte(serializedLen)
		copy(bufPtr[from+1:], bufPtr[from+4:])
		buf.Truncate(buf.Len() - 3)
	}

	// bytes array padding
	if pad := (buf.Len() - from) % 4; pad > 0 {
		buf.Write(make([]byte, 4-pad))
	}
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
		res := make([]byte, ln)
		copy(res, data[offset:])
		return res, nil, nil
	}

	if len(data) < bufSz {
		return nil, nil, errors.New("failed to get payload, too short data")
	}
	res := make([]byte, ln)
	copy(res, data[offset:])
	return res, data[bufSz:], nil
}
