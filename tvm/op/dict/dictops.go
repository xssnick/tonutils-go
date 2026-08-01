package dict

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/op/helpers"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type dictKeyKind uint8

const (
	dictKeySlice dictKeyKind = iota
	dictKeySignedInt
	dictKeyUnsignedInt
)

type dictValueVariant struct {
	offset uint16
	kind   dictKeyKind
	byRef  bool
}

type dictScalarVariant struct {
	offset uint16
	kind   dictKeyKind
}

type dictNearVariant struct {
	offset    uint16
	kind      dictKeyKind
	fetchNext bool
	allowEq   bool
}

type directDictKey struct {
	kind    dictKeyKind
	slice   *cell.Slice
	integer *big.Int
}

type directSubdictPrefix struct {
	kind    dictKeyKind
	bits    uint
	slice   *cell.Slice
	integer *big.Int
}

func (k directDictKey) loadValueInto(dict *cell.Dictionary, value *cell.Slice) error {
	if k.kind == dictKeySlice {
		return dict.LoadValueBySliceKeyInto(k.slice, value)
	}
	return dict.LoadValueByIntKeyInto(k.integer, value)
}

func (k directDictKey) setBuilderWithMode(dict *cell.Dictionary, value *cell.Builder, mode cell.DictSetMode) (bool, error) {
	if k.kind == dictKeySlice {
		return dict.SetBuilderBySliceKeyWithMode(k.slice, value, mode)
	}
	return dict.SetBuilderByIntKeyWithMode(k.integer, value, mode)
}

func (k directDictKey) loadValueAndSetBuilderWithMode(dict *cell.Dictionary, value *cell.Builder, mode cell.DictSetMode) (*cell.Slice, bool, error) {
	if k.kind == dictKeySlice {
		return dict.LoadValueAndSetBuilderBySliceKeyWithMode(k.slice, value, mode)
	}
	return dict.LoadValueAndSetBuilderByIntKeyWithMode(k.integer, value, mode)
}

func (k directDictKey) loadValueAndDelete(dict *cell.Dictionary) (*cell.Slice, error) {
	if k.kind == dictKeySlice {
		return dict.LoadValueAndDeleteBySliceKey(k.slice)
	}
	return dict.LoadValueAndDeleteByIntKey(k.integer)
}

func (k directDictKey) delete(dict *cell.Dictionary) (bool, error) {
	if k.kind == dictKeySlice {
		return dict.DeleteBySliceKey(k.slice)
	}
	return dict.DeleteByIntKey(k.integer)
}

func (k directDictKey) lookupNearest(dict *cell.Dictionary, fetchNext, allowEq, invertFirst bool) (*cell.Cell, *cell.Slice, error) {
	if k.kind == dictKeySlice {
		return dict.LookupNearestKeyBySlice(k.slice, fetchNext, allowEq, invertFirst)
	}
	return dict.LookupNearestKeyByInt(k.integer, fetchNext, allowEq, invertFirst)
}

func (p directSubdictPrefix) cut(dict *cell.Dictionary, removePrefix bool) (bool, error) {
	if p.kind == dictKeySlice {
		var prefix cell.Slice
		if err := p.slice.PreloadSubsliceInto(&prefix, p.bits, 0); err != nil {
			return false, cellUnderflowError(err)
		}
		return dict.CutPrefixSubdictBySlice(&prefix, removePrefix)
	}
	return dict.CutPrefixSubdictByInt(p.integer, p.bits, removePrefix)
}

func checkDictStackDepth(state *vm.State, depth int) error {
	if state.Stack.Len() < depth {
		return vmerr.Error(vmerr.CodeStackUnderflow)
	}
	return nil
}

var dictValueVariants = []dictValueVariant{
	{offset: 0, kind: dictKeySlice, byRef: false},
	{offset: 1, kind: dictKeySlice, byRef: true},
	{offset: 2, kind: dictKeySignedInt, byRef: false},
	{offset: 3, kind: dictKeySignedInt, byRef: true},
	{offset: 4, kind: dictKeyUnsignedInt, byRef: false},
	{offset: 5, kind: dictKeyUnsignedInt, byRef: true},
}

var dictScalarVariants = []dictScalarVariant{
	{offset: 0, kind: dictKeySlice},
	{offset: 1, kind: dictKeySignedInt},
	{offset: 2, kind: dictKeyUnsignedInt},
}

var dictNearVariants = []dictNearVariant{
	{offset: 0, kind: dictKeySlice, fetchNext: true, allowEq: false},
	{offset: 1, kind: dictKeySlice, fetchNext: true, allowEq: true},
	{offset: 2, kind: dictKeySlice, fetchNext: false, allowEq: false},
	{offset: 3, kind: dictKeySlice, fetchNext: false, allowEq: true},
	{offset: 4, kind: dictKeySignedInt, fetchNext: true, allowEq: false},
	{offset: 5, kind: dictKeySignedInt, fetchNext: true, allowEq: true},
	{offset: 6, kind: dictKeySignedInt, fetchNext: false, allowEq: false},
	{offset: 7, kind: dictKeySignedInt, fetchNext: false, allowEq: true},
	{offset: 8, kind: dictKeyUnsignedInt, fetchNext: true, allowEq: false},
	{offset: 9, kind: dictKeyUnsignedInt, fetchNext: true, allowEq: true},
	{offset: 10, kind: dictKeyUnsignedInt, fetchNext: false, allowEq: false},
	{offset: 11, kind: dictKeyUnsignedInt, fetchNext: false, allowEq: true},
}

