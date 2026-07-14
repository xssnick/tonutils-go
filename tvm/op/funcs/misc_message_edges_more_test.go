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

func makeMsgPricesFullSlice(lump, bitPrice, cellPrice uint64, ihrFactor uint32, firstFrac, nextFrac uint16) *cell.Slice {
	return cell.BeginCell().
		MustStoreUInt(0xEA, 8).
		MustStoreUInt(lump, 64).
		MustStoreUInt(bitPrice, 64).
		MustStoreUInt(cellPrice, 64).
		MustStoreUInt(uint64(ihrFactor), 32).
		MustStoreUInt(uint64(firstFrac), 16).
		MustStoreUInt(uint64(nextFrac), 16).
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

func TestSendMsgTupleAmountVersionedExtraSemantics(t *testing.T) {
	extra := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	tests := []struct {
		name       string
		version    int
		param      any
		wantAmount int64
		wantNaN    bool
		wantExtra  bool
		wantErr    bool
		wantCode   int64
	}{
		{
			name:       "legacy keeps non-empty extra",
			version:    9,
			param:      tuple.NewTupleValue(big.NewInt(123), extra),
			wantAmount: 123,
			wantExtra:  true,
		},
		{
			name:       "legacy nil extra is empty",
			version:    9,
			param:      tuple.NewTupleValue(big.NewInt(124), nil),
			wantAmount: 124,
		},
		{
			name:       "legacy non-cell extra is empty",
			version:    9,
			param:      tuple.NewTupleValue(big.NewInt(124), big.NewInt(1)),
			wantAmount: 124,
		},
		{
			name:       "v10 ignores second tuple item",
			version:    10,
			param:      tuple.NewTupleValue(big.NewInt(125), extra),
			wantAmount: 125,
		},
		{
			name:     "non tuple param",
			version:  9,
			param:    big.NewInt(1),
			wantErr:  true,
			wantCode: vmerr.CodeTypeCheck,
		},
		{
			name:     "amount must be int",
			version:  9,
			param:    tuple.NewTupleValue("bad", nil),
			wantErr:  true,
			wantCode: vmerr.CodeTypeCheck,
		},
		{
			name:    "missing amount",
			version: 9,
			param:   tuple.Tuple{},
			wantErr: true,
		},
		{
			name:    "legacy requires extra slot",
			version: 9,
			param:   tuple.NewTupleValue(big.NewInt(126)),
			wantErr: true,
		},
		{
			name:       "v10 accepts missing extra slot",
			version:    10,
			param:      tuple.NewTupleValue(big.NewInt(127)),
			wantAmount: 127,
		},
		{
			name:      "legacy accepts NaN integer tag",
			version:   9,
			param:     tuple.NewTupleValue(vm.NaN{}, nil),
			wantNaN:   true,
			wantExtra: false,
		},
		{
			name:    "v10 accepts NaN integer tag",
			version: 10,
			param:   tuple.NewTupleValue(vm.NaN{}),
			wantNaN: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newFuncTestState(t, map[int]any{7: tc.param})
			st.GlobalVersion = tc.version

			amount, amountNaN, hasExtra, err := sendMsgTupleAmount(st, 7, "BALANCE")
			if tc.wantErr {
				if err == nil {
					t.Fatal("sendMsgTupleAmount should fail")
				}
				if tc.wantCode != 0 {
					if code, ok := vmerr.ErrorCode(err); !ok || code != tc.wantCode {
						t.Fatalf("sendMsgTupleAmount exit = (%d, %t), want %d", code, ok, tc.wantCode)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("sendMsgTupleAmount failed: %v", err)
			}
			if amountNaN != tc.wantNaN || hasExtra != tc.wantExtra {
				t.Fatalf("sendMsgTupleAmount = (%v, NaN=%t, extra=%t), want (NaN=%t, extra=%t)", amount, amountNaN, hasExtra, tc.wantNaN, tc.wantExtra)
			}
			if !amountNaN && amount.Int64() != tc.wantAmount {
				t.Fatalf("sendMsgTupleAmount amount = %s, want %d", amount, tc.wantAmount)
			}
		})
	}

	t.Run("missing param", func(t *testing.T) {
		st := newFuncTestState(t, nil)
		st.GlobalVersion = 9
		if _, _, _, err := sendMsgTupleAmount(st, 7, "BALANCE"); err == nil {
			t.Fatal("sendMsgTupleAmount should fail when the c7 slot is absent")
		}
	})
}

func TestLoadSendMsgLayoutMalformedEdges(t *testing.T) {
	externalHeader := func() *cell.Builder {
		return cell.BeginCell().
			MustStoreBoolBit(true).
			MustStoreBoolBit(true).
			MustStoreAddr(nil).
			MustStoreAddr(nil).
			MustStoreUInt(1, 64).
			MustStoreUInt(2, 32)
	}
	internalHeader := func() *cell.Builder {
		return cell.BeginCell().
			MustStoreBoolBit(false).
			MustStoreUInt(0, 3).
			MustStoreAddr(nil).
			MustStoreAddr(nil).
			MustStoreBigCoins(big.NewInt(1)).
			MustStoreBoolBit(false).
			MustStoreVarUInt(0, 16).
			MustStoreBigCoins(big.NewInt(0)).
			MustStoreUInt(1, 64).
			MustStoreUInt(2, 32)
	}

	validExternalOut := externalHeader().
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		EndCell()
	if layout, err := loadSendMsgLayout(nil, validExternalOut); err != nil {
		t.Fatalf("loadSendMsgLayout(valid external out) failed: %v", err)
	} else if layout.initExists || layout.bodyInRef {
		t.Fatalf("unexpected external layout: %+v", layout)
	}

	validInternal := internalHeader().
		MustStoreBoolBit(false).
		MustStoreBoolBit(false).
		EndCell()
	if layout, err := loadSendMsgLayout(nil, validInternal); err != nil {
		t.Fatalf("loadSendMsgLayout(valid internal) failed: %v", err)
	} else if layout.initExists || layout.bodyInRef || layout.extraFlagsBits == 0 {
		t.Fatalf("unexpected internal layout: %+v", layout)
	}

	tests := []struct {
		name string
		cell *cell.Cell
	}{
		{
			name: "empty root",
			cell: cell.BeginCell().EndCell(),
		},
		{
			name: "external missing direction",
			cell: cell.BeginCell().MustStoreBoolBit(true).EndCell(),
		},
		{
			name: "external inbound rejected",
			cell: cell.BeginCell().MustStoreBoolBit(true).MustStoreBoolBit(false).EndCell(),
		},
		{
			name: "external missing source",
			cell: cell.BeginCell().MustStoreBoolBit(true).MustStoreBoolBit(true).EndCell(),
		},
		{
			name: "external missing destination",
			cell: cell.BeginCell().MustStoreBoolBit(true).MustStoreBoolBit(true).MustStoreAddr(nil).EndCell(),
		},
		{
			name: "external missing created lt",
			cell: cell.BeginCell().MustStoreBoolBit(true).MustStoreBoolBit(true).MustStoreAddr(nil).MustStoreAddr(nil).EndCell(),
		},
		{
			name: "external missing created at",
			cell: cell.BeginCell().MustStoreBoolBit(true).MustStoreBoolBit(true).MustStoreAddr(nil).MustStoreAddr(nil).MustStoreUInt(1, 64).EndCell(),
		},
		{
			name: "external missing init bit",
			cell: externalHeader().EndCell(),
		},
		{
			name: "external missing init ref flag",
			cell: externalHeader().MustStoreBoolBit(true).EndCell(),
		},
		{
			name: "external init ref missing ref",
			cell: externalHeader().MustStoreBoolBit(true).MustStoreBoolBit(true).EndCell(),
		},
		{
			name: "external missing body bit",
			cell: externalHeader().MustStoreBoolBit(false).EndCell(),
		},
		{
			name: "external body ref missing ref",
			cell: externalHeader().MustStoreBoolBit(false).MustStoreBoolBit(true).EndCell(),
		},
		{
			name: "external trailing data after body ref",
			cell: externalHeader().
				MustStoreBoolBit(false).
				MustStoreBoolBit(true).
				MustStoreRef(cell.BeginCell().EndCell()).
				MustStoreBoolBit(true).
				EndCell(),
		},
		{
			name: "internal missing flags",
			cell: cell.BeginCell().MustStoreBoolBit(false).MustStoreUInt(0, 2).EndCell(),
		},
		{
			name: "internal missing source",
			cell: cell.BeginCell().MustStoreBoolBit(false).MustStoreUInt(0, 3).EndCell(),
		},
		{
			name: "internal missing destination",
			cell: cell.BeginCell().MustStoreBoolBit(false).MustStoreUInt(0, 3).MustStoreAddr(nil).EndCell(),
		},
		{
			name: "internal missing amount",
			cell: cell.BeginCell().MustStoreBoolBit(false).MustStoreUInt(0, 3).MustStoreAddr(nil).MustStoreAddr(nil).EndCell(),
		},
		{
			name: "internal missing extra flag",
			cell: cell.BeginCell().MustStoreBoolBit(false).MustStoreUInt(0, 3).MustStoreAddr(nil).MustStoreAddr(nil).MustStoreBigCoins(big.NewInt(1)).EndCell(),
		},
		{
			name: "internal missing extra ref",
			cell: cell.BeginCell().MustStoreBoolBit(false).MustStoreUInt(0, 3).MustStoreAddr(nil).MustStoreAddr(nil).MustStoreBigCoins(big.NewInt(1)).MustStoreBoolBit(true).EndCell(),
		},
		{
			name: "internal missing extra flags",
			cell: cell.BeginCell().MustStoreBoolBit(false).MustStoreUInt(0, 3).MustStoreAddr(nil).MustStoreAddr(nil).MustStoreBigCoins(big.NewInt(1)).MustStoreBoolBit(false).EndCell(),
		},
		{
			name: "internal missing fwd fee",
			cell: cell.BeginCell().MustStoreBoolBit(false).MustStoreUInt(0, 3).MustStoreAddr(nil).MustStoreAddr(nil).MustStoreBigCoins(big.NewInt(1)).MustStoreBoolBit(false).MustStoreVarUInt(0, 16).EndCell(),
		},
		{
			name: "internal missing created lt",
			cell: cell.BeginCell().MustStoreBoolBit(false).MustStoreUInt(0, 3).MustStoreAddr(nil).MustStoreAddr(nil).MustStoreBigCoins(big.NewInt(1)).MustStoreBoolBit(false).MustStoreVarUInt(0, 16).MustStoreBigCoins(big.NewInt(0)).EndCell(),
		},
		{
			name: "internal missing created at",
			cell: cell.BeginCell().MustStoreBoolBit(false).MustStoreUInt(0, 3).MustStoreAddr(nil).MustStoreAddr(nil).MustStoreBigCoins(big.NewInt(1)).MustStoreBoolBit(false).MustStoreVarUInt(0, 16).MustStoreBigCoins(big.NewInt(0)).MustStoreUInt(1, 64).EndCell(),
		},
		{
			name: "internal missing init bit",
			cell: internalHeader().EndCell(),
		},
		{
			name: "internal missing init ref flag",
			cell: internalHeader().MustStoreBoolBit(true).EndCell(),
		},
		{
			name: "internal missing body bit",
			cell: internalHeader().MustStoreBoolBit(false).EndCell(),
		},
		{
			name: "internal body ref missing ref",
			cell: internalHeader().MustStoreBoolBit(false).MustStoreBoolBit(true).EndCell(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadSendMsgLayout(nil, tc.cell); err == nil {
				t.Fatal("loadSendMsgLayout should reject malformed message layout")
			}
		})
	}
}

func TestSendMsgSizingHelpersEdges(t *testing.T) {
	if got := sendMsgStoredCoinsBits(nil, false); got != 4 {
		t.Fatalf("sendMsgStoredCoinsBits(nil) = %d, want 4", got)
	}
	if got := sendMsgStoredCoinsBits(big.NewInt(0), false); got != 4 {
		t.Fatalf("sendMsgStoredCoinsBits(0) = %d, want 4", got)
	}
	if got := sendMsgStoredCoinsBits(big.NewInt(256), false); got != 20 {
		t.Fatalf("sendMsgStoredCoinsBits(256) = %d, want 20", got)
	}
	if got := sendMsgStoredCoinsBits(nil, true); got != sendMsgInvalidStoredCoinsBits {
		t.Fatalf("sendMsgStoredCoinsBits(NaN) = %#x, want %#x", got, sendMsgInvalidStoredCoinsBits)
	}
	if got := sendMsgStoredCoinsBits(big.NewInt(-1), false); got != sendMsgInvalidStoredCoinsBits {
		t.Fatalf("sendMsgStoredCoinsBits(-1) = %#x, want %#x", got, sendMsgInvalidStoredCoinsBits)
	}

	zero := sendMsgBigOrZero(nil)
	if zero == nil || zero.Sign() != 0 {
		t.Fatalf("sendMsgBigOrZero(nil) = %v, want zero", zero)
	}
	amount := big.NewInt(9)
	copied := sendMsgBigOrZero(amount)
	amount.SetInt64(10)
	if copied.Int64() != 9 {
		t.Fatalf("sendMsgBigOrZero should copy input, got %s", copied)
	}

	if bits, err := sendMsgAddressBits(nil); err != nil || bits != 2 {
		t.Fatalf("sendMsgAddressBits(nil) = (%d, %v), want (2, nil)", bits, err)
	}
	oversized := address.NewAddressExt(0, 1020, bytes.Repeat([]byte{0xFF}, 128))
	if _, err := sendMsgAddressBits(oversized); err == nil {
		t.Fatal("sendMsgAddressBits should reject addresses that do not fit into a cell")
	}
}

func fuzzSendMsgSupportedVersion(raw uint8) int {
	version := int(raw)
	if version >= 4 && version <= vm.MaxSupportedGlobalVersion {
		return version
	}
	return 4 + int(raw%uint8(vm.MaxSupportedGlobalVersion-3))
}

func TestFuzzSendMsgVersionedUserFwdFeeCoversSupportedRange(t *testing.T) {
	seen := map[int]bool{}
	for raw := uint8(0); raw < 255; raw++ {
		seen[fuzzSendMsgSupportedVersion(raw)] = true
	}
	seen[fuzzSendMsgSupportedVersion(255)] = true

	for version := 4; version <= vm.MaxSupportedGlobalVersion; version++ {
		if !seen[version] {
			t.Fatalf("SENDMSG user fwd fee fuzz does not cover version %d", version)
		}
	}

	for version := 4; version <= vm.MaxSupportedGlobalVersion; version++ {
		if got := fuzzSendMsgSupportedVersion(uint8(version)); got != version {
			t.Fatalf("fuzzSendMsgSupportedVersion(%d) = %d, want %d", version, got, version)
		}
	}
}

func FuzzSendMsgVersionedUserFwdFeeLowerBound(f *testing.F) {
	for _, version := range []int{4, 13, 14} {
		f.Add(uint8(version), uint16(1), uint16(500))
		f.Add(uint8(version), uint16(499), uint16(500))
		f.Add(uint8(version), uint16(700), uint16(500))
	}

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawComputed, rawUserFwd uint16) {
		version := fuzzSendMsgSupportedVersion(rawVersion)
		computed := int64(rawComputed%1_000) + 1
		userFwd := int64(rawUserFwd % 1_000)

		myAddr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x11}, 32))
		dest := address.NewAddress(0, 0, bytes.Repeat([]byte{0x22}, 32))
		msgCell, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     myAddr,
			DstAddr:     dest,
			Amount:      tlb.FromNanoTONU(100),
			IHRFee:      tlb.FromNanoTONU(0),
			FwdFee:      tlb.FromNanoTONU(uint64(userFwd)),
			Body:        cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}

		prices := makeMsgPricesSlice(uint64(computed), 0, 0)
		st := makeSendMsgEdgeState(t, myAddr, nil, prices, nil)
		mustSetFuncParam(t, st, 9, makeConfigRootRefDict(t, map[uint32]*cell.Cell{
			tlb.ConfigParamMsgForwardPricesBasechain: prices.MustToCell(),
		}))
		st.GlobalVersion = version
		if err = st.Stack.PushCell(msgCell); err != nil {
			t.Fatalf("PushCell failed: %v", err)
		}
		if err = st.Stack.PushInt(big.NewInt(1024)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		err = SENDMSG().Interpret(st)
		if err != nil {
			t.Fatalf("SENDMSG version=%d failed: %v", version, err)
		}

		want := computed
		if version < 14 && userFwd > want {
			want = userFwd
		}
		fee, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		if fee.Int64() != want {
			t.Fatalf("SENDMSG version=%d computed=%d user=%d fee=%s, want %d", version, computed, userFwd, fee, want)
		}
	})
}

