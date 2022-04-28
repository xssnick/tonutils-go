package cell

import (
	"errors"
)

type cellBytesReader struct {
	data []byte
}

var ErrNotEnoughData = errors.New("not enough data in reader")

func newReader(data []byte) *cellBytesReader {
	return &cellBytesReader{
		data: data,
	}
}

func (r *cellBytesReader) ReadBytes(num int) ([]byte, error) {
	if len(r.data) < num {
		return nil, ErrNotEnoughData
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
		return 0, ErrNotEnoughData
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
