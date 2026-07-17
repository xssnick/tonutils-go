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

var dictBigIntOne = big.NewInt(1)

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
	keyCell, err := input.WithoutTrace().ToCell()
	if err != nil {
		return cellUnderflowError(err)
	}

	dict := newPrefixDictWithTrace(op.root, uint(op.bits), state.Cells.Trace())
	value, matched, err := dict.LookupPrefix(keyCell)
	if gasErr := state.Cells.PendingError(); gasErr != nil {
		return gasErr
	}
	if err != nil {
		return mapDictError(err)
	}
	if value == nil {
		return state.Stack.PushOwnedSlice(input)
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
	return state.Jump(newOrdContinuation(value, state.CP))
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
		key, ok, err := popDictKey(state, keyBits, variant.kind, false)
		if err != nil {
			return err
		}
		if !ok {
			return state.Stack.PushBool(false)
		}

		dict := newTracedDict(root, keyBits, state)
		if variant.byRef {
			value, err := loadDictRefValue(dict, key)
			if err != nil {
				if errors.Is(err, cell.ErrNoSuchKeyInDict) {
					return state.Stack.PushBool(false)
				}
				return mapDictError(err)
			}
			if err = state.Stack.PushCell(value); err != nil {
				return err
			}
		} else {
			value, err := dict.LoadValue(key)
			if err != nil {
				if errors.Is(err, cell.ErrNoSuchKeyInDict) {
					return state.Stack.PushBool(false)
				}
				return mapDictError(err)
			}
			if err = state.Stack.PushOwnedSlice(value); err != nil {
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
		key, ok, err := popDictKey(state, keyBits, variant.kind, false)
		if err != nil {
			return err
		}
		if !ok {
			return pushMaybeCell(state.Stack, nil)
		}

		dict := newTracedDict(root, keyBits, state)
		value, err := loadDictRefValue(dict, key)
		if err != nil {
			if errors.Is(err, cell.ErrNoSuchKeyInDict) {
				return pushMaybeCell(state.Stack, nil)
			}
			return mapDictError(err)
		}
		return pushMaybeCell(state.Stack, value)
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
			key, keyErr, err := popDictSetKey(state, keyBits, variant.kind)
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
				changed, err = setDictRefValueWithMode(dict, key, value, mode)
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
				changed, err = dict.SetBuilderWithMode(key, value.ToBuilder(), mode)
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
			key, keyErr, err := popDictSetKey(state, keyBits, variant.kind)
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
			changed, err := dict.SetBuilderWithMode(key, value, mode)
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
			key, keyErr, err := popDictSetKey(state, keyBits, variant.kind)
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
				valueBuilder := cell.BeginCell()
				if err = valueBuilder.StoreRefUncheckedDepth(value); err != nil {
					return err
				}
				oldSlice, _, err := dict.LoadValueAndSetBuilderWithMode(key, valueBuilder, mode)
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
			oldValue, _, err := dict.LoadValueAndSetBuilderWithMode(key, value.ToBuilder(), mode)
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
			key, keyErr, err := popDictSetKey(state, keyBits, variant.kind)
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
			oldValue, _, err := dict.LoadValueAndSetBuilderWithMode(key, value, mode)
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

func loadDictRefValue(dict *cell.Dictionary, key *cell.Cell) (*cell.Cell, error) {
	value, err := dict.LoadValue(key)
	if err != nil {
		return nil, err
	}
	return loadSingleRefDictValue(value)
}

func loadDictRefValueAndDelete(dict *cell.Dictionary, key *cell.Cell) (*cell.Cell, error) {
	value, err := dict.LoadValueAndDelete(key)
	if err != nil {
		return nil, err
	}
	return loadSingleRefDictValue(value)
}

func setDictRefValueWithMode(dict *cell.Dictionary, key, value *cell.Cell, mode cell.DictSetMode) (bool, error) {
	if value == nil {
		return false, errors.New("value ref is nil")
	}
	builder := cell.BeginCell()
	if err := builder.StoreRefUncheckedDepth(value); err != nil {
		return false, err
	}
	return dict.SetBuilderWithMode(key, builder, mode)
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
		key, ok, err := popDictKey(state, keyBits, variant.kind, true)
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
		oldRoot := dict.AsCell()
		_, err = dict.LoadValueAndDelete(key)
		if err != nil && !errors.Is(err, cell.ErrNoSuchKeyInDict) {
			return mapDictError(err)
		}
		newRoot := dict.AsCell()
		if err = pushMaybeCell(state.Stack, newRoot); err != nil {
			return err
		}
		return state.Stack.PushBool(err == nil && !sameMaybeCell(oldRoot, newRoot))
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
		key, ok, err := popDictKey(state, keyBits, variant.kind, true)
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
			value, err := loadDictRefValueAndDelete(dict, key)
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
			if err = state.Stack.PushCell(value); err != nil {
				return err
			}
			return state.Stack.PushBool(true)
		}

		value, err := dict.LoadValueAndDelete(key)
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
		key, keyErr, err := popDictSetKey(state, keyBits, variant.kind)
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
			valueBuilder := cell.BeginCell()
			if err = valueBuilder.StoreRefUncheckedDepth(newValue); err != nil {
				return err
			}
			oldSlice, _, err := dict.LoadValueAndSetBuilderWithMode(key, valueBuilder, cell.DictSetModeSet)
			if err != nil {
				return mapDictError(err)
			}
			oldValue, err = loadSingleRefDictValue(oldSlice)
			if err != nil && !errors.Is(err, cell.ErrNoSuchKeyInDict) {
				return mapDictError(err)
			}
		} else {
			oldValue, err = loadDictRefValueAndDelete(dict, key)
			if err != nil && !errors.Is(err, cell.ErrNoSuchKeyInDict) {
				return mapDictError(err)
			}
			if errors.Is(err, cell.ErrNoSuchKeyInDict) {
				oldValue = nil
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
			dict := newTracedDict(root, keyBits, state)

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

		keyCell, err := keySlice.WithoutTrace().ToCell()
		if err != nil {
			return cellUnderflowError(err)
		}
		dict := newTracedPrefixDict(root, keyBits, state)
		changed, err := dict.SetBuilderWithMode(keyCell, value.ToBuilder(), mode)
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

	keyCell, err := keySlice.WithoutTrace().ToCell()
	if err != nil {
		return cellUnderflowError(err)
	}

	dict := newTracedPrefixDict(root, keyBits, state)
	oldRoot := dict.AsCell()
	_, err = dict.LoadValueAndDelete(keyCell)
	if err != nil && !errors.Is(err, cell.ErrNoSuchKeyInDict) {
		return mapDictError(err)
	}
	newRoot := dict.AsCell()
	if err = pushMaybeCell(state.Stack, newRoot); err != nil {
		return err
	}
	return state.Stack.PushBool(err == nil && !sameMaybeCell(oldRoot, newRoot))
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
		keyCell, err := prefixDictLookupKeyCell(input, keyBits)
		if err != nil {
			return cellUnderflowError(err)
		}
		dict := newTracedPrefixDict(root, keyBits, state)
		value, matched, err := dict.LookupPrefix(keyCell)
		if err != nil {
			return mapDictError(err)
		}
		if value == nil {
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

		prefixSlice, err := input.FetchSubslice(matched, 0)
		if err != nil {
			return cellUnderflowError(err)
		}
		if err = state.Stack.PushOwnedSlice(prefixSlice); err != nil {
			return err
		}
		if op&2 == 0 {
			if err = state.Stack.PushOwnedSlice(value); err != nil {
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
			return state.Jump(newOrdContinuation(value, state.CP))
		default:
			return state.Call(newOrdContinuation(value, state.CP))
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

		dict := newTracedDict(root, keyBits, state)
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
			keyCell, err := sliceKeyCell(keyHint, keyBits)
			if err != nil {
				return err
			}

			nearestKey, value, err = dict.LookupNearestKey(keyCell, variant.fetchNext, variant.allowEq, false)
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
			keyCell, ok := encodeDictIntKey(idx, keyBits, signed)
			if ok {
				nearestKey, value, err = dict.LookupNearestKey(keyCell, variant.fetchNext, variant.allowEq, signed)
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

		key, ok := encodeDictIntKey(idx, keyBits, !unsigned)
		if ok {
			dict := newTracedDict(root, keyBits, state)
			value, lookupErr := dict.LoadValue(key)
			if lookupErr == nil {
				cont := newOrdContinuation(value, state.CP)
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
			_, prefix, err := popSubdictPrefix(state, keyBits, variant.kind)
			if err != nil {
				return err
			}

			dict := newTracedDict(root, keyBits, state)
			if ok, err := dict.CutPrefixSubdict(prefix, removePrefix); err != nil {
				return mapDictError(err)
			} else if !ok {
				return vmerr.Error(vmerr.CodeDict, "cannot construct subdictionary by key prefix")
			}

			return pushMaybeCell(state.Stack, dict.AsCell())
		}
	}
}

func popSubdictPrefix(state *vm.State, keyBits uint, kind dictKeyKind) (uint, *cell.Cell, error) {
	switch kind {
	case dictKeySlice:
		k, err := state.Stack.PopIntRangeInt64(0, int64(keyBits))
		if err != nil {
			return 0, nil, err
		}
		prefixBits := uint(k)
		sl, err := state.Stack.PopSlice()
		if err != nil {
			return 0, nil, err
		}
		prefix, err := sliceKeyCell(sl, prefixBits)
		return prefixBits, prefix, err
	case dictKeySignedInt:
		k, err := state.Stack.PopIntRangeInt64(0, int64(minUint(keyBits, 257)))
		if err != nil {
			return 0, nil, err
		}
		prefixBits := uint(k)
		val, err := state.Stack.PopIntFinite()
		if err != nil {
			return 0, nil, err
		}
		prefix, ok := encodeDictIntKey(val, prefixBits, true)
		if !ok {
			return 0, nil, vmerr.Error(vmerr.CodeCellUnderflow, "not enough bits for a dictionary key prefix")
		}
		return prefixBits, prefix, nil
	default:
		k, err := state.Stack.PopIntRangeInt64(0, int64(minUint(keyBits, 256)))
		if err != nil {
			return 0, nil, err
		}
		prefixBits := uint(k)
		val, err := state.Stack.PopIntFinite()
		if err != nil {
			return 0, nil, err
		}
		prefix, ok := encodeDictIntKey(val, prefixBits, false)
		if !ok {
			return 0, nil, vmerr.Error(vmerr.CodeCellUnderflow, "not enough bits for a dictionary key prefix")
		}
		return prefixBits, prefix, nil
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

func prefixDictLookupKeyCell(input *cell.Slice, keyBits uint) (*cell.Cell, error) {
	keyLen := input.BitsLeft()
	if keyLen > keyBits {
		keyLen = keyBits
	}
	key, err := input.WithoutTrace().PreloadSubslice(keyLen, 0)
	if err != nil {
		return nil, err
	}
	return key.ToCell()
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

func popDictKey(state *vm.State, bits uint, kind dictKeyKind, strict bool) (*cell.Cell, bool, error) {
	switch kind {
	case dictKeySlice:
		key, err := state.Stack.PopSlice()
		if err != nil {
			return nil, false, err
		}
		cellKey, err := sliceKeyCell(key, bits)
		return cellKey, err == nil, err
	case dictKeySignedInt:
		val, err := state.Stack.PopIntFinite()
		if err != nil {
			return nil, false, err
		}
		key, ok := encodeDictIntKey(val, bits, true)
		if !ok {
			if strict {
				return nil, false, vmerr.Error(vmerr.CodeRangeCheck, "not enough bits for a dictionary key")
			}
			return nil, false, nil
		}
		return key, true, nil
	default:
		val, err := state.Stack.PopIntFinite()
		if err != nil {
			return nil, false, err
		}
		key, ok := encodeDictIntKey(val, bits, false)
		if !ok {
			if strict {
				return nil, false, vmerr.Error(vmerr.CodeRangeCheck, "not enough bits for a dictionary key")
			}
			return nil, false, nil
		}
		return key, true, nil
	}
}

func popDictSetKey(state *vm.State, bits uint, kind dictKeyKind) (*cell.Cell, error, error) {
	switch kind {
	case dictKeySlice:
		key, err := state.Stack.PopSlice()
		if err != nil {
			return nil, nil, err
		}
		cellKey, err := sliceKeyCell(key, bits)
		return cellKey, err, nil
	case dictKeySignedInt:
		val, err := state.Stack.PopInt()
		if err != nil {
			return nil, nil, err
		}
		key, ok := encodeDictIntKey(val, bits, true)
		if !ok {
			return nil, nil, vmerr.Error(vmerr.CodeRangeCheck, "not enough bits for a dictionary key")
		}
		return key, nil, nil
	default:
		val, err := state.Stack.PopInt()
		if err != nil {
			return nil, nil, err
		}
		key, ok := encodeDictIntKey(val, bits, false)
		if !ok {
			return nil, nil, vmerr.Error(vmerr.CodeRangeCheck, "not enough bits for a dictionary key")
		}
		return key, nil, nil
	}
}

func sliceKeyCell(sl *cell.Slice, bits uint) (*cell.Cell, error) {
	if sl.BitsLeft() < bits {
		return nil, vmerr.Error(vmerr.CodeCellUnderflow, "not enough bits for a dictionary key")
	}
	data, err := sl.PreloadSlice(bits)
	if err != nil {
		return nil, cellUnderflowError(err)
	}
	return cell.BeginCell().MustStoreSlice(data, bits).EndCell(), nil
}

func newTracedDict(root *cell.Cell, bits uint, state *vm.State) *cell.Dictionary {
	if root == nil {
		return cell.NewDict(bits).SetTrace(state.Cells.Trace())
	}
	return root.AsDict(bits).SetTrace(state.Cells.Trace())
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

func pushMaybeCell(stack *vm.Stack, value *cell.Cell) error {
	if value == nil {
		return stack.PushAny(nil)
	}
	return stack.PushCell(value)
}

func sameMaybeCell(a, b *cell.Cell) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.HashKey() == b.HashKey()
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

func encodeDictIntKey(value *big.Int, bits uint, signed bool) (*cell.Cell, bool) {
	data, ok := encodeDictIntBits(value, bits, signed)
	if !ok {
		return nil, false
	}
	return cell.BeginCell().MustStoreSlice(data, bits).EndCell(), true
}

func encodeDictIntBits(value *big.Int, bits uint, signed bool) ([]byte, bool) {
	if value == nil {
		return nil, false
	}
	if bits == 0 {
		return []byte{}, value.Sign() == 0
	}

	unsignedValue := value
	if signed {
		if value.Sign() < 0 {
			absMinusOne := new(big.Int).Neg(value)
			absMinusOne.Sub(absMinusOne, dictBigIntOne)
			if absMinusOne.BitLen() >= int(bits) {
				return nil, false
			}

			unsignedValue = new(big.Int).Lsh(dictBigIntOne, bits)
			unsignedValue.Add(unsignedValue, value)
		} else if value.BitLen() >= int(bits) {
			return nil, false
		}
	} else if value.Sign() < 0 || value.BitLen() > int(bits) {
		return nil, false
	}

	outLen := int((bits + 7) / 8)
	out := make([]byte, outLen)
	unsignedValue.FillBytes(out)
	if rem := bits % 8; rem != 0 {
		shiftSliceLeft(out, 8-rem)
	}
	return out, true
}

func shiftSliceLeft(data []byte, shift uint) {
	if shift == 0 || len(data) == 0 {
		return
	}
	var carry byte
	for i := len(data) - 1; i >= 0; i-- {
		nextCarry := data[i] >> (8 - shift)
		data[i] = (data[i] << shift) | carry
		carry = nextCarry
	}
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