func FuzzSendMsgVersionedIHRFeeBoundary(f *testing.F) {
	for _, version := range []int{4, 10, 11, 12, 14} {
		f.Add(uint8(version), uint16(100), uint32(1<<16), uint16(0))
		f.Add(uint8(version), uint16(100), uint32(1<<15), uint16(200))
		f.Add(uint8(version), uint16(100), uint32(0), uint16(300))
	}

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawComputed uint16, rawIHRFactor uint32, rawUserIHR uint16) {
		version := fuzzSendMsgSupportedVersion(rawVersion)
		computed := int64(rawComputed%1_000) + 1
		ihrFactor := rawIHRFactor & 0xffff
		userIHR := int64(rawUserIHR % 1_000)

		myAddr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x31}, 32))
		dest := address.NewAddress(0, 0, bytes.Repeat([]byte{0x32}, 32))
		msgCell, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: false,
			SrcAddr:     myAddr,
			DstAddr:     dest,
			Amount:      tlb.FromNanoTONU(100),
			IHRFee:      tlb.FromNanoTONU(uint64(userIHR)),
			FwdFee:      tlb.FromNanoTONU(0),
			Body:        cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}

		prices := makeMsgPricesFullSlice(uint64(computed), 0, 0, ihrFactor, 0, 0)
		st := makeSendMsgEdgeState(t, myAddr, nil, prices, nil)
		mustSetFuncParam(t, st, 9, makeConfigRootRefDict(t, map[uint32]*cell.Cell{
			tlb.ConfigParamMsgForwardPricesBasechain: prices.MustToCell(),
		}))
		st.GlobalVersion = version
		if err = st.Stack.PushCell(msgCell); err != nil {
			t.Fatalf("PushCell failed: %v", err)
		}
		if err = st.Stack.PushInt(big.NewInt(1024)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err = SENDMSG().Interpret(st); err != nil {
			t.Fatalf("SENDMSG version=%d failed: %v", version, err)
		}

		wantIHR := int64(0)
		if version < 11 {
			computedIHR := ceilShiftRight(mulBigUint64(big.NewInt(computed), uint64(ihrFactor)), 16).Int64()
			wantIHR = computedIHR
			if version < 12 && userIHR > wantIHR {
				wantIHR = userIHR
			}
		}
		want := computed + wantIHR

		fee, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		if fee.Int64() != want {
			t.Fatalf("SENDMSG version=%d computed=%d ihr_factor=%d user_ihr=%d fee=%s, want %d", version, computed, ihrFactor, userIHR, fee, want)
		}
	})
}

