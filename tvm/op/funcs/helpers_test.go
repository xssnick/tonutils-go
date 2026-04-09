package funcs

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"math/big"
	"testing"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/sha3"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func newFuncTestState(t *testing.T, params map[int]any) *vm.State {
	t.Helper()

	maxIdx := 0
	for idx := range params {
		if idx > maxIdx {
			maxIdx = idx
		}
	}

	values := tuple.NewTupleSized(maxIdx + 1)
	for idx, val := range params {
		if err := values.Set(idx, val); err != nil {
			t.Fatalf("failed to set param %d: %v", idx, err)
		}
	}

	c7 := tuple.NewTupleSized(1)
	if err := c7.Set(0, values); err != nil {
		t.Fatalf("failed to set c7 params: %v", err)
	}

	st := vm.NewExecutionState(vm.DefaultGlobalVersion, vm.NewGas(), nil, c7, vm.NewStack())
	st.InitForExecution()
	return st
}

func mustSliceData(t *testing.T, sl *cell.Slice) []byte {
	t.Helper()

	data, err := sl.PreloadSlice(sl.BitsLeft())
	if err != nil {
		t.Fatalf("failed to preload slice: %v", err)
	}
	return data
}

func mustTupleInt(t *testing.T, tup tuple.Tuple, idx int) *big.Int {
	t.Helper()

	val, err := tup.Index(idx)
	if err != nil {
		t.Fatalf("failed to read tuple index %d: %v", idx, err)
	}
	num, ok := val.(*big.Int)
	if !ok {
		t.Fatalf("tuple index %d is %T, want *big.Int", idx, val)
	}
	return num
}

func TestPushHostValueAndSmallInt(t *testing.T) {
	intCases := []struct {
		name  string
		value any
		want  int64
	}{
		{name: "int", value: int(12), want: 12},
		{name: "int8", value: int8(-8), want: -8},
		{name: "int16", value: int16(16), want: 16},
		{name: "int32", value: int32(-32), want: -32},
		{name: "int64", value: int64(64), want: 64},
		{name: "uint8", value: uint8(8), want: 8},
		{name: "uint16", value: uint16(16), want: 16},
		{name: "uint32", value: uint32(32), want: 32},
		{name: "uint64", value: uint64(64), want: 64},
	}

	for _, tc := range intCases {
		t.Run(tc.name, func(t *testing.T) {
			st := newFuncTestState(t, nil)
			if err := pushHostValue(st, tc.value); err != nil {
				t.Fatalf("pushHostValue failed: %v", err)
			}
			got, err := st.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("PopIntFinite failed: %v", err)
			}
			if got.Int64() != tc.want {
				t.Fatalf("unexpected integer value: want %d, got %v", tc.want, got)
			}
		})
	}

	st := newFuncTestState(t, nil)
	if err := pushSmallInt(st, -15); err != nil {
		t.Fatalf("pushSmallInt failed: %v", err)
	}
	got, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite failed: %v", err)
	}
	if got.Int64() != -15 {
		t.Fatalf("unexpected pushSmallInt value: %v", got)
	}

	nilCases := []struct {
		name  string
		value any
	}{
		{name: "nil cell", value: (*cell.Cell)(nil)},
		{name: "nil slice", value: (*cell.Slice)(nil)},
		{name: "nil builder", value: (*cell.Builder)(nil)},
	}
	for _, tc := range nilCases {
		t.Run(tc.name, func(t *testing.T) {
			st := newFuncTestState(t, nil)
			if err := pushHostValue(st, tc.value); err != nil {
				t.Fatalf("pushHostValue failed: %v", err)
			}
			val, err := st.Stack.PopAny()
			if err != nil {
				t.Fatalf("PopAny failed: %v", err)
			}
			if val != nil {
				t.Fatalf("expected nil, got %T", val)
			}
		})
	}

	cl := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	st = newFuncTestState(t, nil)
	if err := pushHostValue(st, cl); err != nil {
		t.Fatalf("pushHostValue(cell) failed: %v", err)
	}
	gotCell, err := st.Stack.PopCell()
	if err != nil {
		t.Fatalf("PopCell failed: %v", err)
	}
	if !bytes.Equal(gotCell.Hash(), cl.Hash()) {
		t.Fatal("cell hash mismatch after pushHostValue")
	}

	sl := cell.BeginCell().MustStoreUInt(0xCD, 8).ToSlice()
	st = newFuncTestState(t, nil)
	if err := pushHostValue(st, sl); err != nil {
		t.Fatalf("pushHostValue(slice) failed: %v", err)
	}
	gotSlice, err := st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	if got := mustSliceData(t, gotSlice); !bytes.Equal(got, []byte{0xCD}) {
		t.Fatalf("unexpected slice bytes: %x", got)
	}

	builder := cell.BeginCell().MustStoreUInt(0xEF, 8)
	st = newFuncTestState(t, nil)
	if err := pushHostValue(st, builder); err != nil {
		t.Fatalf("pushHostValue(builder) failed: %v", err)
	}
	gotBuilder, err := st.Stack.PopBuilder()
	if err != nil {
		t.Fatalf("PopBuilder failed: %v", err)
	}
	if got := mustSliceData(t, gotBuilder.ToSlice()); !bytes.Equal(got, []byte{0xEF}) {
		t.Fatalf("unexpected builder bytes: %x", got)
	}

	tup := *tuple.NewTuple(big.NewInt(7), big.NewInt(9))
	st = newFuncTestState(t, nil)
	if err := pushHostValue(st, tup); err != nil {
		t.Fatalf("pushHostValue(tuple) failed: %v", err)
	}
	gotTuple, err := st.Stack.PopTuple()
	if err != nil {
		t.Fatalf("PopTuple failed: %v", err)
	}
	if gotTuple.Len() != 2 || mustTupleInt(t, gotTuple, 0).Int64() != 7 || mustTupleInt(t, gotTuple, 1).Int64() != 9 {
		t.Fatalf("unexpected tuple after pushHostValue: len=%d", gotTuple.Len())
	}

	st = newFuncTestState(t, nil)
	if err := pushHostValue(st, "unsupported"); err == nil {
		t.Fatal("pushHostValue should reject unsupported host values")
	}
}

