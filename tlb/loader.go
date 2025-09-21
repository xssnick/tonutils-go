package tlb

import (
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type Magic struct{}

type Unmarshaler interface {
	LoadFromCell(loader *cell.Slice) error
}

type Marshaller interface {
	ToCell() (*cell.Cell, error)
}

// LoadFromCell automatically parses cell based on struct tags
// ## N - means integer with N bits, if size <= 64 it loads to uint of any size, if > 64 it loads to *big.Int
// ^ - loads ref and calls recursively, if field type is *cell.Cell, it loads without parsing
// . - calls recursively to continue load from current loader (inner struct)
// dict [inline] N - loads dictionary with key size N, example: 'dict 256', inline option can be used if dict is Hashmap and not HashmapE
// bits N - loads bit slice N len to []byte
// bool - loads 1 bit boolean
// addr - loads ton address
// maybe - reads 1 bit, and loads rest if its 1, can be used in combination with others only
// either [leave {bits},{refs}] X Y - reads 1 bit, if its 0 - loads X, if 1 - loads Y,
//
//	tries to serialize first condition, if not succeed (not enough free bits or refs), then second.
//	if 'leave' is specified, then after write it will additionally check specified
//	number of free bits and refs in cell.
//
// ?FieldName - Conditional field loading depending on boolean value of specified field.
// /            Specified field must be declared before tag usage, or it will be always false during loading
// Some tags can be combined, for example "dict 256", "maybe ^"
// Magic can be used to load first bits and check struct type, in tag can be specified magic number itself, in [#]HEX or [$]BIN format
// Example:
// _ Magic `tlb:"#deadbeef"
// _ Magic `tlb:"$1101"
func LoadFromCell(v any, loader *cell.Slice, skipMagic ...bool) error {
	return loadFromCell(v, loader, false, len(skipMagic) > 0 && skipMagic[0])
}

func LoadFromCellAsProof(v any, loader *cell.Slice, skipMagic ...bool) error {
	return loadFromCell(v, loader, true, len(skipMagic) > 0 && skipMagic[0])
}

func loadFromCell(v any, slice *cell.Slice, skipProofBranches, skipMagic bool) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("v should be a pointer and not nil")
	}
	rv = rv.Elem()

	if ld, ok := v.(Unmarshaler); ok {
		err := ld.LoadFromCell(slice)
		if err != nil {
			return fmt.Errorf("failed to load from cell for %s, using manual loader, err: %w", rv.Type().Name(), err)
		}
		return nil
	}

	for i := 0; i < rv.NumField(); i++ {
		loader := slice
		structField := rv.Type().Field(i)
		parseType := structField.Type
		tag := strings.TrimSpace(structField.Tag.Get("tlb"))
		if tag == "-" {
			continue
		}
		settings := strings.Split(tag, " ")

		if len(settings) == 0 {
			continue
		}

		if settings[0][0] == '?' {
			// conditional tlb parse depending on some field value of this struct
			cond := rv.FieldByName(settings[0][1:])
			if !cond.Bool() {
				continue
			}
			settings = settings[1:]
		}

		if settings[0] == "maybe" {
			if parseType.Kind() != reflect.Pointer && parseType.Kind() != reflect.Interface && parseType.Kind() != reflect.Slice {
				return fmt.Errorf("maybe flag can only be applied to interface or pointer, field %s", structField.Name)
			}

			has, err := loader.LoadBoolBit()
			if err != nil {
				return fmt.Errorf("failed to load maybe for %s, err: %w", structField.Name, err)
			}

			if !has {
				continue
			}
			settings = settings[1:]
		}

		if structField.Type.Kind() == reflect.Pointer && structField.Type.Elem().Kind() != reflect.Struct {
			// to same process both pointers and types
			parseType = parseType.Elem()
		}

		if settings[0] == "either" {
			settings = settings[1:]
			if len(settings) < 2 {
				panic("either tag should have 2 args")
			}

			if settings[0] == "leave" {
				settings = settings[1:]

				if len(settings) < 3 {
					panic("either tag should have 2 args and leave tag should have 1 arg")
				}
				// skip leave
				settings = settings[1:]
			}

			isSecond, err := loader.LoadBoolBit()
			if err != nil {
				return fmt.Errorf("failed to load maybe for %s, err: %w", structField.Name, err)
			}

			if !isSecond {
				settings = []string{settings[0]}
			} else {
				settings = []string{settings[1]}
			}
		}

		typeToLoad := structField.Type
		setVal := func(val reflect.Value) {
			if typeToLoad.Kind() == reflect.Pointer && val.Kind() != reflect.Pointer {
				nw := reflect.New(val.Type())

				if val.Type() != parseType {
					val = val.Convert(parseType)
				}

				nw.Elem().Set(val)
				val = nw
			} else if typeToLoad.Kind() != reflect.Pointer && val.Kind() == reflect.Pointer {
				val = val.Elem()
			}

			if typeToLoad == val.Type() {
				rv.Field(i).Set(val)
			} else {
				rv.Field(i).Set(val.Convert(typeToLoad))
			}
		}

		if settings[0] == "^" {
			ref, err := loader.LoadRefCell()
			if err != nil {
				return fmt.Errorf("failed to load ref for %s, err: %w", structField.Name, err)
			}

			if skipProofBranches && ref.GetType() == cell.PrunedCellType {
				continue
			}

			settings = settings[1:]
			loader = ref.BeginParse()
		}

		if structField.Type.Kind() == reflect.Interface {
			allowed := strings.Join(settings, "")
			if !strings.HasPrefix(allowed, "[") || !strings.HasSuffix(allowed, "]") {
				panic("corrupted allowed list tag of field " + structField.Name + ", should be [a,b,c], got " + allowed)
			}

			// cut brackets
			allowed = allowed[1 : len(allowed)-1]
			types := strings.Split(allowed, ",")

			for _, typ := range types {
				t, ok := registered[typ]
				if !ok {
					panic("unregistered type " + typ)
				}

				if !checkMagic(t.Field(0).Tag.Get("tlb"), loader.Copy()) {
					continue
				}

				typeToLoad = t
				break
			}

			if typeToLoad == structField.Type {
				return fmt.Errorf("unexpected data to load, unknown magic")
			}
			settings = settings[:0]
		}

		if len(settings) == 0 || settings[0] == "." {
			nVal, err := structLoad(typeToLoad, loader, false, skipProofBranches)
			if err != nil {
				return fmt.Errorf("failed to load struct for %s, err: %w", structField.Name, err)
			}

			setVal(nVal)
			continue
		}

		// bits
		if settings[0] == "##" {
			num, err := strconv.ParseUint(settings[1], 10, 64)
			if err != nil {
				// we panic, because its developer's issue, need to fix tag
				panic("corrupted num bits in ## tag")
			}

			switch {
			case num <= 64:
				var x any
				switch parseType.Kind() {
				case reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8, reflect.Int:
					x, err = loader.LoadInt(uint(num))
					if err != nil {
						return fmt.Errorf("failed to load %s int %d, err: %w", structField.Name, num, err)
					}

					switch parseType.Kind() {
					case reflect.Int32:
						x = int32(x.(int64))
					case reflect.Int16:
						x = int16(x.(int64))
					case reflect.Int8:
						x = int8(x.(int64))
					case reflect.Int:
						x = int(x.(int64))
					}
				case reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8, reflect.Uint:
					x, err = loader.LoadUInt(uint(num))
					if err != nil {
						return fmt.Errorf("failed to load %s uint %d, err: %w", structField.Name, num, err)
					}

					switch parseType.Kind() {
					case reflect.Uint32:
						x = uint32(x.(uint64))
					case reflect.Uint16:
						x = uint16(x.(uint64))
					case reflect.Uint8:
						x = uint8(x.(uint64))
					case reflect.Uint:
						x = uint(x.(uint64))
					}
				default:
					if parseType == reflect.TypeOf(&big.Int{}) {
						x, err = loader.LoadBigInt(uint(num))
						if err != nil {
							return fmt.Errorf("failed to load bigint %d, err: %w", num, err)
						}
					} else {
						panic("unexpected field type for tag ## - " + parseType.String())
					}
				}

				setVal(reflect.ValueOf(x))
				continue
			case num <= 256:
				x, err := loader.LoadBigInt(uint(num))
				if err != nil {
					return fmt.Errorf("failed to load bigint %d, err: %w", num, err)
				}

				setVal(reflect.ValueOf(x))
				continue
			}
		} else if settings[0] == "addr" {
			x, err := loader.LoadAddr()
			if err != nil {
				return fmt.Errorf("failed to load address, err: %w", err)
			}

			setVal(reflect.ValueOf(x))
			continue
		} else if settings[0] == "bool" {
			x, err := loader.LoadBoolBit()
			if err != nil {
				return fmt.Errorf("failed to load bool, err: %w", err)
			}

			setVal(reflect.ValueOf(x))
			continue
		} else if settings[0] == "bits" {
			num, err := strconv.Atoi(settings[1])
			if err != nil {
				// we panic, because its developer's issue, need to fix tag
				panic("corrupted num bits in bits tag")
			}

			x, err := loader.LoadSlice(uint(num))
			if err != nil {
				return fmt.Errorf("failed to load bits %d for field %s, err: %w", num, structField.Name, err)
			}

			setVal(reflect.ValueOf(x))
			continue
		} else if parseType == reflect.TypeOf(Magic{}) {
			if skipMagic {
				// it can be skipped if parsed before in parent type, to determine child type
				continue
			}

			if !checkMagic(settings[0], loader) {
				return fmt.Errorf("magic is not correct for %s, want %s", rv.Type().String(), settings[0])
			}

			continue
		} else if settings[0] == "dict" {
			inline := false
			if settings[1] == "inline" {
				settings = settings[1:]
				inline = true
			}

			sz, err := strconv.ParseUint(settings[1], 10, 64)
			if err != nil {
				panic(fmt.Sprintf("cannot deserialize field '%s' as dict, bad size '%s'", structField.Name, settings[1]))
			}

			var dict *cell.Dictionary
			if inline {
				dict, err = loader.ToDict(uint(sz))
				if err != nil {
					return fmt.Errorf("failed to load dict for %s, err: %w", structField.Name, err)
				}
			} else {
				dict, err = loader.LoadDict(uint(sz))
				if err != nil {
					return fmt.Errorf("failed to load ref for %s, err: %w", structField.Name, err)
				}
			}

			if len(settings) < 4 || settings[2] != "->" {
				setVal(reflect.ValueOf(dict))
				continue
			}

			mv, err := prepareMap(settings, structField, dict, sz, skipProofBranches)
			if err != nil {
				return fmt.Errorf("failed to prepare map for %s, err: %w", structField.Name, err)
			}
			setVal(mv)
			continue
		} else if settings[0] == "var" {
			if settings[1] == "uint" {
				sz, err := strconv.Atoi(settings[2])
				if err != nil {
					panic(err.Error())
				}

				res, err := loader.LoadVarUInt(uint(sz))
				if err != nil {
					return fmt.Errorf("failed to load var uint: %w", err)
				}
				setVal(reflect.ValueOf(res))
				continue
			} else {
				panic("var of type " + settings[1] + " is not supported")
			}
		}

		panic(fmt.Sprintf("cannot deserialize field '%s' as tag '%s'", structField.Name, tag))
	}

	return nil
}

