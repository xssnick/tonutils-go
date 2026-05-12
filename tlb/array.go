package tlb

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func loadArrayTag(parseType reflect.Type, elemSettings []string, loader *cell.Slice, skipProofBranches bool) (reflect.Value, error) {
	if parseType.Kind() != reflect.Slice && parseType.Kind() != reflect.Array {
		return reflect.Value{}, fmt.Errorf("array tag supports only slice or array, got %s", parseType.String())
	}
	if len(elemSettings) == 0 {
		return reflect.Value{}, fmt.Errorf("array tag requires element tag")
	}

	ln, err := loader.LoadUInt(8)
	if err != nil {
		return reflect.Value{}, fmt.Errorf("failed to load array len: %w", err)
	}

	hasRef, err := loader.LoadBoolBit()
	if err != nil {
		return reflect.Value{}, fmt.Errorf("failed to load array ref bit: %w", err)
	}

	if ln == 0 {
		if hasRef {
			return reflect.Value{}, fmt.Errorf("empty array should not have data ref")
		}
		return makeArrayValue(parseType, reflect.Value{}, 0)
	}
	if !hasRef {
		return reflect.Value{}, fmt.Errorf("array of len %d has no data ref", ln)
	}

	ref, err := loader.LoadRefCell()
	if err != nil {
		return reflect.Value{}, fmt.Errorf("failed to load array data ref: %w", err)
	}

	elemTag := strings.Join(elemSettings, " ")
	elemType := parseType.Elem()
	values := reflect.MakeSlice(reflect.SliceOf(elemType), 0, int(ln))

	for loaded := 0; loaded < int(ln); {
		chunk := ref.MustBeginParse()
		hasNext, err := chunk.LoadBoolBit()
		if err != nil {
			return reflect.Value{}, fmt.Errorf("failed to load array chunk next bit: %w", err)
		}

		elemLoader := chunk
		var next *cell.Cell
		if hasNext {
			if elemLoader.RefsNum() < 1 {
				return reflect.Value{}, fmt.Errorf("array chunk declares next ref, but has no refs")
			}
			next, err = elemLoader.PeekRefCellAt(elemLoader.RefsNum() - 1)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("failed to peek array next chunk ref: %w", err)
			}
			if !elemLoader.SkipLast(0, 1) {
				return reflect.Value{}, fmt.Errorf("failed to isolate array chunk payload")
			}
		}

		for loaded < int(ln) && (elemLoader.BitsLeft() > 0 || elemLoader.RefsNum() > 0) {
			beforeBits, beforeRefs := elemLoader.BitsLeft(), elemLoader.RefsNum()
			elem, err := loadArrayElement(elemType, elemTag, elemLoader, skipProofBranches)
			if err != nil {
				return reflect.Value{}, fmt.Errorf("failed to load array element %d: %w", loaded, err)
			}
			values = reflect.Append(values, elem)
			loaded++

			if elemLoader.BitsLeft() == beforeBits && elemLoader.RefsNum() == beforeRefs {
				return reflect.Value{}, fmt.Errorf("array element %d did not consume data", loaded-1)
			}
		}

		if loaded < int(ln) && elemLoader.BitsLeft() == 0 && elemLoader.RefsNum() == 0 && !hasNext {
			for loaded < int(ln) {
				elem, err := loadArrayElement(elemType, elemTag, elemLoader, skipProofBranches)
				if err != nil {
					return reflect.Value{}, fmt.Errorf("array ended after %d elements, expected %d", loaded, ln)
				}
				values = reflect.Append(values, elem)
				loaded++
			}
		}

		if elemLoader.BitsLeft() != 0 || elemLoader.RefsNum() != 0 {
			return reflect.Value{}, fmt.Errorf("array chunk has trailing data: %d bits, %d refs", elemLoader.BitsLeft(), elemLoader.RefsNum())
		}

		if loaded == int(ln) {
			if hasNext {
				return reflect.Value{}, fmt.Errorf("array has next chunk after declared len %d", ln)
			}
			break
		}
		if !hasNext {
			return reflect.Value{}, fmt.Errorf("array ended after %d elements, expected %d", loaded, ln)
		}
		ref = next
	}

	return makeArrayValue(parseType, values, int(ln))
}

