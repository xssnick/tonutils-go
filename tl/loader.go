package tl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"hash/crc32"
	"net"
	"reflect"
	"strconv"
	"strings"
)

type Serializable interface{}
type Raw []byte

type ParseableTL interface {
	Parse(data []byte) ([]byte, error)
}

type SerializableTL interface {
	Serialize(buf *bytes.Buffer) error
}

type TL interface {
	ParseableTL
	SerializableTL
}

var _SchemaIDByTypeName = map[string]uint32{}
var _SchemaIDByName = map[string]uint32{}
var _SchemaByID = map[uint32]reflect.Type{}

var _BoolTrue = CRC("boolTrue = Bool")
var _BoolFalse = CRC("boolFalse = Bool")

var Logger = func(a ...any) {}

var DefaultSerializeBufferSize = 1024

func Serialize(v Serializable, boxed bool, bufferSize ...int) ([]byte, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, fmt.Errorf("v should not be nil")
		}
		rv = rv.Elem()
	}

	bufSz := DefaultSerializeBufferSize
	if len(bufferSize) > 0 && bufferSize[0] != 0 {
		bufSz = bufferSize[0]
	}

	if rv.Type() == reflect.TypeOf(Raw{}) {
		return rv.Bytes(), nil
	} else if rv.Type() == reflect.TypeOf([]Serializable{}) {
		items := rv.Interface().([]Serializable)
		buf := bytes.NewBuffer(nil)
		buf.Grow(bufSz * len(items))

		for i, sv := range items {
			itemData, err := Serialize(sv, boxed, bufSz)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize %d elem of slice: %w", i, err)
			}
			buf.Write(itemData)
		}

		return buf.Bytes(), nil
	}

	buf := bytes.NewBuffer(nil)
	buf.Grow(bufSz)

	if boxed {
		id, ok := _SchemaIDByTypeName[rv.Type().String()]
		if !ok {
			panic("not registered tl type " + rv.Type().String())
		}
		box := make([]byte, 4)
		binary.LittleEndian.PutUint32(box, id)
		buf.Write(box)
	}

	// if we have custom method, we use it
	if t, ok := v.(SerializableTL); ok {
		if err := t.Serialize(buf); err != nil {
			return nil, fmt.Errorf("failed to serialize %s using manual method: %w", rv.Type().String(), err)
		}
		return buf.Bytes(), nil
	}

	var flags *uint32
	for i := 0; i < rv.NumField(); i++ {
		field := rv.Type().Field(i)
		fieldVal := rv.Field(i)
		tag := strings.TrimSpace(field.Tag.Get("tl"))
		if tag == "-" || len(tag) == 0 {
			continue
		}
		settings := strings.Split(tag, " ")

		if settings[0][0] == '?' {
			if flags == nil {
				panic("flag field should be defined before usage")
			}

			bit, err := strconv.Atoi(settings[0][1:])
			if err != nil {
				panic("invalid flag bit in tag, should be number")
			}
			if bit < 0 || bit > 31 {
				panic("invalid flag bit in tag, should be > 0 && < 32")
			}

			if *flags&(1<<bit) == 0 {
				// no flags for this field set
				continue
			}
			settings = settings[1:]
		}

		if settings[0] == "flags" {
			f := uint32(fieldVal.Uint())
			flags = &f
			settings = []string{"int"}
		}

		if settings[0] == "vector" {
			settings = settings[1:]

			if field.Type.Kind() != reflect.Slice {
				panic("vector should have slice type")
			}

			ln := fieldVal.Len()
			tmp := make([]byte, 4)
			binary.LittleEndian.PutUint32(tmp, uint32(ln))
			buf.Write(tmp)

			for x := 0; x < ln; x++ {
				if err := serializeField(buf, settings, fieldVal.Index(x), bufSz); err != nil {
					return nil, fmt.Errorf("failed to serialize field %s: %w", field.Name, err)
				}
			}
			continue
		}

		if err := serializeField(buf, settings, fieldVal, bufSz); err != nil {
			return nil, fmt.Errorf("failed to serialize field %s: %w", field.Name, err)
		}
	}

	return buf.Bytes(), nil
}