func init() {
	registerSimpleExact(0xF400, "STDICT", execStoreDict)
	registerSimpleExact(0xF401, "SKIPDICT", execSkipDict)
	registerSimpleExact(0xF402, "LDDICTS", execLoadDictSlice(false, false))
	registerSimpleExact(0xF403, "PLDDICTS", execLoadDictSlice(true, false))
	registerSimpleExact(0xF404, "LDDICT", execLoadDict(false, false))
	registerSimpleExact(0xF405, "PLDDICT", execLoadDict(true, false))
	registerSimpleExact(0xF406, "LDDICTQ", execLoadDict(false, true))
	registerSimpleExact(0xF407, "PLDDICTQ", execLoadDict(true, true))

	registerDictValueFamily(0xF40A, "GET", execDictGet)
	registerDictValueFamily(0xF412, "SET", execDictSet(cell.DictSetModeSet))
	registerDictValueFamily(0xF41A, "SETGET", execDictSetGet(cell.DictSetModeSet))
	registerDictValueFamily(0xF422, "REPLACE", execDictSet(cell.DictSetModeReplace))
	registerDictValueFamily(0xF42A, "REPLACEGET", execDictSetGet(cell.DictSetModeReplace))
	registerDictValueFamily(0xF432, "ADD", execDictSet(cell.DictSetModeAdd))
	registerDictValueFamily(0xF43A, "ADDGET", execDictSetGet(cell.DictSetModeAdd))
	registerDictValueFamily(0xF462, "DELGET", execDictDeleteGet)
	registerDictValueFamily(0xF482, "MIN", execDictMinMax(false, false))
	registerDictValueFamily(0xF48A, "MAX", execDictMinMax(true, false))
	registerDictValueFamily(0xF492, "REMMIN", execDictMinMax(false, true))
	registerDictValueFamily(0xF49A, "REMMAX", execDictMinMax(true, true))

	registerDictScalarFamily(0xF441, "SETB", execDictSetBuilder(cell.DictSetModeSet))
	registerDictScalarFamily(0xF445, "SETGETB", execDictSetGetBuilder(cell.DictSetModeSet))
	registerDictScalarFamily(0xF449, "REPLACEB", execDictSetBuilder(cell.DictSetModeReplace))
	registerDictScalarFamily(0xF44D, "REPLACEGETB", execDictSetGetBuilder(cell.DictSetModeReplace))
	registerDictScalarFamily(0xF451, "ADDB", execDictSetBuilder(cell.DictSetModeAdd))
	registerDictScalarFamily(0xF455, "ADDGETB", execDictSetGetBuilder(cell.DictSetModeAdd))
	registerDictScalarFamily(0xF459, "DEL", execDictDelete)
	registerDictScalarFamily(0xF469, "GETOPTREF", execDictGetOptRef)
	registerDictScalarFamily(0xF46D, "SETGETOPTREF", execDictSetGetOptRef)
	registerDictScalarFamily(0xF4B1, "SUBDICTGET", execSubdict(false))
	registerDictScalarFamily(0xF4B5, "SUBDICTRPGET", execSubdict(true))

	registerSimpleExact(0xF470, "PFXDICTSET", execPfxDictSet(cell.DictSetModeSet))
	registerSimpleExact(0xF471, "PFXDICTREPLACE", execPfxDictSet(cell.DictSetModeReplace))
	registerSimpleExact(0xF472, "PFXDICTADD", execPfxDictSet(cell.DictSetModeAdd))
	registerSimpleExact(0xF473, "PFXDICTDEL", execPfxDictDelete)
	registerDictNearFamily(0xF474, execDictGetNear)
	registerSimpleExact(0xF4A8, "PFXDICTGETQ", execPfxDictGet(0))
	registerSimpleExact(0xF4A9, "PFXDICTGET", execPfxDictGet(1))
	registerSimpleExact(0xF4AA, "PFXDICTGETJMP", execPfxDictGet(2))
	registerSimpleExact(0xF4AB, "PFXDICTGETEXEC", execPfxDictGet(3))

	registerSimpleExact(0xF4A0, "DICTIGETJMP", execDictGetExec(false, false, false))
	registerSimpleExact(0xF4A1, "DICTUGETJMP", execDictGetExec(true, false, false))
	registerSimpleExact(0xF4A2, "DICTIGETEXEC", execDictGetExec(false, true, false))
	registerSimpleExact(0xF4A3, "DICTUGETEXEC", execDictGetExec(true, true, false))
	vm.List = append(vm.List, func() vm.OP { return DICTIGETJMPZ() })
	registerSimpleExact(0xF4BD, "DICTUGETJMPZ", execDictGetExec(true, false, true))
	registerSimpleExact(0xF4BE, "DICTIGETEXECZ", execDictGetExec(false, true, true))
	registerSimpleExact(0xF4BF, "DICTUGETEXECZ", execDictGetExec(true, true, true))

	vm.List = append(vm.List, func() vm.OP { return PFXDICTSWITCH(nil) })
}

func registerSimpleExact(opcode uint16, name string, action func(*vm.State) error) {
	op := opcode
	vm.List = append(vm.List, func() vm.OP {
		return &helpers.SimpleOP{
			Action:    action,
			BitPrefix: helpers.UIntPrefix(uint64(op), 16),
			Name:      name,
		}
	})
}

type OpPFXDICTSWITCH struct {
	helpers.Prefixed
	root *cell.Cell
	bits uint64
}

func PFXDICTSWITCH(root *cell.Cell, bits ...uint64) *OpPFXDICTSWITCH {
	keyBits := uint64(0)
	if len(bits) > 0 {
		keyBits = bits[0]
	}
	return &OpPFXDICTSWITCH{
		Prefixed: helpers.SinglePrefixed(helpers.SlicePrefix(13, []byte{0xF4, 0xAC})),
		root:     root,
		bits:     keyBits,
	}
}

func (op *OpPFXDICTSWITCH) Deserialize(code *cell.Slice) error {
	if err := code.SkipBits(13); err != nil {
		return err
	}
	hasRoot, err := code.LoadBoolBit()
	if err != nil {
		return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
	}
	rootCell, err := code.PeekRefCell()
	if err != nil {
		return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
	}
	if err = code.SkipBitsAndRefs(0, 1); err != nil {
		return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
	}
	bits, err := code.LoadUInt(10)
	if err != nil {
		return vmerr.Error(vmerr.CodeInvalidOpcode, err.Error())
	}
	op.bits = bits
	if hasRoot {
		op.root = rootCell
	} else {
		op.root = nil
	}
	return nil
}

func (op *OpPFXDICTSWITCH) Serialize() *cell.Builder {
	if op.root == nil {
		panic("PFXDICTSWITCH requires dictionary ref")
	}
	return cell.BeginCell().
		MustStoreSlice([]byte{0xF4, 0xAC}, 13).
		MustStoreMaybeRef(op.root).
		MustStoreUInt(op.bits, 10)
}

func (op *OpPFXDICTSWITCH) SerializeText() string {
	if op.root == nil {
		return fmt.Sprintf("PFXDICTSWITCH %d (<nil>)", op.bits)
	}
	return fmt.Sprintf("PFXDICTSWITCH %d (%s)", op.bits, op.root.Dump())
}

func (op *OpPFXDICTSWITCH) InstructionBits() int64 {
	return 24
}

func (op *OpPFXDICTSWITCH) Interpret(state *vm.State) error {
	input, err := state.Stack.PopSlice()
	if err != nil {
		return err
	}

	dict := newReadOnlyPrefixDictWithTrace(op.root, uint(op.bits), state.Cells.Trace())
	var value cell.Slice
	matched, err := dict.LookupPrefixBySliceInto(input, &value)
	if gasErr := state.Cells.PendingError(); gasErr != nil {
		return gasErr
	}
	if errors.Is(err, cell.ErrNoSuchKeyInDict) {
		return state.Stack.PushOwnedSlice(input)
	}
	if err != nil {
		return mapDictError(err)
	}

	prefixSlice, err := input.FetchSubslice(matched, 0)
	if err != nil {
		return cellUnderflowError(err)
	}
	if err = state.Stack.PushOwnedSlice(prefixSlice); err != nil {
		return cellUnderflowError(err)
	}
	if err = state.Stack.PushOwnedSlice(input); err != nil {
		return err
	}
	return state.Jump(newOrdContinuation(&value, state.CP))
}