func storeArrayTag(root *cell.Builder, fieldVal reflect.Value, parseType reflect.Type, elemSettings []string) error {
	if parseType.Kind() != reflect.Slice && parseType.Kind() != reflect.Array {
		return fmt.Errorf("array tag supports only slice or array, got %s", parseType.String())
	}
	if len(elemSettings) == 0 {
		return fmt.Errorf("array tag requires element tag")
	}

	ln := fieldVal.Len()
	if ln > 255 {
		return fmt.Errorf("array len %d exceeds max 255", ln)
	}

	if err := root.StoreUInt(uint64(ln), 8); err != nil {
		return fmt.Errorf("failed to store array len: %w", err)
	}
	if ln == 0 {
		if err := root.StoreBoolBit(false); err != nil {
			return fmt.Errorf("failed to store empty array ref bit: %w", err)
		}
		return nil
	}

	elemTag := strings.Join(elemSettings, " ")
	elemType := parseType.Elem()
	elems := make([]*cell.Cell, ln)
	for i := 0; i < ln; i++ {
		elem, err := storeArrayElement(fieldVal.Index(i), elemType, elemTag)
		if err != nil {
			return fmt.Errorf("failed to store array element %d: %w", i, err)
		}
		elems[i] = elem
	}

	first, err := buildArrayChunks(elems)
	if err != nil {
		return err
	}

	if err = root.StoreBoolBit(true); err != nil {
		return fmt.Errorf("failed to store array ref bit: %w", err)
	}
	if err = root.StoreRef(first); err != nil {
		return fmt.Errorf("failed to store array data ref: %w", err)
	}
	return nil
}

func makeArrayValue(parseType reflect.Type, values reflect.Value, ln int) (reflect.Value, error) {
	if parseType.Kind() == reflect.Slice {
		if values.IsValid() {
			if values.Type() != parseType {
				values = values.Convert(parseType)
			}
			return values, nil
		}
		return reflect.MakeSlice(parseType, 0, 0), nil
	}

	if parseType.Len() != ln {
		return reflect.Value{}, fmt.Errorf("array len mismatch, got %d want %d", ln, parseType.Len())
	}
	out := reflect.New(parseType).Elem()
	for i := 0; i < ln; i++ {
		out.Index(i).Set(values.Index(i))
	}
	return out, nil
}

func storeArrayElement(elem reflect.Value, elemType reflect.Type, elemTag string) (*cell.Cell, error) {
	if elem.Type() != elemType {
		if elem.Type().ConvertibleTo(elemType) {
			elem = elem.Convert(elemType)
		} else {
			return nil, fmt.Errorf("array element has type %s, want %s", elem.Type(), elemType)
		}
	}

	boxType := arrayElementBoxType(elemType, elemTag)
	box := reflect.New(boxType).Elem()
	box.Field(0).Set(elem)

	return ToCell(box.Interface())
}

func loadArrayElement(elemType reflect.Type, elemTag string, loader *cell.Slice, skipProofBranches bool) (reflect.Value, error) {
	boxType := arrayElementBoxType(elemType, elemTag)
	box := reflect.New(boxType)
	if err := loadFromCell(box.Interface(), loader, skipProofBranches, false); err != nil {
		return reflect.Value{}, err
	}
	return box.Elem().Field(0), nil
}

func arrayElementBoxType(elemType reflect.Type, elemTag string) reflect.Type {
	return reflect.StructOf([]reflect.StructField{{
		Name: "Value",
		Type: elemType,
		Tag:  reflect.StructTag(fmt.Sprintf("tlb:%q", elemTag)),
	}})
}

func buildArrayChunks(elems []*cell.Cell) (*cell.Cell, error) {
	idx := len(elems)
	var next *cell.Cell

	for idx > 0 {
		start := idx
		bits := uint(1)
		refs := uint(0)
		if next != nil {
			refs = 1
		}

		for start > 0 {
			elem := elems[start-1]
			nextBits := bits + elem.BitsSize()
			nextRefs := refs + elem.RefsNum()
			if nextBits >= 1024 || nextRefs > 4 {
				break
			}
			start--
			bits = nextBits
			refs = nextRefs
		}

		if start == idx {
			return nil, fmt.Errorf("array element %d does not fit into chunk", idx-1)
		}

		chunk := cell.BeginCell()
		if err := chunk.StoreBoolBit(next != nil); err != nil {
			return nil, fmt.Errorf("failed to store array chunk next bit: %w", err)
		}
		for i := start; i < idx; i++ {
			if err := chunk.StoreBuilder(elems[i].ToBuilder()); err != nil {
				return nil, fmt.Errorf("failed to store array element %d into chunk: %w", i, err)
			}
		}
		if next != nil {
			if err := chunk.StoreRef(next); err != nil {
				return nil, fmt.Errorf("failed to store array next chunk ref: %w", err)
			}
		}

		next = chunk.EndCell()
		idx = start
	}

	return next, nil
}
