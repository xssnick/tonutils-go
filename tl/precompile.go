package tl

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"net"
	"reflect"
	"runtime"
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
	_ExecuteTypeIP6
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
				tp:     f.Type,
				fields: []*fieldInfo{elemInfo},
				fabric: func() reflect.Value {
					return reflect.New(f.Type)
				},
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

			var num uint32
			if len(tags) > 1 {
				numP, err := strconv.ParseUint(tags[1], 10, 32)
				if err != nil {
					panic("cells num tag should be positive integer")
				}
				num = uint32(numP)
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

				list := parseAllowed(tags[2:])
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
						if f.Type == reflect.TypeOf(net.IP{}) {
							info.typ = _ExecuteTypeIP6
						} else {
							info.typ = _ExecuteTypeInt128
						}
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
	idNum           uint32
	tp              reflect.Type
	fabric          func() reflect.Value
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
			si := _structInfoTableByType[val.Type()]
			if si == nil {
				return reflect.Value{}, fmt.Errorf("tl type %s is not compilled", val.Type().String())
			}

			rvx := si.fabric()
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

	si := _structInfoTableByType[rv.Type().Elem()]
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

	if err := executeSerialize(buf, ptr, t); err != nil {
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
		return nil, nil, fmt.Errorf("struct id %s is not registered %s", hex.EncodeToString(buf[:4]), hex.EncodeToString(buf))
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
		info = _structInfoTableByType[srcE.Type()]
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

		e := info.fabric()
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
		if len(buf) < 4 {
			return nil, fmt.Errorf("not enough bytes to parse boxed %s type id", t.tp.String())
		}

		if t.id == nil || binary.LittleEndian.Uint32(buf) != t.idNum {
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

	if buf, err = executeParse(buf, ptr, t, noCopy); err != nil {
		return nil, fmt.Errorf("failed to parse %s type: %w", t.tp.String(), err)
	}
	return buf, nil
}

// MaxVectorElements limits vector element count accepted from untrusted TL input.
// Set it to 0 or a negative value to disable the limit.
var MaxVectorElements = 1 << 20

// MaxVectorAllocationBytes limits memory allocated for vector slice storage.
// Set it to 0 or a negative value to disable the limit.
var MaxVectorAllocationBytes = 256 << 20

func minVectorElementSize(si *structInfo) int {
	if len(si.fields) != 1 {
		return 0
	}

	field := si.fields[0]
	if sz := serializedFixedSize(field); sz > 0 {
		return sz
	}

	switch field.typ {
	case _ExecuteTypeString, _ExecuteTypeBytes, _ExecuteTypeSingleCell, _ExecuteTypeSliceCell, _ExecuteTypeVector:
		return 4
	case _ExecuteTypeStruct:
		flags := field.meta.(uint32)
		if flags&_StructFlagsBytes != 0 || flags&_StructFlagsBoxed != 0 || flags&_StructFlagsInterface != 0 {
			return 4
		}
	}

	return 0
}

func executeParse(buf []byte, base unsafe.Pointer, si *structInfo, noCopy bool) ([]byte, error) {
	if !si.finalized {
		return nil, fmt.Errorf("TL struct %s is not registered", si.tp.String())
	}

	defer runtime.KeepAlive((*byte)(base))

	var flags uint32
	for _, field := range si.fields {
		if field.hasFlags && (1<<field.flag)&flags == 0 {
			// skip serialization if flag is not set
			continue
		}

		ptr := unsafe.Add(base, field.offset)

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
			var s string
			if s, buf, err = fromBytesString(buf); err != nil {
				return nil, fmt.Errorf("failed to parse string field %s: %w", field.String(), err)
			}
			*(*string)(ptr) = s
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
		case _ExecuteTypeIP6:
			if len(buf) < 16 {
				return nil, fmt.Errorf("not enough bytes to parse ip v6 field %s", field.String())
			}

			bts := make([]byte, 16)
			copy(bts, buf[:16])
			*(*[]byte)(ptr) = bts
			buf = buf[16:]
		case _ExecuteTypeSingleCell:
			var bts []byte
			if bts, buf, err = fromBytesNoCopy(buf); err != nil {
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
			if bts, buf, err = fromBytesNoCopy(buf); err != nil {
				return nil, fmt.Errorf("failed to parse slice cell bytes, field %s: %w", field.String(), err)
			}

			flag := field.meta.(uint32)
			if len(bts) == 0 && flag&(1<<31) != 0 {
				*(*[]*cell.Cell)(ptr) = nil
				break
			}

			c, err := cell.FromBOCMultiRoot(bts)
			if err != nil {
				return nil, fmt.Errorf("failed to parse slice cell field %s: %w", field.String(), err)
			}

			num := flag & 0x7FFFFFFF
			if num > 0 && uint32(len(c)) != num {
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
				if source, buf, err = fromBytesNoCopy(buf); err != nil {
					return nil, fmt.Errorf("failed to parse struct bytes, field %s: %w", field.String(), err)
				}
			}

			if structFlags&_StructFlagsPointer != 0 { // pointer
				nw := info.fabric().UnsafePointer()
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

						e := info.fabric()
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

					e := info.fabric()
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
			*(*bool)(ptr) = binary.LittleEndian.Uint32(buf) == _BoolTrueID
			buf = buf[4:]
		case _ExecuteTypeVector:
			if len(buf) < 4 {
				return nil, fmt.Errorf("not enough bytes to parse vector field %s", field.String())
			}
			lnRaw := binary.LittleEndian.Uint32(buf)
			buf = buf[4:]
			if MaxVectorElements > 0 && uint64(lnRaw) > uint64(MaxVectorElements) {
				return nil, fmt.Errorf("too many elements in vector field %s: %d > %d", field.String(), lnRaw, MaxVectorElements)
			}

			minElemSize := minVectorElementSize(field.structInfo)
			if minElemSize > 0 && uint64(minElemSize)*uint64(lnRaw) > uint64(len(buf)) {
				return nil, fmt.Errorf("not enough bytes to parse vector field %s with %d elements, need at least %d bytes", field.String(), lnRaw, uint64(minElemSize)*uint64(lnRaw))
			}
			if minElemSize == 0 && uint64(lnRaw) > uint64(len(buf)) {
				return nil, fmt.Errorf("not enough bytes to parse vector field %s with %d elements", field.String(), lnRaw)
			}
			if uint64(lnRaw) > uint64(1)<<(strconv.IntSize-1)-1 {
				return nil, fmt.Errorf("too many elements in vector field %s: %d overflows int", field.String(), lnRaw)
			}

			sz := field.structInfo.tp.Elem().Size()
			if MaxVectorAllocationBytes > 0 && sz > 0 {
				allocBytes := uint64(lnRaw) * uint64(sz)
				if allocBytes > uint64(MaxVectorAllocationBytes) {
					return nil, fmt.Errorf("too big vector allocation in field %s: %d > %d bytes", field.String(), allocBytes, MaxVectorAllocationBytes)
				}
			}

			ln := int(lnRaw)

			sl := reflect.MakeSlice(field.structInfo.tp, ln, ln)

			ePtr := unsafe.Pointer(sl.Pointer())

			for x := 0; x < ln; x++ {
				buf, err = executeParse(buf, ePtr, field.structInfo, noCopy)
				if err != nil {
					return nil, fmt.Errorf("failed to parse %s type, vector element %d: %w", si.tp.String(), x, err)
				}

				ePtr = unsafe.Add(ePtr, sz)
			}

			*(*reflect.SliceHeader)(ptr) = reflect.SliceHeader{
				Data: sl.Pointer(),
				Len:  ln,
				Cap:  ln,
			}
			runtime.KeepAlive(sl)
		default:
			return nil, fmt.Errorf("unknown type %d for field %s", t, field.String())
		}
	}
	return buf, nil
}

func serializedFixedSize(field *fieldInfo) int {
	switch field.typ {
	case _ExecuteTypeFlags, _ExecuteTypeInt, _ExecuteTypeBool, _ExecuteTypeInt32Bytes, _ExecuteTypeIP4:
		return 4
	case _ExecuteTypeLong, _ExecuteTypeInt64Bytes:
		return 8
	case _ExecuteTypeInt128, _ExecuteTypeIP6:
		return 16
	case _ExecuteTypeInt256:
		return 32
	default:
		return 0
	}
}

func growSerializeVector(buf *bytes.Buffer, field *fieldInfo, hdr *reflect.SliceHeader, elemSize uintptr) error {
	if hdr.Len == 0 || len(field.structInfo.fields) != 1 {
		return nil
	}

	elemField := field.structInfo.fields[0]
	total := 4
	if sz := serializedFixedSize(elemField); sz > 0 {
		total += hdr.Len * sz
		if total > buf.Available() {
			buf.Grow(total)
		}
		return nil
	}

	ePtr := unsafe.Pointer(hdr.Data)
	switch elemField.typ {
	case _ExecuteTypeString:
		if hdr.Len < 8 && len(*(*string)(ePtr)) < 4096 {
			return nil
		}

		for x := 0; x < hdr.Len; x++ {
			s := *(*string)(ePtr)
			sz, err := tlBytesEncodedSize(len(s))
			if err != nil {
				return err
			}
			total += sz
			ePtr = unsafe.Add(ePtr, elemSize)
		}
	case _ExecuteTypeBytes:
		if hdr.Len < 8 && len(*(*[]byte)(ePtr)) < 4096 {
			return nil
		}

		for x := 0; x < hdr.Len; x++ {
			b := *(*[]byte)(ePtr)
			sz, err := tlBytesEncodedSize(len(b))
			if err != nil {
				return err
			}
			total += sz
			ePtr = unsafe.Add(ePtr, elemSize)
		}
	default:
		return nil
	}

	if total > buf.Available() {
		buf.Grow(total)
	}
	return nil
}

func executeSerialize(buf *bytes.Buffer, base unsafe.Pointer, si *structInfo) error {
	if !si.finalized {
		return fmt.Errorf("TL struct %s is not registered", si.tp.String())
	}

	defer runtime.KeepAlive((*byte)(base))

	var flags uint32
	for _, field := range si.fields {
		if field.hasFlags && (1<<field.flag)&flags == 0 {
			// skip serialization if flag is not set
			continue
		}

		ptr := unsafe.Add(base, field.offset)

		switch t := field.typ; t {
		case _ExecuteTypeFlags:
			flags = *(*uint32)(ptr)
			writeUint32(buf, flags)
		case _ExecuteTypeString:
			if err := toStringToBuffer(buf, *(*string)(ptr)); err != nil {
				return fmt.Errorf("failed to serialize string field %s: %w", field.String(), err)
			}
		case _ExecuteTypeBytes:
			if err := ToBytesToBuffer(buf, *(*[]byte)(ptr)); err != nil {
				return fmt.Errorf("failed to serialize bytes field %s: %w", field.String(), err)
			}
		case _ExecuteTypeInt256:
			if bts := *(*[]byte)(ptr); len(bts) == 32 {
				buf.Write(*(*[]byte)(ptr))
			} else if len(bts) == 0 {
				writeZeros(buf, 32)
			} else {
				return fmt.Errorf("invalid int256 size %d in field %s", len(bts), field.String())
			}
		case _ExecuteTypeInt128:
			if bts := *(*[]byte)(ptr); len(bts) == 16 {
				buf.Write(*(*[]byte)(ptr))
			} else if len(bts) == 0 {
				writeZeros(buf, 16)
			} else {
				return fmt.Errorf("invalid int128 size %d in field %s", len(bts), field.String())
			}
		case _ExecuteTypeInt64Bytes:
			if bts := *(*[]byte)(ptr); len(bts) == 8 {
				buf.Write(*(*[]byte)(ptr))
			} else if len(bts) == 0 {
				writeZeros(buf, 8)
			} else {
				return fmt.Errorf("invalid int64 size %d in field %s", len(bts), field.String())
			}
		case _ExecuteTypeInt32Bytes:
			if bts := *(*[]byte)(ptr); len(bts) == 4 {
				buf.Write(*(*[]byte)(ptr))
			} else if len(bts) == 0 {
				writeZeros(buf, 4)
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
				writeZeros(buf, 4)
			} else {
				return fmt.Errorf("invalid ip size %d in field %s", len(ipBytes), field.String())
			}
		case _ExecuteTypeIP6:
			ipBytes := *(*net.IP)(ptr)
			if len(ipBytes) == net.IPv4len {
				ipBytes = ipBytes.To16()
				if ipBytes == nil {
					return fmt.Errorf("invalid ip v6 in field %s", field.String())
				}
			}
			if len(ipBytes) == net.IPv6len {
				buf.Write(ipBytes)
			} else if len(ipBytes) == 0 {
				writeZeros(buf, 16)
			} else {
				return fmt.Errorf("invalid ip size %d in field %s", len(ipBytes), field.String())
			}
		case _ExecuteTypeSingleCell:
			c := *(**cell.Cell)(ptr)
			if c == nil {
				if field.meta.(bool) {
					_ = ToBytesToBuffer(buf, nil)
					break
				}
				return fmt.Errorf("nil cell is not allowed in field %s", field.String())
			}

			if err := ToBytesToBuffer(buf, (*(**cell.Cell)(ptr)).ToBOCWithFlags(false)); err != nil {
				return fmt.Errorf("failed to serialize cell field %s: %w", field.String(), err)
			}
		case _ExecuteTypeSliceCell:
			c := *(*[]*cell.Cell)(ptr)
			flag := field.meta.(uint32)
			num := flag & 0x7FFFFFFF

			if len(c) == 0 && flag&(1<<31) != 0 {
				_ = ToBytesToBuffer(buf, nil)
				break
			}

			if num > 0 && uint32(len(c)) != num {
				return fmt.Errorf("incorrect cells len %d in field %s", len(c), field.String())
			}
			if err := ToBytesToBuffer(buf, cell.ToBOCWithFlags(c, false)); err != nil {
				return fmt.Errorf("failed to serialize slice cell field %s: %w", field.String(), err)
			}
		case _ExecuteTypeStruct:
			info := field.structInfo
			structFlags := field.meta.(uint32)
			var boxed = structFlags&_StructFlagsBoxed != 0

			var asBytes = structFlags&_StructFlagsBytes != 0
			var remapFrom int

			if asBytes {
				remapFrom = buf.Len()
				// bytes slice max length reserve
				writeZeros(buf, 4)
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
					info = _structInfoTableByType[e.Elem().Type()]
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
			var val uint32
			switch field.meta.(reflect.Kind) {
			case reflect.Uint64, reflect.Int64:
				val = uint32(*(*uint64)(ptr))
			case reflect.Int32, reflect.Uint32:
				val = *(*uint32)(ptr)
			case reflect.Uint16, reflect.Int16:
				val = uint32(*(*uint16)(ptr))
			case reflect.Uint8, reflect.Int8:
				val = uint32(*(*uint8)(ptr))
			case reflect.Int, reflect.Uint:
				val = uint32(*(*uint)(ptr))
			default:
				return fmt.Errorf("unsupported number type: %s", field.String())
			}

			writeUint32(buf, val)
		case _ExecuteTypeLong:
			var val uint64
			switch field.meta.(reflect.Kind) {
			case reflect.Uint64, reflect.Int64:
				val = *(*uint64)(ptr)
			case reflect.Int32, reflect.Uint32:
				val = uint64(*(*uint32)(ptr))
			case reflect.Uint16, reflect.Int16:
				val = uint64(*(*uint16)(ptr))
			case reflect.Uint8, reflect.Int8:
				val = uint64(*(*uint8)(ptr))
			case reflect.Int, reflect.Uint:
				val = uint64(*(*uint)(ptr))
			default:
				return fmt.Errorf("unsupported number type: %s", field.String())
			}

			writeUint64(buf, val)
		case _ExecuteTypeBool:
			if *(*bool)(ptr) {
				buf.Write(_BoolTrue)
			} else {
				buf.Write(_BoolFalse)
			}
		case _ExecuteTypeVector:
			hdr := (*reflect.SliceHeader)(ptr)
			ln := hdr.Len

			sz := field.structInfo.tp.Elem().Size()
			if err := growSerializeVector(buf, field, hdr, sz); err != nil {
				return fmt.Errorf("failed to reserve vector field %s: %w", field.String(), err)
			}
			writeUint32(buf, uint32(ln))

			ePtr := unsafe.Pointer(hdr.Data)

			for x := 0; x < ln; x++ {
				if err := executeSerialize(buf, ePtr, field.structInfo); err != nil {
					return fmt.Errorf("failed to serialize %s type, vector element %d: %w", si.tp.String(), x, err)
				}
				ePtr = unsafe.Add(ePtr, sz)
			}
		default:
			return fmt.Errorf("unknown type %d for field %s", t, field.String())
		}
	}

	return nil
}
