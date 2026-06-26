package funcs

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func mustStdAddrSlice(t *testing.T) (*address.Address, *cell.Slice, []byte) {
	t.Helper()

	data := bytes.Repeat([]byte{0x11}, 32)
	addr := address.NewAddress(0, 0, data)
	return addr, cell.BeginCell().MustStoreAddr(addr).ToSlice(), data
}

func mustExtAddrSlice(t *testing.T) (*address.Address, *cell.Slice) {
	t.Helper()

	addr := address.NewAddressExt(0, 16, []byte{0xAB, 0xCD})
	return addr, cell.BeginCell().MustStoreAddr(addr).ToSlice()
}

func makeSendMsgState(t *testing.T, myAddr *address.Address) *vm.State {
	t.Helper()

	cfg := tuple.NewTupleSized(7)
	msgSlice := cell.BeginCell().
		MustStoreUInt(0xEA, 8).
		MustStoreUInt(100, 64).
		MustStoreUInt(1<<16, 64).
		MustStoreUInt(2<<16, 64).
		MustStoreUInt(77, 32).
		MustStoreUInt(9, 16).
		MustStoreUInt(10, 16).
		ToSlice()
	if err := cfg.Set(5, msgSlice); err != nil {
		t.Fatalf("failed to set msg config: %v", err)
	}

	st := newFuncTestState(t, map[int]any{
		paramIdxUnpackedConfig: cfg,
		7:                      tuple.NewTupleValue(big.NewInt(1000), makeExtraBalanceDict(t, map[uint32]uint64{7: 55})),
		8:                      cell.BeginCell().MustStoreAddr(myAddr).ToSlice(),
	})
	st.InitForExecution()
	return st
}