func registerDictValueFamily(base uint16, suffix string, factory func(dictValueVariant) func(*vm.State) error) {
	for _, variant := range dictValueVariants {
		variant := variant
		registerSimpleExact(base+variant.offset, dictValueName(variant, suffix), factory(variant))
	}
}

func registerDictScalarFamily(base uint16, suffix string, factory func(dictScalarVariant) func(*vm.State) error) {
	for _, variant := range dictScalarVariants {
		variant := variant
		registerSimpleExact(base+variant.offset, dictScalarName(variant, suffix), factory(variant))
	}
}

func registerDictNearFamily(base uint16, factory func(dictNearVariant) func(*vm.State) error) {
	for _, variant := range dictNearVariants {
		variant := variant
		registerSimpleExact(base+variant.offset, dictNearName(variant), factory(variant))
	}
}

func dictValueName(variant dictValueVariant, suffix string) string {
	prefix := "DICT"
	switch variant.kind {
	case dictKeySignedInt:
		prefix += "I"
	case dictKeyUnsignedInt:
		prefix += "U"
	}
	prefix += suffix
	if variant.byRef {
		prefix += "REF"
	}
	return prefix
}

func dictScalarName(variant dictScalarVariant, suffix string) string {
	prefix := "DICT"
	switch variant.kind {
	case dictKeySignedInt:
		prefix += "I"
	case dictKeyUnsignedInt:
		prefix += "U"
	}
	return prefix + suffix
}

func dictNearName(variant dictNearVariant) string {
	name := "DICT"
	switch variant.kind {
	case dictKeySignedInt:
		name += "I"
	case dictKeyUnsignedInt:
		name += "U"
	}
	name += "GET"
	if variant.fetchNext {
		name += "NEXT"
	} else {
		name += "PREV"
	}
	if variant.allowEq {
		name += "EQ"
	}
	return name
}

func execStoreDict(state *vm.State) error {
	if err := checkDictStackDepth(state, 2); err != nil {
		return err
	}

	builder, err := state.Stack.PopBuilder()
	if err != nil {
		return err
	}
	dict, err := state.Stack.PopMaybeCell()
	if err != nil {
		return err
	}
	if err = builder.StoreMaybeRefUncheckedDepth(dict); err != nil {
		return cellOverflowError(err)
	}
	return state.Stack.PushOwnedBuilder(builder)
}

func execSkipDict(state *vm.State) error {
	sl, err := state.Stack.PopSlice()
	if err != nil {
		return err
	}
	refs := dictNonEmpty(sl)
	if refs < 0 {
		return vmerr.Error(vmerr.CodeCellUnderflow, "invalid dictionary serialization")
	}
	if err = sl.SkipBitsAndRefs(1, refs); err != nil {
		return cellUnderflowError(err)
	}
	return state.Stack.PushOwnedSlice(sl)
}

func execLoadDictSlice(preload bool, quiet bool) func(*vm.State) error {
	return func(state *vm.State) error {
		sl, err := state.Stack.PopSlice()
		if err != nil {
			return err
		}
		refs := dictNonEmpty(sl)
		if refs < 0 {
			if !quiet {
				return vmerr.Error(vmerr.CodeCellUnderflow, "invalid dictionary serialization")
			}
			if !preload {
				if err = state.Stack.PushOwnedSlice(sl); err != nil {
					return err
				}
			}
			return state.Stack.PushBool(false)
		}

		var dictSlice *cell.Slice
		if preload {
			dictSlice, err = sl.PreloadSubslice(1, refs)
		} else {
			dictSlice, err = sl.FetchSubslice(1, refs)
		}
		if err != nil {
			return cellUnderflowError(err)
		}

		if err = state.Stack.PushOwnedSlice(dictSlice); err != nil {
			return err
		}
		if !preload {
			if err = state.Stack.PushOwnedSlice(sl); err != nil {
				return err
			}
		}
		if quiet {
			return state.Stack.PushBool(true)
		}
		return nil
	}
}

func execLoadDict(preload bool, quiet bool) func(*vm.State) error {
	return func(state *vm.State) error {
		sl, err := state.Stack.PopSlice()
		if err != nil {
			return err
		}
		refs := dictNonEmpty(sl)
		if refs < 0 {
			if !quiet {
				return vmerr.Error(vmerr.CodeCellUnderflow, "invalid dictionary serialization")
			}
			if !preload {
				if err = state.Stack.PushOwnedSlice(sl); err != nil {
					return err
				}
			}
			return state.Stack.PushBool(false)
		}

		var dictRoot *cell.Cell
		if refs > 0 {
			dictRoot, err = sl.PeekRefCell()
			if err != nil {
				return cellUnderflowError(err)
			}
		}
		if err = pushMaybeCell(state.Stack, dictRoot); err != nil {
			return err
		}
		if !preload {
			if err = sl.SkipBitsAndRefs(1, refs); err != nil {
				return cellUnderflowError(err)
			}
			if err = state.Stack.PushOwnedSlice(sl); err != nil {
				return err
			}
		}
		if quiet {
			return state.Stack.PushBool(true)
		}
		return nil
	}
}

func execDictGet(variant dictValueVariant) func(*vm.State) error {
	return func(state *vm.State) error {
		if err := checkDictStackDepth(state, 3); err != nil {
			return err
		}

		keyBits, root, err := popDictRootAndLen(state)
		if err != nil {
			return err
		}
		key, ok, err := popDirectDictKey(state, keyBits, variant.kind, false)
		if err != nil {
			return err
		}
		if !ok {
			return state.Stack.PushBool(false)
		}

		dict := newReadOnlyTracedDict(root, keyBits, state)
		var value cell.Slice
		if err = key.loadValueInto(dict, &value); err != nil {
			if errors.Is(err, cell.ErrNoSuchKeyInDict) {
				return state.Stack.PushBool(false)
			}
			return mapDictError(err)
		}
		if variant.byRef {
			ref, err := loadSingleRefDictValue(&value)
			if err != nil {
				return mapDictError(err)
			}
			if err = state.Stack.PushCell(ref); err != nil {
				return err
			}
		} else {
			if err = state.Stack.PushOwnedSlice(&value); err != nil {
				return err
			}
		}
		return state.Stack.PushBool(true)
	}
}

