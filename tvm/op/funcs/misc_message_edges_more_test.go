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

func makeMsgPricesSlice(lump, bitPrice, cellPrice uint64) *cell.Slice {
	return cell.BeginCell().
		MustStoreUInt(0xEA, 8).
		MustStoreUInt(lump, 64).
		MustStoreUInt(bitPrice, 64).
		MustStoreUInt(cellPrice, 64).
		MustStoreUInt(0, 32).
		MustStoreUInt(0, 16).
		MustStoreUInt(0, 16).
		ToSlice()
}

func makeSendMsgEdgeState(t *testing.T, myAddr *address.Address, mcPrices, wcPrices, sizeLimit *cell.Slice) *vm.State {
	t.Helper()

	cfg := tuple.NewTupleSized(7)
	if mcPrices != nil {
		if err := cfg.Set(4, mcPrices); err != nil {
			t.Fatalf("failed to set masterchain msg prices: %v", err)
		}
	}
	if wcPrices != nil {
		if err := cfg.Set(5, wcPrices); err != nil {
			t.Fatalf("failed to set workchain msg prices: %v", err)
		}
	}
	if sizeLimit != nil {
		if err := cfg.Set(6, sizeLimit); err != nil {
			t.Fatalf("failed to set size limit config: %v", err)
		}
	}

	st := newFuncTestState(t, map[int]any{
		paramIdxUnpackedConfig: cfg,
		7:                      tuple.NewTupleValue(big.NewInt(1000), makeExtraBalanceDict(t, map[uint32]uint64{7: 55})),
		8:                      cell.BeginCell().MustStoreAddr(myAddr).ToSlice(),
	})
	st.InitForExecution()
	return st
}

func makeNonEmptyExtraCurrencies(t *testing.T) *cell.Dictionary {
	t.Helper()

	dict := cell.NewDict(32)
	key := cell.BeginCell().MustStoreUInt(1, 32).EndCell()
	val := cell.BeginCell().MustStoreVarUInt(2, 32).EndCell()
	if err := dict.Set(key, val); err != nil {
		t.Fatalf("failed to set extra currencies entry: %v", err)
	}
	return dict
}

func TestMiscMessageMoreParsedTupleAndRewrites(t *testing.T) {
	t.Run("push parsed message tuple covers remaining kinds", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := pushParsedMessageTuple(st, &parsedMsgAddress{Kind: 0}); err != nil {
			t.Fatalf("pushParsedMessageTuple(kind0) failed: %v", err)
		}
		tup, err := st.Stack.PopTuple()
		if err != nil {
			t.Fatalf("PopTuple failed: %v", err)
		}
		if tup.Len() != 1 || mustTupleInt(t, tup, 0).Int64() != 0 {
			t.Fatalf("unexpected kind0 tuple: len=%d", tup.Len())
		}

		addrExt := cell.BeginCell().MustStoreUInt(0xAB, 8).ToSlice()
		st = newFuncTestState(t, nil)
		if err := pushParsedMessageTuple(st, &parsedMsgAddress{Kind: 1, Addr: addrExt}); err != nil {
			t.Fatalf("pushParsedMessageTuple(kind1) failed: %v", err)
		}
		tup, err = st.Stack.PopTuple()
		if err != nil {
			t.Fatalf("PopTuple failed: %v", err)
		}
		if tup.Len() != 2 {
			t.Fatalf("unexpected kind1 tuple len: %d", tup.Len())
		}

		stdAddr := cell.BeginCell().MustStoreUInt(0xCD, 8).MustStoreUInt(0, 248).ToSlice()
		st = newFuncTestState(t, nil)
		if err := pushParsedMessageTuple(st, &parsedMsgAddress{Kind: 2, Workchain: -1, Addr: stdAddr}); err != nil {
			t.Fatalf("pushParsedMessageTuple(kind2) failed: %v", err)
		}
		tup, err = st.Stack.PopTuple()
		if err != nil {
			t.Fatalf("PopTuple failed: %v", err)
		}
		if tup.Len() != 4 || mustTupleInt(t, tup, 0).Int64() != 2 || mustTupleInt(t, tup, 2).Int64() != -1 {
			t.Fatalf("unexpected kind2 tuple: len=%d", tup.Len())
		}
	})

	t.Run("rewrite std addr rejects variable-length internal addresses", func(t *testing.T) {
		varAddr := address.NewAddressVar(0, 0, 20, []byte{0xDE, 0xA0, 0x00})
		src := cell.BeginCell().MustStoreAddr(varAddr).ToSlice()

		st := newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(src); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := REWRITESTDADDRQ().Interpret(st); err != nil {
			t.Fatalf("REWRITESTDADDRQ(var) failed: %v", err)
		}
		ok, err := st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("REWRITESTDADDRQ(var) = (%v, %v), want false", ok, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushSlice(src); err != nil {
			t.Fatalf("PushSlice failed: %v", err)
		}
		if err := REWRITESTDADDR().Interpret(st); err == nil {
			t.Fatal("REWRITESTDADDR should reject variable-length internal addresses")
		}
	})
}

