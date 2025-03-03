package tl

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"net"
	"reflect"
	"strconv"
	"strings"
	"unsafe"
)

const (
	_ExecuteTypeFlags = iota
	_ExecuteTypeString
	_ExecuteTypeBytes
	_ExecuteTypeInt256
	_ExecuteTypeInt128
	_ExecuteTypeInt64Bytes
	_ExecuteTypeInt32Bytes
	_ExecuteTypeIP4
	_ExecuteTypeSingleCell
	_ExecuteTypeSliceCell
	_ExecuteTypeStruct
	_ExecuteTypeInt
	_ExecuteTypeLong
	_ExecuteTypeBool
	_ExecuteTypeVector
)

const (
	_StructFlagsPointer = 1 << iota
	_StructFlagsBoxed
	_StructFlagsInterface
	_StructFlagsBytes
)

func compileField(parent reflect.Type, f reflect.StructField, tags []string) *fieldInfo {
	if f.Anonymous {
		panic("anonymous fields are not allowed in TL structs")
	}

	info := &fieldInfo{
		offset:     f.Offset,
		name:       f.Name,
		parentType: parent,
		fieldType:  f.Type,
	}

	var structFlags uint32
	var flagsUsed = false

	for {
		if strings.HasPrefix(tags[0], "?") {
			if flagsUsed {
				panic("field can only contain singe flag")
			}

			bit, err := strconv.Atoi(tags[0][1:])
			if err != nil {
				panic("invalid flag bit in tag, should be number")
			}
			if bit < 0 || bit > 31 {
				panic("invalid flag bit in tag, should be > 0 && < 32")
			}

			flagsUsed = true
			info.hasFlags = true
			info.flag = uint32(bit)
			tags = tags[1:]
			continue
		}

		switch tags[0] {
		case "flags":
			switch f.Type.Kind() {
			case reflect.Uint32, reflect.Int32:
				info.typ = _ExecuteTypeFlags
			default:
				panic("flags type should be 32 bit [u]int")
			}
		case "vector":
			if f.Type.Kind() != reflect.Slice {
				panic("vector should have slice type")
			}
			info.typ = _ExecuteTypeVector

			elem := f.Type.Elem()
			switch elem.Kind() {
			case reflect.Slice, reflect.Interface, reflect.Pointer, reflect.Struct, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
				reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
				reflect.Uint64, reflect.String:
			default:
				panic(fmt.Sprintf("unsupported type %s for vector", f.Type.String()))
			}

			elemInfo := compileField(f.Type, reflect.StructField{
				Name:   "[Slice Element]",
				Type:   elem,
				Offset: 0,
			}, tags[1:])

			// creating virtual struct with one field
			// to simplify serialization in runtime
			info.structInfo = &structInfo{
				tp:        f.Type,
				fields:    []*fieldInfo{elemInfo},
				finalized: true,
				tlName:    "##unusable_name",
			}
			break
		case "string":
			if f.Type.Kind() == reflect.String {
				info.typ = _ExecuteTypeString
				break
			}
		case "cell":
			optional := len(tags) > 1 && tags[1] == "optional"
			if optional {
				tags = tags[1:]
			}

			var err error
			var num int
			if len(tags) > 1 {
				num, err = strconv.Atoi(tags[1])
				if err != nil || num <= 0 {
					panic("cells num tag should be positive integer")
				}
			}

			if f.Type == reflect.TypeOf(&cell.Cell{}) {
				if num > 0 {
					panic("field type should be cell slice to use cells num tag")
				}
				info.meta = optional
				info.typ = _ExecuteTypeSingleCell
				break
			} else if f.Type == reflect.TypeOf([]*cell.Cell{}) {
				if optional {
					num |= 1 << 31
				}
				info.meta = num
				info.typ = _ExecuteTypeSliceCell
				break
			}
			panic("for cell tag only *cell.Cell is supported")
		case "struct":
			isBoxed := len(tags) > 1 && tags[1] == "boxed"
			if isBoxed {
				structFlags |= _StructFlagsBoxed
			}

			if f.Type.Kind() == reflect.Pointer {
				if f.Type.Elem().Kind() != reflect.Struct {
					panic("pointer must point to struct in TL type with tag `struct`")
				}
				structFlags |= _StructFlagsPointer
				info.structInfo = getStructInfoReference(f.Type.Elem())
			} else if f.Type.Kind() == reflect.Interface {
				if !isBoxed {
					panic("interface type in " + f.Name + " field should be boxed")
				}
				structFlags |= _StructFlagsInterface

				list := splitAllowed(tags[2:])
				for _, s := range list {
					info.allowedTypes = append(info.allowedTypes, getStructInfoReferenceByShortName(s))
				}
			} else if f.Type.Kind() == reflect.Struct {
				info.structInfo = getStructInfoReference(f.Type)
			} else {
				panic("not supported type for struct serialization")
			}

			info.typ = _ExecuteTypeStruct
			info.meta = structFlags
		case "int256", "int128", "bytes":
			if tags[0] == "bytes" && len(tags) > 1 && tags[1] == "struct" {
				tags = tags[1:]
				structFlags |= _StructFlagsBytes
				// parse as struct now
				continue
			}

			switch f.Type.Kind() {
			case reflect.Slice:
				switch f.Type.Elem().Kind() {
				case reflect.Uint8:
					if tags[0] == "int256" {
						info.typ = _ExecuteTypeInt256
					} else if tags[0] == "int128" {
						info.typ = _ExecuteTypeInt128
					} else {
						info.typ = _ExecuteTypeBytes
					}
				default:
					panic("for int256 and int128 only bytes slice supported")
				}
			default:
				panic("for int256 and int128 only bytes slice supported")
			}
		case "bool":
			info.typ = _ExecuteTypeBool
		case "int", "long":
			switch f.Type.Kind() {
			case reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8, reflect.Int,
				reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8, reflect.Uint:
				if tags[0] == "int" {
					info.typ = _ExecuteTypeInt
				} else if tags[0] == "long" {
					info.typ = _ExecuteTypeLong
				}
				info.meta = f.Type.Kind()
			case reflect.Slice:
				switch f.Type.Elem().Kind() {
				case reflect.Uint8:
					info.typ = _ExecuteTypeInt64Bytes
					if tags[0] == "int" {
						info.typ = _ExecuteTypeInt32Bytes
					}

					if f.Type == reflect.TypeOf(net.IP{}) {
						info.typ = _ExecuteTypeIP4
					}
				default:
					panic("must be slice of bytes")
				}
			default:
				panic("for int and long only bytes slice and integers are supported")
			}
		default:
			panic("unknown tag:" + tags[0])
		}
		break
	}

	return info
}

