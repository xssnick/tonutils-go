//go:build !tinygo

package tlb

import (
	"fmt"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/big"
	"reflect"
	"strconv"
	"strings"
)

func prepareMap(settings []string, structField reflect.StructField, dict *cell.Dictionary, sz uint64, skipProofBranches bool) (reflect.Value, error) {
	if structField.Type.Kind() != reflect.Map {
		return reflect.Value{}, fmt.Errorf("can map dictionary only into the map")
	}
	if structField.Type.Key() != reflect.TypeOf("") {
		return reflect.Value{}, fmt.Errorf("can map dictionary only into the map with string key")
	}

	mappedDict := reflect.MakeMapWithSize(reflect.MapOf(structField.Type.Key(), structField.Type.Elem()), 0)
	dictVT := reflect.StructOf([]reflect.StructField{{
		Name: "Value",
		Type: structField.Type.Elem(),
		Tag:  reflect.StructTag(fmt.Sprintf("tlb:%q", strings.Join(settings[3:], " "))),
	}})

	values, err := dict.LoadAll(skipProofBranches)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("failed to load dict values for %v: %w", structField.Name, err)
	}

	for _, kv := range values {
		dictK, err := kv.Key.LoadBigUInt(uint(sz))
		if err != nil {
			return reflect.Value{}, fmt.Errorf("failed to load dict key for %s: %w", structField.Name, err)
		}

		dictV := reflect.New(dictVT).Interface()
		if err = loadFromCell(dictV, kv.Value, skipProofBranches, false); err != nil {
			return reflect.Value{}, fmt.Errorf("failed to parse dict value for %v: %w", structField.Name, err)
		}

		mappedDict.SetMapIndex(reflect.ValueOf(dictK.String()), reflect.ValueOf(dictV).Elem().Field(0))
	}

	return mappedDict, nil
}

func prepareDict(fieldVal reflect.Value, settings []string, structField reflect.StructField) (*cell.Dictionary, error) {
	if fieldVal.Kind() != reflect.Map {
		return nil, fmt.Errorf("want to create dictionary from map, but instead got %s type", fieldVal.Type())
	}
	if fieldVal.Type().Key() != reflect.TypeOf("") {
		return nil, fmt.Errorf("map key should be string, but instead got %s type", fieldVal.Type().Key())
	}

	sz, err := strconv.ParseUint(settings[0], 10, 64)
	if err != nil {
		panic(fmt.Sprintf("cannot deserialize field '%s' as dict, bad size '%s'", structField.Name, settings[0]))
	}

	dict := cell.NewDict(uint(sz))

	for _, mapK := range fieldVal.MapKeys() {
		mapKI, ok := big.NewInt(0).SetString(mapK.Interface().(string), 10)
		if !ok {
			return nil, fmt.Errorf("cannot parse '%s' map key to big int of '%s' field", mapK.Interface().(string), structField.Name)
		}

		mapKB := cell.BeginCell()
		if err := mapKB.StoreBigInt(mapKI, uint(sz)); err != nil {
			return nil, fmt.Errorf("store big int of size %d to %s field", sz, structField.Name)
		}

		mapV := fieldVal.MapIndex(mapK)

		cellVT := reflect.StructOf([]reflect.StructField{{
			Name: "Value",
			Type: mapV.Type(),
			Tag:  reflect.StructTag(fmt.Sprintf("tlb:%q", strings.Join(settings[2:], " "))),
		}})
		cellV := reflect.New(cellVT).Elem()
		cellV.Field(0).Set(mapV)

		mapVC, err := ToCell(cellV.Interface())
		if err != nil {
			return nil, fmt.Errorf("creating cell for dict value of '%s' field: %w", structField.Name, err)
		}

		if err := dict.Set(mapKB.EndCell(), mapVC); err != nil {
			return nil, fmt.Errorf("set dict key/value on '%s' field: %w", structField.Name, err)
		}
	}

	return dict, nil
}