func checkMagic(tag string, loader *cell.Slice) bool {
	var sz, base int
	if strings.HasPrefix(tag, "#") {
		base = 16
		sz = (len(tag) - 1) * 4
	} else if strings.HasPrefix(tag, "$") {
		base = 2
		sz = len(tag) - 1
	} else {
		panic("unknown magic value type in tag: " + tag)
	}

	if sz > 64 {
		panic("too big magic value type in tag")
	}

	magic, err := strconv.ParseInt(tag[1:], base, 64)
	if err != nil {
		panic("corrupted magic value in tag")
	}

	ldMagic, err := loader.LoadUInt(uint(sz))
	if err != nil {
		return false
	}
	return ldMagic == uint64(magic)
}

func ToCell(v any) (*cell.Cell, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, fmt.Errorf("v should not be nil")
		}
		rv = rv.Elem()
	}

	if ld, ok := v.(Marshaller); ok {
		c, err := ld.ToCell()
		if err != nil {
			return nil, fmt.Errorf("failed to store to cell for %s, using manual storer, err: %w", reflect.TypeOf(v).PkgPath(), err)
		}
		return c, nil
	}

	root := cell.BeginCell()

next:
	for i := 0; i < rv.NumField(); i++ {
		structField := rv.Type().Field(i)
		parseType := structField.Type
		fieldVal := rv.Field(i)
		tag := strings.TrimSpace(structField.Tag.Get("tlb"))
		if tag == "-" {
			continue
		}
		settings := strings.Split(tag, " ")

		if len(settings) == 0 {
			continue
		}

		if len(settings[0]) > 0 && settings[0][0] == '?' {
			// conditional tlb parse depending on some field value of this struct
			cond := rv.FieldByName(settings[0][1:])
			if !cond.Bool() {
				continue
			}
			settings = settings[1:]
		}

		if settings[0] == "maybe" {
			if structField.Type.Kind() != reflect.Pointer && structField.Type.Kind() != reflect.Interface && structField.Type.Kind() != reflect.Slice {
				return nil, fmt.Errorf("maybe flag can only be applied to interface or pointer, field %s", structField.Name)
			}

			if fieldVal.IsNil() {
				if err := root.StoreBoolBit(false); err != nil {
					return nil, fmt.Errorf("cannot store maybe bit: %w", err)
				}
				continue
			}

			if err := root.StoreBoolBit(true); err != nil {
				return nil, fmt.Errorf("cannot store maybe bit: %w", err)
			}
			settings = settings[1:]
		}

		if structField.Type.Kind() == reflect.Pointer && structField.Type.Elem().Kind() != reflect.Struct {
			// to same process both pointers and types
			parseType = parseType.Elem()
			fieldVal = fieldVal.Elem()
		}

		if settings[0] == "either" {
			settings = settings[1:]

			if len(settings) < 2 {
				panic("either tag should have 2 args")
			}

			leaveBits, leaveRefs := 0, 0
			if settings[0] == "leave" {
				settings = settings[1:]

				if len(settings) < 3 {
					panic("either tag should have 2 args and leave tag should have 1 arg")
				}

				spl := strings.Split(settings[0], ",")
				settings = settings[1:]

				val, err := strconv.ParseUint(spl[0], 10, 10)
				if err != nil {
					panic("invalid argument for either leave bits")
				}
				// set how many free bits we need to have after either written
				leaveBits = int(val)

				if len(spl) > 1 {
					val, err = strconv.ParseUint(spl[1], 10, 10)
					if err != nil {
						panic("invalid argument for either leave refs")
					}
					// set how many free efs we need to have after either written
					leaveRefs = int(val)
				}
			}

			// we try first option, if it is overflows then we try second
			for x := 0; x < 2; x++ {
				builder := cell.BeginCell()
				if err := storeField([]string{settings[x]}, builder, structField, fieldVal, parseType); err != nil {
					return nil, fmt.Errorf("failed to serialize field %s to cell as either %d: %w", structField.Name, x, err)
				}

				// check if we have enough free bits
				if x == 0 && (int(root.BitsLeft())-int(builder.BitsUsed()+1) < leaveBits || int(root.RefsLeft())-int(builder.RefsUsed()) < leaveRefs) {
					// if not, then we try second option
					continue
				}

				if err := root.StoreUInt(uint64(x), 1); err != nil {
					return nil, fmt.Errorf("cannot store either bit: %w", err)
				}
				if err := root.StoreBuilder(builder); err != nil {
					return nil, fmt.Errorf("failed to concat builder of field %s to cell as either %d: %w", structField.Name, x, err)
				}

				continue next
			}

			return nil, fmt.Errorf("failed to serialize either field %s to cell: no valid options", structField.Name)
		}

		if err := storeField(settings, root, structField, fieldVal, parseType); err != nil {
			return nil, fmt.Errorf("failed to serialize field %s to cell: %w", structField.Name, err)
		}
	}

	return root.EndCell(), nil
}