type structInfo struct {
	id              []byte
	tp              reflect.Type
	fields          []*fieldInfo
	manualSerialize bool
	manualParse     bool
	finalized       bool
	raw             bool
	tlName          string
}

type fieldInfo struct {
	offset       uintptr
	typ          int
	meta         any
	hasFlags     bool
	flag         uint32
	name         string
	structInfo   *structInfo
	allowedTypes []*structInfo
	parentType   reflect.Type
	fieldType    reflect.Type
}

func (f *fieldInfo) checkIsAllowed(si *structInfo) bool {
	if len(f.allowedTypes) == 0 {
		return true
	}

	for _, ft := range f.allowedTypes {
		if ft.tlName == si.tlName {
			return true
		}
	}
	return false
}

func (f *fieldInfo) String() string {
	return f.name + " of type " + f.parentType.String()
}

func addressablePtr(val reflect.Value) (reflect.Value, error) {
	switch val.Kind() {
	case reflect.Pointer:
		if val.IsNil() {
			return reflect.Value{}, fmt.Errorf("value should not be nil")
		}
		return val, nil
	case reflect.Struct:
		if !val.CanAddr() {
			rvx := reflect.New(val.Type())
			rvx.Elem().Set(val)
			return rvx, nil
		}
		return val.Addr(), nil
	default:
		return reflect.Value{}, fmt.Errorf("unsupported value kind %s (%s)", val.Kind().String(), val.Type().String())
	}
}