func execDictGetOptRef(variant dictScalarVariant) func(*vm.State) error {
	return func(state *vm.State) error {
		if err := checkDictStackDepth(state, 3); err != nil {
			return err
		}

		keyBits, root, err := popDictRootAndLen(state)
		if err != nil {
			return err
		}
		key, ok, err := popDirectDictKey(state, keyBits, variant.kind, false)
		if err != nil {
			return err
		}
		if !ok {
			return pushMaybeCell(state.Stack, nil)
		}

		dict := newReadOnlyTracedDict(root, keyBits, state)
		var value cell.Slice
		if err = key.loadValueInto(dict, &value); err != nil {
			if errors.Is(err, cell.ErrNoSuchKeyInDict) {
				return pushMaybeCell(state.Stack, nil)
			}
			return mapDictError(err)
		}
		ref, err := loadSingleRefDictValue(&value)
		if err != nil {
			return mapDictError(err)
		}
		return pushMaybeCell(state.Stack, ref)
	}
}

func execDictSet(mode cell.DictSetMode) func(dictValueVariant) func(*vm.State) error {
	return func(variant dictValueVariant) func(*vm.State) error {
		return func(state *vm.State) error {
			if err := checkDictStackDepth(state, 4); err != nil {
				return err
			}

			keyBits, root, err := popDictRootAndLen(state)
			if err != nil {
				return err
			}
			key, keyErr, err := popDirectDictSetKey(state, keyBits, variant.kind)
			if err != nil {
				return err
			}

			dict := newTracedDict(root, keyBits, state)
			var changed bool
			if variant.byRef {
				value, err := state.Stack.PopCell()
				if err != nil {
					return err
				}
				if keyErr != nil {
					return keyErr
				}
				var valueBuilder cell.Builder
				if err = valueBuilder.StoreRefUncheckedDepth(value); err != nil {
					return err
				}
				changed, err = key.setBuilderWithMode(dict, &valueBuilder, mode)
				if err != nil {
					return mapDictError(err)
				}
			} else {
				value, err := state.Stack.PopSlice()
				if err != nil {
					return err
				}
				if keyErr != nil {
					return keyErr
				}
				var valueBuilder cell.Builder
				value.ToBuilderInto(&valueBuilder)
				changed, err = key.setBuilderWithMode(dict, &valueBuilder, mode)
				if err != nil {
					return mapDictError(err)
				}
			}

			if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
				return err
			}
			if mode == cell.DictSetModeSet {
				if !changed {
					return vmerr.Error(vmerr.CodeFatal)
				}
				return nil
			}
			return state.Stack.PushBool(changed)
		}
	}
}

func execDictSetBuilder(mode cell.DictSetMode) func(dictScalarVariant) func(*vm.State) error {
	return func(variant dictScalarVariant) func(*vm.State) error {
		return func(state *vm.State) error {
			if err := checkDictStackDepth(state, 4); err != nil {
				return err
			}

			keyBits, root, err := popDictRootAndLen(state)
			if err != nil {
				return err
			}
			key, keyErr, err := popDirectDictSetKey(state, keyBits, variant.kind)
			if err != nil {
				return err
			}
			value, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if keyErr != nil {
				return keyErr
			}

			dict := newTracedDict(root, keyBits, state)
			changed, err := key.setBuilderWithMode(dict, value, mode)
			if err != nil {
				return mapDictError(err)
			}
			if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
				return err
			}
			if mode == cell.DictSetModeSet {
				if !changed {
					return vmerr.Error(vmerr.CodeFatal)
				}
				return nil
			}
			return state.Stack.PushBool(changed)
		}
	}
}

func execDictSetGet(mode cell.DictSetMode) func(dictValueVariant) func(*vm.State) error {
	return func(variant dictValueVariant) func(*vm.State) error {
		return func(state *vm.State) error {
			if err := checkDictStackDepth(state, 4); err != nil {
				return err
			}

			keyBits, root, err := popDictRootAndLen(state)
			if err != nil {
				return err
			}
			key, keyErr, err := popDirectDictSetKey(state, keyBits, variant.kind)
			if err != nil {
				return err
			}

			dict := newTracedDict(root, keyBits, state)

			if variant.byRef {
				value, err := state.Stack.PopCell()
				if err != nil {
					return err
				}
				if keyErr != nil {
					return keyErr
				}
				var valueBuilder cell.Builder
				if err = valueBuilder.StoreRefUncheckedDepth(value); err != nil {
					return err
				}
				oldSlice, _, err := key.loadValueAndSetBuilderWithMode(dict, &valueBuilder, mode)
				if err != nil {
					return mapDictError(err)
				}
				oldValue, err := loadSingleRefDictValue(oldSlice)
				if err != nil && !errors.Is(err, cell.ErrNoSuchKeyInDict) {
					return mapDictError(err)
				}
				if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
					return err
				}
				return pushSetGetResultRef(state, oldValue, mode)
			}

			value, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if keyErr != nil {
				return keyErr
			}
			var valueBuilder cell.Builder
			value.ToBuilderInto(&valueBuilder)
			oldValue, _, err := key.loadValueAndSetBuilderWithMode(dict, &valueBuilder, mode)
			if err != nil {
				return mapDictError(err)
			}
			if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
				return err
			}
			return pushSetGetResultSlice(state, oldValue, mode)
		}
	}
}

func execDictSetGetBuilder(mode cell.DictSetMode) func(dictScalarVariant) func(*vm.State) error {
	return func(variant dictScalarVariant) func(*vm.State) error {
		return func(state *vm.State) error {
			if err := checkDictStackDepth(state, 4); err != nil {
				return err
			}

			keyBits, root, err := popDictRootAndLen(state)
			if err != nil {
				return err
			}
			key, keyErr, err := popDirectDictSetKey(state, keyBits, variant.kind)
			if err != nil {
				return err
			}
			value, err := state.Stack.PopBuilder()
			if err != nil {
				return err
			}
			if keyErr != nil {
				return keyErr
			}

			dict := newTracedDict(root, keyBits, state)
			oldValue, _, err := key.loadValueAndSetBuilderWithMode(dict, value, mode)
			if err != nil {
				return mapDictError(err)
			}
			if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
				return err
			}
			return pushSetGetResultSlice(state, oldValue, mode)
		}
	}
}

func pushSetGetResultSlice(state *vm.State, oldValue *cell.Slice, mode cell.DictSetMode) error {
	if oldValue != nil {
		if err := state.Stack.PushOwnedSlice(oldValue); err != nil {
			return err
		}
		return state.Stack.PushBool(mode != cell.DictSetModeAdd)
	}
	return state.Stack.PushBool(mode == cell.DictSetModeAdd)
}

func loadSingleRefDictValue(value *cell.Slice) (*cell.Cell, error) {
	if value == nil {
		return nil, cell.ErrNoSuchKeyInDict
	}
	if value.BitsLeft() != 0 || value.RefsNum() != 1 {
		return nil, errors.New("value is not a single ref")
	}
	return value.PeekRefCell()
}