func storeField(settings []string, root *cell.Builder, structField reflect.StructField, fieldVal reflect.Value, parseType reflect.Type) error {
	builder := root

	asRef := false
	if settings[0] == "^" {
		if cellType == parseType {
			// store cell as ref directly
			if err := root.StoreRef(fieldVal.Interface().(*cell.Cell)); err != nil {
				return fmt.Errorf("failed to store cell to ref for %s, err: %w", structField.Name, err)
			}
			return nil
		}

		asRef = true
		settings = settings[1:]
		builder = cell.BeginCell()
	}

	if structField.Type.Kind() == reflect.Interface {
		allowed := strings.Join(settings, "")
		if !strings.HasPrefix(allowed, "[") || !strings.HasSuffix(allowed, "]") {
			panic("corrupted allowed list tag of field " + structField.Name + ", should be [a,b,c], got " + allowed)
		}

		// cut brackets
		allowed = allowed[1 : len(allowed)-1]
		types := strings.Split(allowed, ",")

		t := fieldVal.Elem().Type()
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}

		found := false
		for _, typ := range types {
			if t.Name() == typ {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("unexpected data to serialize, not registered magic in tag for %s", t.String())
		}
		settings = settings[:0]
	}

	if len(settings) == 0 || settings[0] == "." {
		c, err := structStore(fieldVal, structField.Type.Name())
		if err != nil {
			return err
		}

		err = builder.StoreBuilder(c.ToBuilder())
		if err != nil {
			return fmt.Errorf("failed to store cell to builder for %s, err: %w", structField.Name, err)
		}
	} else if settings[0] == "##" {
		num, err := strconv.ParseUint(settings[1], 10, 64)
		if err != nil {
			// we panic, because its developer's issue, need to fix tag
			panic("corrupted num bits in ## tag")
		}

		switch {
		case num <= 64:
			switch parseType.Kind() {
			case reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8, reflect.Int:
				err = builder.StoreInt(fieldVal.Int(), uint(num))
				if err != nil {
					return fmt.Errorf("failed to store int %d, err: %w", num, err)
				}
			case reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8, reflect.Uint:
				err = builder.StoreUInt(fieldVal.Uint(), uint(num))
				if err != nil {
					return fmt.Errorf("failed to store int %d, err: %w", num, err)
				}
			default:
				if parseType == reflect.TypeOf(&big.Int{}) {
					err = builder.StoreBigInt(fieldVal.Interface().(*big.Int), uint(num))
					if err != nil {
						return fmt.Errorf("failed to store bigint %d, err: %w", num, err)
					}
				} else {
					panic("unexpected field type for tag ## - " + parseType.String())
				}
			}
		case num <= 256:
			err := builder.StoreBigInt(fieldVal.Interface().(*big.Int), uint(num))
			if err != nil {
				return fmt.Errorf("failed to store bigint %d, err: %w", num, err)
			}
		}
	} else if settings[0] == "addr" {
		err := builder.StoreAddr(fieldVal.Interface().(*address.Address))
		if err != nil {
			return fmt.Errorf("failed to store address, err: %w", err)
		}
	} else if settings[0] == "bool" {
		err := builder.StoreBoolBit(fieldVal.Bool())
		if err != nil {
			return fmt.Errorf("failed to store bool, err: %w", err)
		}
	} else if settings[0] == "bits" {
		num, err := strconv.Atoi(settings[1])
		if err != nil {
			// we panic, because its developer's issue, need to fix tag
			panic("corrupted num bits in bits tag")
		}

		err = builder.StoreSlice(fieldVal.Bytes(), uint(num))
		if err != nil {
			return fmt.Errorf("failed to store bits %d, err: %w", num, err)
		}
	} else if parseType == reflect.TypeOf(Magic{}) {
		var sz, base int
		if strings.HasPrefix(settings[0], "#") {
			base = 16
			sz = (len(settings[0]) - 1) * 4
		} else if strings.HasPrefix(settings[0], "$") {
			base = 2
			sz = len(settings[0]) - 1
		} else {
			panic("unknown magic value type in tag")
		}

		if sz > 64 {
			panic("too big magic value type in tag")
		}

		magic, err := strconv.ParseInt(settings[0][1:], base, 64)
		if err != nil {
			panic("corrupted magic value in tag")
		}

		err = builder.StoreUInt(uint64(magic), uint(sz))
		if err != nil {
			return fmt.Errorf("failed to store magic: %w", err)
		}
	} else if settings[0] == "dict" {
		var dict *cell.Dictionary

		settings = settings[1:]

		isInline := len(settings) > 0 && settings[0] == "inline"
		if isInline {
			settings = settings[1:]
		}

		if len(settings) < 3 || settings[1] != "->" {
			dict = fieldVal.Interface().(*cell.Dictionary)
		} else {
			var err error
			dict, err = prepareDict(fieldVal, settings, structField)
			if err != nil {
				return fmt.Errorf("failed to prepare dict for %s, err: %w", structField.Name, err)
			}
		}

		if isInline {
			dCell, err := dict.ToCell()
			if err != nil {
				return fmt.Errorf("failed to serialize inline dict to cell for %s, err: %w", structField.Name, err)
			}

			if dCell == nil {
				return fmt.Errorf("inline dict in field %s cannot be empty", structField.Name)
			}

			if err = builder.StoreBuilder(dCell.ToBuilder()); err != nil {
				return fmt.Errorf("failed to store inline dict for %s, err: %w", structField.Name, err)
			}
		} else {
			if err := builder.StoreDict(dict); err != nil {
				return fmt.Errorf("failed to store dict for %s, err: %w", structField.Name, err)
			}
		}
	} else if settings[0] == "var" {
		if settings[1] == "uint" {
			sz, err := strconv.Atoi(settings[2])
			if err != nil {
				panic(err.Error())
			}

			err = builder.StoreBigVarUInt(fieldVal.Interface().(*big.Int), uint(sz))
			if err != nil {
				return fmt.Errorf("failed to store var uint: %w", err)
			}
		} else {
			panic("var of type " + settings[1] + " is not supported")
		}
	} else {
		panic(fmt.Sprintf("cannot serialize field '%s' as tag '%s', use manual serialization", structField.Name, structField.Tag.Get("tlb")))
	}

	if asRef {
		err := root.StoreRef(builder.EndCell())
		if err != nil {
			return fmt.Errorf("failed to store cell to ref for %s, err: %w", structField.Name, err)
		}
	}

	return nil
}