func Parse(v Serializable, data []byte, boxed bool, names ...string) (_ []byte, err error) {
	src := reflect.ValueOf(v)
	// println("PR", src.Type().String())
	if src.Kind() != reflect.Pointer || src.IsNil() {
		return nil, fmt.Errorf("v should be a pointer and not nil")
	}
	srcElem := src.Elem()

	var rv reflect.Value

	if boxed {
		if len(data) < 4 {
			return nil, fmt.Errorf("failed to parse id of %s, too short data", srcElem.Type().String())
		}
		dataID := binary.LittleEndian.Uint32(data)

		sch, ok := _SchemaByID[dataID]
		if !ok {
			return nil, fmt.Errorf("schema for id %s (%d) is not found, during parsing of: %s", hex.EncodeToString(data[:4]), int32(dataID), srcElem.Type().String())
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

		if srcElem.Kind() != reflect.Interface && sch != srcElem.Type() {
			return nil, fmt.Errorf("required schema %s not match actual %s", srcElem.Type().String(), sch.String())
		} else if srcElem.Kind() == reflect.Interface {
			rv = reflect.New(sch)
		}
		data = data[4:]
	}

	isInterface := rv.IsValid()
	if !isInterface {
		rv = src
	}

	// if we have custom method, we use it
	if t, ok := rv.Interface().(ParseableTL); ok {
		data, err = t.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s using manual method: %w", rv.Type().String(), err)
		}
		rv = rv.Elem()
	} else {
		// we work with rv type as with underlying struct to process each field
		rv = rv.Elem()

		if rv.Kind() == reflect.Interface {
			panic("interfaces can be parsed only when boxing is enabled")
		}

		var flags *uint32
		for i := 0; i < rv.NumField(); i++ {
			field := rv.Type().Field(i)
			value := rv.Field(i)

			tag := strings.TrimSpace(field.Tag.Get("tl"))
			if tag == "-" || len(tag) == 0 {
				continue
			}
			settings := strings.Split(tag, " ")

			if settings[0][0] == '?' {
				if flags == nil {
					panic("flag field should be defined before usage")
				}

				bit, err := strconv.Atoi(settings[0][1:])
				if err != nil {
					panic("invalid flag bit in tag, should be number")
				}
				if bit < 0 || bit > 31 {
					panic("invalid flag bit in tag, should be > 0 && < 32")
				}

				if *flags&(1<<bit) == 0 {
					// no flags for this field set
					continue
				}
				settings = settings[1:]
			}

			var isFlags bool
			if settings[0] == "flags" {
				isFlags = true
				settings = []string{"int"}
			}

			if settings[0] == "vector" {
				settings = settings[1:]

				if len(data) < 4 {
					return nil, fmt.Errorf("failed to parse vector size of %s, err: too short data", rv.Type().String())
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
				return nil, fmt.Errorf("failed to parse field %s of %s, err: %w", field.Name, rv.Type().String(), err)
			}
			rv.Field(i).Set(value)

			if isFlags {
				f := uint32(value.Uint())
				flags = &f
			}
		}
	}

	if isInterface {
		srcElem.Set(rv)
	}

	return data, nil
}

func serializeField(buf *bytes.Buffer, tags []string, value reflect.Value, bufSz int) (err error) {
	switch tags[0] {
	case "string":
		switch value.Type().Kind() {
		case reflect.String:
			ToBytesToBuffer(buf, []byte(value.String()))
			return nil
		}
	case "cell":
		optional := len(tags) > 1 && tags[1] == "optional"
		if optional {
			tags = tags[1:]
		}

		var num int
		if len(tags) > 1 {
			num, err = strconv.Atoi(tags[1])
			if err != nil || num <= 0 {
				panic("cells num tag should be positive integer")
			}
		}

		if value.IsNil() || (value.Kind() == reflect.Slice && value.Len() == 0) {
			if optional {
				ToBytesToBuffer(buf, nil)
				return nil
			}
			return fmt.Errorf("nil cell is not allowed in field %s", value.Type().String())
		}

		if value.Type() == cellType {
			if num > 0 {
				panic("field type should be cell slice to use cells num tag")
			}
			ToBytesToBuffer(buf, value.Interface().(*cell.Cell).ToBOCWithFlags(false))
			return nil
		} else if value.Type() == cellArrType {
			cells := value.Interface().([]*cell.Cell)
			if num > 0 && num != len(cells) {
				return fmt.Errorf("incorrect cells len %d in field %s", len(cells), value.Type().String())
			}
			ToBytesToBuffer(buf, cell.ToBOCWithFlags(cells, false))
			return nil
		}
		panic("for cell tag only *cell.Cell is supported")
	case "int256", "bytes":
		if tags[0] == "bytes" && len(tags) > 1 && tags[1] == "struct" {
			res, err := Serialize(value.Interface(), len(tags) > 2 && tags[2] == "boxed", bufSz)
			if err != nil {
				return fmt.Errorf("failed to serialize struct to bytes: %w", err)
			}
			ToBytesToBuffer(buf, res)
			return nil
		}

		typ := value.Type()
		switch typ.Kind() {
		case reflect.Slice:
			switch typ.Elem().Kind() {
			case reflect.Uint8:
				if tags[0] == "int256" {
					bts := value.Bytes()
					if len(bts) == 0 {
						// consider it as 0
						bts = make([]byte, 32)
					} else if len(bts) != 32 {
						return fmt.Errorf("not 32 bytes for int256 value")
					}
					buf.Write(bts)
				} else {
					ToBytesToBuffer(buf, value.Bytes())
				}
				return nil
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
			buf.Write(tmp)
			return nil
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
			buf.Write(tmp)
			return nil
		case reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8, reflect.Uint:
			var tmp []byte
			if tags[0] == "int" {
				tmp = make([]byte, 4)
				binary.LittleEndian.PutUint32(tmp, uint32(value.Uint()))
			} else {
				tmp = make([]byte, 8)
				binary.LittleEndian.PutUint64(tmp, value.Uint())
			}
			buf.Write(tmp)
			return nil
		case reflect.Slice:
			switch value.Type().Elem().Kind() {
			case reflect.Uint8:
				sz := 8
				if tags[0] == "int" {
					sz = 4
				}

				if value.Len() != sz {
					return fmt.Errorf("failed to serialize %s for %s, err: incorrect size of slice, shpuld be %d", tags[0], value.Type().String(), sz)
				}

				ipBytes := value.Bytes()
				data := make([]byte, len(ipBytes))
				copy(data, ipBytes)

				if value.Type() == reflect.TypeOf(net.IP{}) {
					data[0], data[1], data[2], data[3] = data[3], data[2], data[1], data[0]
				}

				buf.Write(data)
				return nil
			}
		}
	case "struct":
		isBoxed := len(tags) > 1 && tags[1] == "boxed"

		if value.Type().Kind() == reflect.Interface {
			if !isBoxed {
				return fmt.Errorf("interface type in %s field should be boxed", value.Type().String())
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
				return fmt.Errorf("tl object has not allowed type, should be one of %s, got type %s", list, elem.String())
			}
		}

		subBuf, err := Serialize(value.Interface(), isBoxed, bufSz)
		if err != nil {
			return fmt.Errorf("failed to serialize %s type as struct, err: %w", value.Type().String(), err)
		}
		buf.Write(subBuf)
		return nil
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

var cellType = reflect.TypeOf(&cell.Cell{})
var cellArrType = reflect.TypeOf([]*cell.Cell{})

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
	case "cell":
		optional := len(tags) > 1 && tags[1] == "optional"
		if optional {
			tags = tags[1:]
		}

		var num int
		if len(tags) > 1 {
			num, err = strconv.Atoi(tags[1])
			if err != nil || num <= 0 {
				panic("cells num tag should be positive integer")
			}
		}

		var val []byte
		val, data, err = FromBytes(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse bytes for %s, err: %w", value.Type().String(), err)
		}

		var cells []*cell.Cell
		if len(val) > 0 {
			cells, err = cell.FromBOCMultiRoot(val)
			if err != nil {
				return nil, fmt.Errorf("failed to parse boc from bytes for %s, err: %w", value.Type().String(), err)
			}
		}

		if len(cells) == 0 {
			if optional {
				return data, nil
			}
			return nil, fmt.Errorf("nil cell is not allowed in field %s", value.Type().String())
		}

		if value.Type() == cellType {
			if num > 0 {
				panic("field type should be cell slice to use cells num tag")
			}
			value.Set(reflect.ValueOf(cells[0]))
			return data, nil
		} else if value.Type() == cellArrType {
			if num > 0 && num != len(cells) {
				return nil, fmt.Errorf("incorrect cells len %d in field %s", len(cells), value.Type().String())
			}
			value.Set(reflect.ValueOf(cells))
			return data, nil
		}
		panic("for cell tag only *cell.Cell and []*cell.Cell are supported")
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

			val, err = Parse(&v, val, isBoxed, allowed...)
			if err != nil {
				return nil, fmt.Errorf("failed to parse struct from bytes for %s, err: %w", value.Type().String(), err)
			}

			if len(val) > 0 && value.Type().Kind() == reflect.Interface {
				// it was prefix (e.g. overlay id), parse actual value
				var v2 Serializable
				val, err = Parse(&v2, val, isBoxed, allowed...)
				if err != nil {
					return nil, fmt.Errorf("failed to parse struct from bytes for %s, err: %w", value.Type().String(), err)
				}
				v = []Serializable{v, v2}
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
		case reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8, reflect.Uint:
			var val uint64
			if tags[0] != "long" {
				if len(data) < 4 {
					return nil, fmt.Errorf("failed to parse int for %s, err: too short data", value.Type().String())
				}
				val = uint64(binary.LittleEndian.Uint32(data))
				data = data[4:]
			} else {
				if len(data) < 8 {
					return nil, fmt.Errorf("failed to parse long for %s, err: too short data", value.Type().String())
				}
				val = binary.LittleEndian.Uint64(data)
				data = data[8:]
			}
			value.SetUint(val)
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

				val := data[:sz]
				data = data[sz:]

				if value.Type() == reflect.TypeOf(net.IP{}) {
					ip := make([]byte, 4)
					copy(ip, val)

					ip[0], ip[1], ip[2], ip[3] = ip[3], ip[2], ip[1], ip[0]
					value.SetBytes(ip)
					return data, nil
				}

				value.SetBytes(val)
				return data, nil
			}
		}
	case "struct":
		newTyp := value.Type()
		if newTyp.Kind() == reflect.Pointer {
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

		if value.Type().Kind() != reflect.Pointer {
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
	nameParts := strings.SplitN(name, "#", 2)

	var id uint32
	if len(nameParts) > 1 {
		b, err := hex.DecodeString(nameParts[1])
		if err != nil {
			panic("invalid predefined id for " + name + ": " + err.Error())
		}
		id = binary.BigEndian.Uint32(b)
	} else {
		id = CRC(tl)
	}
	_SchemaByID[id] = t
	_SchemaIDByTypeName[t.String()] = id
	_SchemaIDByName[nameParts[0]] = id

	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, id)

	Logger("TL Registered:", hex.EncodeToString(b), tl)
	return id
}

var ieeeTable = crc32.MakeTable(crc32.IEEE)

func CRC(schema string) uint32 {
	schema = strings.ReplaceAll(schema, "(", "")
	schema = strings.ReplaceAll(schema, ")", "")
	return crc32.Checksum([]byte(schema), ieeeTable)
}

func Hash(key any) ([]byte, error) {
	data, err := Serialize(key, true)
	if err != nil {
		return nil, fmt.Errorf("key serialize err: %w", err)
	}

	hash := sha256.New()
	hash.Write(data)
	return hash.Sum(nil), nil
}
