package tl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

var tlZeroBytes [32]byte

func writeUint32(buf *bytes.Buffer, val uint32) {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], val)
	buf.Write(tmp[:])
}

func writeUint64(buf *bytes.Buffer, val uint64) {
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], val)
	buf.Write(tmp[:])
}

func writeZeros(buf *bytes.Buffer, n int) {
	for n > len(tlZeroBytes) {
		buf.Write(tlZeroBytes[:])
		n -= len(tlZeroBytes)
	}

	if n > 0 {
		buf.Write(tlZeroBytes[:n])
	}
}

func tlBytesEncodedSize(dataLen int) (int, error) {
	if dataLen >= 1<<24 {
		return 0, fmt.Errorf("too big bytes len, TL bytes array limited by 1<<24")
	}

	offset := 1
	if dataLen >= 0xFE {
		offset = 4
	}

	sz := dataLen + offset
	if pad := sz % 4; pad != 0 {
		sz += 4 - pad
	}

	return sz, nil
}

func ToBytesToBuffer(buf *bytes.Buffer, data []byte) error {
	pad, err := writeBytesHeader(buf, len(data))
	if err != nil {
		return err
	}

	buf.Write(data)
	writeZeros(buf, pad)
	return nil
}

func toStringToBuffer(buf *bytes.Buffer, data string) error {
	pad, err := writeBytesHeader(buf, len(data))
	if err != nil {
		return err
	}

	buf.WriteString(data)
	writeZeros(buf, pad)
	return nil
}

func writeBytesHeader(buf *bytes.Buffer, dataLen int) (int, error) {
	sz, err := tlBytesEncodedSize(dataLen)
	if err != nil {
		return 0, err
	}
	buf.Grow(sz)

	headerLen := 1
	if dataLen >= 0xFE {
		headerLen = 4
		writeUint32(buf, uint32(dataLen<<8)|0xFE)
	} else {
		buf.WriteByte(byte(dataLen))
	}

	return sz - dataLen - headerLen, nil
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
		writeZeros(buf, 4-pad)
	}
}

func FromBytes(data []byte) (loaded []byte, buffer []byte, err error) {
	return fromBytes(data, true)
}

func fromBytesNoCopy(data []byte) (loaded []byte, buffer []byte, err error) {
	return fromBytes(data, false)
}

func fromBytesString(data []byte) (loaded string, buffer []byte, err error) {
	bts, buffer, err := fromBytes(data, false)
	if err != nil {
		return "", nil, err
	}

	return string(bts), buffer, nil
}

func fromBytes(data []byte, copyPayload bool) (loaded []byte, buffer []byte, err error) {
	if len(data) == 0 {
		return nil, nil, errors.New("failed to load length, too short data")
	}

	offset := 1
	ln := int(data[0])
	if ln == 0xFE {
		if len(data) < 4 {
			return nil, nil, errors.New("failed to load long bytes length, too short data")
		}
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
		return copyBytesResult(data[offset:offset+ln], copyPayload), nil, nil
	}

	if len(data) < bufSz {
		return nil, nil, errors.New("failed to get payload, too short data")
	}
	return copyBytesResult(data[offset:offset+ln], copyPayload), data[bufSz:], nil
}

func copyBytesResult(data []byte, copyPayload bool) []byte {
	if !copyPayload {
		return data
	}

	res := make([]byte, len(data))
	copy(res, data)
	return res
}