func loadDictMinMaxRefValue(dict *cell.Dictionary, fetchMax, invertFirst, remove bool) (*cell.Cell, *cell.Cell, error) {
	var (
		keyCell  *cell.Cell
		valSlice *cell.Slice
		err      error
	)

	if remove {
		keyCell, valSlice, err = dict.LoadMinMaxAndDelete(fetchMax, invertFirst)
	} else {
		keyCell, valSlice, err = dict.LoadMinMax(fetchMax, invertFirst)
	}
	if err != nil {
		return nil, nil, err
	}

	valRef, err := loadSingleRefDictValue(valSlice)
	if err != nil {
		return nil, nil, err
	}
	return keyCell, valRef, nil
}

func pushSetGetResultRef(state *vm.State, oldValue *cell.Cell, mode cell.DictSetMode) error {
	if oldValue != nil {
		if err := state.Stack.PushCell(oldValue); err != nil {
			return err
		}
		return state.Stack.PushBool(mode != cell.DictSetModeAdd)
	}
	return state.Stack.PushBool(mode == cell.DictSetModeAdd)
}

func execDictDelete(variant dictScalarVariant) func(*vm.State) error {
	return func(state *vm.State) error {
		if err := checkDictStackDepth(state, 3); err != nil {
			return err
		}

		keyBits, root, err := popDictRootAndLen(state)
		if err != nil {
			return err
		}
		key, ok, err := popDirectDictKey(state, keyBits, variant.kind, true)
		if err != nil {
			return err
		}
		dict := newTracedDict(root, keyBits, state)
		if !ok {
			if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
				return err
			}
			return state.Stack.PushBool(false)
		}
		changed, err := key.delete(dict)
		if err != nil {
			return mapDictError(err)
		}
		if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
			return err
		}
		return state.Stack.PushBool(changed)
	}
}

func execDictDeleteGet(variant dictValueVariant) func(*vm.State) error {
	return func(state *vm.State) error {
		if err := checkDictStackDepth(state, 3); err != nil {
			return err
		}

		keyBits, root, err := popDictRootAndLen(state)
		if err != nil {
			return err
		}
		key, ok, err := popDirectDictKey(state, keyBits, variant.kind, true)
		if err != nil {
			return err
		}
		dict := newTracedDict(root, keyBits, state)
		if !ok {
			if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
				return err
			}
			return state.Stack.PushBool(false)
		}

		if variant.byRef {
			valueSlice, err := key.loadValueAndDelete(dict)
			if err != nil {
				if errors.Is(err, cell.ErrNoSuchKeyInDict) {
					if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
						return err
					}
					return state.Stack.PushBool(false)
				}
				return mapDictError(err)
			}
			value, err := loadSingleRefDictValue(valueSlice)
			if err != nil {
				return mapDictError(err)
			}
			if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
				return err
			}
			if err = state.Stack.PushCell(value); err != nil {
				return err
			}
			return state.Stack.PushBool(true)
		}

		value, err := key.loadValueAndDelete(dict)
		if err != nil {
			if errors.Is(err, cell.ErrNoSuchKeyInDict) {
				if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
					return err
				}
				return state.Stack.PushBool(false)
			}
			return mapDictError(err)
		}
		if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
			return err
		}
		if err = state.Stack.PushOwnedSlice(value); err != nil {
			return err
		}
		return state.Stack.PushBool(true)
	}
}

func execDictSetGetOptRef(variant dictScalarVariant) func(*vm.State) error {
	return func(state *vm.State) error {
		if err := checkDictStackDepth(state, 4); err != nil {
			return err
		}

		keyBits, root, err := popDictRootAndLen(state)
		if err != nil {
			return err
		}
		key, keyErr, err := popDirectDictSetKey(state, keyBits, variant.kind)
		if err != nil {
			return err
		}
		newValue, err := state.Stack.PopMaybeCell()
		if err != nil {
			return err
		}
		if keyErr != nil {
			return keyErr
		}

		dict := newTracedDict(root, keyBits, state)
		var oldValue *cell.Cell
		if newValue != nil {
			var valueBuilder cell.Builder
			if err = valueBuilder.StoreRefUncheckedDepth(newValue); err != nil {
				return err
			}
			oldSlice, _, err := key.loadValueAndSetBuilderWithMode(dict, &valueBuilder, cell.DictSetModeSet)
			if err != nil {
				return mapDictError(err)
			}
			oldValue, err = loadSingleRefDictValue(oldSlice)
			if err != nil && !errors.Is(err, cell.ErrNoSuchKeyInDict) {
				return mapDictError(err)
			}
		} else {
			oldSlice, deleteErr := key.loadValueAndDelete(dict)
			err = deleteErr
			if err != nil && !errors.Is(err, cell.ErrNoSuchKeyInDict) {
				return mapDictError(err)
			}
			if errors.Is(err, cell.ErrNoSuchKeyInDict) {
				oldValue = nil
			} else {
				oldValue, err = loadSingleRefDictValue(oldSlice)
				if err != nil {
					return mapDictError(err)
				}
			}
		}

		if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
			return err
		}
		return pushMaybeCell(state.Stack, oldValue)
	}
}

func execDictMinMax(fetchMax bool, remove bool) func(dictValueVariant) func(*vm.State) error {
	return func(variant dictValueVariant) func(*vm.State) error {
		return func(state *vm.State) error {
			if err := checkDictStackDepth(state, 2); err != nil {
				return err
			}

			keyBits, root, err := popDictMinMaxRootAndLen(state, variant.kind)
			if err != nil {
				return err
			}
			var dict *cell.Dictionary
			if remove {
				dict = newTracedDict(root, keyBits, state)
			} else {
				dict = newReadOnlyTracedDict(root, keyBits, state)
			}

			invertFirst := variant.kind == dictKeySignedInt
			var (
				keyCell  *cell.Cell
				valSlice *cell.Slice
				valRef   *cell.Cell
			)

			if variant.byRef {
				if remove {
					keyCell, valRef, err = loadDictMinMaxRefValue(dict, fetchMax, invertFirst, true)
				} else {
					keyCell, valRef, err = loadDictMinMaxRefValue(dict, fetchMax, invertFirst, false)
				}
			} else {
				if remove {
					keyCell, valSlice, err = dict.LoadMinMaxAndDelete(fetchMax, invertFirst)
				} else {
					keyCell, valSlice, err = dict.LoadMinMax(fetchMax, invertFirst)
				}
			}
			if err != nil {
				if errors.Is(err, cell.ErrNoSuchKeyInDict) {
					if remove {
						if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
							return err
						}
					}
					return state.Stack.PushBool(false)
				}
				return mapDictError(err)
			}
			if remove {
				if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
					return err
				}
			}

			if variant.byRef {
				if err = state.Stack.PushCell(valRef); err != nil {
					return err
				}
			} else {
				if err = state.Stack.PushOwnedSlice(valSlice); err != nil {
					return err
				}
			}

			if variant.kind == dictKeySlice {
				if err = state.ConsumeGas(vm.CellCreateGasPrice); err != nil {
					return err
				}
			}
			if err = pushDictKeyValue(state, keyCell, variant.kind); err != nil {
				return err
			}
			return state.Stack.PushBool(true)
		}
	}
}