func Serialize(v Serializable, boxed bool, bufOpt ...*bytes.Buffer) ([]byte, error) {
	startLen := 0
	var buf *bytes.Buffer
	if len(bufOpt) > 0 && bufOpt[0] != nil {
		buf = bufOpt[0]
		startLen = buf.Len()

		if raw, ok := v.(Raw); ok {
			buf.Write(raw)
			return buf.Bytes()[startLen:], nil
		}
	} else {
		if raw, ok := v.(Raw); ok {
			return raw, nil
		}

		buf = bytes.NewBuffer(nil)
		buf.Grow(DefaultSerializeBufferSize)
	}

	if list, ok := v.([]Serializable); ok {
		for i, v := range list {
			e := reflect.ValueOf(v)
			if err := serializeStruct(e, buf, boxed, nil); err != nil {
				return nil, fmt.Errorf("serialization of type %s failed (for slice element %d): %w", e.Type().String(), i, err)
			}
		}
		return buf.Bytes()[startLen:], nil
	}

	e := reflect.ValueOf(v)
	if err := serializeStruct(e, buf, boxed, nil); err != nil {
		return nil, fmt.Errorf("serialization of type %s failed: %w", e.Type().String(), err)
	}

	return buf.Bytes()[startLen:], nil
}

func serializeStruct(v reflect.Value, buf *bytes.Buffer, boxed bool, field *fieldInfo) error {
	rv, err := addressablePtr(v)
	if err != nil {
		return err
	}

	si := _structInfoTable[rv.Type().Elem().String()]
	if si == nil {
		return fmt.Errorf("tl type %s is not compilled", rv.Type().String())
	}

	if field != nil && !field.checkIsAllowed(si) {
		return fmt.Errorf("invalid type %s is not allowed at field %s", si.tp.String(), field.String())
	}

	return serializePrecompiled(rv.UnsafePointer(), si, boxed, buf)
}

var rawStructInfo = &structInfo{
	raw:       true,
	finalized: true,
}

func serializePrecompiled(ptr unsafe.Pointer, t *structInfo, boxed bool, buf *bytes.Buffer) error {
	if t.raw {
		buf.Write(*(*Raw)(ptr))
		return nil
	}

	bufStart := buf.Len()
	if boxed {
		if t.id == nil {
			panic("boxed while id not defined")
		}
		buf.Write(t.id)
	}

	if t.manualSerialize {
		if err := reflect.NewAt(t.tp, ptr).Interface().(SerializableTL).Serialize(buf); err != nil {
			buf.Truncate(bufStart)
			return fmt.Errorf("failed to serialize %s using manual method: %w", t.tp.String(), err)
		}
		return nil
	}

	if err := executeSerialize(buf, uintptr(ptr), t); err != nil {
		buf.Truncate(bufStart)
		return fmt.Errorf("failed to serialize %s type: %w", t.tp.String(), err)
	}

	return nil
}

func parseBoxedType(buf []byte) ([]byte, *structInfo, error) {
	if len(buf) < 4 {
		return nil, nil, fmt.Errorf("not enough bytes to parse struct interface")
	}

	info := _SchemaByID[binary.LittleEndian.Uint32(buf)]
	if info == nil {
		return nil, nil, fmt.Errorf("struct id %s is not registered", hex.EncodeToString(buf[:4]))
	}
	return buf[4:], info, nil
}

func Parse(v Serializable, data []byte, boxed bool) (_ []byte, err error) {
	src := reflect.ValueOf(v)
	if src.Kind() != reflect.Pointer || src.IsNil() {
		return nil, fmt.Errorf("v should be a pointer and not nil")
	}
	srcE := src.Elem()

	var info *structInfo
	switch srcE.Kind() {
	case reflect.Struct:
		info = _structInfoTable[srcE.Type().String()]
		if info == nil {
			return nil, fmt.Errorf("tl type %s is not compilled", srcE.Type().String())
		}

		if data, err = parsePrecompiled(src.UnsafePointer(), info, boxed, data, false); err != nil {
			return nil, err
		}
	case reflect.Interface:
		if !boxed {
			return nil, fmt.Errorf("to parse into interface type should be boxed")
		}

		if data, info, err = parseBoxedType(data); err != nil {
			return nil, err
		}

		e := reflect.New(info.tp)
		if data, err = parsePrecompiled(e.UnsafePointer(), info, false, data, false); err != nil {
			return nil, err
		}
		srcE.Set(e.Elem())
	default:
		return nil, fmt.Errorf("unsupported v kind %s, underlying value should be struct or interface", src.Kind().String())
	}

	return data, nil
}

