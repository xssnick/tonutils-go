package tl

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"reflect"
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
var _SchemaByID = map[uint32]*structInfo{}

var _BoolTrue = func() []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, CRC("boolTrue = Bool"))
	return buf
}()

var _BoolFalse = func() []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, CRC("boolFalse = Bool"))
	return buf
}()

var Logger = func(a ...any) {}

var DefaultSerializeBufferSize = 1024

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

var _structInfoTable = map[string]*structInfo{}
var _structInfoTableTLNames = map[string]*structInfo{}

type unregisteredTL struct{}

func finalizeStructInfoReference(si *structInfo) {
	if len(si.tlName) > 0 {
		siCached := _structInfoTableTLNames[si.tlName]
		if siCached != nil {
			if siCached.finalized {
				Logger("TL struct names conflict: " + si.tp.String() + " and " + siCached.tp.String() + " (" + si.tlName + ")")
			}
			*siCached = *si
		} else {
			_structInfoTableTLNames[si.tlName] = si
		}

		_SchemaByID[binary.LittleEndian.Uint32(si.id)] = si
	}

	siType := _structInfoTable[si.tp.String()]
	if siType == nil {
		_structInfoTable[si.tp.String()] = si
	} else if siType != si {
		*siType = *si
	}
	si.finalized = true
}

func getStructInfoReference(t reflect.Type) *structInfo {
	if t.Kind() != reflect.Struct {
		panic("tl type kind should be a struct, not a pointer or something else")
	}

	if t == reflect.TypeOf(Raw{}) {
		return rawStructInfo
	}

	si := _structInfoTable[t.String()]
	if si == nil {
		si = &structInfo{tp: reflect.TypeOf(unregisteredTL{})}
		_structInfoTable[t.String()] = si
	}
	return si
}

func getStructInfoReferenceByShortName(name string) *structInfo {
	si := _structInfoTableTLNames[name]
	if si == nil {
		si = &structInfo{tp: reflect.TypeOf(unregisteredTL{})}
		_structInfoTableTLNames[name] = si
	}
	return si
}

func Register(typ any, tl string) uint32 {
	t := reflect.TypeOf(typ)

	si := getStructInfoReference(t)
	if si.finalized {
		panic(fmt.Errorf("tl object has already been registered with type %s", t.String()))
	}

	var id uint32
	if len(tl) > 0 {
		name := strings.SplitN(tl, " ", 2)[0]
		nameParts := strings.SplitN(name, "#", 2)

		if len(nameParts) > 1 {
			b, err := hex.DecodeString(nameParts[1])
			if err != nil {
				panic("invalid predefined id for " + name + ": " + err.Error())
			}
			id = binary.BigEndian.Uint32(b)
		} else {
			id = CRC(tl)
		}
		_SchemaIDByName[nameParts[0]] = id
		_SchemaIDByTypeName[t.String()] = id

		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, id)

		si.id = b
		si.tlName = nameParts[0]
	}
	si.tp = t

	nw := reflect.New(t).Interface()

	// if we have custom method, we use it
	if _, ok := nw.(SerializableTL); ok {
		si.manualSerialize = true
	}
	if _, ok := nw.(ParseableTL); ok {
		si.manualParse = true
	}

	if !si.manualSerialize || !si.manualParse {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)

			tag := strings.TrimSpace(f.Tag.Get("tl"))
			if len(tag) == 0 {
				panic("every TL struct field must have `tl` tag, if you want to skip field use tag `-`")
			}

			if tag == "-" {
				continue
			}
			tags := strings.Split(tag, " ")

			if cf := compileField(t, f, tags); cf != nil {
				si.fields = append(si.fields, cf)
			}
		}
	}

	finalizeStructInfoReference(si)

	if len(tl) > 0 {
		Logger("TL Registered:", hex.EncodeToString(si.id), tl)
	} else {
		Logger("TL Registered without id:", si.tp.String())
	}
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

	hash := sha256.Sum256(data)
	return hash[:], nil
}