func execPfxDictSet(mode cell.DictSetMode) func(*vm.State) error {
	return func(state *vm.State) error {
		required := 3
		if state.GlobalVersion >= 9 {
			required = 4
		}
		if err := checkDictStackDepth(state, required); err != nil {
			return err
		}

		n, err := state.Stack.PopIntRangeInt64(0, 1023)
		if err != nil {
			return err
		}
		root, err := state.Stack.PopMaybeCell()
		if err != nil {
			return err
		}
		keySlice, err := state.Stack.PopSlice()
		if err != nil {
			return err
		}
		value, err := state.Stack.PopSlice()
		if err != nil {
			return err
		}

		keyBits := uint(n)
		if keySlice.BitsLeft() > keyBits {
			if err = pushMaybeCell(state.Stack, root); err != nil {
				return err
			}
			return state.Stack.PushBool(false)
		}

		dict := newTracedPrefixDict(root, keyBits, state)
		var valueBuilder cell.Builder
		value.ToBuilderInto(&valueBuilder)
		changed, err := dict.SetBuilderBySliceKeyWithMode(keySlice, &valueBuilder, mode)
		if err != nil {
			return mapDictError(err)
		}
		if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
			return err
		}
		return state.Stack.PushBool(changed)
	}
}

func execPfxDictDelete(state *vm.State) error {
	required := 2
	if state.GlobalVersion >= 9 {
		required = 3
	}
	if err := checkDictStackDepth(state, required); err != nil {
		return err
	}

	n, err := state.Stack.PopIntRangeInt64(0, 1023)
	if err != nil {
		return err
	}
	root, err := state.Stack.PopMaybeCell()
	if err != nil {
		return err
	}
	keySlice, err := state.Stack.PopSlice()
	if err != nil {
		return err
	}
	keyBits := uint(n)
	if keySlice.BitsLeft() > keyBits {
		if err = pushMaybeCell(state.Stack, root); err != nil {
			return err
		}
		return state.Stack.PushBool(false)
	}

	dict := newTracedPrefixDict(root, keyBits, state)
	_, err = dict.LoadValueAndDeleteBySliceKey(keySlice)
	changed := err == nil
	if err != nil && !errors.Is(err, cell.ErrNoSuchKeyInDict) {
		return mapDictError(err)
	}
	if err = pushMaybeCell(state.Stack, dict.AsCell()); err != nil {
		return err
	}
	return state.Stack.PushBool(changed)
}

func execPfxDictGet(op int) func(*vm.State) error {
	return func(state *vm.State) error {
		if state.Stack.Len() < 3 {
			return vmerr.Error(vmerr.CodeStackUnderflow)
		}

		n, err := state.Stack.PopIntRangeInt64(0, 1023)
		if err != nil {
			return err
		}
		root, err := state.Stack.PopMaybeCell()
		if err != nil {
			return err
		}
		input, err := state.Stack.PopSlice()
		if err != nil {
			return err
		}

		keyBits := uint(n)
		dict := newReadOnlyPrefixDictWithTrace(root, keyBits, state.Cells.Trace())
		var value cell.Slice
		matched, err := dict.LookupPrefixBySliceInto(input, &value)
		if errors.Is(err, cell.ErrNoSuchKeyInDict) {
			if op&1 != 0 {
				return vmerr.Error(vmerr.CodeCellUnderflow, "cannot parse a prefix belonging to a given prefix code dictionary")
			}
			if err = state.Stack.PushOwnedSlice(input); err != nil {
				return err
			}
			if op == 0 {
				return state.Stack.PushBool(false)
			}
			return nil
		}
		if err != nil {
			return mapDictError(err)
		}

		prefixSlice, err := input.FetchSubslice(matched, 0)
		if err != nil {
			return cellUnderflowError(err)
		}
		if err = state.Stack.PushOwnedSlice(prefixSlice); err != nil {
			return err
		}
		if op&2 == 0 {
			if err = state.Stack.PushOwnedSlice(&value); err != nil {
				return err
			}
		}
		if err = state.Stack.PushOwnedSlice(input); err != nil {
			return err
		}

		switch op {
		case 0:
			return state.Stack.PushBool(true)
		case 1:
			return nil
		case 2:
			return state.Jump(newOrdContinuation(&value, state.CP))
		default:
			return state.Call(newOrdContinuation(&value, state.CP))
		}
	}
}

func execDictGetNear(variant dictNearVariant) func(*vm.State) error {
	return func(state *vm.State) error {
		if err := checkDictStackDepth(state, 3); err != nil {
			return err
		}

		var (
			keyBits uint
			root    *cell.Cell
			err     error
		)

		if variant.kind == dictKeySlice {
			keyBits, root, err = popDictRootAndLen(state)
		} else {
			keyBits, root, err = popDictMinMaxRootAndLen(state, variant.kind)
		}
		if err != nil {
			return err
		}

		dict := newReadOnlyTracedDict(root, keyBits, state)
		invertFirst := variant.kind == dictKeySignedInt

		var (
			nearestKey *cell.Cell
			value      *cell.Slice
		)

		switch variant.kind {
		case dictKeySlice:
			keyHint, err := state.Stack.PopSlice()
			if err != nil {
				return err
			}
			if keyHint.BitsLeft() < keyBits {
				return vmerr.Error(vmerr.CodeCellUnderflow, "not enough bits for a dictionary key")
			}

			key := directDictKey{kind: variant.kind, slice: keyHint}
			nearestKey, value, err = key.lookupNearest(dict, variant.fetchNext, variant.allowEq, false)
			if err != nil {
				if errors.Is(err, cell.ErrNoSuchKeyInDict) {
					return state.Stack.PushBool(false)
				}
				return mapDictError(err)
			}
		default:
			idx, err := state.Stack.PopIntFinite()
			if err != nil {
				return err
			}

			signed := variant.kind == dictKeySignedInt
			if dictIntKeyFits(idx, keyBits, signed) {
				key := directDictKey{kind: variant.kind, integer: idx}
				nearestKey, value, err = key.lookupNearest(dict, variant.fetchNext, variant.allowEq, signed)
				if err != nil && !errors.Is(err, cell.ErrNoSuchKeyInDict) {
					return mapDictError(err)
				}
			} else if (idx.Sign() >= 0) != variant.fetchNext {
				nearestKey, value, err = dict.LoadMinMax(!variant.fetchNext, invertFirst)
				if err != nil && !errors.Is(err, cell.ErrNoSuchKeyInDict) {
					return mapDictError(err)
				}
			}

			if nearestKey == nil || value == nil {
				return state.Stack.PushBool(false)
			}
		}

		if err = state.Stack.PushOwnedSlice(value); err != nil {
			return err
		}
		if variant.kind == dictKeySlice {
			if err = state.ConsumeGas(vm.CellCreateGasPrice); err != nil {
				return err
			}
		}
		if err = pushDictKeyValue(state, nearestKey, variant.kind); err != nil {
			return err
		}
		return state.Stack.PushBool(true)
	}
}

