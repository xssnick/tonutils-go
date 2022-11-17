package tl

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"net"
	"reflect"
	"strings"
)

type Serializable interface{}

type TL interface {
	Parse(data []byte) ([]byte, error)
	Serialize() ([]byte, error)
}

var _SchemaIDByTypeName = map[string]uint32{}
var _SchemaIDByName = map[string]uint32{}
var _SchemaByID = map[uint32]reflect.Type{}

var _BoolTrue = tlCRC("boolTrue = Bool")
var _BoolFalse = tlCRC("boolFalse = Bool")

func Serialize(v Serializable, boxed bool) ([]byte, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, fmt.Errorf("v should not be nil")
		}
		rv = rv.Elem()
	}

	var buf []byte

	if boxed {
		id, ok := _SchemaIDByTypeName[rv.Type().String()]
		if !ok {
			panic("not registered tl type " + rv.Type().String())
		}
		buf = make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, id)
	}

	// if we have custom method, we use it
	if t, ok := v.(TL); ok {
		data, err := t.Serialize()
		if err != nil {
			return nil, fmt.Errorf("failed to serialize %s using manual method: %w", rv.Type().String(), err)
		}
		return append(buf, data...), nil
	}

	for i := 0; i < rv.NumField(); i++ {
		field := rv.Type().Field(i)
		fieldVal := rv.Field(i)
		tag := strings.TrimSpace(field.Tag.Get("tl"))
		if tag == "-" || len(tag) == 0 {
			continue
		}
		settings := strings.Split(tag, " ")

		if settings[0] == "vector" {
			settings = settings[1:]

			if field.Type.Kind() != reflect.Slice {
				panic("vector should have slice type")
			}

			tmp := make([]byte, 4)
			binary.LittleEndian.PutUint32(tmp, uint32(fieldVal.Len()))
			buf = append(buf, tmp...)

			for x := 0; x < fieldVal.Len(); x++ {
				subBuf, err := serializeField(settings, fieldVal.Index(x))
				if err != nil {
					return nil, fmt.Errorf("failed to serialize field %s: %w", field.Name, err)
				}
				buf = append(buf, subBuf...)
			}
			continue
		}

		subBuf, err := serializeField(settings, fieldVal)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize field %s: %w", field.Name, err)
		}
		buf = append(buf, subBuf...)
	}

	return buf, nil
}

func Parse(v Serializable, data []byte, boxed bool, names ...string) (_ []byte, err error) {
	src := reflect.ValueOf(v)
	if src.Kind() != reflect.Pointer || src.IsNil() {
		return nil, fmt.Errorf("v should be a pointer and not nil")
	}
	src = src.Elem()

	// if we have custom method, we use it
	if t, ok := v.(TL); ok {
		data, err = t.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s using manual method: %w", src.Type().String(), err)
		}
		return data, nil
	}

	rv := src

	if boxed {
		if len(data) < 4 {
			return nil, fmt.Errorf("failed to parse id of %s, too short data", src.Type().String())
		}
		dataID := binary.LittleEndian.Uint32(data)

		sch, ok := _SchemaByID[dataID]
		if !ok {
			return nil, fmt.Errorf("schema for id %s is not found, during parsing of: %s", hex.EncodeToString(data[:4]), rv.Type().String())
		}

		if len(names) > 0 {
			ok = false
			for _, name := range names {
				if _SchemaIDByName[name] == dataID {
					ok = true
					break
				}
			}

			if !ok {
				return nil, fmt.Errorf("schema id %s not match required list [%s]", hex.EncodeToString(data[:4]), strings.Join(names, ","))
			}
		}

		if src.Kind() != reflect.Interface && sch != src.Type() {
			return nil, fmt.Errorf("required schema %s not match actual %s", src.Type().String(), sch.String())
		} else if src.Kind() == reflect.Interface {
			rv = reflect.New(sch).Elem()
		}
		data = data[4:]
	}

	if rv.Kind() == reflect.Interface {
		panic("interfaces can be parsed only when boxing is enabled")
	}

	for i := 0; i < rv.NumField(); i++ {
		field := rv.Type().Field(i)
		value := rv.Field(i)

		tag := strings.TrimSpace(field.Tag.Get("tl"))
		if tag == "-" || len(tag) == 0 {
			continue
		}
		settings := strings.Split(tag, " ")

		if settings[0] == "vector" {
			settings = settings[1:]

			if len(data) < 4 {
				return nil, fmt.Errorf("failed to parse vector size of %s, err: too short data", src.Type().String())
			}
			sz := binary.LittleEndian.Uint32(data)
			data = data[4:]

			if field.Type.Kind() != reflect.Slice {
				panic("vector should have slice type")
			}

			// in case if we have something, to overwrite
			value.SetLen(0)

			for x := uint32(0); x < sz; x++ {
				vl := reflect.New(field.Type.Elem()).Elem()

				data, err = parseField(data, settings, &vl)
				if err != nil {
					return nil, fmt.Errorf("failed to parse vector field %s, element %d of size %d, err: %w", field.Name, x, sz, err)
				}

				value = reflect.Append(value, vl)
			}

			rv.Field(i).Set(value)
			continue
		}

		data, err = parseField(data, settings, &value)
		if err != nil {
			return nil, fmt.Errorf("failed to parse field %s, err: %w", field.Name, err)
		}
		rv.Field(i).Set(value)
	}

	// in case of interface
	if src != rv {
		src.Set(rv)
	}

	return data, nil
}