func TestMiscMessageMoreStoreAndSendMsgBranches(t *testing.T) {
	t.Run("optional std addr covers non-slice and quiet nil branches", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := STSTDADDR().Interpret(st); err == nil {
			t.Fatal("STSTDADDR should fail when the builder is missing")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STSTDADDR().Interpret(st); err == nil {
			t.Fatal("STSTDADDR should fail when the source address is missing")
		}

		_, stdAddr, _ := mustStdAddrSlice(t)
		fullBuilder := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xFF}, 128), 1023)
		st = newFuncTestState(t, nil)
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
		if restoredBuilder, err := st.Stack.PopBuilder(); err != nil || restoredBuilder.ToSlice().BitsLeft() != 1023 {
			t.Fatalf("unexpected restored builder: (%v, %v)", restoredBuilder, err)
		}
		if restoredAddr, err := st.Stack.PopSlice(); err != nil || !isValidStdMsgAddr(restoredAddr) {
			t.Fatalf("unexpected restored addr: (%v, %v)", restoredAddr, err)
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushAny(big.NewInt(1)); err != nil {
			t.Fatalf("PushAny failed: %v", err)
		}
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STOPTSTDADDR().Interpret(st); err == nil {
			t.Fatal("STOPTSTDADDR should reject non-slice stack values")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushAny(nil); err != nil {
			t.Fatalf("PushAny(nil) failed: %v", err)
		}
		if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STOPTSTDADDRQ().Interpret(st); err != nil {
			t.Fatalf("STOPTSTDADDRQ(nil) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || ok {
			t.Fatalf("STOPTSTDADDRQ(nil) = (%v, %v), want false", ok, err)
		}
		builder, err := st.Stack.PopBuilder()
		if err != nil {
			t.Fatalf("PopBuilder failed: %v", err)
		}
		if builder.ToSlice().BitsLeft() != 2 || !bytes.Equal(mustSliceData(t, builder.ToSlice()), []byte{0x00}) {
			t.Fatalf("unexpected nil optional address encoding: bits=%d data=%x", builder.ToSlice().BitsLeft(), mustSliceData(t, builder.ToSlice()))
		}

		fullBuilder = cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xFF}, 128), 1023)
		st = newFuncTestState(t, nil)
		if err := st.Stack.PushAny(nil); err != nil {
			t.Fatalf("PushAny(nil) failed: %v", err)
		}
		if err := st.Stack.PushBuilder(fullBuilder); err != nil {
			t.Fatalf("PushBuilder failed: %v", err)
		}
		if err := STOPTSTDADDRQ().Interpret(st); err != nil {
			t.Fatalf("STOPTSTDADDRQ(full,nil) failed: %v", err)
		}
		ok, err = st.Stack.PopBool()
		if err != nil || !ok {
			t.Fatalf("STOPTSTDADDRQ(full,nil) = (%v, %v), want true", ok, err)
		}
		if restoredBuilder, err := st.Stack.PopBuilder(); err != nil || restoredBuilder.ToSlice().BitsLeft() != 1023 {
			t.Fatalf("unexpected restored builder: (%v, %v)", restoredBuilder, err)
		}
		if restored, err := st.Stack.PopAny(); err != nil || restored != nil {
			t.Fatalf("unexpected restored optional value: (%v, %v)", restored, err)
		}

		for _, tc := range []struct {
			name        string
			version     int
			wantRestore any
		}{
			{name: "v13 restores null", version: 13, wantRestore: nil},
			{name: "v14 keeps invalid value", version: 14, wantRestore: big.NewInt(100)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				st := newFuncTestState(t, nil)
				st.GlobalVersion = tc.version
				if err := st.Stack.PushInt(big.NewInt(100)); err != nil {
					t.Fatalf("PushInt failed: %v", err)
				}
				if err := st.Stack.PushBuilder(cell.BeginCell()); err != nil {
					t.Fatalf("PushBuilder failed: %v", err)
				}
				if err := STOPTSTDADDRQ().Interpret(st); err != nil {
					t.Fatalf("STOPTSTDADDRQ(int) failed: %v", err)
				}
				ok, err := st.Stack.PopBool()
				if err != nil || !ok {
					t.Fatalf("STOPTSTDADDRQ(int) = (%v, %v), want true", ok, err)
				}
				if _, err = st.Stack.PopBuilder(); err != nil {
					t.Fatalf("PopBuilder failed: %v", err)
				}
				restored, err := st.Stack.PopAny()
				if err != nil {
					t.Fatalf("PopAny failed: %v", err)
				}
				want, _ := tc.wantRestore.(*big.Int)
				got, _ := restored.(*big.Int)
				if want == nil {
					if restored != nil {
						t.Fatalf("restored value = %v, want nil", restored)
					}
				} else if got == nil || got.Cmp(want) != 0 {
					t.Fatalf("restored value = %v, want %v", restored, want)
				}
			})
		}
	})

	t.Run("sendmsg v13 uses user fwd fee lower bound and v14 ignores it", func(t *testing.T) {
		myAddr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x11}, 32))
		dest := address.NewAddress(0, 0, bytes.Repeat([]byte{0x22}, 32))

		msgCell, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     myAddr,
			DstAddr:     dest,
			Amount:      tlb.FromNanoTONU(100),
			IHRFee:      tlb.FromNanoTONU(0),
			FwdFee:      tlb.FromNanoTONU(500),
			Body:        cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}

		for _, tc := range []struct {
			version int
			want    int64
		}{
			{version: 13, want: 500},
			{version: 14, want: 1},
		} {
			st := makeSendMsgEdgeState(t, myAddr, nil, makeMsgPricesSlice(1, 0, 0), nil)
			st.GlobalVersion = tc.version
			if err := st.Stack.PushCell(msgCell); err != nil {
				t.Fatalf("PushCell failed: %v", err)
			}
			if err := st.Stack.PushInt(big.NewInt(1024)); err != nil {
				t.Fatalf("PushInt failed: %v", err)
			}
			if err := SENDMSG().Interpret(st); err != nil {
				t.Fatalf("SENDMSG(version %d fee-only) failed: %v", tc.version, err)
			}
			fee, err := st.Stack.PopIntFinite()
			if err != nil {
				t.Fatalf("PopIntFinite failed: %v", err)
			}
			if fee.Cmp(big.NewInt(tc.want)) != 0 {
				t.Fatalf("SENDMSG version %d fee = %v, want %d", tc.version, fee, tc.want)
			}
		}
	})

	t.Run("sendmsg enforces size limits after skipping extra-currency refs", func(t *testing.T) {
		myAddr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x33}, 32))
		dest := address.NewAddress(0, 0, bytes.Repeat([]byte{0x44}, 32))
		sizeLimit := cell.BeginCell().
			MustStoreUInt(0x01, 8).
			MustStoreUInt(0, 32).
			MustStoreUInt(0, 32).
			ToSlice()
		st := makeSendMsgEdgeState(t, myAddr, nil, makeMsgPricesSlice(1, 0, 0), sizeLimit)

		body := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xAA}, 125), 1000).EndCell()
		msgCell, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled:     true,
			SrcAddr:         myAddr,
			DstAddr:         dest,
			Amount:          tlb.FromNanoTONU(100),
			ExtraCurrencies: makeNonEmptyExtraCurrencies(t),
			IHRFee:          tlb.FromNanoTONU(0),
			FwdFee:          tlb.FromNanoTONU(0),
			Body:            body,
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}
		if refs := msgCell.MustBeginParse().RefsNum(); refs < 2 {
			t.Fatalf("test message should have at least two refs, got %d", refs)
		}

		if err := st.Stack.PushCell(msgCell); err != nil {
			t.Fatalf("PushCell failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(1024)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := SENDMSG().Interpret(st); err == nil {
			t.Fatal("SENDMSG should fail when the tail exceeds the configured max cell limit")
		}
	})

	t.Run("sendmsg rejects non-internal myaddr", func(t *testing.T) {
		dest := address.NewAddress(0, 0, bytes.Repeat([]byte{0x44}, 32))
		msgCell, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     address.NewAddressNone(),
			DstAddr:     dest,
			Amount:      tlb.FromNanoTONU(100),
			Body:        cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}

		for _, myAddr := range []*address.Address{
			address.NewAddressNone(),
			address.NewAddressExt(0, 16, []byte{0xAB, 0xCD}),
		} {
			st := makeSendMsgEdgeState(t, myAddr, nil, makeMsgPricesSlice(1, 0, 0), nil)
			for _, version := range []int{13, 14} {
				st.GlobalVersion = version
				if err := st.Stack.PushCell(msgCell); err != nil {
					t.Fatalf("PushCell failed: %v", err)
				}
				if err := st.Stack.PushInt(big.NewInt(1024)); err != nil {
					t.Fatalf("PushInt failed: %v", err)
				}
				err := SENDMSG().Interpret(st)
				if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeRangeCheck {
					t.Fatalf("SENDMSG version %d with MYADDR %s exit = (%d, %t), want range_chk", version, myAddr.String(), code, ok)
				}
			}
		}
	})

	t.Run("raw reserve ops enforce stack arity", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		if err := RAWRESERVE().Interpret(st); err == nil {
			t.Fatal("RAWRESERVE should fail when the mode is missing")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RAWRESERVE().Interpret(st); err == nil {
			t.Fatal("RAWRESERVE should fail when the amount is missing")
		}

		st = newFuncTestState(t, nil)
		if err := RAWRESERVEX().Interpret(st); err == nil {
			t.Fatal("RAWRESERVEX should fail when the mode is missing")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RAWRESERVEX().Interpret(st); err == nil {
			t.Fatal("RAWRESERVEX should fail when the extra-currency cell is missing")
		}

		st = newFuncTestState(t, nil)
		if err := st.Stack.PushAny(nil); err != nil {
			t.Fatalf("PushAny(nil) failed: %v", err)
		}
		if err := st.Stack.PushInt(big.NewInt(0)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err := RAWRESERVEX().Interpret(st); err == nil {
			t.Fatal("RAWRESERVEX should fail when the amount is missing")
		}
	})
}