func TestExportFeeAndConfigHelpers(t *testing.T) {
	buf, err := exportUnsignedBytes(big.NewInt(0x1234), 4, "range")
	if err != nil {
		t.Fatalf("exportUnsignedBytes failed: %v", err)
	}
	if want := []byte{0x00, 0x00, 0x12, 0x34}; !bytes.Equal(buf, want) {
		t.Fatalf("unexpected unsigned export: want %x, got %x", want, buf)
	}
	if _, err = exportUnsignedBytes(big.NewInt(-1), 4, "negative"); err == nil {
		t.Fatal("negative integers should fail exportUnsignedBytes")
	}
	if _, err = exportUnsignedBytes(new(big.Int).Lsh(big.NewInt(1), 33), 4, "overflow"); err == nil {
		t.Fatal("overflowing integers should fail exportUnsignedBytes")
	}

	if got := ceilShiftRight(big.NewInt(0), 3); got.Sign() != 0 {
		t.Fatalf("ceilShiftRight(0) = %v, want 0", got)
	}
	if got := ceilShiftRight(big.NewInt(17), 1); got.Int64() != 9 {
		t.Fatalf("ceilShiftRight(17,1) = %v, want 9", got)
	}
	if got := maxBig(big.NewInt(5), big.NewInt(7)); got.Int64() != 7 {
		t.Fatalf("maxBig returned %v, want 7", got)
	}

	storageSlice := cell.BeginCell().
		MustStoreUInt(0xCC, 8).
		MustStoreUInt(1, 32).
		MustStoreUInt(2, 64).
		MustStoreUInt(3, 64).
		MustStoreUInt(4, 64).
		MustStoreUInt(5, 64).
		ToSlice()
	storagePrices, err := parseTonStoragePrices(storageSlice)
	if err != nil {
		t.Fatalf("parseTonStoragePrices failed: %v", err)
	}
	if storagePrices.ValidSince != 1 || storagePrices.BitPrice != 2 || storagePrices.CellPrice != 3 || storagePrices.MCBitPrice != 4 || storagePrices.MCCellPrice != 5 {
		t.Fatalf("unexpected storage prices: %+v", storagePrices)
	}
	if got := storagePrices.computeStorageFee(false, 10, 20, 3); got.Int64() != 1 {
		t.Fatalf("unexpected ordinary storage fee: %v", got)
	}
	if got := storagePrices.computeStorageFee(true, 1<<16, 1, 1); got.Int64() != 9 {
		t.Fatalf("unexpected masterchain storage fee: %v", got)
	}
	if parsedNil, err := parseTonStoragePrices(nil); err != nil || parsedNil != nil {
		t.Fatalf("parseTonStoragePrices(nil) = (%v, %v), want (nil, nil)", parsedNil, err)
	}
	if _, err = parseTonStoragePrices(cell.BeginCell().MustStoreUInt(0x00, 8).ToSlice()); err == nil {
		t.Fatal("invalid storage prices tag should fail")
	}

	gasSlice := cell.BeginCell().
		MustStoreUInt(0xD1, 8).
		MustStoreUInt(10, 64).
		MustStoreUInt(3, 64).
		MustStoreUInt(0xDE, 8).
		MustStoreUInt(1<<16, 64).
		MustStoreUInt(100, 64).
		MustStoreUInt(80, 64).
		MustStoreUInt(7, 64).
		MustStoreUInt(1000, 64).
		MustStoreUInt(11, 64).
		MustStoreUInt(12, 64).
		ToSlice()
	gasPrices, err := parseTonGasPrices(gasSlice)
	if err != nil {
		t.Fatalf("parseTonGasPrices failed: %v", err)
	}
	if gasPrices.FlatGasLimit != 10 || gasPrices.FlatGasPrice != 3 || gasPrices.SpecialGasLimit != 80 {
		t.Fatalf("unexpected gas prices: %+v", gasPrices)
	}
	if got := gasPrices.computeGasPrice(9); got.Int64() != 3 {
		t.Fatalf("unexpected flat gas fee: %v", got)
	}
	if got := gasPrices.computeGasPrice(13); got.Int64() != 6 {
		t.Fatalf("unexpected extended gas fee: %v", got)
	}
	if _, err = parseTonGasPrices(nil); err == nil {
		t.Fatal("nil gas prices slice should fail")
	}
	if _, err = parseTonGasPrices(cell.BeginCell().MustStoreUInt(0x00, 8).ToSlice()); err == nil {
		t.Fatal("invalid gas prices tag should fail")
	}

	msgSlice := cell.BeginCell().
		MustStoreUInt(0xEA, 8).
		MustStoreUInt(100, 64).
		MustStoreUInt(1<<16, 64).
		MustStoreUInt(2<<16, 64).
		MustStoreUInt(77, 32).
		MustStoreUInt(9, 16).
		MustStoreUInt(10, 16).
		ToSlice()
	msgConfigSlice := cell.BeginCell().
		MustStoreUInt(0xEA, 8).
		MustStoreUInt(100, 64).
		MustStoreUInt(1<<16, 64).
		MustStoreUInt(2<<16, 64).
		MustStoreUInt(77, 32).
		MustStoreUInt(9, 16).
		MustStoreUInt(10, 16).
		ToSlice()
	msgPrices, err := parseTonMsgPrices(msgSlice)
	if err != nil {
		t.Fatalf("parseTonMsgPrices failed: %v", err)
	}
	if msgPrices.LumpPrice != 100 || msgPrices.IHRFactor != 77 || msgPrices.FirstFrac != 9 || msgPrices.NextFrac != 10 {
		t.Fatalf("unexpected msg prices: %+v", msgPrices)
	}
	if got := msgPrices.computeForwardFee(2, 3); got.Int64() != 107 {
		t.Fatalf("unexpected forward fee: %v", got)
	}
	if _, err = parseTonMsgPrices(nil); err == nil {
		t.Fatal("nil msg prices slice should fail")
	}
	if _, err = parseTonMsgPrices(cell.BeginCell().MustStoreUInt(0x00, 8).ToSlice()); err == nil {
		t.Fatal("invalid msg prices tag should fail")
	}

	configTuple := tuple.NewTupleSized(6)
	if err = configTuple.Set(0, cell.BeginCell().MustStoreUInt(0xCC, 8).MustStoreUInt(1, 32).MustStoreUInt(2, 64).MustStoreUInt(3, 64).MustStoreUInt(4, 64).MustStoreUInt(5, 64).ToSlice()); err != nil {
		t.Fatalf("failed to set storage config: %v", err)
	}
	if err = configTuple.Set(2, gasSlice); err != nil {
		t.Fatalf("failed to set mc gas config: %v", err)
	}
	if err = configTuple.Set(5, msgConfigSlice); err != nil {
		t.Fatalf("failed to set msg config: %v", err)
	}
	st := newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: configTuple})
	if parsed, err := getTonGasPrices(st, true); err != nil || parsed.FlatGasLimit != 10 {
		t.Fatalf("getTonGasPrices(masterchain) = (%+v, %v)", parsed, err)
	}
	if parsed, err := getTonMsgPrices(st, false); err != nil || parsed.LumpPrice != 100 {
		t.Fatalf("getTonMsgPrices(workchain) = (%+v, %v)", parsed, err)
	}
	if parsed, err := getTonStoragePrices(st); err != nil || parsed.ValidSince != 1 {
		t.Fatalf("getTonStoragePrices = (%+v, %v)", parsed, err)
	}

	stack := vm.NewStack()
	if err = stack.PushInt(big.NewInt(42)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if got, err := popUint64NonNegative(stack); err != nil || got != 42 {
		t.Fatalf("popUint64NonNegative = (%d, %v), want (42, nil)", got, err)
	}
	if err = stack.PushInt(big.NewInt(-1)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if _, err = popUint64NonNegative(stack); err == nil {
		t.Fatal("negative integers should fail popUint64NonNegative")
	}
	if err = stack.PushInt(new(big.Int).Lsh(big.NewInt(1), 63)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if _, err = popUint64NonNegative(stack); err == nil {
		t.Fatal("integers wider than 63 bits should fail popUint64NonNegative")
	}
}

func TestHashExtHelpersAndSHA256U(t *testing.T) {
	data := []byte("abc")
	sha256Sum := sha256.Sum256(data)
	expected := map[int][]byte{
		0: sha256Sum[:],
	}
	sha512Sum := sha512.Sum512(data)
	expected[1] = sha512Sum[:]
	blake2bSum := blake2b.Sum512(data)
	expected[2] = blake2bSum[:]
	keccak256 := sha3.NewLegacyKeccak256()
	_, _ = keccak256.Write(data)
	expected[3] = keccak256.Sum(nil)
	keccak512 := sha3.NewLegacyKeccak512()
	_, _ = keccak512.Write(data)
	expected[4] = keccak512.Sum(nil)

	for id, want := range expected {
		hasher, err := newHashExtHasher(id)
		if err != nil {
			t.Fatalf("newHashExtHasher(%d) failed: %v", id, err)
		}
		if err = hasher.Append(data); err != nil {
			t.Fatalf("Append failed for hash id %d: %v", id, err)
		}
		if got := hasher.Finish(); !bytes.Equal(got, want) {
			t.Fatalf("unexpected digest for hash id %d:\nwant %x\ngot  %x", id, want, got)
		}
		if hasher.BytesPerGasUnit() <= 0 {
			t.Fatalf("unexpected BytesPerGasUnit for hash id %d", id)
		}
	}
	if _, err := newHashExtHasher(99); err == nil {
		t.Fatal("unknown hash id should fail")
	}

	dstBits := 0
	buf := appendBits(nil, &dstBits, []byte{0xA0}, 3)
	buf = appendBits(buf, &dstBits, []byte{0xF8}, 5)
	if dstBits != 8 || !bytes.Equal(buf, []byte{0xBF}) {
		t.Fatalf("unexpected appendBits result: bits=%d bytes=%x", dstBits, buf)
	}

	sl := cell.BeginCell().MustStoreSlice([]byte{0xAB, 0xC0}, 10).ToSlice()
	raw, bits, err := valueBitsForHashExt(sl)
	if err != nil {
		t.Fatalf("valueBitsForHashExt(slice) failed: %v", err)
	}
	if bits != 10 || !bytes.Equal(raw, []byte{0xAB, 0xC0}) {
		t.Fatalf("unexpected slice bits: bits=%d bytes=%x", bits, raw)
	}
	builder := cell.BeginCell().MustStoreSlice([]byte{0xCC}, 6)
	raw, bits, err = valueBitsForHashExt(builder)
	if err != nil {
		t.Fatalf("valueBitsForHashExt(builder) failed: %v", err)
	}
	if bits != 6 || len(raw) != 1 {
		t.Fatalf("unexpected builder bits: bits=%d bytes=%x", bits, raw)
	}
	if _, _, err = valueBitsForHashExt(big.NewInt(1)); err == nil {
		t.Fatal("non-slice/builder values should fail valueBitsForHashExt")
	}

	st := newFuncTestState(t, nil)
	if err = st.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = st.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	items, b, err := collectHashExtItems(st, 2, false, false)
	if err != nil {
		t.Fatalf("collectHashExtItems failed: %v", err)
	}
	if b != nil {
		t.Fatal("builder should be nil when appendMode is false")
	}
	if items[0].(*big.Int).Int64() != 1 || items[1].(*big.Int).Int64() != 2 {
		t.Fatalf("unexpected item order: %#v", items)
	}

	st = newFuncTestState(t, nil)
	baseBuilder := cell.BeginCell().MustStoreUInt(0xAA, 8)
	if err = st.Stack.PushBuilder(baseBuilder); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err = st.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = st.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	items, b, err = collectHashExtItems(st, 2, true, true)
	if err != nil {
		t.Fatalf("collectHashExtItems(append) failed: %v", err)
	}
	if b == nil || items[0].(*big.Int).Int64() != 2 || items[1].(*big.Int).Int64() != 1 {
		t.Fatalf("unexpected append-mode result: items=%#v builder=%v", items, b)
	}

	st = newFuncTestState(t, nil)
	hashInput := cell.BeginCell().MustStoreSlice(data, uint(len(data))*8).ToSlice()
	if err = st.Stack.PushSlice(hashInput); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = st.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = HASHEXT(0).Interpret(st); err != nil {
		t.Fatalf("HASHEXT sha256 failed: %v", err)
	}
	hashInt, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite failed: %v", err)
	}
	if want := new(big.Int).SetBytes(expected[0]); hashInt.Cmp(want) != 0 {
		t.Fatalf("unexpected HASHEXT sha256 result: want %x, got %x", want.Bytes(), hashInt.Bytes())
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(hashInput); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = st.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = st.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = HASHEXT(255).Interpret(st); err != nil {
		t.Fatalf("HASHEXT dynamic sha512 failed: %v", err)
	}
	outTuple, err := st.Stack.PopTuple()
	if err != nil {
		t.Fatalf("PopTuple failed: %v", err)
	}
	if outTuple.Len() != 2 {
		t.Fatalf("unexpected sha512 tuple length: %d", outTuple.Len())
	}
	if mustTupleInt(t, outTuple, 0).Cmp(new(big.Int).SetBytes(expected[1][:32])) != 0 ||
		mustTupleInt(t, outTuple, 1).Cmp(new(big.Int).SetBytes(expected[1][32:])) != 0 {
		t.Fatalf("unexpected sha512 tuple output")
	}

	st = newFuncTestState(t, nil)
	appendBuilder := cell.BeginCell().MustStoreUInt(0xAA, 8)
	if err = st.Stack.PushBuilder(appendBuilder); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err = st.Stack.PushSlice(hashInput); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = st.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = HASHEXT(1<<9 | 3).Interpret(st); err != nil {
		t.Fatalf("HASHEXTA keccak256 failed: %v", err)
	}
	outBuilder, err := st.Stack.PopBuilder()
	if err != nil {
		t.Fatalf("PopBuilder failed: %v", err)
	}
	if got := mustSliceData(t, outBuilder.ToSlice()); !bytes.Equal(got, append([]byte{0xAA}, expected[3]...)) {
		t.Fatalf("unexpected append-mode hash output: %x", got)
	}

	st = newFuncTestState(t, nil)
	misaligned := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()
	if err = st.Stack.PushSlice(misaligned); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = st.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = HASHEXT(0).Interpret(st); err == nil {
		t.Fatal("misaligned hash input should fail")
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(cell.BeginCell().MustStoreSlice(data, uint(len(data))*8).ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = SHA256U().Interpret(st); err != nil {
		t.Fatalf("SHA256U failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Cmp(new(big.Int).SetBytes(expected[0])) != 0 {
		t.Fatalf("SHA256U = (%v, %v), want (%x, nil)", got, err, expected[0])
	}
	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(misaligned); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = SHA256U().Interpret(st); err == nil {
		t.Fatal("SHA256U should reject non-byte-aligned slices")
	}
}

func TestPRNGHelpersAndOps(t *testing.T) {
	prevBlocks := *tuple.NewTuple(big.NewInt(1), big.NewInt(2), big.NewInt(3))
	inMsg := *tuple.NewTuple(big.NewInt(4), big.NewInt(5), big.NewInt(6))
	seed := big.NewInt(5)
	st := newFuncTestState(t, map[int]any{
		paramIdxPrevBlocksInfo: prevBlocks,
		paramIdxInMsgParams:    inMsg,
		paramIdxRandomSeed:     seed,
	})

	if got, err := getPrevBlocksTuple(st); err != nil || mustTupleInt(t, got, 1).Int64() != 2 {
		t.Fatalf("getPrevBlocksTuple = (%v, %v)", got, err)
	}
	if got, err := getInMsgParamsTuple(st); err != nil || mustTupleInt(t, got, 2).Int64() != 6 {
		t.Fatalf("getInMsgParamsTuple = (%v, %v)", got, err)
	}
	if err := pushInMsgParam(st, 1); err != nil {
		t.Fatalf("pushInMsgParam failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 5 {
		t.Fatalf("pushInMsgParam stack result = (%v, %v)", got, err)
	}

	if got, err := getRandSeed(st); err != nil || got.Cmp(seed) != 0 {
		t.Fatalf("getRandSeed = (%v, %v)", got, err)
	}
	seedBytes, err := randSeedBytes(st)
	if err != nil {
		t.Fatalf("randSeedBytes failed: %v", err)
	}
	if len(seedBytes) != 32 || seedBytes[31] != 5 {
		t.Fatalf("unexpected randSeedBytes output: %x", seedBytes)
	}

	sum := sha512.Sum512(seedBytes)
	expectedNext := new(big.Int).SetBytes(sum[:32])
	expectedVal := new(big.Int).SetBytes(sum[32:])
	val, err := generateRandU256(st)
	if err != nil {
		t.Fatalf("generateRandU256 failed: %v", err)
	}
	if val.Cmp(expectedVal) != 0 {
		t.Fatalf("unexpected generateRandU256 value: want %x, got %x", expectedVal.Bytes(), val.Bytes())
	}
	nextSeed, err := getRandSeed(st)
	if err != nil {
		t.Fatalf("getRandSeed(after generate) failed: %v", err)
	}
	if nextSeed.Cmp(expectedNext) != 0 {
		t.Fatalf("unexpected next seed: want %x, got %x", expectedNext.Bytes(), nextSeed.Bytes())
	}

	if err = setRandSeed(st, big.NewInt(77)); err != nil {
		t.Fatalf("setRandSeed failed: %v", err)
	}
	if got, err := getRandSeed(st); err != nil || got.Int64() != 77 {
		t.Fatalf("setRandSeed result = (%v, %v)", got, err)
	}
	if err = setRandSeed(st, nil); err == nil {
		t.Fatal("setRandSeed should reject nil seeds")
	}
	if err = setRandSeed(st, new(big.Int).Lsh(big.NewInt(1), 257)); err == nil {
		t.Fatal("setRandSeed should reject wide seeds")
	}

	st = newFuncTestState(t, map[int]any{paramIdxRandomSeed: big.NewInt(11)})
	if err = st.Stack.PushInt(big.NewInt(99)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = SETRAND().Interpret(st); err != nil {
		t.Fatalf("SETRAND failed: %v", err)
	}
	if got, err := getRandSeed(st); err != nil || got.Int64() != 99 {
		t.Fatalf("SETRAND seed = (%v, %v)", got, err)
	}

	st = newFuncTestState(t, map[int]any{paramIdxRandomSeed: big.NewInt(11)})
	if err = st.Stack.PushInt(big.NewInt(9)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = ADDRAND().Interpret(st); err != nil {
		t.Fatalf("ADDRAND failed: %v", err)
	}
	expectedMix := sha256.Sum256(append(
		big.NewInt(11).FillBytes(make([]byte, 32)),
		big.NewInt(9).FillBytes(make([]byte, 32))...,
	))
	if got, err := getRandSeed(st); err != nil || got.Cmp(new(big.Int).SetBytes(expectedMix[:])) != 0 {
		t.Fatalf("ADDRAND seed = (%v, %v)", got, err)
	}

	st = newFuncTestState(t, map[int]any{paramIdxRandomSeed: big.NewInt(13)})
	seedBytes = big.NewInt(13).FillBytes(make([]byte, 32))
	sum = sha512.Sum512(seedBytes)
	expectedVal = new(big.Int).SetBytes(sum[32:])
	if err = RANDU256().Interpret(st); err != nil {
		t.Fatalf("RANDU256 failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Cmp(expectedVal) != 0 {
		t.Fatalf("RANDU256 = (%v, %v)", got, err)
	}

	st = newFuncTestState(t, map[int]any{paramIdxRandomSeed: big.NewInt(17)})
	seedBytes = big.NewInt(17).FillBytes(make([]byte, 32))
	sum = sha512.Sum512(seedBytes)
	randVal := new(big.Int).SetBytes(sum[32:])
	if err = st.Stack.PushInt(big.NewInt(10)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = RAND().Interpret(st); err != nil {
		t.Fatalf("RAND failed: %v", err)
	}
	wantRand := new(big.Int).Mul(big.NewInt(10), randVal)
	wantRand.Rsh(wantRand, 256)
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Cmp(wantRand) != 0 {
		t.Fatalf("RAND = (%v, %v), want %v", got, err, wantRand)
	}

	if _, err = getPrevBlocksTuple(newFuncTestState(t, map[int]any{paramIdxPrevBlocksInfo: big.NewInt(1)})); err == nil {
		t.Fatal("getPrevBlocksTuple should reject non-tuple values")
	}
	if _, err = getInMsgParamsTuple(newFuncTestState(t, map[int]any{paramIdxInMsgParams: big.NewInt(1)})); err == nil {
		t.Fatal("getInMsgParamsTuple should reject non-tuple values")
	}
	if _, err = getRandSeed(newFuncTestState(t, map[int]any{paramIdxRandomSeed: "bad"})); err == nil {
		t.Fatal("getRandSeed should reject non-big.Int values")
	}
	if _, err = getRandSeed(newFuncTestState(t, map[int]any{paramIdxRandomSeed: new(big.Int).Neg(big.NewInt(1))})); err == nil {
		t.Fatal("getRandSeed should reject negative seeds")
	}
}

func TestNowGetParamAndCodepageOps(t *testing.T) {
	st := newFuncTestState(t, map[int]any{
		0:  int64(21),
		3:  int64(1234),
		9:  cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell(),
		15: int64(21),
	})

	if err := NOW().Interpret(st); err != nil {
		t.Fatalf("NOW failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 1234 {
		t.Fatalf("NOW = (%v, %v)", got, err)
	}

	getParam := GETPARAM(15)
	code := getParam.Serialize().ToSlice()
	decoded := GETPARAM(0)
	if err := decoded.Deserialize(code); err != nil {
		t.Fatalf("GETPARAM deserialize failed: %v", err)
	}
	if err := decoded.Interpret(st); err != nil {
		t.Fatalf("GETPARAM interpret failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 21 {
		t.Fatalf("GETPARAM result = (%v, %v)", got, err)
	}

	getParamLong := GETPARAMLONG(15)
	code = getParamLong.Serialize().ToSlice()
	decodedLong := GETPARAMLONG(0)
	if err := decodedLong.Deserialize(code); err != nil {
		t.Fatalf("GETPARAMLONG deserialize failed: %v", err)
	}
	if err := decodedLong.Interpret(st); err != nil {
		t.Fatalf("GETPARAMLONG interpret failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Int64() != 21 {
		t.Fatalf("GETPARAMLONG result = (%v, %v)", got, err)
	}

	if err := CONFIGDICT().Interpret(st); err != nil {
		t.Fatalf("CONFIGDICT failed: %v", err)
	}
	width, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(width) failed: %v", err)
	}
	cfg, err := st.Stack.PopCell()
	if err != nil {
		t.Fatalf("PopCell(config) failed: %v", err)
	}
	if width.Int64() != 32 || cfg == nil {
		t.Fatalf("unexpected CONFIGDICT output: width=%v cell=%v", width, cfg)
	}

	if err := setCodepage(st, 0); err != nil {
		t.Fatalf("setCodepage(0) failed: %v", err)
	}
	if err := setCodepage(st, 1); err == nil {
		t.Fatal("setCodepage should reject unsupported codepages")
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SETCPX().Interpret(st); err != nil {
		t.Fatalf("SETCPX failed: %v", err)
	}
	if st.CP != 0 {
		t.Fatalf("unexpected codepage after SETCPX: %d", st.CP)
	}
	if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SETCPX().Interpret(st); err == nil {
		t.Fatal("SETCPX should reject unsupported codepages")
	}

	st = newFuncTestState(t, nil)
	if err := RIST255_PUSHL().Interpret(st); err != nil {
		t.Fatalf("RIST255_PUSHL failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Cmp(ristretto255L) != 0 {
		t.Fatalf("RIST255_PUSHL = (%v, %v)", got, err)
	}
	if err := BLS_PUSHR().Interpret(st); err != nil {
		t.Fatalf("BLS_PUSHR failed: %v", err)
	}
	if got, err := st.Stack.PopIntFinite(); err != nil || got.Cmp(blsOrder) != 0 {
		t.Fatalf("BLS_PUSHR = (%v, %v)", got, err)
	}
}

func TestSendRawMsg(t *testing.T) {
	st := newFuncTestState(t, nil)
	st.InitForExecution()
	msg := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	if err := st.Stack.PushCell(msg); err != nil {
		t.Fatalf("PushCell failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(7)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SENDRAWMSG().Interpret(st); err != nil {
		t.Fatalf("SENDRAWMSG failed: %v", err)
	}
	if st.Reg.D[1] == nil {
		t.Fatal("SENDRAWMSG should update the action register")
	}
}