func FuzzSendMsgVersionedExtraFlagsRootSizeBoundary(f *testing.F) {
	for _, version := range []int{11, 12, 13, 14} {
		f.Add(uint8(version), uint16(360), uint64(256))
		f.Add(uint8(version), uint16(340), uint64(256))
		f.Add(uint8(version), uint16(360), uint64(0))
	}

	f.Fuzz(func(t *testing.T, rawVersion uint8, rawBodyBits uint16, rawExtraFlags uint64) {
		version := fuzzSendMsgSupportedVersion(rawVersion)
		bodyBits := uint(rawBodyBits % 420)
		extraFlags := rawExtraFlags % 65536

		body := cell.BeginCell()
		if bodyBits > 0 {
			body.MustStoreSlice(bytes.Repeat([]byte{byte(rawExtraFlags)}, int((bodyBits+7)/8)), bodyBits)
		}

		myAddr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x41}, 32))
		dest := address.NewAddress(0, 0, bytes.Repeat([]byte{0x42}, 32))
		msgCell, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     address.NewAddressNone(),
			DstAddr:     dest,
			Amount:      tlb.FromNanoTONU(100),
			IHRFee:      tlb.FromNanoTONU(extraFlags),
			FwdFee:      tlb.FromNanoTONU(0),
			Body:        body.EndCell(),
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}
		layout, err := loadSendMsgLayout(nil, msgCell)
		if err != nil {
			t.Fatalf("loadSendMsgLayout failed: %v", err)
		}
		if layout.bodyInRef || layout.initExists || layout.bodyRefs != 0 {
			t.Fatalf("test message layout = %+v, want inline body without init/refs", layout)
		}

		myAddrBits, err := sendMsgAddressBits(myAddr)
		if err != nil {
			t.Fatalf("myaddr bits: %v", err)
		}
		destAddrBits, err := sendMsgAddressBits(dest)
		if err != nil {
			t.Fatalf("dest bits: %v", err)
		}
		extraBits := sendMsgStoredCoinsBits(big.NewInt(0), false)
		if version >= 12 {
			extraBits = layout.extraFlagsBits
		}
		rootBits := 4 + myAddrBits + destAddrBits + sendMsgStoredCoinsBits(big.NewInt(100), false) + 1 + 32 + 64
		rootBits += sendMsgStoredCoinsBits(big.NewInt(0), false) + extraBits
		rootBits++ // no state init
		rootBits++ // inline body selector
		rootBits += layout.bodyBits - 1

		want := int64(0)
		if rootBits > 1023 {
			want = 1
		}

		prices := makeMsgPricesSlice(0, 0, 1<<16)
		st := makeSendMsgEdgeState(t, myAddr, nil, prices, nil)
		mustSetFuncParam(t, st, 9, makeConfigRootRefDict(t, map[uint32]*cell.Cell{
			tlb.ConfigParamMsgForwardPricesBasechain: prices.MustToCell(),
		}))
		st.GlobalVersion = version
		if err = st.Stack.PushCell(msgCell); err != nil {
			t.Fatalf("PushCell failed: %v", err)
		}
		if err = st.Stack.PushInt(big.NewInt(1024)); err != nil {
			t.Fatalf("PushInt failed: %v", err)
		}
		if err = SENDMSG().Interpret(st); err != nil {
			t.Fatalf("SENDMSG version=%d failed: %v", version, err)
		}
		fee, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		if fee.Int64() != want {
			t.Fatalf("SENDMSG version=%d body_bits=%d extra_flags=%d root_bits=%d fee=%s, want %d", version, bodyBits, extraFlags, rootBits, fee, want)
		}
	})
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
		if restoredAddr, err := st.Stack.PopSlice(); err != nil || !isValidStdMsgAddr(restoredAddr, vm.MaxSupportedGlobalVersion) {
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
			name       string
			version    int
			wantLegacy bool
		}{
			{name: "v12 returns legacy null slice", version: 12, wantLegacy: true},
			{name: "v13 returns legacy null slice", version: 13, wantLegacy: true},
			{name: "v14 keeps invalid value", version: 14},
		} {
			t.Run(tc.name, func(t *testing.T) {
				st := newFuncTestState(t, nil)
				st.GlobalVersion = tc.version
				if err := st.Stack.PushInt(big.NewInt(100)); err != nil {
					t.Fatalf("PushInt failed: %v", err)
				}
				if err := st.Stack.PushBuilder(cell.BeginCell().MustStoreUInt(0xAB, 8)); err != nil {
					t.Fatalf("PushBuilder failed: %v", err)
				}
				if err := STOPTSTDADDRQ().Interpret(st); err != nil {
					t.Fatalf("STOPTSTDADDRQ(int) failed: %v", err)
				}
				ok, err := st.Stack.PopBool()
				if err != nil || !ok {
					t.Fatalf("STOPTSTDADDRQ(int) = (%v, %v), want true", ok, err)
				}
				restoredBuilder, err := st.Stack.PopBuilder()
				if err != nil {
					t.Fatalf("PopBuilder failed: %v", err)
				}
				if restoredBuilder.ToSlice().BitsLeft() != 8 || !bytes.Equal(mustSliceData(t, restoredBuilder.ToSlice()), []byte{0xAB}) {
					t.Fatalf("restored builder changed: bits=%d data=%x", restoredBuilder.ToSlice().BitsLeft(), mustSliceData(t, restoredBuilder.ToSlice()))
				}
				restored, err := st.Stack.PopAny()
				if err != nil {
					t.Fatalf("PopAny failed: %v", err)
				}
				if tc.wantLegacy {
					legacy, ok := restored.(*cell.Slice)
					if !ok || legacy != nil {
						t.Fatalf("restored value = %T %v, want legacy null cell slice", restored, restored)
					}
					return
				}

				got, ok := restored.(*big.Int)
				if !ok || got.Cmp(big.NewInt(100)) != 0 {
					t.Fatalf("restored value = %T %v, want 100", restored, restored)
				}
			})
		}
	})

	t.Run("sendmsg ignores user fwd fee lower bound from v14", func(t *testing.T) {
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

	t.Run("sendmsg fee-only mode64 and mode128 read c7 amount tuples", func(t *testing.T) {
		myAddr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x11}, 32))
		dest := address.NewAddress(0, 0, bytes.Repeat([]byte{0x22}, 32))

		msgCell, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     myAddr,
			DstAddr:     dest,
			Amount:      tlb.FromNanoTONU(100),
			IHRFee:      tlb.FromNanoTONU(0),
			FwdFee:      tlb.FromNanoTONU(0),
			Body:        cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}

		extra := cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell()
		for _, tc := range []struct {
			name     string
			mode     int64
			incoming tuple.Tuple
		}{
			{name: "mode64 incoming value", mode: 1024 + 64, incoming: tuple.NewTupleValue(big.NewInt(777), extra)},
			{name: "mode128 balance", mode: 1024 + 128},
		} {
			t.Run(tc.name, func(t *testing.T) {
				st := makeSendMsgEdgeState(t, myAddr, nil, makeMsgPricesSlice(1, 1, 0), nil)
				st.GlobalVersion = 9
				if tc.incoming.Len() > 0 {
					mustSetFuncParam(t, st, 11, tc.incoming)
				}
				if err := st.Stack.PushCell(msgCell); err != nil {
					t.Fatalf("PushCell failed: %v", err)
				}
				if err := st.Stack.PushInt(big.NewInt(tc.mode)); err != nil {
					t.Fatalf("PushInt failed: %v", err)
				}
				if err := SENDMSG().Interpret(st); err != nil {
					t.Fatalf("SENDMSG(%s) failed: %v", tc.name, err)
				}
				fee, err := st.Stack.PopIntFinite()
				if err != nil {
					t.Fatalf("PopIntFinite failed: %v", err)
				}
				if fee.Sign() <= 0 {
					t.Fatalf("SENDMSG(%s) fee = %s, want positive", tc.name, fee)
				}
			})
		}
	})

	t.Run("sendmsg mode64 and mode128 reject malformed c7 amount tuples", func(t *testing.T) {
		myAddr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x11}, 32))
		dest := address.NewAddress(0, 0, bytes.Repeat([]byte{0x22}, 32))

		msgCell, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     myAddr,
			DstAddr:     dest,
			Amount:      tlb.FromNanoTONU(100),
			IHRFee:      tlb.FromNanoTONU(0),
			FwdFee:      tlb.FromNanoTONU(0),
			Body:        cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}

		for _, tc := range []struct {
			name  string
			mode  int64
			idx   int
			value any
		}{
			{name: "mode64 bad incoming", mode: 1024 + 64, idx: 11, value: big.NewInt(1)},
			{name: "mode128 bad balance", mode: 1024 + 128, idx: 7, value: big.NewInt(1)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				st := makeSendMsgEdgeState(t, myAddr, nil, makeMsgPricesSlice(1, 1, 0), nil)
				st.GlobalVersion = 9
				mustSetFuncParam(t, st, tc.idx, tc.value)
				if err := st.Stack.PushCell(msgCell); err != nil {
					t.Fatalf("PushCell failed: %v", err)
				}
				if err := st.Stack.PushInt(big.NewInt(tc.mode)); err != nil {
					t.Fatalf("PushInt failed: %v", err)
				}
				err := SENDMSG().Interpret(st)
				if code, ok := vmerr.ErrorCode(err); !ok || code != vmerr.CodeTypeCheck {
					t.Fatalf("SENDMSG(%s) exit = (%d, %t), want type_check", tc.name, code, ok)
				}
			})
		}
	})

	t.Run("sendmsg switches from root prices to unpacked prices at v6", func(t *testing.T) {
		myAddr := address.NewAddress(0, 0, bytes.Repeat([]byte{0x11}, 32))
		dest := address.NewAddress(0, 0, bytes.Repeat([]byte{0x22}, 32))

		msgCell, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     myAddr,
			DstAddr:     dest,
			Amount:      tlb.FromNanoTONU(100),
			IHRFee:      tlb.FromNanoTONU(0),
			FwdFee:      tlb.FromNanoTONU(0),
			Body:        cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		})
		if err != nil {
			t.Fatalf("ToCell failed: %v", err)
		}

		rootPrices := makeMsgPricesSlice(11, 0, 0)
		unpackedPrices := makeMsgPricesSlice(99, 0, 0)
		unpacked := tuple.NewTupleSized(7)
		if err = unpacked.Set(5, unpackedPrices); err != nil {
			t.Fatalf("failed to set unpacked msg prices: %v", err)
		}

		for _, tc := range []struct {
			version int
			want    int64
		}{
			{version: 5, want: 11},
			{version: 6, want: 99},
		} {
			st := newFuncTestState(t, map[int]any{
				8:                      cell.BeginCell().MustStoreAddr(myAddr).ToSlice(),
				9:                      makeConfigRootRefDict(t, map[uint32]*cell.Cell{tlb.ConfigParamMsgForwardPricesBasechain: rootPrices.MustToCell()}),
				paramIdxUnpackedConfig: unpacked,
			})
			st.GlobalVersion = tc.version
			st.InitForExecution()
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

	t.Run("sendmsg caps statistics at the size limit", func(t *testing.T) {
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
		if err := SENDMSG().Interpret(st); err != nil {
			t.Fatalf("SENDMSG should use capped statistics: %v", err)
		}
		fee, err := st.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("PopIntFinite failed: %v", err)
		}
		if fee.Cmp(big.NewInt(1)) != 0 {
			t.Fatalf("capped SENDMSG fee = %s, want 1", fee)
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