func parsePrecompiled(ptr unsafe.Pointer, t *structInfo, boxed bool, buf []byte, noCopy bool) ([]byte, error) {
	if t.raw {
		if noCopy {
			*(*Raw)(ptr) = buf
			return nil, nil
		}
		*(*Raw)(ptr) = append([]byte{}, buf...)
		return nil, nil
	}

	if boxed {
		if !bytes.Equal(t.id, buf[:4]) {
			return nil, fmt.Errorf("invalid TL type id %s, want %s for %s", hex.EncodeToString(buf[:4]), hex.EncodeToString(t.id), t.tp.String())
		}
		buf = buf[4:]
	}

	var err error
	if t.manualParse {
		if buf, err = reflect.NewAt(t.tp, ptr).Interface().(ParseableTL).Parse(buf); err != nil {
			return nil, fmt.Errorf("failed to parse %s using manual method: %w", t.tp.String(), err)
		}
		return buf, nil
	}

	if buf, err = executeParse(buf, uintptr(ptr), t, noCopy); err != nil {
		return nil, fmt.Errorf("failed to parse %s type: %w", t.tp.String(), err)
	}
	return buf, nil
}

func executeParse(buf []byte, startPtr uintptr, si *structInfo, noCopy bool) ([]byte, error) {
	if !si.finalized {
		return nil, fmt.Errorf("TL struct %s is not registered", si.tp.String())
	}

	// TODO: noCopy mode

	var flags uint32
	for _, field := range si.fields {
		if field.hasFlags && (1<<field.flag)&flags == 0 {
			// skip serialization if flag is not set
			continue
		}

		//goland:noinspection GoVetUnsafePointer
		ptr := unsafe.Pointer(startPtr + field.offset)

		var err error
		switch t := field.typ; t {
		case _ExecuteTypeFlags:
			if len(buf) < 4 {
				return nil, fmt.Errorf("not enough bytes to parse flags field %s", field.String())
			}

			flags = binary.LittleEndian.Uint32(buf)
			*(*uint32)(ptr) = flags
			buf = buf[4:]
		case _ExecuteTypeString:
			var bts []byte
			if bts, buf, err = FromBytes(buf); err != nil {
				return nil, fmt.Errorf("failed to parse string field %s: %w", field.String(), err)
			}
			*(*string)(ptr) = string(bts)
		case _ExecuteTypeBytes:
			var bts []byte
			if bts, buf, err = FromBytes(buf); err != nil {
				return nil, fmt.Errorf("failed to parse bytes field %s: %w", field.String(), err)
			}
			*(*[]byte)(ptr) = bts
		case _ExecuteTypeInt256:
			if len(buf) < 32 {
				return nil, fmt.Errorf("not enough bytes to parse int256 field %s", field.String())
			}

			bts := make([]byte, 32)
			copy(bts, buf)
			*(*[]byte)(ptr) = bts
			buf = buf[32:]
		case _ExecuteTypeInt128:
			if len(buf) < 16 {
				return nil, fmt.Errorf("not enough bytes to parse int128 field %s", field.String())
			}

			bts := make([]byte, 16)
			copy(bts, buf)
			*(*[]byte)(ptr) = bts
			buf = buf[16:]
		case _ExecuteTypeInt64Bytes:
			if len(buf) < 8 {
				return nil, fmt.Errorf("not enough bytes to parse long field %s", field.String())
			}

			bts := make([]byte, 8)
			copy(bts, buf)
			*(*[]byte)(ptr) = bts
			buf = buf[8:]
		case _ExecuteTypeInt32Bytes:
			if len(buf) < 4 {
				return nil, fmt.Errorf("not enough bytes to parse int field %s", field.String())
			}

			bts := make([]byte, 4)
			copy(bts, buf)
			*(*[]byte)(ptr) = bts
			buf = buf[4:]
		case _ExecuteTypeIP4:
			if len(buf) < 4 {
				return nil, fmt.Errorf("not enough bytes to parse ip v4 field %s", field.String())
			}

			bts := make([]byte, 4)
			bts[0], bts[1], bts[2], bts[3] = buf[3], buf[2], buf[1], buf[0]
			*(*[]byte)(ptr) = bts
			buf = buf[4:]
		case _ExecuteTypeSingleCell:
			var bts []byte
			if bts, buf, err = FromBytes(buf); err != nil {
				return nil, fmt.Errorf("failed to parse single cell bytes, field %s: %w", field.String(), err)
			}

			if len(bts) == 0 && field.meta.(bool) {
				*(**cell.Cell)(ptr) = nil
				break
			}

			c, err := cell.FromBOC(bts)
			if err != nil {
				return nil, fmt.Errorf("failed to parse single cell field %s: %w", field.String(), err)
			}
			*(**cell.Cell)(ptr) = c
		case _ExecuteTypeSliceCell:
			var bts []byte
			if bts, buf, err = FromBytes(buf); err != nil {
				return nil, fmt.Errorf("failed to parse slice cell bytes, field %s: %w", field.String(), err)
			}

			flag := field.meta.(int)
			if len(bts) == 0 && flag&(1<<31) != 0 {
				*(*[]*cell.Cell)(ptr) = nil
				break
			}

			c, err := cell.FromBOCMultiRoot(bts)
			if err != nil {
				return nil, fmt.Errorf("failed to parse slice cell field %s: %w", field.String(), err)
			}

			num := flag & 0x7FFFFFFF
			if num > 0 && len(c) != num {
				return nil, fmt.Errorf("incorrect cells num %d in field %s, want %d", len(c), field.String(), field.meta.(int))
			}
			*(*[]*cell.Cell)(ptr) = c
		case _ExecuteTypeStruct:
			info := field.structInfo
			structFlags := field.meta.(uint32)
			var boxed = structFlags&_StructFlagsBoxed != 0
			var asBytes = structFlags&_StructFlagsBytes != 0
			var parsed = false

			source := buf
			if asBytes {
				if source, buf, err = FromBytes(buf); err != nil {
					return nil, fmt.Errorf("failed to parse struct bytes, field %s: %w", field.String(), err)
				}
			}

			if structFlags&_StructFlagsPointer != 0 { // pointer
				nw := reflect.New(info.tp).UnsafePointer()
				*(*unsafe.Pointer)(ptr) = nw
				ptr = nw
			} else if structFlags&_StructFlagsInterface != 0 {
				if asBytes {
					var list = make([]Serializable, 0, 2)
					// array of types
					for len(source) > 0 {
						if source, info, err = parseBoxedType(source); err != nil {
							return nil, fmt.Errorf("failed to parse struct boxed type, field %s: %w", field.String(), err)
						}

						if !field.checkIsAllowed(info) {
							return nil, fmt.Errorf("invalid type %s is not allowed at field %s", info.tp.String(), field.String())
						}

						e := reflect.New(info.tp)
						if source, err = parsePrecompiled(e.UnsafePointer(), info, false, source, noCopy); err != nil {
							return nil, err
						}
						list = append(list, e.Elem().Interface())
					}

					switch len(list) {
					case 1:
						*(*Serializable)(ptr) = list[0]
					case 0:
						return nil, fmt.Errorf("empty bytes slice cannot be parse as struct interface")
					default:
						var val Serializable
						val = list
						*(*Serializable)(ptr) = val
					}
				} else {
					if source, info, err = parseBoxedType(source); err != nil {
						return nil, fmt.Errorf("failed to parse struct boxed type, field %s: %w", field.String(), err)
					}

					if !field.checkIsAllowed(info) {
						return nil, fmt.Errorf("invalid type %s is not allowed at field %s", info.tp.String(), field.String())
					}

					e := reflect.New(info.tp)
					if source, err = parsePrecompiled(e.UnsafePointer(), info, false, source, noCopy); err != nil {
						return nil, err
					}
					*(*Serializable)(ptr) = e.Elem().Interface()
				}
				parsed = true
			}

			if !parsed {
				if source, err = parsePrecompiled(ptr, info, boxed, source, noCopy); err != nil {
					return nil, err
				}
			}

			if !asBytes {
				buf = source
			}
		case _ExecuteTypeInt:
			if len(buf) < 4 {
				return nil, fmt.Errorf("not enough bytes to parse int field %s", field.String())
			}

			val := binary.LittleEndian.Uint32(buf)
			buf = buf[4:]

			switch field.meta.(reflect.Kind) {
			case reflect.Uint64, reflect.Int64:
				*(*uint64)(ptr) = uint64(val)
			case reflect.Int32, reflect.Uint32:
				*(*uint32)(ptr) = val
			case reflect.Uint16, reflect.Int16:
				*(*uint16)(ptr) = uint16(val)
			case reflect.Uint8, reflect.Int8:
				*(*uint8)(ptr) = uint8(val)
			case reflect.Int, reflect.Uint:
				*(*int)(ptr) = int(val)
			default:
				return nil, fmt.Errorf("unsupported number type: %s", field.String())
			}
		case _ExecuteTypeLong:
			if len(buf) < 8 {
				return nil, fmt.Errorf("not enough bytes to parse long field %s", field.String())
			}

			val := binary.LittleEndian.Uint64(buf)
			buf = buf[8:]

			switch field.meta.(reflect.Kind) {
			case reflect.Uint64, reflect.Int64:
				*(*uint64)(ptr) = val
			case reflect.Int32, reflect.Uint32:
				*(*uint32)(ptr) = uint32(val)
			case reflect.Uint16, reflect.Int16:
				*(*uint16)(ptr) = uint16(val)
			case reflect.Uint8, reflect.Int8:
				*(*uint8)(ptr) = uint8(val)
			case reflect.Int, reflect.Uint:
				*(*int)(ptr) = int(val)
			default:
				return nil, fmt.Errorf("unsupported number type: %s", field.String())
			}
		case _ExecuteTypeBool:
			if len(buf) < 4 {
				return nil, fmt.Errorf("not enough bytes to parse bool field %s", field.String())
			}
			*(*bool)(ptr) = bytes.Equal(buf[:4], _BoolTrue)
			buf = buf[4:]
		case _ExecuteTypeVector:
			if len(buf) < 4 {
				return nil, fmt.Errorf("not enough bytes to parse vector field %s", field.String())
			}
			ln := int(binary.LittleEndian.Uint32(buf))
			buf = buf[4:]

			sl := reflect.MakeSlice(field.structInfo.tp, ln, ln)

			sz := field.structInfo.tp.Elem().Size()
			ePtr := sl.Pointer()

			for x := 0; x < ln; x++ {
				buf, err = executeParse(buf, ePtr, field.structInfo, noCopy)
				if err != nil {
					return nil, fmt.Errorf("failed to parse %s type, vector element %d: %w", si.tp.String(), x, err)
				}

				ePtr += sz
			}

			*(*reflect.SliceHeader)(ptr) = reflect.SliceHeader{
				Data: sl.Pointer(),
				Len:  ln,
				Cap:  ln,
			}
		default:
			return nil, fmt.Errorf("unknown type %d for field %s", t, field.String())
		}
	}
	return buf, nil
}