var cellType = reflect.TypeOf(&cell.Cell{})

func structLoad(field reflect.Type, loader *cell.Slice, skipMagic, skipProofBranches bool) (reflect.Value, error) {
	if cellType == field {
		c, err := loader.ToCell()
		if err != nil {
			return reflect.Value{}, fmt.Errorf("failed to convert slice to cell: %w", err)
		}
		return reflect.ValueOf(c), nil
	}

	newTyp := field
	if newTyp.Kind() == reflect.Ptr {
		newTyp = newTyp.Elem()
	}

	nVal := reflect.New(newTyp)

	err := loadFromCell(nVal.Interface(), loader, skipProofBranches, skipMagic)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("failed to load from cell for %s, err: %w", field.Name(), err)
	}

	if field.Kind() != reflect.Ptr {
		nVal = nVal.Elem()
	}

	return nVal, nil
}

func structStore(field reflect.Value, name string) (*cell.Cell, error) {
	if field.Type() == cellType {
		if field.IsNil() {
			return cell.BeginCell().EndCell(), nil
		}
		return field.Interface().(*cell.Cell), nil
	}

	inf := field.Interface()

	c, err := ToCell(inf)
	if err != nil {
		return nil, fmt.Errorf("failed to store to cell for %s of type %s, err: %w", name, field.Type().String(), err)
	}
	return c, nil
}