func serializeField(tags []string, value reflect.Value) (buf []byte, err error) {
	switch tags[0] {
	case "string":
		switch value.Type().Kind() {
		case reflect.String:
			return ToBytes([]byte(value.String())), nil
		}
	case "int256", "bytes":
		if tags[0] == "bytes" && len(tags) > 1 && tags[1] == "struct" {
			res, err := Serialize(value.Interface(), len(tags) > 2 && tags[2] == "boxed")
			if err != nil {
				return nil, fmt.Errorf("failed to serialize struct to bytes: %w", err)
			}
			return ToBytes(res), nil
		}

		switch value.Type().Kind() {
		case reflect.Slice:
			switch value.Type().Elem().Kind() {
			case reflect.Uint8:
				if tags[0] == "int256" {
					if len(value.Bytes()) != 32 {
						return nil, fmt.Errorf("not 32 bytes for int256 value")
					}
					buf = append(buf, value.Bytes()...)
				} else {
					buf = append(buf, ToBytes(value.Bytes())...)
				}
				return buf, nil
			}
		}
		panic("for int256 only bytes array/slice supported")
	case "bool":
		switch value.Type().Kind() {
		case reflect.Bool:
			tmp := make([]byte, 4)
			if value.Bool() {
				binary.LittleEndian.PutUint32(tmp, _BoolTrue)
			} else {
				binary.LittleEndian.PutUint32(tmp, _BoolFalse)
			}
			buf = append(buf, tmp...)
			return buf, nil
		}
	case "int", "long":
		switch value.Type().Kind() {
		case reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8, reflect.Int:
			var tmp []byte
			if tags[0] == "int" {
				tmp = make([]byte, 4)
				binary.LittleEndian.PutUint32(tmp, uint32(value.Int()))
			} else {
				tmp = make([]byte, 8)
				binary.LittleEndian.PutUint64(tmp, uint64(value.Int()))
			}
			buf = append(buf, tmp...)
			return buf, nil
		case reflect.Slice:
			switch value.Type().Elem().Kind() {
			case reflect.Uint8:
				sz := 8
				if tags[0] == "int" {
					sz = 4
				}

				if value.Len() != sz {
					return nil, fmt.Errorf("failed to serialize %s for %s, err: incorrect size of slice, shpuld be %d", tags[0], value.Type().String(), sz)
				}

				ipBytes := value.Bytes()
				data := make([]byte, len(ipBytes))
				copy(data, ipBytes)

				if value.Type() == reflect.TypeOf(net.IP{}) {
					data[0], data[1], data[2], data[3] = data[3], data[2], data[1], data[0]
				}

				buf = append(buf, data...)
				return buf, nil
			}
		}
	case "struct":
		isBoxed := len(tags) > 1 && tags[1] == "boxed"

		if value.Type().Kind() == reflect.Interface {
			if !isBoxed {
				return nil, fmt.Errorf("interface type in %s field should be boxed", value.Type().String())
			}

			list := splitAllowed(tags[2:])

			// check for allowed types
			elem := value.Elem()
			gotID, found := _SchemaIDByTypeName[elem.Type().String()]
			if found {
				found = false
				for _, s := range list {
					if gotID == _SchemaIDByName[s] {
						found = true
						break
					}
				}
			}

			if !found {
				return nil, fmt.Errorf("tl object has not allowed type, should be one of %s, got type %s", list, elem.String())
			}
		}

		subBuf, err := Serialize(value.Interface(), isBoxed)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize %s type as struct, err: %w", value.Type().String(), err)
		}
		buf = append(buf, subBuf...)
		return buf, nil
	}

	panic(fmt.Sprintf("cannot serialize type '%s' as tag '%s', use manual serialization", value.Type().String(), strings.Join(tags, " ")))
}

