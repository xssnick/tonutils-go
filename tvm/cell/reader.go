package cell

import (
	"fmt"
)

type cellBytesReader struct {
	data []byte
}

// NotEnoughDataError is returned when a read needs more bits/bytes than left.
// The message is formatted lazily: cell underflow is a regular control-flow
// path for TVM programs, so constructing it must stay cheap.
type NotEnoughDataError struct {
	Has  int
	Need int
}

func (e NotEnoughDataError) Error() string {
	return fmt.Sprintf("not enough data in reader, need %d, has %d", e.Need, e.Has)
}

// IsNotEnoughDataError reports whether err wraps a NotEnoughDataError.
// It walks the unwrap chain manually to avoid errors.As target boxing.
func IsNotEnoughDataError(err error) bool {
	for err != nil {
		if _, ok := err.(NotEnoughDataError); ok {
			return true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, sub := range x.Unwrap() {
				if IsNotEnoughDataError(sub) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
	return false
}

var ErrNotEnoughData = func(has, need int) error {
	return NotEnoughDataError{Has: has, Need: need}
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
