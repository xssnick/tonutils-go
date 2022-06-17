package tl

import (
	"encoding/binary"
	"fmt"
	"reflect"
)

type MarshalerTL interface {
	MarshalTL() ([]byte, error)
}

func Marshal(o any) ([]byte, error) {
	if m, ok := o.(MarshalerTL); ok {
		return m.MarshalTL()
	}

	val := reflect.ValueOf(o)

	switch val.Kind() {
	case reflect.Uint32:
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(val.Uint()))
		return b, nil

	case reflect.Int32:
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(int32(val.Int())))
		return b, nil

	case reflect.Int64:
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, uint64(val.Int()))
		return b, nil

	case reflect.Uint64:
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, val.Uint())
		return b, nil

	case reflect.Slice:
		if val.Type().Elem().Kind() != reflect.Uint8 {
			return nil, fmt.Errorf("encoding slice of %v not supported", val.Type().Elem().Kind())
		}

		data := val.Bytes()
		b := append(EncodeLength(len(data)), data...)
		return zeroPadding(b), nil

	case reflect.Array:
		if val.Type().Elem().Kind() != reflect.Uint8 {
			return nil, fmt.Errorf("encoding array of %v not supported", val.Type().Elem().Kind())
		}

		b := make([]byte, 0, val.Len()+8)
		b = append(b, EncodeLength(val.Len())...)

		for i := 0; i < val.Len(); i++ {
			b = append(b, uint8(val.Index(i).Uint()))
		}
		return zeroPadding(b), nil

	case reflect.Struct:
		var buf []byte

		for i := 0; i < val.NumField(); i++ {
			b, err := Marshal(val.Field(i).Interface())
			if err != nil {
				return nil, err
			}

			buf = append(buf, b...)
		}

		return buf, nil

	default:
		return nil, fmt.Errorf("type %v not emplemented", val.Kind())
	}
}

func zeroPadding(b []byte) []byte {
	tail := len(b) % 4
	if tail != 0 {
		return append(b, make([]byte, 4-tail)...)
	}
	return b
}

func EncodeLength(i int) []byte {
	if i >= 0xFE {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(i<<8))
		b[0] = 0xFE
		return b
	} else {
		return []byte{byte(i)}
	}
}
