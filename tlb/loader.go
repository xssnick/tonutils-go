package tlb

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type Magic struct{}

type manualLoader interface {
	LoadFromCell(loader *cell.Slice) error
}

// LoadFromCell automatically parses cell based on struct tags
// ## N - means integer with N bits, if size <= 64 it loads to uint of any size, if > 64 it loads to *big.Int
// ^ - loads ref and calls recursively, if field type is *cell.Cell, it loads without parsing
// . - calls recursively to continue load from current loader (inner struct)
// [^]dict N [-> array [^]] - loads dictionary with key size N, transformation '->' can be applied to convert dict to array, example: 'dict 256 -> array ^' will give you array of deserialized refs (^) of values
// bits N - loads bit slice N len to []byte
// bool - loads 1 bit boolean
// addr - loads ton address
// maybe - reads 1 bit, and loads rest if its 1, can be used in combination with others only
// either X Y - reads 1 bit, if its 0 - loads X, if 1 - loads Y
// Some tags can be combined, for example "dict 256", "maybe ^"
// Magic can be used to load first bits and check struct type, in tag can be specified magic number itself, in [#]HEX or [$]BIN format
// Example:
// _ Magic `tlb:"#deadbeef"
// _ Magic `tlb:"$1101"
func LoadFromCell(v any, loader *cell.Slice) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("v should be a pointer and not nil")
	}
	rv = rv.Elem()

	for i := 0; i < rv.NumField(); i++ {
		field := rv.Type().Field(i)
		tag := strings.TrimSpace(field.Tag.Get("tlb"))
		settings := strings.Split(tag, " ")

		if len(settings) == 0 {
			continue
		}

		if settings[0] == "maybe" {
			has, err := loader.LoadBoolBit()
			if err != nil {
				return fmt.Errorf("failed to load maybe for %s, err: %w", field.Name, err)
			}

			if !has {
				continue
			}
			settings = settings[1:]
		}

		if settings[0] == "either" {
			if len(settings) < 3 {
				panic("either tag should have 2 args")
			}
			isSecond, err := loader.LoadBoolBit()
			if err != nil {
				return fmt.Errorf("failed to load maybe for %s, err: %w", field.Name, err)
			}

			if !isSecond {
				settings = []string{settings[1]}
			} else {
				settings = []string{settings[2]}
			}
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
				switch field.Type.Kind() {
				case reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8, reflect.Int:
					x, err = loader.LoadInt(uint(num))
					if err != nil {
						return fmt.Errorf("failed to load int %d, err: %w", num, err)
					}
				default:
					x, err = loader.LoadUInt(uint(num))
					if err != nil {
						return fmt.Errorf("failed to load uint %d, err: %w", num, err)
					}
				}

				rv.Field(i).Set(reflect.ValueOf(x).Convert(field.Type))
				continue
			case num <= 256:
				x, err := loader.LoadBigInt(uint(num))
				if err != nil {
					return fmt.Errorf("failed to load bigint %d, err: %w", num, err)
				}

				rv.Field(i).Set(reflect.ValueOf(x))
				continue
			}
		} else if settings[0] == "addr" {
			x, err := loader.LoadAddr()
			if err != nil {
				return fmt.Errorf("failed to load address, err: %w", err)
			}

			rv.Field(i).Set(reflect.ValueOf(x))
			continue
		} else if settings[0] == "bool" {
			x, err := loader.LoadBoolBit()
			if err != nil {
				return fmt.Errorf("failed to load bool, err: %w", err)
			}

			rv.Field(i).Set(reflect.ValueOf(x))
			continue
		} else if settings[0] == "bits" {
			num, err := strconv.Atoi(settings[1])
			if err != nil {
				// we panic, because its developer's issue, need to fix tag
				panic("corrupted num bits in bits tag")
			}

			x, err := loader.LoadSlice(uint(num))
			if err != nil {
				return fmt.Errorf("failed to load uint %d, err: %w", num, err)
			}

			rv.Field(i).Set(reflect.ValueOf(x))
			continue
		} else if settings[0] == "^" || settings[0] == "." {
			next := loader

			if settings[0] == "^" {
				ref, err := loader.LoadRef()
				if err != nil {
					return fmt.Errorf("failed to load ref for %s, err: %w", field.Name, err)
				}
				next = ref
			}

			switch field.Type {
			case reflect.TypeOf(&cell.Cell{}):
				c, err := next.ToCell()
				if err != nil {
					return fmt.Errorf("failed to convert ref to cell for %s, err: %w", field.Name, err)
				}

				rv.Field(i).Set(reflect.ValueOf(c))
				continue
			default:
				nVal, err := structLoad(field.Type, next)
				if err != nil {
					return err
				}

				rv.Field(i).Set(nVal)
				continue
			}
		} else if field.Type == reflect.TypeOf(Magic{}) {
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

			ldMagic, err := loader.LoadUInt(uint(sz))
			if err != nil {
				return fmt.Errorf("failed to load magic: %w", err)
			}

			if ldMagic != uint64(magic) {
				return fmt.Errorf("magic is not correct")
			}
			continue
		} else if settings[0] == "dict" {
			sz, err := strconv.ParseUint(settings[1], 10, 64)
			if err != nil {
				panic(fmt.Sprintf("cannot deserialize field '%s' as dict, bad size '%s'", field.Name, settings[1]))
			}

			dict, err := loader.LoadDict(uint(sz))
			if err != nil {
				return fmt.Errorf("failed to load ref for %s, err: %w", field.Name, err)
			}

			if len(settings) >= 4 {
				// transformation
				if settings[2] == "->" {
					isRef := false
					if len(settings) >= 5 {
						if settings[4] == "^" {
							isRef = true
						}
					}

					switch settings[3] {
					case "array":
						arr := rv.Field(i)
						for _, kv := range dict.All() {
							ld := kv.Value.BeginParse()
							if isRef {
								ld, err = ld.LoadRef()
								if err != nil {
									return fmt.Errorf("failed to load ref in dict transform: %w", err)
								}
							}

							nVal, err := structLoad(field.Type.Elem(), ld)
							if err != nil {
								return fmt.Errorf("failed to load struct in dict transform: %w", err)
							}

							arr = reflect.Append(arr, nVal)
						}
						rv.Field(i).Set(arr)
						continue
					default:
						panic("transformation to this type is not supported")
					}
				}
			}

			rv.Field(i).Set(reflect.ValueOf(dict))
			continue
		}

		panic(fmt.Sprintf("cannot deserialize field '%s' as tag '%s'", field.Name, tag))
	}

	return nil
}

func structLoad(field reflect.Type, loader *cell.Slice) (reflect.Value, error) {
	newTyp := field
	if newTyp.Kind() == reflect.Ptr {
		newTyp = newTyp.Elem()
	}

	nVal := reflect.New(newTyp)
	inf := nVal.Interface()

	if ld, ok := inf.(manualLoader); ok {
		err := ld.LoadFromCell(loader)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("failed to load from cell for %s, using manual loader, err: %w", field.Name(), err)
		}
	} else {
		err := LoadFromCell(nVal.Interface(), loader)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("failed to load from cell for %s, err: %w", field.Name(), err)
		}
	}

	if field.Kind() != reflect.Ptr {
		nVal = nVal.Elem()
	}

	return nVal, nil
}
