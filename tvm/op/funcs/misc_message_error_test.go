package funcs

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestMiscMessageAdditionalPaths(t *testing.T) {
	_, stdAddr, _ := mustStdAddrSlice(t)
	_, extAddr := mustExtAddrSlice(t)

	t.Run("VarIntAndSizeErrors", func(t *testing.T) {
		root := cell.BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()).EndCell()

		st := newFuncTestState(t, nil)
		if err := st.Stack.PushCell(root); err != nil {
			t.Fatalf("PushCell failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := CDATASIZE().Interpret(st); err == nil {
			t.Fatal("CDATASIZE should fail when the cell limit is too small")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := LDVARUINT32().Interpret(st); err == nil {
			t.Fatal("LDVARUINT32 should reject empty slices")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := STVARUINT32().Interpret(st); err == nil {
			t.Fatal("STVARUINT32 should reject negative values")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushBuilder(cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xFF}, 128), 1023)); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := STVARINT32().Interpret(st); err == nil {
			t.Fatal("STVARINT32 should reject builders without space")
		}

		if _, err := loadSliceBits(cell.BeginCell().MustStoreUInt(0xAA, 8).ToSlice(), 16); err == nil {
			t.Fatal("loadSliceBits should reject short slices")
		}
	})

	t.Run("TupleAndLoadFailures", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := pushParsedMessageTuple(st, &parsedMsgAddress{Kind: 9}); err == nil {
			t.Fatal("pushParsedMessageTuple should reject unknown kinds")
		}

		varAddr := address.NewAddressVar(0, 0, 20, []byte{0xDE, 0xA0, 0x00})
		st = newFuncTestState(t, nil)
		if err := pushParsedMessageTuple(st, &parsedMsgAddress{
			Kind:      3,
			Workchain: 0,
			Addr:      cell.BeginCell().MustStoreAddr(varAddr).ToSlice(),
		}); err != nil {
			t.Fatalf("pushParsedMessageTuple(var) failed: %v", err)
		}
		tup, err := st.Stack.PopTuple()
		if err != nil {
			t.Fatalf("PopTuple failed: %v", err)
		}
		if tup.Len() != 4 || mustTupleInt(t, tup, 0).Int64() != 3 {
			t.Fatalf("unexpected var-address tuple: len=%d", tup.Len())
		}

		short := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()
		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(short); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := LDMSGADDRQ().Interpret(st); err != nil {
			t.Fatalf("LDMSGADDRQ(short) failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("LDMSGADDRQ(short) = (%v, %v), want false", ok, err)
		}
		rest, err := st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		if rest.BitsLeft() != short.BitsLeft() {
			t.Fatalf("LDMSGADDRQ should restore the original slice, got %d bits", rest.BitsLeft())
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(short); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := LDOPTSTDADDR().Interpret(st); err == nil {
			t.Fatal("LDOPTSTDADDR should reject short slices")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(short); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := LDOPTSTDADDRQ().Interpret(st); err != nil {
			t.Fatalf("LDOPTSTDADDRQ(short) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("LDOPTSTDADDRQ(short) = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(extAddr); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := LDSTDADDR().Interpret(st); err == nil {
			t.Fatal("LDSTDADDR should reject non-standard addresses")
		}

		if parsed, _, ok := parseMessageAddress(cell.BeginCell().MustStoreUInt(0b11, 2).ToSlice()); ok || parsed != nil {
			t.Fatalf("parseMessageAddress(type=3) = (%v, %v), want failure", parsed, ok)
		}
		if parsed, _, ok := parseMessageAddress(cell.BeginCell().MustStoreUInt(0b10, 2).MustStoreUInt(1, 1).ToSlice()); ok || parsed != nil {
			t.Fatalf("parseMessageAddress(anycast) = (%v, %v), want failure", parsed, ok)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreAddr(address.NewAddressNone()).MustStoreUInt(1, 1).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := PARSEMSGADDR().Interpret(st); err == nil {
			t.Fatal("PARSEMSGADDR should reject trailing bits")
		}
	})

	t.Run("RewriteAndStoreFailures", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(extAddr); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := REWRITESTDADDR().Interpret(st); err == nil {
			t.Fatal("REWRITESTDADDR should reject non-MsgAddressInt values")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(extAddr); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STSTDADDR().Interpret(st); err == nil {
			t.Fatal("STSTDADDR should reject non-standard addresses")
		}

		st = newFuncTestState(t, nil)
		fullBuilder := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xFF}, 128), 1023)
		if err := st.Stack.PushSlice(stdAddr); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushBuilder(fullBuilder); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STSTDADDRQ().Interpret(st); err != nil {
			t.Fatalf("STSTDADDRQ(full) failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("STSTDADDRQ(full) = (%v, %v), want true", ok, err)
		}
		if _, err := st.Stack.PopBuilder(); err != nil {
			t.Fatalf("PopBuilder failed: %v", err)
		}
		restored, err := st.Stack.PopSlice()
		if err != nil {
			t.Fatalf("PopSlice failed: %v", err)
		}
		if !isValidStdMsgAddr(restored) {
			t.Fatalf("STSTDADDRQ should restore the source address, got %x", mustSliceData(t, restored))
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushAny(big.NewInt(1)); err != nil {
			t.Fatalf("PushAny failed: %v", err)
		}
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STOPTSTDADDR().Interpret(st); err == nil {
			t.Fatal("STOPTSTDADDR should reject non-slice values")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushAny(nil); err != nil {
			t.Fatalf("PushAny(nil) failed: %v", err)
		}
		if err := st.Stack.PushBuilder(fullBuilder); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STOPTSTDADDR().Interpret(st); err == nil {
			t.Fatal("STOPTSTDADDR should reject full builders even for nil values")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushAny(stdAddr); err != nil {
			t.Fatalf("PushAny failed: %v", err)
		}
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STOPTSTDADDRQ().Interpret(st); err != nil {
			t.Fatalf("STOPTSTDADDRQ(valid) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("STOPTSTDADDRQ(valid) = (%v, %v), want false", ok, err)
		}
		builder, err := st.Stack.PopBuilder()
		if err != nil {
			t.Fatalf("PopBuilder failed: %v", err)
		}
		if builder.ToSlice().BitsLeft() <= 2 {
			t.Fatalf("STOPTSTDADDRQ(valid) should serialize an address, got %d bits", builder.ToSlice().BitsLeft())
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(stdAddr); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STSTDADDRQ().Interpret(st); err != nil {
			t.Fatalf("STSTDADDRQ(valid) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("STSTDADDRQ(valid) = (%v, %v), want false", ok, err)
		}
		if _, err := st.Stack.PopBuilder(); err != nil {
			t.Fatalf("PopBuilder failed: %v", err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(stdAddr); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := st.Stack.PushBuilder(cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xFF}, 128), 1023)); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STSTDADDR().Interpret(st); err == nil {
			t.Fatal("STSTDADDR should reject builders without room")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushAny(stdAddr); err != nil {
			t.Fatalf("PushAny failed: %v", err)
		}
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STOPTSTDADDR().Interpret(st); err != nil {
			t.Fatalf("STOPTSTDADDR(valid) failed: %v", err)
		}
		if _, err := st.Stack.PopBuilder(); err != nil {
			t.Fatalf("PopBuilder failed: %v", err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushAny(extAddr); err != nil {
			t.Fatalf("PushAny failed: %v", err)
		}
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STOPTSTDADDRQ().Interpret(st); err != nil {
			t.Fatalf("STOPTSTDADDRQ(invalid addr) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("STOPTSTDADDRQ(invalid addr) = (%v, %v), want true", ok, err)
		}
		if _, err := st.Stack.PopBuilder(); err != nil {
			t.Fatalf("PopBuilder failed: %v", err)
		}
		if restored, err := st.Stack.PopSlice(); err != nil || restored == nil {
			t.Fatalf("STOPTSTDADDRQ(invalid addr) restore = (%v, %v)", restored, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(cell.BeginCell().MustStoreAddr(address.NewAddressNone()).ToSlice()); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := REWRITEVARADDR().Interpret(st); err == nil {
			t.Fatal("REWRITEVARADDR should reject addr_none")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(extAddr); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := REWRITEVARADDRQ().Interpret(st); err != nil {
			t.Fatalf("REWRITEVARADDRQ(ext) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("REWRITEVARADDRQ(ext) = (%v, %v), want false", ok, err)
		}
	})

	t.Run("ActionErrors", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := installAction(st, func(*cell.Builder) error { return cell.ErrTooMuchRefs }); err == nil {
			t.Fatal("installAction should wrap cell overflow errors")
		}
		if err := installAction(st, func(*cell.Builder) error { return errors.New("boom") }); err == nil {
			t.Fatal("installAction should return generic builder errors")
		}

		stat := newStorageStat(10, nil)
		if addMessageTailStorage(stat, cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(), 1) {
			t.Fatal("addMessageTailStorage should fail when skipping too many refs")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RAWRESERVE().Interpret(st); err == nil {
			t.Fatal("RAWRESERVE should reject negative amounts")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushAny(nil); err != nil {
			t.Fatalf("PushAny(nil) failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RAWRESERVEX().Interpret(st); err == nil {
			t.Fatal("RAWRESERVEX should reject negative amounts")
		}

		code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
		st = newFuncTestState(t, nil)
		if err := st.Stack.PushCell(code); err != nil {
			t.Fatalf("PushCell failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(31)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := SETLIBCODE().Interpret(st); err == nil {
			t.Fatal("SETLIBCODE should reject unsupported modes")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(-1)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := CHANGELIB().Interpret(st); err == nil {
			t.Fatal("CHANGELIB should reject negative hashes")
		}

		st = newFuncTestState(t, map[int]any{8: big.NewInt(1)})
		if _, err := getMyAddr(st); err == nil {
			t.Fatal("getMyAddr should reject non-slice params")
		}

	})
}