func execDictGetExec(unsigned bool, call bool, keepOnMiss bool) func(*vm.State) error {
	return func(state *vm.State) error {
		if err := checkDictStackDepth(state, 3); err != nil {
			return err
		}

		keyBits, root, err := popDictRootAndLen(state)
		if err != nil {
			return err
		}
		idx, err := state.Stack.PopIntFinite()
		if err != nil {
			return err
		}

		kind := dictKeySignedInt
		if unsigned {
			kind = dictKeyUnsignedInt
		}
		if dictIntKeyFits(idx, keyBits, !unsigned) {
			dict := newReadOnlyTracedDict(root, keyBits, state)
			var value cell.Slice
			key := directDictKey{kind: kind, integer: idx}
			lookupErr := key.loadValueInto(dict, &value)
			if lookupErr == nil {
				cont := newOrdContinuation(&value, state.CP)
				if call {
					return state.Call(cont)
				}
				return state.Jump(cont)
			}
			if !errors.Is(lookupErr, cell.ErrNoSuchKeyInDict) {
				return mapDictError(lookupErr)
			}
		}

		if keepOnMiss {
			return state.Stack.PushInt(idx)
		}
		return nil
	}
}

func execSubdict(removePrefix bool) func(dictScalarVariant) func(*vm.State) error {
	return func(variant dictScalarVariant) func(*vm.State) error {
		return func(state *vm.State) error {
			if err := checkDictStackDepth(state, 4); err != nil {
				return err
			}

			n, err := state.Stack.PopIntRangeInt64(0, 1023)
			if err != nil {
				return err
			}
			root, err := state.Stack.PopMaybeCell()
			if err != nil {
				return err
			}

			keyBits := uint(n)
			prefix, err := popSubdictPrefix(state, keyBits, variant.kind)
			if err != nil {
				return err
			}

			dict := newTracedDict(root, keyBits, state)
			if ok, err := prefix.cut(dict, removePrefix); err != nil {
				return mapDictError(err)
			} else if !ok {
				return vmerr.Error(vmerr.CodeDict, "cannot construct subdictionary by key prefix")
			}

			return pushMaybeCell(state.Stack, dict.AsCell())
		}
	}
}

func popSubdictPrefix(state *vm.State, keyBits uint, kind dictKeyKind) (directSubdictPrefix, error) {
	prefix := directSubdictPrefix{kind: kind}
	switch kind {
	case dictKeySlice:
		k, err := state.Stack.PopIntRangeInt64(0, int64(keyBits))
		if err != nil {
			return directSubdictPrefix{}, err
		}
		prefix.bits = uint(k)
		sl, err := state.Stack.PopSlice()
		if err != nil {
			return directSubdictPrefix{}, err
		}
		if sl.BitsLeft() < prefix.bits {
			return directSubdictPrefix{}, vmerr.Error(vmerr.CodeCellUnderflow, "not enough bits for a dictionary key")
		}
		prefix.slice = sl
		return prefix, nil
	case dictKeySignedInt:
		k, err := state.Stack.PopIntRangeInt64(0, int64(minUint(keyBits, 257)))
		if err != nil {
			return directSubdictPrefix{}, err
		}
		prefix.bits = uint(k)
		val, err := state.Stack.PopIntFinite()
		if err != nil {
			return directSubdictPrefix{}, err
		}
		if !dictIntKeyFits(val, prefix.bits, true) {
			return directSubdictPrefix{}, vmerr.Error(vmerr.CodeCellUnderflow, "not enough bits for a dictionary key prefix")
		}
		prefix.integer = val
		return prefix, nil
	default:
		k, err := state.Stack.PopIntRangeInt64(0, int64(minUint(keyBits, 256)))
		if err != nil {
			return directSubdictPrefix{}, err
		}
		prefix.bits = uint(k)
		val, err := state.Stack.PopIntFinite()
		if err != nil {
			return directSubdictPrefix{}, err
		}
		if !dictIntKeyFits(val, prefix.bits, false) {
			return directSubdictPrefix{}, vmerr.Error(vmerr.CodeCellUnderflow, "not enough bits for a dictionary key prefix")
		}
		prefix.integer = val
		return prefix, nil
	}
}

func newOrdContinuation(code *cell.Slice, cp int) *vm.OrdinaryContinuation {
	return &vm.OrdinaryContinuation{
		Data: vm.ControlData{
			NumArgs: vm.ControlDataAllArgs,
			CP:      cp,
		},
		Code: code,
	}
}

func pushDictKeyValue(state *vm.State, key *cell.Cell, kind dictKeyKind) error {
	switch kind {
	case dictKeySlice:
		s, err := key.BeginParse()
		if err != nil {
			return cellUnderflowError(err)
		}
		return state.Stack.PushOwnedSlice(s)
	case dictKeySignedInt:
		s, err := key.BeginParse()
		if err != nil {
			return cellUnderflowError(err)
		}
		val, err := s.LoadBigInt(key.BitsSize())
		if err != nil {
			return cellUnderflowError(err)
		}
		return state.Stack.PushInt(val)
	default:
		s, err := key.BeginParse()
		if err != nil {
			return cellUnderflowError(err)
		}
		val, err := s.LoadBigUInt(key.BitsSize())
		if err != nil {
			return cellUnderflowError(err)
		}
		return state.Stack.PushInt(val)
	}
}

func popDictRootAndLen(state *vm.State) (uint, *cell.Cell, error) {
	if err := checkDictStackDepth(state, 2); err != nil {
		return 0, nil, err
	}

	n, err := state.Stack.PopIntRangeInt64(0, 1023)
	if err != nil {
		return 0, nil, err
	}
	root, err := state.Stack.PopMaybeCell()
	if err != nil {
		return 0, nil, err
	}
	return uint(n), root, nil
}

