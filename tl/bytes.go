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

func appendUint32(dst []byte, val uint32) []byte {
	dst = append(dst, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(dst[len(dst)-4:], val)
	return dst
}

func appendUint64(dst []byte, val uint64) []byte {
	dst = append(dst, 0, 0, 0, 0, 0, 0, 0, 0)
	binary.LittleEndian.PutUint64(dst[len(dst)-8:], val)
	return dst
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

func growAppend(dst []byte, n int) []byte {
	if n <= cap(dst)-len(dst) {
		return dst
	}

	next := make([]byte, len(dst), len(dst)+n)
	copy(next, dst)
	return next
}

func appendZeros(dst []byte, n int) []byte {
	if n <= 0 {
		return dst
	}

	oldLen := len(dst)
	newLen := oldLen + n
	if cap(dst) < newLen {
		next := make([]byte, newLen)
		copy(next, dst)
		return next
	}

	dst = dst[:newLen]
	clear(dst[oldLen:])
	return dst
}

func tlBytesEncodedSize(dataLen int) (int, error) {
	if dataLen < 0 {
		return 0, errors.New("negative TL bytes length")
	}

	offset := 1
	switch {
	case dataLen < 0xFE:
	case dataLen < 1<<24:
		offset = 4
	case uint64(dataLen) < uint64(1)<<32:
		offset = 8
	default:
		return 0, fmt.Errorf("too big bytes len %d, TL bytes array limited by 1<<32", dataLen)
	}

	maxInt := int(^uint(0) >> 1)
	if dataLen > maxInt-offset-3 {
		return 0, fmt.Errorf("TL bytes encoded size overflows int for length %d", dataLen)
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

// AppendBytes appends data as a TL bytes field.
func AppendBytes(dst []byte, data []byte) ([]byte, error) {
	return appendTLBytes(dst, data)
}

func appendTLBytes(dst []byte, data []byte) ([]byte, error) {
	pad, dst, err := appendBytesHeader(dst, len(data))
	if err != nil {
		return nil, err
	}

	dst = append(dst, data...)
	dst = appendZeros(dst, pad)
	return dst, nil
}

func appendTLString(dst []byte, data string) ([]byte, error) {
	pad, dst, err := appendBytesHeader(dst, len(data))
	if err != nil {
		return nil, err
	}

	dst = append(dst, data...)
	dst = appendZeros(dst, pad)
	return dst, nil
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

	maxInt := int(^uint(0) >> 1)
	if buf.Len() > maxInt-sz {
		return 0, fmt.Errorf("TL bytes encoded size overflows buffer length for payload %d", dataLen)
	}

	buf.Grow(sz)

	headerLen := 1
	switch {
	case dataLen < 0xFE:
		buf.WriteByte(byte(dataLen))
	case dataLen < 1<<24:
		headerLen = 4
		writeUint32(buf, uint32(dataLen)<<8|0xFE)
	default:
		headerLen = 8

		var header [8]byte
		header[0] = 0xFF
		binary.LittleEndian.PutUint32(header[1:5], uint32(dataLen))
		buf.Write(header[:])
	}

	return sz - dataLen - headerLen, nil
}

func appendBytesHeader(dst []byte, dataLen int) (int, []byte, error) {
	sz, err := tlBytesEncodedSize(dataLen)
	if err != nil {
		return 0, nil, err
	}

	maxInt := int(^uint(0) >> 1)
	if len(dst) > maxInt-sz {
		return 0, nil, fmt.Errorf("TL bytes encoded size overflows destination length for payload %d", dataLen)
	}

	dst = growAppend(dst, sz)

	headerLen := 1
	switch {
	case dataLen < 0xFE:
		dst = append(dst, byte(dataLen))
	case dataLen < 1<<24:
		headerLen = 4
		dst = appendUint32(dst, uint32(dataLen)<<8|0xFE)
	default:
		headerLen = 8

		var header [8]byte
		header[0] = 0xFF
		binary.LittleEndian.PutUint32(header[1:5], uint32(dataLen))
		dst = append(dst, header[:]...)
	}

	return sz - dataLen - headerLen, dst, nil
}

func RemapBufferAsSlice(buf *bytes.Buffer, from int) {
	serializedLen := buf.Len() - (from + 4)
	if serializedLen < 0 || uint64(serializedLen) >= uint64(1)<<32 {
		panic(fmt.Sprintf("TL bytes length %d is out of range", serializedLen))
	}

	bufPtr := buf.Bytes()
	switch {
	case serializedLen < 0xFE:
		bufPtr[from] = byte(serializedLen)
		copy(bufPtr[from+1:], bufPtr[from+4:])
		buf.Truncate(buf.Len() - 3)
	case serializedLen < 1<<24:
		binary.LittleEndian.PutUint32(bufPtr[from:], uint32(serializedLen)<<8|0xFE)
	default:
		oldLen := buf.Len()
		buf.Grow(4)
		writeZeros(buf, 4)

		bufPtr = buf.Bytes()
		copy(bufPtr[from+8:], bufPtr[from+4:oldLen])
		clear(bufPtr[from : from+8])
		bufPtr[from] = 0xFF
		binary.LittleEndian.PutUint32(bufPtr[from+1:from+5], uint32(serializedLen))
	}

	// bytes array padding
	if pad := (buf.Len() - from) % 4; pad > 0 {
		writeZeros(buf, 4-pad)
	}
}

func RemapSliceAsTLBytes(dst []byte, from int) []byte {
	serializedLen := len(dst) - (from + 4)
	if serializedLen < 0 || uint64(serializedLen) >= uint64(1)<<32 {
		panic(fmt.Sprintf("TL bytes length %d is out of range", serializedLen))
	}

	switch {
	case serializedLen < 0xFE:
		dst[from] = byte(serializedLen)
		copy(dst[from+1:], dst[from+4:])
		dst = dst[:len(dst)-3]
	case serializedLen < 1<<24:
		binary.LittleEndian.PutUint32(dst[from:], uint32(serializedLen)<<8|0xFE)
	default:
		oldLen := len(dst)
		dst = appendZeros(dst, 4)
		copy(dst[from+8:], dst[from+4:oldLen])
		clear(dst[from : from+8])
		dst[from] = 0xFF
		binary.LittleEndian.PutUint32(dst[from+1:from+5], uint32(serializedLen))
	}

	if pad := (len(dst) - from) % 4; pad > 0 {
		dst = appendZeros(dst, 4-pad)
	}
	return dst
}

func FromBytes(data []byte) (loaded []byte, buffer []byte, err error) {
	return fromBytes(data, true)
}

// FromBytesNoCopy reads TL bytes and returns a slice pointing into data.
// The caller must keep data immutable and alive while loaded is used.
func FromBytesNoCopy(data []byte) (loaded []byte, buffer []byte, err error) {
	return fromBytes(data, false)
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
	payloadLen := uint64(data[0])
	switch data[0] {
	case 0xFE:
		if len(data) < 4 {
			return nil, nil, errors.New("failed to load long bytes length, too short data")
		}
		payloadLen = uint64(data[1]) | uint64(data[2])<<8 | uint64(data[3])<<16
		offset = 4
	case 0xFF:
		if len(data) < 8 {
			return nil, nil, errors.New("failed to load extended bytes length, too short data")
		}
		if data[5] != 0 || data[6] != 0 || data[7] != 0 {
			return nil, nil, errors.New("failed to load extended bytes length, exceeds uint32")
		}
		payloadLen = uint64(binary.LittleEndian.Uint32(data[1:5]))
		offset = 8
	}

	maxInt := int(^uint(0) >> 1)
	if payloadLen > uint64(maxInt-offset-3) {
		return nil, nil, fmt.Errorf("failed to load bytes length %d, encoded size overflows int", payloadLen)
	}
	ln := int(payloadLen)

	// bytes length should be dividable by 4, add additional offset to buffer if it is not
	bufSz := ln + offset
	if add := bufSz % 4; add != 0 {
		bufSz += 4 - add
	}

	// The padding must be present -- td::TlParser::fetch_string consumes
	// sizeof(int32)+result_aligned_len unconditionally, with no "it is the end
	// of the buffer" escape hatch (tdutils/td/utils/tl_parsers.h:148-182), and
	// the vector preflight relies on a bytes field never costing less than 4.
	// Its content is not checked: the reference skips those bytes without
	// looking at them, so rejecting non-zero padding would drop frames every
	// other implementation accepts.
	if len(data) < bufSz {
		return nil, nil, fmt.Errorf("failed to get payload with len %d and alignment padding, too short data", ln)
	}

	loaded = copyBytesResult(data[offset:offset+ln], copyPayload)
	if len(data) == bufSz {
		return loaded, nil, nil
	}
	return loaded, data[bufSz:], nil
}

func copyBytesResult(data []byte, copyPayload bool) []byte {
	if !copyPayload {
		return data[:len(data):len(data)]
	}

	res := make([]byte, len(data))
	copy(res, data)
	return res
}