func splitAllowed(leftTags []string) []string {
	if len(leftTags) == 0 {
		return nil
	}

	allowed := strings.Join(leftTags, "")
	if !strings.HasPrefix(allowed, "[") || !strings.HasSuffix(allowed, "]") {
		panic("corrupted allowed list tag, should be [a,b,c], got " + allowed)
	}

	// cut brackets
	allowed = allowed[1 : len(allowed)-1]
	list := strings.Split(allowed, ",")

	return list
}

func parseField(data []byte, tags []string, value *reflect.Value) (_ []byte, err error) {
	switch tags[0] {
	case "string":
		switch value.Type().Kind() {
		case reflect.String:
			var val []byte
			val, data, err = FromBytes(data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse string for %s, err: %w", value.Type().String(), err)
			}
			value.SetString(string(val))
			return data, nil
		}
	case "int256", "bytes":
		var val []byte

		if tags[0] == "bytes" && len(tags) > 1 && tags[1] == "struct" {
			val, data, err = FromBytes(data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse bytes for %s, err: %w", value.Type().String(), err)
			}

			isBoxed := len(tags) > 2 && tags[2] == "boxed"

			var allowed []string
			if value.Type().Kind() == reflect.Interface {
				if !isBoxed {
					return nil, fmt.Errorf("interface type in %s field should be boxed", value.Type().String())
				}

				allowed = splitAllowed(tags[3:])
			}

			v := value.Interface()

			_, err = Parse(&v, val, isBoxed, allowed...)
			if err != nil {
				return nil, fmt.Errorf("failed to parse struct from bytes for %s, err: %w", value.Type().String(), err)
			}

			value.Set(reflect.ValueOf(v))
			return data, nil
		}

		switch value.Type().Kind() {
		case reflect.Slice:
			switch value.Type().Elem().Kind() {
			case reflect.Uint8:
				if tags[0] == "int256" {
					if len(data) < 32 {
						return nil, fmt.Errorf("failed to parse int256 for %s, err: too short data", value.Type().String())
					}
					val, data = data[:32], data[32:]
				} else {
					val, data, err = FromBytes(data)
					if err != nil {
						return nil, fmt.Errorf("failed to parse bytes for %s, err: %w", value.Type().String(), err)
					}
				}
				value.SetBytes(val)
				return data, nil
			}
		}
		panic("for int256 and bytes only bytes array/slice supported")
	case "bool":
		switch value.Type().Kind() {
		case reflect.Bool:
			if len(data) < 4 {
				return nil, fmt.Errorf("failed to parse int for %s, err: too short data", value.Type().String())
			}
			value.SetBool(binary.LittleEndian.Uint32(data) == _BoolTrue)
			data = data[4:]
			return data, nil
		}
	case "int", "long":
		switch value.Type().Kind() {
		case reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8, reflect.Int:
			var val int64
			if tags[0] != "long" {
				if len(data) < 4 {
					return nil, fmt.Errorf("failed to parse int for %s, err: too short data", value.Type().String())
				}
				val = int64(binary.LittleEndian.Uint32(data))
				data = data[4:]
			} else {
				if len(data) < 8 {
					return nil, fmt.Errorf("failed to parse long for %s, err: too short data", value.Type().String())
				}
				val = int64(binary.LittleEndian.Uint64(data))
				data = data[8:]
			}
			value.SetInt(val)
			return data, nil
		case reflect.Slice:
			switch value.Type().Elem().Kind() {
			case reflect.Uint8:
				sz := 8
				if tags[0] != "long" {
					sz = 4
				}

				if len(data) < sz {
					return nil, fmt.Errorf("failed to parse %s for %s, err: too short data", tags[0], value.Type().String())
				}

				var val []byte
				val, data = data[:sz], data[sz:]

				if value.Type() == reflect.TypeOf(net.IP{}) {
					val[0], val[1], val[2], val[3] = val[3], val[2], val[1], val[0]
				}

				value.SetBytes(val)

				return data, nil
			}
		}
	case "struct":
		newTyp := value.Type()
		if newTyp.Kind() == reflect.Ptr {
			newTyp = newTyp.Elem()
		}

		nVal := reflect.New(newTyp)
		inf := nVal.Interface()

		isBoxed := len(tags) > 1 && tags[1] == "boxed"

		if value.Type().Kind() == reflect.Interface && !isBoxed {
			return nil, fmt.Errorf("interface type in %s field should be boxed", value.Type().String())
		}

		data, err = Parse(inf, data, isBoxed)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s field as struct, err: %w", value.Type().String(), err)
		}

		if value.Type().Kind() == reflect.Interface {
			allowed := strings.Join(tags[2:], "")
			if !strings.HasPrefix(allowed, "[") || !strings.HasSuffix(allowed, "]") {
				panic("corrupted allowed list tag, should be [a,b,c], got " + allowed)
			}

			// cut brackets
			allowed = allowed[1 : len(allowed)-1]

			// check for allowed types
			elem := nVal.Elem().Elem()
			gotID, found := _SchemaIDByTypeName[elem.Type().String()]
			if found {
				found = false
				list := strings.Split(allowed, ",")
				for _, s := range list {
					if gotID == _SchemaIDByName[s] {
						found = true
						break
					}
				}
			}

			if !found {
				return nil, fmt.Errorf("parsed tl object has not allowed type, should be one of [%s], got type %s", allowed, elem.String())
			}
		}

		if value.Type().Kind() != reflect.Ptr {
			nVal = nVal.Elem()
		}

		value.Set(nVal)
		return data, nil
	}
	panic(fmt.Sprintf("cannot parse type '%s' as tag '%s', use manual parsing", value.Type().String(), strings.Join(tags, " ")))
}

func Register(typ any, tl string) uint32 {
	t := reflect.TypeOf(typ)

	if t.Kind() != reflect.Struct {
		panic("tl type kind should be a struct, not a pointer or something else")
	}

	name := strings.SplitN(tl, " ", 2)[0]
	id := tlCRC(tl)
	_SchemaByID[id] = t
	_SchemaIDByTypeName[t.String()] = id
	_SchemaIDByName[name] = id

	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, id)

	println("TL Registered:", hex.EncodeToString(b), tl)
	return id
}

func tlCRC(schema string) uint32 {
	schema = strings.ReplaceAll(schema, "(", "")
	schema = strings.ReplaceAll(schema, ")", "")
	data := []byte(schema)
	crc := crc32.Checksum(data, crc32.MakeTable(crc32.IEEE))

	return crc
}
