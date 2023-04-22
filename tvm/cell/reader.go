package cell

import (
	"fmt"
)

type cellBytesReader struct {
	data []byte
}

var ErrNotEnoughData = func(has, need int) error {
	return fmt.Errorf("not enough data in reader, need %d, has %d", need, has)
}

func newReader(data []byte) *cellBytesReader {
	return &cellBytesReader{
		data: data,
	}
}

func (r *cellBytesReader) ReadBytes(num int) ([]byte, error) {
	if len(r.data) < num {
		return nil, ErrNotEnoughData(len(r.data), num)
	}

	return r.MustReadBytes(num), nil
}

func (r *cellBytesReader) MustReadBytes(num int) []byte {
	ret := r.data[:num]
	r.data = r.data[num:]
	return ret
}

func (r *cellBytesReader) ReadByte() (byte, error) {
	if len(r.data) < 1 {
		return 0, ErrNotEnoughData(len(r.data), 1)
	}

	return r.MustReadByte(), nil
}

func (r *cellBytesReader) MustReadByte() byte {
	ret := r.data[0]
	r.data = r.data[1:]
	return ret
}

func (r *cellBytesReader) LeftLen() int {
	return len(r.data)
}
