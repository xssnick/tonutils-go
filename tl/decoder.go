package tl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"reflect"
)

func Unmarshal(b []byte, o any) error {
	buf := bytes.NewReader(b)
	return decode(buf, reflect.ValueOf(o))
}

func decode(buf *bytes.Reader, val reflect.Value) error {
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}
	if !val.CanSet() {
		return fmt.Errorf("value can't be changed")
	}

	switch val.Kind() {
	case reflect.Uint32, reflect.Int32:
		b := make([]byte, 4)

		_, err := io.ReadFull(buf, b)
		if err != nil {
			return err
		}

		if val.Kind() == reflect.Uint32 {
			val.SetUint(uint64(binary.LittleEndian.Uint32(b)))
		} else {
			val.SetInt(int64(binary.LittleEndian.Uint32(b)))
		}
		return nil

	case reflect.Uint64, reflect.Int64:
		b := make([]byte, 8)

		_, err := io.ReadFull(buf, b)
		if err != nil {
			return err
		}

		if val.Kind() == reflect.Uint64 {
			val.SetUint(binary.LittleEndian.Uint64(b))
		} else {
			val.SetInt(int64(binary.LittleEndian.Uint64(b)))
		}
		return nil

	case reflect.Slice:
		if val.Type().Elem().Kind() != reflect.Uint8 {
			return fmt.Errorf("decoding slice of %v not supported", val.Type().Elem().Kind())
		}

		data, err := readByteSlice(buf)
		if err != nil {
			return err
		}

		val.SetBytes(data)
		return nil

	case reflect.Array:
		if val.Type().Elem().Kind() != reflect.Uint8 {
			return fmt.Errorf("decoding array of %v not supported", val.Type().Elem().Kind())
		}

		data, err := readByteSlice(buf)
		if err != nil {
			return err
		}

		if val.Type().Len() != len(data) {
			return fmt.Errorf("mismatched lenghth of decoded byte slice (%v) and array (%v)", len(data), val.Type().Len())
		}

		reflect.Copy(val, reflect.ValueOf(data))
		return nil

	case reflect.Struct:
		for i := 0; i < val.NumField(); i++ {
			if !val.Field(i).CanSet() {
				return fmt.Errorf("can't set field %v", i)
			}
			err := decode(buf, val.Field(i))
			if err != nil {
				return err
			}
		}
		return nil

	default:
		return fmt.Errorf("type %v not emplemented", val.Kind())
	}
}
func readByteSlice(buf *bytes.Reader) ([]byte, error) {
	firstByte, err := buf.ReadByte()
	if err != nil {
		return nil, err
	}

	var data []byte
	var full int

	if firstByte < 0xFE {
		data = make([]byte, int(firstByte))

		_, err := io.ReadFull(buf, data)
		if err != nil {
			return nil, err
		}

		full = 1 + len(data)

	} else if firstByte == 0xFE {
		sizeBuf := make([]byte, 4)

		_, err := io.ReadFull(buf, sizeBuf[:3])
		if err != nil {
			return nil, err
		}

		data = make([]byte, binary.LittleEndian.Uint32(sizeBuf))

		_, err = io.ReadFull(buf, data)
		if err != nil {
			return nil, err
		}

		full = 4 + len(data)

	} else {
		return nil, fmt.Errorf("invalid bytes prefix")
	}

	for ; full%4 != 0; full++ {
		_, err = buf.ReadByte()
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}