func popDictMinMaxRootAndLen(state *vm.State, kind dictKeyKind) (uint, *cell.Cell, error) {
	if err := checkDictStackDepth(state, 2); err != nil {
		return 0, nil, err
	}

	maxBits := int64(1023)
	switch kind {
	case dictKeySignedInt:
		maxBits = 257
	case dictKeyUnsignedInt:
		maxBits = 256
	}
	n, err := state.Stack.PopIntRangeInt64(0, maxBits)
	if err != nil {
		return 0, nil, err
	}
	root, err := state.Stack.PopMaybeCell()
	if err != nil {
		return 0, nil, err
	}
	return uint(n), root, nil
}

func popDirectDictKey(state *vm.State, bits uint, kind dictKeyKind, strict bool) (directDictKey, bool, error) {
	key := directDictKey{kind: kind}
	switch kind {
	case dictKeySlice:
		sl, err := state.Stack.PopSlice()
		if err != nil {
			return directDictKey{}, false, err
		}
		if sl.BitsLeft() < bits {
			return directDictKey{}, false, vmerr.Error(vmerr.CodeCellUnderflow, "not enough bits for a dictionary key")
		}
		key.slice = sl
		return key, true, nil
	case dictKeySignedInt, dictKeyUnsignedInt:
		value, err := state.Stack.PopIntFinite()
		if err != nil {
			return directDictKey{}, false, err
		}
		if !dictIntKeyFits(value, bits, kind == dictKeySignedInt) {
			if strict {
				return directDictKey{}, false, vmerr.Error(vmerr.CodeRangeCheck, "not enough bits for a dictionary key")
			}
			return directDictKey{}, false, nil
		}
		key.integer = value
		return key, true, nil
	default:
		panic("unsupported dictionary key kind")
	}
}

func popDirectDictSetKey(state *vm.State, bits uint, kind dictKeyKind) (directDictKey, error, error) {
	key := directDictKey{kind: kind}
	switch kind {
	case dictKeySlice:
		sl, err := state.Stack.PopSlice()
		if err != nil {
			return directDictKey{}, nil, err
		}
		key.slice = sl
		if sl.BitsLeft() < bits {
			return key, vmerr.Error(vmerr.CodeCellUnderflow, "not enough bits for a dictionary key"), nil
		}
		return key, nil, nil
	case dictKeySignedInt, dictKeyUnsignedInt:
		value, err := state.Stack.PopInt()
		if err != nil {
			return directDictKey{}, nil, err
		}
		key.integer = value
		if !dictIntKeyFits(value, bits, kind == dictKeySignedInt) {
			return key, nil, vmerr.Error(vmerr.CodeRangeCheck, "not enough bits for a dictionary key")
		}
		return key, nil, nil
	default:
		panic("unsupported dictionary key kind")
	}
}

func dictIntKeyFits(value *big.Int, bits uint, signed bool) bool {
	if value == nil {
		return false
	}
	if bits == 0 {
		return value.Sign() == 0
	}
	if !signed {
		return value.Sign() >= 0 && uint(value.BitLen()) <= bits
	}
	if value.Sign() >= 0 {
		return uint(value.BitLen()) < bits
	}

	bitLen := uint(value.BitLen())
	return bitLen < bits || bitLen == bits && value.TrailingZeroBits() == bits-1
}

func newTracedDict(root *cell.Cell, bits uint, state *vm.State) *cell.Dictionary {
	if root == nil {
		return cell.NewDict(bits).SetTrace(state.Cells.Trace())
	}
	return root.AsDict(bits).SetTrace(state.Cells.Trace())
}

func newReadOnlyTracedDict(root *cell.Cell, bits uint, state *vm.State) *cell.Dictionary {
	if root == nil {
		return cell.NewDict(bits).SetTrace(state.Cells.Trace())
	}
	return root.AsDictWithTrace(bits, state.Cells.Trace())
}

func newTracedPrefixDict(root *cell.Cell, bits uint, state *vm.State) *cell.PrefixDictionary {
	return newPrefixDictWithTrace(root, bits, state.Cells.Trace())
}

func newPrefixDictWithTrace(root *cell.Cell, bits uint, trace *cell.Trace) *cell.PrefixDictionary {
	if root == nil {
		return cell.NewPrefixDict(bits).SetTrace(trace)
	}
	return root.AsPrefixDict(bits).SetTrace(trace)
}

func newReadOnlyPrefixDictWithTrace(root *cell.Cell, bits uint, trace *cell.Trace) *cell.PrefixDictionary {
	if root == nil {
		return cell.NewPrefixDict(bits).SetTrace(trace)
	}
	return root.AsPrefixDictWithTrace(bits, trace)
}

func pushMaybeCell(stack *vm.Stack, value *cell.Cell) error {
	if value == nil {
		return stack.PushAny(nil)
	}
	return stack.PushCell(value)
}

func dictNonEmpty(sl *cell.Slice) int {
	if sl.BitsLeft() < 1 {
		return -1
	}
	res, err := sl.PreloadUInt(1)
	if err != nil {
		return -1
	}
	if sl.RefsNum() < int(res) {
		return -1
	}
	return int(res)
}

func minUint(a uint, b int) uint {
	if a < uint(b) {
		return a
	}
	return uint(b)
}

func cellUnderflowError(err error) error {
	if err == nil {
		return nil
	}
	return vmerr.Error(vmerr.CodeCellUnderflow, err.Error())
}

func cellOverflowError(err error) error {
	if err == nil {
		return nil
	}
	return vmerr.Error(vmerr.CodeCellOverflow, err.Error())
}

func mapDictError(err error) error {
	if err == nil {
		return nil
	}
	if vmErr := new(vmerr.VMError); errors.As(err, vmErr) {
		return err
	}
	switch {
	case errors.Is(err, cell.ErrNoSuchKeyInDict):
		return vmerr.Error(vmerr.CodeDict, err.Error())
	case errors.Is(err, cell.ErrNoMoreRefs):
		return cellUnderflowError(err)
	case errors.Is(err, cell.ErrTooMuchRefs),
		errors.Is(err, cell.ErrNotFit1023),
		errors.Is(err, cell.ErrCellDepthLimit),
		errors.Is(err, cell.ErrRefCannotBeNil):
		return cellOverflowError(err)
	case cell.IsNotEnoughDataError(err),
		errors.Is(err, cell.ErrLabelExceedsKeyBits),
		errors.Is(err, cell.ErrDictHasSpecialCells):
		return cellUnderflowError(err)
	default:
		return vmerr.Error(vmerr.CodeDict, err.Error())
	}
}

func DICTIGETJMPZ() *helpers.SimpleOP {
	return &helpers.SimpleOP{
		Action:    execDictGetExec(false, false, true),
		Name:      "DICTIGETJMPZ",
		BitPrefix: helpers.BytesPrefix(0xF4, 0xBC),
	}
}