func executeSerialize(buf *bytes.Buffer, startPtr uintptr, si *structInfo) error {
	if !si.finalized {
		return fmt.Errorf("TL struct %s is not registered", si.tp.String())
	}

	var flags uint32
	for _, field := range si.fields {
		if field.hasFlags && (1<<field.flag)&flags == 0 {
			// skip serialization if flag is not set
			continue
		}

		//goland:noinspection GoVetUnsafePointer
		ptr := unsafe.Pointer(startPtr + field.offset)

		switch t := field.typ; t {
		case _ExecuteTypeFlags:
			flags = *(*uint32)(ptr)

			tmp := make([]byte, 4)
			binary.LittleEndian.PutUint32(tmp, flags)
			buf.Write(tmp)
		case _ExecuteTypeString:
			ToBytesToBuffer(buf, []byte(*(*string)(ptr)))
		case _ExecuteTypeBytes:
			ToBytesToBuffer(buf, *(*[]byte)(ptr))
		case _ExecuteTypeInt256:
			if bts := *(*[]byte)(ptr); len(bts) == 32 {
				buf.Write(*(*[]byte)(ptr))
			} else if len(bts) == 0 {
				buf.Write(make([]byte, 32))
			} else {
				return fmt.Errorf("invalid int256 size %d in field %s", len(bts), field.String())
			}
		case _ExecuteTypeInt128:
			if bts := *(*[]byte)(ptr); len(bts) == 16 {
				buf.Write(*(*[]byte)(ptr))
			} else if len(bts) == 0 {
				buf.Write(make([]byte, 16))
			} else {
				return fmt.Errorf("invalid int128 size %d in field %s", len(bts), field.String())
			}
		case _ExecuteTypeInt64Bytes:
			if bts := *(*[]byte)(ptr); len(bts) == 8 {
				buf.Write(*(*[]byte)(ptr))
			} else if len(bts) == 0 {
				buf.Write(make([]byte, 8))
			} else {
				return fmt.Errorf("invalid int64 size %d in field %s", len(bts), field.String())
			}
		case _ExecuteTypeInt32Bytes:
			if bts := *(*[]byte)(ptr); len(bts) == 4 {
				buf.Write(*(*[]byte)(ptr))
			} else if len(bts) == 0 {
				buf.Write(make([]byte, 4))
			} else {
				return fmt.Errorf("invalid int32 size %d in field %s", len(bts), field.String())
			}
		case _ExecuteTypeIP4:
			ipBytes := *(*net.IP)(ptr)
			if len(ipBytes) == net.IPv6len {
				ipBytes = ipBytes.To4()
				if ipBytes == nil {
					return fmt.Errorf("invalid ip v4 in field %s", field.String())
				}
			}
			if len(ipBytes) == net.IPv4len {
				for i := 3; i >= 0; i-- {
					buf.WriteByte(ipBytes[i])
				}
			} else if len(ipBytes) == 0 {
				buf.Write(make([]byte, 4))
			} else {
				return fmt.Errorf("invalid ip size %d in field %s", len(ipBytes), field.String())
			}
		case _ExecuteTypeSingleCell:
			c := *(**cell.Cell)(ptr)
			if c == nil {
				if field.meta.(bool) {
					ToBytesToBuffer(buf, nil)
					break
				}
				return fmt.Errorf("nil cell is not allowed in field %s", field.String())
			}
			ToBytesToBuffer(buf, (*(**cell.Cell)(ptr)).ToBOCWithFlags(false))
		case _ExecuteTypeSliceCell:
			c := *(*[]*cell.Cell)(ptr)
			flag := field.meta.(int)
			num := flag & 0x7FFFFFFF

			if len(c) == 0 && flag&(1<<31) != 0 {
				ToBytesToBuffer(buf, nil)
				break
			}

			if num > 0 && len(c) != num {
				return fmt.Errorf("incorrect cells len %d in field %s", len(c), field.String())
			}
			ToBytesToBuffer(buf, cell.ToBOCWithFlags(c, false))
		case _ExecuteTypeStruct:
			info := field.structInfo
			structFlags := field.meta.(uint32)
			var boxed = structFlags&_StructFlagsBoxed != 0

			var asBytes = structFlags&_StructFlagsBytes != 0
			var remapFrom int

			if asBytes {
				remapFrom = buf.Len()
				// bytes slice max length reserve
				buf.Write(make([]byte, 4))
			}

			var serialized bool
			if structFlags&_StructFlagsPointer != 0 { // pointer
				ptr = *(*unsafe.Pointer)(ptr)
			} else if structFlags&_StructFlagsInterface != 0 {
				ifc := *(*Serializable)(ptr)
				switch v := ifc.(type) {
				case Raw:
					ptr = unsafe.Pointer(&v)
					info = rawStructInfo
				case []Serializable:
					// serialize each element and write them as Raw, to pack into main struct after
					for i, val := range v {
						e := reflect.ValueOf(val)
						if err := serializeStruct(e, buf, boxed, field); err != nil {
							return fmt.Errorf("serialization of type %s failed (for interface slice element %d): %w", e.Type().String(), i, err)
						}
					}
					serialized = true
				default:
					e, err := addressablePtr(reflect.ValueOf(ifc))
					if err != nil {
						return fmt.Errorf("invalid type for interface in field %s: %w", field.String(), err)
					}

					ptr = e.UnsafePointer()
					info = _structInfoTable[e.Elem().Type().String()]
					if info == nil {
						return fmt.Errorf("unregistered TL type %s for interface in field %s", e.Elem().Type().String(), field.String())
					}

					if !field.checkIsAllowed(info) {
						return fmt.Errorf("invalid type %s is not allowed at field %s", info.tp.String(), field.String())
					}
				}
			}

			if !serialized {
				if info == nil {
					return fmt.Errorf("unregistered TL type in field %s", field.String())
				}

				if err := serializePrecompiled(ptr, info, boxed, buf); err != nil {
					return err
				}
			}

			if asBytes {
				RemapBufferAsSlice(buf, remapFrom)
			}
		case _ExecuteTypeInt:
			tmp := make([]byte, 4)

			switch field.meta.(reflect.Kind) {
			case reflect.Uint64, reflect.Int64:
				binary.LittleEndian.PutUint32(tmp, uint32(*(*uint64)(ptr)))
			case reflect.Int32, reflect.Uint32:
				binary.LittleEndian.PutUint32(tmp, *(*uint32)(ptr))
			case reflect.Uint16, reflect.Int16:
				binary.LittleEndian.PutUint32(tmp, uint32(*(*uint16)(ptr)))
			case reflect.Uint8, reflect.Int8:
				binary.LittleEndian.PutUint32(tmp, uint32(*(*uint8)(ptr)))
			case reflect.Int, reflect.Uint:
				binary.LittleEndian.PutUint32(tmp, uint32(*(*uint)(ptr)))
			default:
				return fmt.Errorf("unsupported number type: %s", field.String())
			}

			buf.Write(tmp)
		case _ExecuteTypeLong:
			tmp := make([]byte, 8)

			switch field.meta.(reflect.Kind) {
			case reflect.Uint64, reflect.Int64:
				binary.LittleEndian.PutUint64(tmp, *(*uint64)(ptr))
			case reflect.Int32, reflect.Uint32:
				binary.LittleEndian.PutUint64(tmp, uint64(*(*uint32)(ptr)))
			case reflect.Uint16, reflect.Int16:
				binary.LittleEndian.PutUint64(tmp, uint64(*(*uint16)(ptr)))
			case reflect.Uint8, reflect.Int8:
				binary.LittleEndian.PutUint64(tmp, uint64(*(*uint8)(ptr)))
			case reflect.Int, reflect.Uint:
				binary.LittleEndian.PutUint64(tmp, uint64(*(*uint)(ptr)))
			default:
				return fmt.Errorf("unsupported number type: %s", field.String())
			}

			buf.Write(tmp)
		case _ExecuteTypeBool:
			if *(*bool)(ptr) {
				buf.Write(_BoolTrue)
			} else {
				buf.Write(_BoolFalse)
			}
		case _ExecuteTypeVector:
			st := reflect.NewAt(field.structInfo.tp, ptr).Elem()
			ln := st.Len()

			tmp := make([]byte, 4)
			binary.LittleEndian.PutUint32(tmp, uint32(ln))
			buf.Write(tmp)

			sz := field.structInfo.tp.Elem().Size()
			ePtr := st.Pointer()

			for x := 0; x < ln; x++ {
				if err := executeSerialize(buf, ePtr, field.structInfo); err != nil {
					return fmt.Errorf("failed to serialize %s type, vector element %d: %w", si.tp.String(), x, err)
				}
				ePtr += sz
			}
		default:
			return fmt.Errorf("unknown type %d for field %s", t, field.String())
		}
	}

	return nil
}