func TestStorageStatsDataSizeAndVarInts(t *testing.T) {
	ref := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()
	root := cell.BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(ref).EndCell()

	stat := newStorageStat(10, nil)
	ok, err := stat.addCell(root)
	if err != nil || !ok {
		t.Fatal("addCell(root) should succeed")
	}
	if stat.cells != 2 || stat.bits != 16 || stat.refs != 1 {
		t.Fatalf("unexpected storage stats: %+v", stat)
	}
	ok, err = stat.addCell(root)
	if err != nil || !ok || stat.cells != 2 {
		t.Fatal("duplicate cells should not be counted twice")
	}

	limited := newStorageStat(1, nil)
	ok, err = limited.addCell(root)
	if err != nil {
		t.Fatalf("addCell failed with unexpected error: %v", err)
	}
	if ok {
		t.Fatal("addCell should fail when the cell limit is exceeded")
	}

	sliceStat := newStorageStat(10, nil)
	ok, err = sliceStat.addSlice(root.MustBeginParse())
	if err != nil || !ok || sliceStat.refs != 1 || sliceStat.bits != 16 || sliceStat.cells != 1 {
		t.Fatalf("unexpected slice stats: %+v", sliceStat)
	}

	st := newFuncTestState(t, nil)
	st.InitForExecution()
	if err := st.Stack.PushCell(root); err != nil {
		t.Fatalf("PushCell failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(10)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := CDATASIZE().Interpret(st); err != nil {
		t.Fatalf("CDATASIZE failed: %v", err)
	}
	refs, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(refs) failed: %v", err)
	}
	bits, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(bits) failed: %v", err)
	}
	cellsCnt, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(cells) failed: %v", err)
	}
	if refs.Int64() != 1 || bits.Int64() != 16 || cellsCnt.Int64() != 2 {
		t.Fatalf("unexpected CDATASIZE result: cells=%v bits=%v refs=%v", cellsCnt, bits, refs)
	}

	st = newFuncTestState(t, nil)
	st.InitForExecution()
	if err := st.Stack.PushCell(root); err != nil {
		t.Fatalf("PushCell failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := CDATASIZEQ().Interpret(st); err != nil {
		t.Fatalf("CDATASIZEQ failed: %v", err)
	}
	ok, err = st.Stack.PopBool()
	if err != nil || ok {
		t.Fatalf("CDATASIZEQ ok = (%v, %v), want false", ok, err)
	}

	unsignedBuilder := cell.BeginCell()
	okStore, err := storeVarInteger(unsignedBuilder, big.NewInt(255), 5, false, false)
	if err != nil || !okStore {
		t.Fatalf("storeVarInteger(unsigned) = (%v, %v)", okStore, err)
	}
	val, rest, ok := loadVarIntegerFromSlice(unsignedBuilder.ToSlice(), 5, false)
	if !ok || val.Int64() != 255 || rest.BitsLeft() != 0 {
		t.Fatalf("loadVarIntegerFromSlice(unsigned) = (%v, %v, %v)", val, rest, ok)
	}

	signedBuilder := cell.BeginCell()
	okStore, err = storeVarInteger(signedBuilder, big.NewInt(-2), 4, true, false)
	if err != nil || !okStore {
		t.Fatalf("storeVarInteger(signed) = (%v, %v)", okStore, err)
	}
	val, rest, ok = loadVarIntegerFromSlice(signedBuilder.ToSlice(), 4, true)
	if !ok || val.Int64() != -2 || rest.BitsLeft() != 0 {
		t.Fatalf("loadVarIntegerFromSlice(signed) = (%v, %v, %v)", val, rest, ok)
	}
	for _, tc := range []struct {
		name    string
		value   int64
		wantLen uint64
	}{
		{name: "zero", value: 0, wantLen: 0},
		{name: "min one byte", value: -128, wantLen: 1},
		{name: "max one byte", value: 127, wantLen: 1},
		{name: "below one byte", value: -129, wantLen: 2},
		{name: "above one byte", value: 128, wantLen: 2},
	} {
		t.Run("SignedVarIntegerLength/"+tc.name, func(t *testing.T) {
			b := cell.BeginCell()
			okStore, err := storeVarInteger(b, big.NewInt(tc.value), 4, true, false)
			if err != nil || !okStore {
				t.Fatalf("storeVarInteger(%d) = (%v, %v)", tc.value, okStore, err)
			}
			sl := b.ToSlice()
			if got := sl.MustLoadUInt(4); got != tc.wantLen {
				t.Fatalf("stored length = %d, want %d", got, tc.wantLen)
			}
			val, rest, ok := loadVarIntegerFromSlice(b.ToSlice(), 4, true)
			if !ok || val.Int64() != tc.value || rest.BitsLeft() != 0 {
				t.Fatalf("loadVarIntegerFromSlice(%d) = (%v, %v, %v)", tc.value, val, rest, ok)
			}
		})
	}

	if _, _, ok = loadVarIntegerFromSlice(cell.BeginCell().ToSlice(), 4, false); ok {
		t.Fatal("loading a varint from an empty slice should fail")
	}
	if _, err = storeVarInteger(cell.BeginCell(), nil, 4, false, false); err == nil {
		t.Fatal("storing a nil integer should fail")
	}
	if _, err = storeVarInteger(cell.BeginCell(), big.NewInt(-1), 4, false, false); err == nil {
		t.Fatal("unsigned varints should reject negative values")
	}
	fullBuilder := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xFF}, 128), 1023)
	if okStore, err = storeVarInteger(fullBuilder, big.NewInt(1), 5, false, true); err != nil || okStore {
		t.Fatalf("quiet store on a full builder = (%v, %v), want (false, nil)", okStore, err)
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(-2)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := STVARINT16().Interpret(st); err != nil {
		t.Fatalf("STVARINT16 failed: %v", err)
	}
	storedBuilder, err := st.Stack.PopBuilder()
	if err != nil {
		t.Fatalf("PopBuilder failed: %v", err)
	}
	st = newFuncTestState(t, nil)
	if err := st.Stack.PushSlice(storedBuilder.ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := LDVARINT16().Interpret(st); err != nil {
		t.Fatalf("LDVARINT16 failed: %v", err)
	}
	restSlice, err := st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	val, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite failed: %v", err)
	}
	if val.Int64() != -2 || restSlice.BitsLeft() != 0 {
		t.Fatalf("unexpected varint op round-trip: val=%v restBits=%d", val, restSlice.BitsLeft())
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushSlice(root.MustBeginParse()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(10)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SDATASIZE().Interpret(st); err != nil {
		t.Fatalf("SDATASIZE failed: %v", err)
	}
	refs, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(refs) failed: %v", err)
	}
	bits, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(bits) failed: %v", err)
	}
	cellsCnt, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(cells) failed: %v", err)
	}
	if refs.Int64() != 1 || bits.Int64() != 16 || cellsCnt.Int64() != 1 {
		t.Fatalf("unexpected SDATASIZE result: cells=%v bits=%v refs=%v", cellsCnt, bits, refs)
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushSlice(root.MustBeginParse()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SDATASIZEQ().Interpret(st); err != nil {
		t.Fatalf("SDATASIZEQ failed: %v", err)
	}
	ok, err = st.Stack.PopBool()
	if err != nil || ok {
		t.Fatalf("SDATASIZEQ ok = (%v, %v), want false", ok, err)
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(255)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := STVARUINT32().Interpret(st); err != nil {
		t.Fatalf("STVARUINT32 failed: %v", err)
	}
	storedBuilder, err = st.Stack.PopBuilder()
	if err != nil {
		t.Fatalf("PopBuilder failed: %v", err)
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushSlice(storedBuilder.ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := LDVARUINT32().Interpret(st); err != nil {
		t.Fatalf("LDVARUINT32 failed: %v", err)
	}
	restSlice, err = st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	val, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite failed: %v", err)
	}
	if val.Int64() != 255 || restSlice.BitsLeft() != 0 {
		t.Fatalf("unexpected unsigned varint32 round-trip: val=%v restBits=%d", val, restSlice.BitsLeft())
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(-200)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := STVARINT32().Interpret(st); err != nil {
		t.Fatalf("STVARINT32 failed: %v", err)
	}
	storedBuilder, err = st.Stack.PopBuilder()
	if err != nil {
		t.Fatalf("PopBuilder failed: %v", err)
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushSlice(storedBuilder.ToSlice()); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err := LDVARINT32().Interpret(st); err != nil {
		t.Fatalf("LDVARINT32 failed: %v", err)
	}
	restSlice, err = st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	val, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite failed: %v", err)
	}
	if val.Int64() != -200 || restSlice.BitsLeft() != 0 {
		t.Fatalf("unexpected signed varint32 round-trip: val=%v restBits=%d", val, restSlice.BitsLeft())
	}
}

func TestMessageAddressHelpersAndOps(t *testing.T) {
	if bits, ok := parseMaybeAnycast(cell.BeginCell().MustStoreUInt(0, 1).ToSlice(), vm.DefaultGlobalVersion); !ok || bits != nil {
		t.Fatalf("parseMaybeAnycast(false) = (%v, %v)", bits, ok)
	}
	anycast := cell.BeginCell().MustStoreUInt(1, 1).MustStoreUInt(3, 5).MustStoreUInt(0b101, 3).ToSlice()
	if _, ok := parseMaybeAnycast(anycast.Copy(), vm.DefaultGlobalVersion); ok {
		t.Fatal("parseMaybeAnycast(true) should fail on current global versions")
	}
	if bits, ok := parseMaybeAnycast(anycast.Copy(), 9); !ok || bits == nil || bits.BitsLeft() != 3 {
		t.Fatalf("parseMaybeAnycast(v9) = (%v, %v), want 3-bit prefix", bits, ok)
	}

	_, stdAddr, stdData := mustStdAddrSlice(t)
	_, extAddr := mustExtAddrSlice(t)
	noneAddr := cell.BeginCell().MustStoreAddr(address.NewAddressNone()).ToSlice()

	parsed, rest, ok := parseMessageAddress(noneAddr, vm.DefaultGlobalVersion)
	if !ok || parsed.Kind != 0 || rest.BitsLeft() != 0 {
		t.Fatalf("parseMessageAddress(none) = (%+v, %v, %v)", parsed, rest, ok)
	}

	parsed, rest, ok = parseMessageAddress(extAddr, vm.DefaultGlobalVersion)
	if !ok || parsed.Kind != 1 || parsed.Addr.BitsLeft() != 16 || rest.BitsLeft() != 0 {
		t.Fatalf("parseMessageAddress(ext) = (%+v, %v, %v)", parsed, rest, ok)
	}

	builder := cell.BeginCell().MustStoreAddr(address.NewAddress(0, 0, stdData)).MustStoreUInt(0xA, 4)
	src := builder.ToSlice()
	parsed, rest, ok = parseMessageAddress(src, vm.DefaultGlobalVersion)
	if !ok || parsed.Kind != 2 || parsed.Workchain != 0 || rest.BitsLeft() != 4 {
		t.Fatalf("parseMessageAddress(std) = (%+v, %v, %v)", parsed, rest, ok)
	}
	stdParsed := parsed
	stdRest := rest
	varAddr := cell.BeginCell().
		MustStoreUInt(0b11, 2).
		MustStoreUInt(0, 1).
		MustStoreUInt(8, 9).
		MustStoreInt(7, 32).
		MustStoreUInt(0xAB, 8).
		ToSlice()
	parsed, rest, ok = parseMessageAddress(varAddr, 9)
	if !ok || parsed.Kind != 3 || parsed.Workchain != 7 || parsed.Addr.BitsLeft() != 8 || rest.BitsLeft() != 0 {
		t.Fatalf("parseMessageAddress(v9 var) = (%+v, %v, %v)", parsed, rest, ok)
	}
	if parsed, _, ok = parseMessageAddress(varAddr, 10); ok || parsed != nil {
		t.Fatalf("parseMessageAddress(v10 var) = (%+v, %v), want failure", parsed, ok)
	}
	shortAnycastVarAddr := cell.BeginCell().
		MustStoreUInt(0b11, 2).
		MustStoreBoolBit(true).
		MustStoreUInt(4, 5).
		MustStoreUInt(0b1010, 4).
		MustStoreUInt(2, 9).
		MustStoreInt(0, 32).
		MustStoreUInt(0, 2).
		ToSlice()
	parsed, rest, ok = parseMessageAddress(shortAnycastVarAddr, 9)
	if !ok || parsed.Kind != 3 || parsed.Anycast.BitsLeft() != 4 || parsed.Addr.BitsLeft() != 2 || rest.BitsLeft() != 0 {
		t.Fatalf("parseMessageAddress(v9 short anycast var) = (%+v, %v, %v)", parsed, rest, ok)
	}
	{
		st := newFuncTestState(t, nil)
		st.GlobalVersion = 9
		if err := st.Stack.PushSlice(varAddr.Copy()); err != nil {
			t.Fatalf("PushSlice v9 var addr failed: %v", err)
		}
		if err := PARSEMSGADDR().Interpret(st); err != nil {
			t.Fatalf("PARSEMSGADDR v9 var addr failed: %v", err)
		}
		tup, err := st.Stack.PopTuple()
		if err != nil {
			t.Fatalf("PopTuple v9 var addr failed: %v", err)
		}
		if tup.Len() != 4 || mustTupleInt(t, tup, 0).Int64() != 3 || mustTupleInt(t, tup, 2).Int64() != 7 {
			t.Fatalf("unexpected PARSEMSGADDR v9 var tuple: len=%d", tup.Len())
		}

		st = newFuncTestState(t, nil)
		st.GlobalVersion = 10
		if err = st.Stack.PushSlice(varAddr.Copy()); err != nil {
			t.Fatalf("PushSlice v10 var addr failed: %v", err)
		}
		err = PARSEMSGADDR().Interpret(st)
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeCellUnderflow {
			t.Fatalf("PARSEMSGADDR v10 var addr error = %v, want cell underflow", err)
		}
	}
	{
		st := newFuncTestState(t, nil)
		st.GlobalVersion = 9
		if err := st.Stack.PushSlice(shortAnycastVarAddr.Copy()); err != nil {
			t.Fatalf("PushSlice v9 short anycast var failed: %v", err)
		}
		if err := REWRITEVARADDRQ().Interpret(st); err != nil {
			t.Fatalf("REWRITEVARADDRQ v9 short anycast var failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("REWRITEVARADDRQ v9 short anycast var ok = (%v, %v), want false", ok, err)
		}
		if st.Stack.Len() != 0 {
			t.Fatalf("REWRITEVARADDRQ v9 short anycast var left %d stack values", st.Stack.Len())
		}

		st = newFuncTestState(t, nil)
		st.GlobalVersion = 9
		if err = st.Stack.PushSlice(shortAnycastVarAddr.Copy()); err != nil {
			t.Fatalf("PushSlice v9 short anycast var failed: %v", err)
		}
		err = REWRITEVARADDR().Interpret(st)
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeCellUnderflow {
			t.Fatalf("REWRITEVARADDR v9 short anycast var error = %v, want cell underflow", err)
		}
		if st.Stack.Len() != 0 {
			t.Fatalf("REWRITEVARADDR v9 short anycast var left %d stack values", st.Stack.Len())
		}
	}
	{
		anycastStd := cell.BeginCell().
			MustStoreUInt(0b10, 2).
			MustStoreUInt(1, 1).
			MustStoreUInt(3, 5).
			MustStoreUInt(0b101, 3).
			MustStoreInt(0, 8).
			MustStoreUInt(0, 256).
			ToSlice()
		st := newFuncTestState(t, nil)
		st.GlobalVersion = 9
		if err := st.Stack.PushSlice(anycastStd.Copy()); err != nil {
			t.Fatalf("PushSlice v9 anycast addr failed: %v", err)
		}
		if err := REWRITESTDADDRQ().Interpret(st); err != nil {
			t.Fatalf("REWRITESTDADDRQ v9 anycast failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("REWRITESTDADDRQ v9 anycast ok = (%v, %v), want true", ok, err)
		}
		addrInt, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite v9 anycast failed: %v", err)
		}
		if want := new(big.Int).Lsh(big.NewInt(5), 253); addrInt.Cmp(want) != 0 {
			t.Fatalf("REWRITESTDADDRQ v9 anycast addr = %x, want %x", addrInt, want)
		}
		wc, err := st.Stack.PopIntFinite()
		if err != nil || wc.Sign() != 0 {
			t.Fatalf("REWRITESTDADDRQ v9 anycast wc = (%v, %v), want 0", wc, err)
		}
		if got := st.Gas.Used(); got != 0 {
			t.Fatalf("REWRITESTDADDRQ v9 anycast gas = %d, want 0", got)
		}

		st = newFuncTestState(t, nil)
		st.GlobalVersion = 9
		if err = st.Stack.PushSlice(anycastStd.Copy()); err != nil {
			t.Fatalf("PushSlice v9 anycast var rewrite failed: %v", err)
		}
		if err = REWRITEVARADDRQ().Interpret(st); err != nil {
			t.Fatalf("REWRITEVARADDRQ v9 anycast failed: %v", err)
		}
		if got, want := st.Gas.Used(), int64(vm.CellCreateGasPrice+vm.CellLoadGasPrice); got != want {
			t.Fatalf("REWRITEVARADDRQ v9 anycast gas = %d, want %d", got, want)
		}
		if ok, err = st.Stack.PopBool(); err != nil || !ok {
			t.Fatalf("REWRITEVARADDRQ v9 anycast ok = (%v, %v), want true", ok, err)
		}
		rewritten, err := st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice v9 anycast var rewrite failed: %v", err)
		}
		if rewritten.BitsLeft() != 256 {
			t.Fatalf("REWRITEVARADDRQ v9 anycast bits = %d, want 256", rewritten.BitsLeft())
		}
		if wc, err = st.Stack.PopIntFinite(); err != nil || wc.Sign() != 0 {
			t.Fatalf("REWRITEVARADDRQ v9 anycast wc = (%v, %v), want 0", wc, err)
		}

		st = newFuncTestState(t, nil)
		st.GlobalVersion = 10
		if err = st.Stack.PushSlice(anycastStd.Copy()); err != nil {
			t.Fatalf("PushSlice v10 anycast addr failed: %v", err)
		}
		if err = REWRITESTDADDRQ().Interpret(st); err != nil {
			t.Fatalf("REWRITESTDADDRQ v10 anycast failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("REWRITESTDADDRQ v10 anycast ok = (%v, %v), want false", ok, err)
		}
	}
	consumed, err := consumedPrefixSlice(src, stdRest)
	if err != nil {
		t.Fatalf("consumedPrefixSlice failed: %v", err)
	}
	if !isValidStdMsgAddr(consumed, vm.DefaultGlobalVersion) {
		t.Fatal("consumed std address should validate as MsgAddressInt")
	}

	st := newFuncTestState(t, nil)
	if err = pushParsedMessageTuple(st, stdParsed); err != nil {
		t.Fatalf("pushParsedMessageTuple failed: %v", err)
	}
	tup, err := st.Stack.PopTuple()
	if err != nil {
		t.Fatalf("PopTuple failed: %v", err)
	}
	if tup.Len() != 4 || mustTupleInt(t, tup, 0).Int64() != 2 || mustTupleInt(t, tup, 2).Int64() != 0 {
		t.Fatalf("unexpected parsed tuple: len=%d", tup.Len())
	}

	addrBits := cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice()
	prefix := cell.BeginCell().MustStoreUInt(0xF, 4).ToSlice()
	rewritten, ok := rewriteAddrBits(addrBits, prefix)
	if !ok || !bytes.Equal(mustSliceData(t, rewritten), []byte{0xFA}) {
		t.Fatalf("rewriteAddrBits = (%x, %v)", mustSliceData(t, rewritten), ok)
	}
	if _, ok = rewriteAddrBits(addrBits, cell.BeginCell().MustStoreUInt(0xFF, 12).ToSlice()); ok {
		t.Fatal("rewriteAddrBits should reject prefixes longer than the address")
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(src); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = LDMSGADDR().Interpret(st); err != nil {
		t.Fatalf("LDMSGADDR failed: %v", err)
	}
	restAfterLoad, err := st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice(rest) failed: %v", err)
	}
	addrSlice, err := st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice(addr) failed: %v", err)
	}
	if restAfterLoad.BitsLeft() != 4 || !isValidStdMsgAddr(addrSlice, vm.DefaultGlobalVersion) {
		t.Fatalf("unexpected LDMSGADDR result: restBits=%d", restAfterLoad.BitsLeft())
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(src); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = LDMSGADDRQ().Interpret(st); err != nil {
		t.Fatalf("LDMSGADDRQ failed: %v", err)
	}
	ok, err = st.Stack.PopBool()
	if err != nil || !ok {
		t.Fatalf("LDMSGADDRQ ok = (%v, %v), want true", ok, err)
	}
	restAfterLoad, err = st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice(rest) failed: %v", err)
	}
	addrSlice, err = st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice(addr) failed: %v", err)
	}
	if restAfterLoad.BitsLeft() != 4 || !isValidStdMsgAddr(addrSlice, vm.DefaultGlobalVersion) {
		t.Fatalf("unexpected LDMSGADDRQ result: restBits=%d", restAfterLoad.BitsLeft())
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(src); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = LDSTDADDR().Interpret(st); err != nil {
		t.Fatalf("LDSTDADDR failed: %v", err)
	}
	restAfterLoad, err = st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice(rest) failed: %v", err)
	}
	addrSlice, err = st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice(addr) failed: %v", err)
	}
	if restAfterLoad.BitsLeft() != 4 || !isValidStdMsgAddr(addrSlice, vm.DefaultGlobalVersion) {
		t.Fatalf("unexpected LDSTDADDR result: restBits=%d", restAfterLoad.BitsLeft())
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(extAddr); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = LDSTDADDRQ().Interpret(st); err != nil {
		t.Fatalf("LDSTDADDRQ failed: %v", err)
	}
	ok, err = st.Stack.PopBool()
	if err != nil || ok {
		t.Fatalf("LDSTDADDRQ ok = (%v, %v), want false", ok, err)
	}

	optSrc := cell.BeginCell().MustStoreAddr(address.NewAddressNone()).MustStoreUInt(0x5, 3).ToSlice()
	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(optSrc); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = LDOPTSTDADDR().Interpret(st); err != nil {
		t.Fatalf("LDOPTSTDADDR failed: %v", err)
	}
	restSlice, err := st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	raw, err := st.Stack.PopAny()
	if err != nil {
		t.Fatalf("PopAny failed: %v", err)
	}
	if raw != nil || restSlice.BitsLeft() != 3 {
		t.Fatalf("unexpected LDOPTSTDADDR result: raw=%v restBits=%d", raw, restSlice.BitsLeft())
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(optSrc); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = LDOPTSTDADDRQ().Interpret(st); err != nil {
		t.Fatalf("LDOPTSTDADDRQ failed: %v", err)
	}
	ok, err = st.Stack.PopBool()
	if err != nil || !ok {
		t.Fatalf("LDOPTSTDADDRQ ok = (%v, %v), want true", ok, err)
	}
	restSlice, err = st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	raw, err = st.Stack.PopAny()
	if err != nil {
		t.Fatalf("PopAny failed: %v", err)
	}
	if raw != nil || restSlice.BitsLeft() != 3 {
		t.Fatalf("unexpected LDOPTSTDADDRQ result: raw=%v restBits=%d", raw, restSlice.BitsLeft())
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(addrSlice); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = PARSEMSGADDR().Interpret(st); err != nil {
		t.Fatalf("PARSEMSGADDR failed: %v", err)
	}
	tup, err = st.Stack.PopTuple()
	if err != nil {
		t.Fatalf("PopTuple failed: %v", err)
	}
	if tup.Len() != 4 || mustTupleInt(t, tup, 0).Int64() != 2 {
		t.Fatalf("unexpected PARSEMSGADDR tuple: len=%d", tup.Len())
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(src); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = PARSEMSGADDRQ().Interpret(st); err != nil {
		t.Fatalf("PARSEMSGADDRQ failed: %v", err)
	}
	ok, err = st.Stack.PopBool()
	if err != nil || ok {
		t.Fatalf("PARSEMSGADDRQ ok = (%v, %v), want false", ok, err)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(addrSlice); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = REWRITESTDADDR().Interpret(st); err != nil {
		t.Fatalf("REWRITESTDADDR failed: %v", err)
	}
	addrInt, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(addr) failed: %v", err)
	}
	wcInt, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(wc) failed: %v", err)
	}
	if wcInt.Int64() != 0 || !bytes.Equal(addrInt.FillBytes(make([]byte, 32)), stdData) {
		t.Fatalf("unexpected rewritten address: wc=%v addr=%x", wcInt, addrInt.FillBytes(make([]byte, 32)))
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(addrSlice); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = REWRITEVARADDR().Interpret(st); err != nil {
		t.Fatalf("REWRITEVARADDR failed: %v", err)
	}
	rewrittenSlice, err := st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	wcInt, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(wc) failed: %v", err)
	}
	if wcInt.Int64() != 0 || !bytes.Equal(mustSliceData(t, rewrittenSlice), stdData) {
		t.Fatalf("unexpected REWRITEVARADDR result: wc=%v addr=%x", wcInt, mustSliceData(t, rewrittenSlice))
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(extAddr); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = REWRITESTDADDRQ().Interpret(st); err != nil {
		t.Fatalf("REWRITESTDADDRQ failed: %v", err)
	}
	ok, err = st.Stack.PopBool()
	if err != nil || ok {
		t.Fatalf("REWRITESTDADDRQ ok = (%v, %v), want false", ok, err)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(addrSlice); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = REWRITEVARADDRQ().Interpret(st); err != nil {
		t.Fatalf("REWRITEVARADDRQ failed: %v", err)
	}
	ok, err = st.Stack.PopBool()
	if err != nil || !ok {
		t.Fatalf("REWRITEVARADDRQ ok = (%v, %v), want true", ok, err)
	}
	rewrittenSlice, err = st.Stack.PopSlice()
	if err != nil {
		t.Fatalf("PopSlice failed: %v", err)
	}
	wcInt, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite(wc) failed: %v", err)
	}
	if wcInt.Int64() != 0 || !bytes.Equal(mustSliceData(t, rewrittenSlice), stdData) {
		t.Fatalf("unexpected REWRITEVARADDRQ result: wc=%v addr=%x", wcInt, mustSliceData(t, rewrittenSlice))
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(addrSlice); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = st.Stack.PushBuilder(cell.BeginCell()); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err = STSTDADDR().Interpret(st); err != nil {
		t.Fatalf("STSTDADDR failed: %v", err)
	}
	storedBuilder, err := st.Stack.PopBuilder()
	if err != nil {
		t.Fatalf("PopBuilder failed: %v", err)
	}
	if got := mustSliceData(t, storedBuilder.ToSlice()); len(got) == 0 {
		t.Fatal("STSTDADDR should serialize the address into the builder")
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushSlice(extAddr); err != nil {
		t.Fatalf("PushSlice failed: %v", err)
	}
	if err = st.Stack.PushBuilder(cell.BeginCell()); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err = STSTDADDRQ().Interpret(st); err != nil {
		t.Fatalf("STSTDADDRQ failed: %v", err)
	}
	ok, err = st.Stack.PopBool()
	if err != nil || !ok {
		t.Fatalf("STSTDADDRQ ok = (%v, %v), want true", ok, err)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushAny(nil); err != nil {
		t.Fatalf("PushAny(nil) failed: %v", err)
	}
	if err = st.Stack.PushBuilder(cell.BeginCell()); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err = STOPTSTDADDR().Interpret(st); err != nil {
		t.Fatalf("STOPTSTDADDR failed: %v", err)
	}
	storedBuilder, err = st.Stack.PopBuilder()
	if err != nil {
		t.Fatalf("PopBuilder failed: %v", err)
	}
	if storedBuilder.ToSlice().BitsLeft() != 2 {
		t.Fatalf("STOPTSTDADDR(nil) should append 2 bits, got %d", storedBuilder.ToSlice().BitsLeft())
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushAny(addrSlice); err != nil {
		t.Fatalf("PushAny failed: %v", err)
	}
	if err = st.Stack.PushBuilder(cell.BeginCell()); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err = STOPTSTDADDR().Interpret(st); err != nil {
		t.Fatalf("STOPTSTDADDR(valid) failed: %v", err)
	}
	storedBuilder, err = st.Stack.PopBuilder()
	if err != nil {
		t.Fatalf("PopBuilder failed: %v", err)
	}
	if storedBuilder.ToSlice().BitsLeft() <= 2 {
		t.Fatalf("STOPTSTDADDR(valid) should serialize a full address, got %d bits", storedBuilder.ToSlice().BitsLeft())
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushAny(big.NewInt(1)); err != nil {
		t.Fatalf("PushAny failed: %v", err)
	}
	if err = st.Stack.PushBuilder(cell.BeginCell()); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err = STOPTSTDADDRQ().Interpret(st); err != nil {
		t.Fatalf("STOPTSTDADDRQ failed: %v", err)
	}
	ok, err = st.Stack.PopBool()
	if err != nil || !ok {
		t.Fatalf("STOPTSTDADDRQ ok = (%v, %v), want true", ok, err)
	}
	if _, err = st.Stack.PopBuilder(); err != nil {
		t.Fatalf("PopBuilder failed: %v", err)
	}
	raw, err = st.Stack.PopAny()
	if err != nil {
		t.Fatalf("PopAny failed: %v", err)
	}
	if got, ok := raw.(*big.Int); !ok || got.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("quiet invalid STOPTSTDADDR should restore original value in v14, got %T %v", raw, raw)
	}

	st = newFuncTestState(t, nil)
	if err = st.Stack.PushAny(nil); err != nil {
		t.Fatalf("PushAny(nil) failed: %v", err)
	}
	if err = st.Stack.PushBuilder(cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xFF}, 128), 1023)); err != nil {
		t.Fatalf("PushBuilder failed: %v", err)
	}
	if err = STOPTSTDADDRQ().Interpret(st); err != nil {
		t.Fatalf("STOPTSTDADDRQ(full) failed: %v", err)
	}
	ok, err = st.Stack.PopBool()
	if err != nil || !ok {
		t.Fatalf("STOPTSTDADDRQ(full) = (%v, %v)", ok, err)
	}

	if got, err := addressFromSlice(stdAddr); err != nil || got.StringRaw() != address.NewAddress(0, 0, stdData).StringRaw() {
		t.Fatalf("addressFromSlice = (%v, %v)", got, err)
	}
	if _, err = addressFromSlice(nil); err == nil {
		t.Fatal("addressFromSlice should reject nil slices")
	}

	st = newFuncTestState(t, map[int]any{8: addrSlice})
	myAddr, err := getMyAddr(st)
	if err != nil || myAddr.StringRaw() != address.NewAddress(0, 0, stdData).StringRaw() {
		t.Fatalf("getMyAddr = (%v, %v)", myAddr, err)
	}

	cfg := tuple.NewTupleValue(
		nil, nil, nil, nil, nil, nil,
		cell.BeginCell().MustStoreUInt(0x01, 8).MustStoreUInt(7, 32).MustStoreUInt(123, 32).ToSlice(),
	)
	st = newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: cfg})
	if maxCells, err := getSizeLimitsMaxMsgCells(st); err != nil || maxCells != 123 {
		t.Fatalf("getSizeLimitsMaxMsgCells = (%d, %v)", maxCells, err)
	}
	st.GlobalVersion = 5
	if maxCells, err := getSizeLimitsMaxMsgCells(st); err != nil || maxCells != 1<<13 {
		t.Fatalf("pre-v6 getSizeLimitsMaxMsgCells = (%d, %v)", maxCells, err)
	}
	st.GlobalVersion = 6
	if maxCells, err := getSizeLimitsMaxMsgCells(st); err != nil || maxCells != 123 {
		t.Fatalf("v6 getSizeLimitsMaxMsgCells = (%d, %v)", maxCells, err)
	}
	if maxCells, err := getSizeLimitsMaxMsgCells(newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: tuple.NewTupleValue(nil, nil, nil, nil, nil, nil, nil)})); err != nil || maxCells != 1<<13 {
		t.Fatalf("getSizeLimitsMaxMsgCells(default) = (%d, %v)", maxCells, err)
	}
	cfg = tuple.NewTupleValue(
		nil, nil, nil, nil, nil, nil,
		cell.BeginCell().MustStoreUInt(0x02, 8).MustStoreUInt(9, 32).MustStoreUInt(321, 32).ToSlice(),
	)
	st = newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: cfg})
	if maxCells, err := getSizeLimitsMaxMsgCells(st); err != nil || maxCells != 321 {
		t.Fatalf("getSizeLimitsMaxMsgCells(v2) = (%d, %v)", maxCells, err)
	}
	if _, err := getSizeLimitsMaxMsgCells(newFuncTestState(t, map[int]any{paramIdxUnpackedConfig: tuple.NewTupleValue(nil, nil, nil, nil, nil, nil, cell.BeginCell().MustStoreUInt(0x03, 8).ToSlice())})); err == nil {
		t.Fatal("getSizeLimitsMaxMsgCells should reject invalid config tags")
	}
}

func TestActionInstallAndLibraryOps(t *testing.T) {
	st := newFuncTestState(t, nil)
	st.InitForExecution()

	if err := st.Stack.PushInt(big.NewInt(10)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(3)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := RAWRESERVE().Interpret(st); err != nil {
		t.Fatalf("RAWRESERVE failed: %v", err)
	}
	tag, err := st.Reg.D[1].MustBeginParse().LoadUInt(32)
	if err != nil || tag != 0x36e6b809 {
		t.Fatalf("unexpected RAWRESERVE tag: %x / %v", tag, err)
	}

	st = newFuncTestState(t, nil)
	st.InitForExecution()
	if err := st.Stack.PushInt(big.NewInt(10)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushAny(nil); err != nil {
		t.Fatalf("PushAny(nil) failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(4)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := RAWRESERVEX().Interpret(st); err != nil {
		t.Fatalf("RAWRESERVEX failed: %v", err)
	}

	code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	st = newFuncTestState(t, nil)
	st.InitForExecution()
	if err := st.Stack.PushCell(code); err != nil {
		t.Fatalf("PushCell failed: %v", err)
	}
	if err := SETCODE().Interpret(st); err != nil {
		t.Fatalf("SETCODE failed: %v", err)
	}
	tag, err = st.Reg.D[1].MustBeginParse().LoadUInt(32)
	if err != nil || tag != 0xAD4DE08E {
		t.Fatalf("unexpected SETCODE tag: %x / %v", tag, err)
	}

	st = newFuncTestState(t, nil)
	if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	mode, err := popLibMode(st)
	if err != nil || mode != 2 {
		t.Fatalf("popLibMode = (%v, %v)", mode, err)
	}
	if err := st.Stack.PushInt(big.NewInt(31)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if _, err = popLibMode(st); err == nil {
		t.Fatal("popLibMode should reject unsupported flag combinations")
	}

	st = newFuncTestState(t, nil)
	st.InitForExecution()
	if err := st.Stack.PushCell(code); err != nil {
		t.Fatalf("PushCell failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SETLIBCODE().Interpret(st); err != nil {
		t.Fatalf("SETLIBCODE failed: %v", err)
	}
	tag, err = st.Reg.D[1].MustBeginParse().LoadUInt(32)
	if err != nil || tag != 0x26FA1DD4 {
		t.Fatalf("unexpected SETLIBCODE tag: %x / %v", tag, err)
	}

	st = newFuncTestState(t, nil)
	st.InitForExecution()
	hash := new(big.Int).SetBytes(bytes.Repeat([]byte{0x22}, 32))
	if err := st.Stack.PushInt(hash); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(2)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := CHANGELIB().Interpret(st); err != nil {
		t.Fatalf("CHANGELIB failed: %v", err)
	}
	tag, err = st.Reg.D[1].MustBeginParse().LoadUInt(32)
	if err != nil || tag != 0x26FA1DD4 {
		t.Fatalf("unexpected CHANGELIB tag: %x / %v", tag, err)
	}
}

func TestActionModeVersionBoundaries(t *testing.T) {
	t.Run("RAWRESERVE mode 16 starts at v4", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		st.InitForExecution()
		st.GlobalVersion = 3
		if err := st.Stack.PushInt(big.NewInt(10)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(16)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		err := RAWRESERVE().Interpret(st)
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
			t.Fatalf("pre-v4 RAWRESERVE mode 16 error = %v, want range check", err)
		}

		st = newFuncTestState(t, nil)
		st.InitForExecution()
		st.GlobalVersion = 4
		if err = st.Stack.PushInt(big.NewInt(10)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err = st.Stack.PushInt(big.NewInt(16)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err = RAWRESERVE().Interpret(st); err != nil {
			t.Fatalf("v4 RAWRESERVE mode 16 failed: %v", err)
		}
	})

	t.Run("SETLIBCODE mode 16 starts at v4", func(t *testing.T) {
		code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		st := newFuncTestState(t, nil)
		st.InitForExecution()
		st.GlobalVersion = 3
		if err := st.Stack.PushCell(code); err != nil {
			t.Fatalf("PushCell failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(16)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		err := SETLIBCODE().Interpret(st)
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
			t.Fatalf("pre-v4 SETLIBCODE mode 16 error = %v, want range check", err)
		}

		st = newFuncTestState(t, nil)
		st.InitForExecution()
		st.GlobalVersion = 4
		if err = st.Stack.PushCell(code); err != nil {
			t.Fatalf("PushCell failed: %v", err)
		}
		if err = st.Stack.PushInt(big.NewInt(16)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err = SETLIBCODE().Interpret(st); err != nil {
			t.Fatalf("v4 SETLIBCODE mode 16 failed: %v", err)
		}
	})

	t.Run("CHANGELIB mode 16 starts at v4", func(t *testing.T) {
		hash := new(big.Int).SetBytes(bytes.Repeat([]byte{0x22}, 32))
		st := newFuncTestState(t, nil)
		st.InitForExecution()
		st.GlobalVersion = 3
		if err := st.Stack.PushInt(hash); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(16)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		err := CHANGELIB().Interpret(st)
		if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
			t.Fatalf("pre-v4 CHANGELIB mode 16 error = %v, want range check", err)
		}

		st = newFuncTestState(t, nil)
		st.InitForExecution()
		st.GlobalVersion = 4
		if err = st.Stack.PushInt(hash); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err = st.Stack.PushInt(big.NewInt(16)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err = CHANGELIB().Interpret(st); err != nil {
			t.Fatalf("v4 CHANGELIB mode 16 failed: %v", err)
		}
	})
}

func TestSendMsgAndTailStorage(t *testing.T) {
	myAddr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x11}, 32))
	dest := address.NewAddress(0, 0, bytes.Repeat([]byte{0x22}, 32))
	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		Bounced:     false,
		SrcAddr:     myAddr,
		DstAddr:     dest,
		Amount:      tlb.FromNanoTONU(100),
		IHRFee:      tlb.FromNanoTONU(0),
		FwdFee:      tlb.FromNanoTONU(50),
		CreatedLT:   1,
		CreatedAt:   2,
		Body:        cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
	})
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}

	stat := newStorageStat(10, nil)
	ok, err := addMessageTailStorage(stat, msgCell, 0)
	if err != nil || !ok {
		t.Fatalf("unexpected tail storage stats: %+v", stat)
	}

	st := makeSendMsgState(t, myAddr)
	beforeHash := st.Reg.D[1].Hash()
	if err := st.Stack.PushCell(msgCell); err != nil {
		t.Fatalf("PushCell failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(1024)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SENDMSG().Interpret(st); err != nil {
		t.Fatalf("SENDMSG(no-send) failed: %v", err)
	}
	fee, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite failed: %v", err)
	}
	if fee.Sign() <= 0 {
		t.Fatalf("unexpected SENDMSG fee: %v", fee)
	}
	if !bytes.Equal(st.Reg.D[1].Hash(), beforeHash) {
		t.Fatal("SENDMSG with +1024 flag should not install an action")
	}

	st = makeSendMsgState(t, myAddr)
	if err := st.Stack.PushCell(msgCell); err != nil {
		t.Fatalf("PushCell failed: %v", err)
	}
	if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err := SENDMSG().Interpret(st); err != nil {
		t.Fatalf("SENDMSG(send) failed: %v", err)
	}
	fee, err = st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite failed: %v", err)
	}
	if fee.Sign() <= 0 {
		t.Fatalf("unexpected SENDMSG fee: %v", fee)
	}
	tag, err := st.Reg.D[1].MustBeginParse().LoadUInt(32)
	if err != nil || tag != 0x0EC3C86D {
		t.Fatalf("unexpected SENDMSG action tag: %x / %v", tag, err)
	}
}

func TestSendMsgFeeOnlyMovesInlineBodyWhenRewrittenRootOverflows(t *testing.T) {
	myAddr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x33}, 32))
	body := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xAB}, 50), 400).EndCell()
	msgCell, err := tlb.ToCell(&tlb.InternalMessage{
		IHRDisabled: true,
		Bounce:      true,
		Bounced:     false,
		SrcAddr:     address.NewAddressNone(),
		DstAddr:     myAddr,
		Amount:      tlb.FromNanoTONU(100),
		IHRFee:      tlb.FromNanoTONU(0),
		FwdFee:      tlb.FromNanoTONU(0),
		CreatedLT:   1,
		CreatedAt:   2,
		Body:        body,
	})
	if err != nil {
		t.Fatalf("ToCell failed: %v", err)
	}

	stat := newStorageStat(10, nil)
	ok, err := addMessageTailStorage(stat, msgCell, 0)
	if err != nil || !ok {
		t.Fatalf("unexpected tail storage stats: %+v", stat)
	}
	if stat.cells != 0 || stat.bits != 0 {
		t.Fatalf("message body should be inline before SENDMSG rewrite, got %+v", stat)
	}

	st := makeSendMsgState(t, myAddr)
	if err = st.Stack.PushCell(msgCell); err != nil {
		t.Fatalf("PushCell failed: %v", err)
	}
	if err = st.Stack.PushInt(big.NewInt(1024)); err != nil {
		t.Fatalf("PushInt failed: %v", err)
	}
	if err = SENDMSG().Interpret(st); err != nil {
		t.Fatalf("SENDMSG(no-send) failed: %v", err)
	}
	fee, err := st.Stack.PopIntFinite()
	if err != nil {
		t.Fatalf("PopIntFinite failed: %v", err)
	}
	if want := big.NewInt(502); fee.Cmp(want) != 0 {
		t.Fatalf("unexpected SENDMSG fee: want %v, got %v", want, fee)
	}
}
