package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

func TestMessageEmulationHelpersDefaultsAndCopies(t *testing.T) {
	t.Run("BodyAndBalanceHelpers", func(t *testing.T) {
		body := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
		if got := messageBodyCell(body); got != body {
			t.Fatal("non-nil body should be returned as-is")
		}
		if got := messageBodyCell(nil); got == nil || got.BitsSize() != 0 || got.RefsNum() != 0 {
			t.Fatal("nil body should become an empty cell")
		}

		if got := messageEmulationBalance(nil); got.Sign() != 0 {
			t.Fatalf("nil balance should become zero, got %s", got.String())
		}

		orig := big.NewInt(123)
		cp := messageEmulationBalance(orig)
		orig.SetInt64(999)
		if cp.Int64() != 123 {
			t.Fatalf("balance helper should copy input, got %s", cp.String())
		}
	})

	t.Run("TupleDefaults", func(t *testing.T) {
		incoming := messageIncomingValue(tuple.Tuple{})
		if incoming.Len() != 2 {
			t.Fatalf("unexpected incoming value len: %d", incoming.Len())
		}
		val, err := incoming.RawIndex(0)
		if err != nil {
			t.Fatal(err)
		}
		if got := val.(*big.Int).Int64(); got != 0 {
			t.Fatalf("unexpected incoming grams: %d", got)
		}

		if got := messageUnpackedConfig(MessageEmulationConfig{}, 0); got != nil {
			t.Fatalf("zero config should not synthesize unpacked config, got %T", got)
		}

		cfgTuple := tuple.NewTupleValue("cfg")
		if got := messageUnpackedConfig(MessageEmulationConfig{UnpackedConfig: cfgTuple}, 0); func() int {
			tup := got.(tuple.Tuple)
			return tup.Len()
		}() != 1 {
			t.Fatalf("explicit unpacked config should pass through, got %T", got)
		}

		synth := messageUnpackedConfig(MessageEmulationConfig{GlobalID: 0x11223344}, 0).(tuple.Tuple)
		if synth.Len() != 7 {
			t.Fatalf("unexpected synthesized unpacked config len: %d", synth.Len())
		}
		raw, err := synth.RawIndex(1)
		if err != nil {
			t.Fatal(err)
		}
		if got := raw.(*cell.Slice).MustLoadUInt(32); got != 0x11223344 {
			t.Fatalf("unexpected synthesized global id: %x", got)
		}

		fromRoot := messageUnpackedConfig(MessageEmulationConfig{
			ConfigRoot: messageUnpackedConfigRoot(t),
		}, 150).(tuple.Tuple)
		if fromRoot.Len() != 7 {
			t.Fatalf("unexpected root unpacked config len: %d", fromRoot.Len())
		}
		assertMessageTupleSliceUInt(t, fromRoot, 0, 8, 0xCC, "storage prices tag")
		assertMessageTupleSliceUInt(t, fromRoot, 0, 40, 0xCC00000064, "storage prices current entry")
		assertMessageTupleSliceUInt(t, fromRoot, 1, 32, 0x55667788, "global id")
		assertMessageTupleSliceUInt(t, fromRoot, 2, 8, 0xD1, "masterchain gas prices tag")
		assertMessageTupleSliceUInt(t, fromRoot, 3, 8, 0xD1, "basechain gas prices tag")
		assertMessageTupleSliceUInt(t, fromRoot, 4, 8, 0xEA, "masterchain message prices tag")
		assertMessageTupleSliceUInt(t, fromRoot, 5, 8, 0xEA, "basechain message prices tag")
		assertMessageTupleSliceUInt(t, fromRoot, 6, 8, 0x01, "size limits tag")

		params := messageInMsgParams(tuple.Tuple{})
		if params.Len() != 10 {
			t.Fatalf("unexpected default in_msg_params len: %d", params.Len())
		}
		got, err := params.RawIndex(2)
		if err != nil {
			t.Fatal(err)
		}
		if got.(*cell.Slice).BitsLeft() != 2 {
			t.Fatal("default in_msg_params should contain a 2-bit mode slice")
		}
	})

	t.Run("SeedAndNormalization", func(t *testing.T) {
		seed, err := messageEmulationSeed(nil)
		if err != nil {
			t.Fatal(err)
		}
		if seed.Sign() != 0 {
			t.Fatalf("empty seed should map to zero, got %s", seed.String())
		}

		seed, err = messageEmulationSeed([]byte{0x01, 0x02, 0x03})
		if err != nil {
			t.Fatal(err)
		}
		if want := big.NewInt(0x010203); seed.Cmp(want) != 0 {
			t.Fatalf("unexpected seed value: got %s want %s", seed.String(), want.String())
		}

		if got := normalizeMessageTupleValue(int16(-7)); got != int16(-7) {
			t.Fatalf("host ints should not be normalized to TVM ints, got %T %v", got, got)
		}

		var nilInt *big.Int
		var nilSlice *cell.Slice
		var nilBuilder *cell.Builder
		if got := normalizeMessageTupleValue(nilInt); got != nil {
			t.Fatalf("nil big.Int pointer should normalize to nil, got %T", got)
		}
		if got := normalizeMessageTupleValue(nilSlice); got != nil {
			t.Fatalf("nil slice pointer should normalize to nil, got %T", got)
		}
		if got := normalizeMessageTupleValue(nilBuilder); got != nil {
			t.Fatalf("nil builder pointer should normalize to nil, got %T", got)
		}

		orig := big.NewInt(55)
		cp := normalizeMessageTupleValue(orig).(*big.Int)
		orig.SetInt64(99)
		if cp.Int64() != 55 {
			t.Fatalf("big.Int normalization should copy input, got %d", cp.Int64())
		}

		if got := messageTupleMaybeInt(nil); got != nil {
			t.Fatalf("nil maybe int should stay nil, got %T", got)
		}
		maybeOrig := big.NewInt(77)
		maybeCopy := messageTupleMaybeInt(maybeOrig).(*big.Int)
		maybeOrig.SetInt64(88)
		if maybeCopy.Int64() != 77 {
			t.Fatalf("maybe int should copy input, got %d", maybeCopy.Int64())
		}

		slice := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().MustBeginParse()
		sliceCopy := normalizeMessageTupleValue(slice).(*cell.Slice)
		if _, err := slice.LoadUInt(1); err != nil {
			t.Fatal(err)
		}
		if sliceCopy.BitsLeft() != 8 {
			t.Fatalf("slice normalization should copy input, bits left %d", sliceCopy.BitsLeft())
		}

		builder := cell.BeginCell().MustStoreUInt(0xA, 4)
		builderCopy := normalizeMessageTupleValue(builder).(*cell.Builder)
		builder.MustStoreUInt(1, 1)
		if builderCopy.BitsUsed() != 4 {
			t.Fatalf("builder normalization should copy input, bits used %d", builderCopy.BitsUsed())
		}

		if got := normalizeMessageTupleValue("plain"); got.(string) != "plain" {
			t.Fatalf("unexpected passthrough value: %v", got)
		}
	})

	t.Run("GasDefaults", func(t *testing.T) {
		custom := vmcore.NewGas(vmcore.GasConfig{Max: 11, Limit: 7, Credit: 3})
		if got := defaultExternalMessageGas(custom); got != custom {
			t.Fatal("custom external gas should pass through unchanged")
		}
		if got := defaultInternalMessageGas(custom, 10); got != custom {
			t.Fatal("custom internal gas should pass through unchanged")
		}
		if got := defaultTickTockTransactionGas(custom); got != custom {
			t.Fatal("custom tick/tock gas should pass through unchanged")
		}

		external := defaultExternalMessageGas(vmcore.Gas{})
		if external.Max != DefaultExternalMessageGasMax || external.Credit != DefaultExternalMessageGasCredit {
			t.Fatalf("unexpected external default gas: %+v", external)
		}

		internal := defaultInternalMessageGas(vmcore.Gas{}, 7)
		wantLimit := int64(7) * InternalMessageGasAmountFactor
		if internal.Max != DefaultInternalMessageGasMax || internal.Limit != wantLimit || internal.Base != wantLimit || internal.Remaining != wantLimit {
			t.Fatalf("unexpected internal default gas: %+v", internal)
		}

		tickTock := defaultTickTockTransactionGas(vmcore.Gas{})
		if tickTock.Max != DefaultTickTockTransactionGasMax || tickTock.Limit != DefaultTickTockTransactionGasMax || tickTock.Credit != 0 {
			t.Fatalf("unexpected tick/tock default gas: %+v", tickTock)
		}
	})
}

func messageUnpackedConfigRoot(t *testing.T) *cell.Cell {
	t.Helper()

	storageDict := cell.NewDict(32)
	if err := storageDict.SetIntKey(big.NewInt(100), makeStoragePricesSlice(100, 3, 5, 7, 11).MustToCell()); err != nil {
		t.Fatalf("failed to seed storage prices 100: %v", err)
	}
	if err := storageDict.SetIntKey(big.NewInt(200), makeStoragePricesSlice(200, 13, 17, 19, 23).MustToCell()); err != nil {
		t.Fatalf("failed to seed storage prices 200: %v", err)
	}

	return mustConfigDictCell(t, map[uint32]*cell.Cell{
		tlb.ConfigParamStoragePrices:               storageDict.AsCell(),
		tlb.ConfigParamGlobalID:                    cell.BeginCell().MustStoreUInt(0x55667788, 32).EndCell(),
		tlb.ConfigParamGasPricesMasterchain:        makeGasPricesSlice(100, 77, 200, 1000, 1200, 50, 2000, 3000, 4000, true).MustToCell(),
		tlb.ConfigParamGasPricesBasechain:          makeGasPricesSlice(100, 55, 150, 900, 900, 40, 1800, 2800, 3800, true).MustToCell(),
		tlb.ConfigParamMsgForwardPricesMasterchain: makeMsgPricesSlice(1000, 200, 300, 500, 1000, 2000).MustToCell(),
		tlb.ConfigParamMsgForwardPricesBasechain:   makeMsgPricesSlice(900, 120, 220, 400, 800, 1200).MustToCell(),
		tlb.ConfigParamSizeLimits:                  makeSizeLimitsSlice(1<<20, 128).MustToCell(),
	})
}

func assertMessageTupleSliceUInt(t *testing.T, tup tuple.Tuple, idx int, bits uint, want uint64, name string) {
	t.Helper()

	raw, err := tup.RawIndex(idx)
	if err != nil {
		t.Fatalf("%s index %d: %v", name, idx, err)
	}
	sl, ok := raw.(*cell.Slice)
	if !ok {
		t.Fatalf("%s index %d type = %T, want *cell.Slice", name, idx, raw)
	}
	got, err := sl.Copy().LoadUInt(bits)
	if err != nil {
		t.Fatalf("%s load %d bits: %v", name, bits, err)
	}
	if got != want {
		t.Fatalf("%s = %x, want %x", name, got, want)
	}
}

func TestMessageEmulationAccountAddr(t *testing.T) {
	addrInt, err := messageEmulationAccountAddr(tickTockTestAddr)
	if err != nil {
		t.Fatalf("messageEmulationAccountAddr failed: %v", err)
	}
	if addrInt.Cmp(new(big.Int).SetBytes(tickTockTestAddr.Data())) != 0 {
		t.Fatalf("unexpected account int: got %s", addrInt.String())
	}

	if _, err = messageEmulationAccountAddr(nil); err == nil {
		t.Fatal("nil address should fail")
	}
	if _, err = messageEmulationAccountAddr(address.NewAddressNone()); err == nil {
		t.Fatal("non-std address should fail")
	}
}

func TestSameCellHashBoundaries(t *testing.T) {
	if !sameCellHash(nil, nil) {
		t.Fatal("nil cells should match")
	}
	cellA := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	cellACopy := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
	cellB := cell.BeginCell().MustStoreUInt(0xAC, 8).EndCell()

	if sameCellHash(nil, cellA) || sameCellHash(cellA, nil) {
		t.Fatal("nil and non-nil cells should not match")
	}
	if !sameCellHash(cellA, cellA) || !sameCellHash(cellA, cellACopy) {
		t.Fatal("cells with the same hash should match")
	}
	if sameCellHash(cellA, cellB) {
		t.Fatal("cells with different hashes should not match")
	}
}

func TestBuildMessageEmulationC7CopiesGlobals(t *testing.T) {
	cfg := MessageEmulationConfig{
		Now:                 12345,
		BlockLT:             77,
		LogicalTime:         88,
		ConfigRoot:          cell.BeginCell().MustStoreUInt(1, 1).EndCell(),
		IncomingValue:       tuple.NewTupleValue(big.NewInt(9), nil),
		StorageFees:         11,
		PrevBlocks:          "prev",
		DuePayment:          "due",
		PrecompiledGasUsage: big.NewInt(5),
		InMsgParams:         tuple.NewTupleValue("params"),
		GlobalID:            7,
		Globals: map[int]any{
			2: big.NewInt(55),
		},
	}

	balance := big.NewInt(321)
	code := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell()
	c7, err := buildMessageEmulationC7(tonopsTestAddr, code, cfg, balance, vmcore.DefaultGlobalVersion)
	if err != nil {
		t.Fatal(err)
	}

	if c7.Len() != 3 {
		t.Fatalf("unexpected top-level tuple len: %d", c7.Len())
	}

	innerRaw, err := c7.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	inner := innerRaw.(tuple.Tuple)
	if inner.Len() != 18 {
		t.Fatalf("unexpected inner tuple len: %d", inner.Len())
	}

	nowRaw, err := inner.RawIndex(3)
	if err != nil {
		t.Fatal(err)
	}
	if got := nowRaw.(*big.Int).Int64(); got != 12345 {
		t.Fatalf("unexpected now field: %d", got)
	}

	balanceRaw, err := inner.RawIndex(7)
	if err != nil {
		t.Fatal(err)
	}
	balanceTuple := balanceRaw.(tuple.Tuple)
	valueRaw, err := balanceTuple.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := valueRaw.(*big.Int).Int64(); got != 321 {
		t.Fatalf("unexpected balance in c7: %d", got)
	}

	globalRaw, err := c7.RawIndex(2)
	if err != nil {
		t.Fatal(err)
	}
	if got := globalRaw.(*big.Int).Int64(); got != 55 {
		t.Fatalf("unexpected global value: %d", got)
	}
}

func TestBuildMessageEmulationC7UsesRealNilForAbsentPrecompiledGas(t *testing.T) {
	c7, err := buildMessageEmulationC7(tonopsTestAddr, cell.BeginCell().EndCell(), MessageEmulationConfig{}, big.NewInt(0), vmcore.DefaultGlobalVersion)
	if err != nil {
		t.Fatal(err)
	}

	innerRaw, err := c7.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	inner := innerRaw.(tuple.Tuple)
	precompiledRaw, err := inner.RawIndex(16)
	if err != nil {
		t.Fatal(err)
	}
	if precompiledRaw != nil {
		t.Fatalf("precompiled gas field = %#v, want real nil", precompiledRaw)
	}
}

func TestBuildMessageEmulationC7ClonesMutableConfigValues(t *testing.T) {
	unpackedSlice := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell().MustBeginParse()
	prevSlice := cell.BeginCell().MustStoreUInt(0xCD, 8).EndCell().MustBeginParse()
	cfg := MessageEmulationConfig{
		ConfigRoot:     cell.BeginCell().MustStoreUInt(1, 1).EndCell(),
		PrevBlocks:     tuple.NewTupleValue(prevSlice),
		UnpackedConfig: tuple.NewTupleValue(unpackedSlice),
	}

	c7, err := buildMessageEmulationC7(tonopsTestAddr, cell.BeginCell().EndCell(), cfg, big.NewInt(0), vmcore.DefaultGlobalVersion)
	if err != nil {
		t.Fatal(err)
	}

	paramsRaw, err := c7.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	params := paramsRaw.(tuple.Tuple)

	prevRaw, err := params.RawIndex(13)
	if err != nil {
		t.Fatal(err)
	}
	prev := prevRaw.(tuple.Tuple)
	copiedPrevRaw, err := prev.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = copiedPrevRaw.(*cell.Slice).LoadUInt(8); err != nil {
		t.Fatal(err)
	}
	if prevSlice.BitsLeft() != 8 {
		t.Fatalf("prev blocks slice was mutated, bits left %d", prevSlice.BitsLeft())
	}

	unpackedRaw, err := params.RawIndex(14)
	if err != nil {
		t.Fatal(err)
	}
	unpacked := unpackedRaw.(tuple.Tuple)
	copiedUnpackedRaw, err := unpacked.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = copiedUnpackedRaw.(*cell.Slice).LoadUInt(8); err != nil {
		t.Fatal(err)
	}
	if unpackedSlice.BitsLeft() != 8 {
		t.Fatalf("unpacked config slice was mutated, bits left %d", unpackedSlice.BitsLeft())
	}
}

func FuzzMessageTupleNormalizationIsolation(f *testing.F) {
	f.Add(uint64(0xA5), byte(8), int64(10), int64(20))
	f.Add(uint64(0x1234), byte(16), int64(-1), int64(0))
	f.Add(uint64(0xDEADBEEF), byte(32), int64(1<<31-1), int64(-1<<31))

	f.Fuzz(func(t *testing.T, rawPayload uint64, rawBits byte, rawTopInt, rawNestedInt int64) {
		if empty := normalizeMessageTupleValue(tuple.Tuple{}).(tuple.Tuple); empty.Len() != 0 {
			t.Fatalf("empty tuple normalized len = %d, want 0", empty.Len())
		}

		bits := uint(rawBits%63) + 1
		payload := rawPayload
		if bits < 64 {
			payload &= (uint64(1) << bits) - 1
		}

		topInt := big.NewInt(rawTopInt)
		nestedInt := big.NewInt(rawNestedInt)
		slice := cell.BeginCell().MustStoreUInt(payload, bits).EndCell().MustBeginParse()
		builder := cell.BeginCell().MustStoreUInt(payload, bits)
		nested := tuple.NewTupleValue(nestedInt)
		orig := tuple.NewTupleValue(topInt, slice, builder, nested)

		clonedRaw := normalizeMessageTupleValue(orig)
		cloned, ok := clonedRaw.(tuple.Tuple)
		if !ok {
			t.Fatalf("normalized tuple = %T, want tuple.Tuple", clonedRaw)
		}
		if cloned.Len() != 4 {
			t.Fatalf("cloned tuple len = %d, want 4", cloned.Len())
		}

		topInt.SetInt64(rawTopInt + 1)
		nestedInt.SetInt64(rawNestedInt + 1)
		if _, err := slice.LoadUInt(1); err != nil {
			t.Fatalf("consume original slice: %v", err)
		}
		builder.MustStoreUInt(1, 1)
		if err := nested.Set(0, big.NewInt(rawNestedInt+2)); err != nil {
			t.Fatalf("mutate nested tuple: %v", err)
		}

		clonedTopRaw, err := cloned.RawIndex(0)
		if err != nil {
			t.Fatal(err)
		}
		if got := clonedTopRaw.(*big.Int).Int64(); got != rawTopInt {
			t.Fatalf("cloned top int = %d, want %d", got, rawTopInt)
		}

		clonedSliceRaw, err := cloned.RawIndex(1)
		if err != nil {
			t.Fatal(err)
		}
		clonedSlice := clonedSliceRaw.(*cell.Slice)
		if clonedSlice.BitsLeft() != bits {
			t.Fatalf("cloned slice bits left = %d, want %d", clonedSlice.BitsLeft(), bits)
		}

		clonedBuilderRaw, err := cloned.RawIndex(2)
		if err != nil {
			t.Fatal(err)
		}
		clonedBuilder := clonedBuilderRaw.(*cell.Builder)
		if clonedBuilder.BitsUsed() != bits {
			t.Fatalf("cloned builder bits = %d, want %d", clonedBuilder.BitsUsed(), bits)
		}

		clonedNestedRaw, err := cloned.RawIndex(3)
		if err != nil {
			t.Fatal(err)
		}
		clonedNested := clonedNestedRaw.(tuple.Tuple)
		clonedNestedIntRaw, err := clonedNested.RawIndex(0)
		if err != nil {
			t.Fatal(err)
		}
		if got := clonedNestedIntRaw.(*big.Int).Int64(); got != rawNestedInt {
			t.Fatalf("cloned nested int = %d, want %d", got, rawNestedInt)
		}
	})
}

func TestBuildMessageEmulationC7GlobalVersionLength(t *testing.T) {
	for _, tc := range []struct {
		version uint32
		wantLen int
	}{
		{version: 0, wantLen: 10},
		{version: 3, wantLen: 10},
		{version: 4, wantLen: 14},
		{version: 5, wantLen: 14},
		{version: 6, wantLen: 17},
		{version: 10, wantLen: 17},
		{version: 11, wantLen: 18},
		{version: 14, wantLen: 18},
	} {
		t.Run(big.NewInt(int64(tc.version)).String(), func(t *testing.T) {
			c7, err := buildMessageEmulationC7(tonopsTestAddr, cell.BeginCell().EndCell(), MessageEmulationConfig{}, big.NewInt(0), tc.version)
			if err != nil {
				t.Fatal(err)
			}

			raw, err := c7.RawIndex(0)
			if err != nil {
				t.Fatal(err)
			}
			inner := raw.(tuple.Tuple)
			if inner.Len() != tc.wantLen {
				t.Fatalf("inner c7 len = %d, want %d", inner.Len(), tc.wantLen)
			}
		})
	}
}

func FuzzBuildMessageEmulationC7VersionFieldBoundaries(f *testing.F) {
	for version := uint32(MinSupportedGlobalVersion); version <= uint32(MaxSupportedGlobalVersion); version++ {
		f.Add(version, uint8(version), uint64(0x1000)+uint64(version))
	}

	f.Fuzz(func(t *testing.T, rawVersion uint32, rawGlobalIndex uint8, marker uint64) {
		version := tvmFuzzGlobalVersionUint32(rawVersion)
		globalIndex := int(rawGlobalIndex%5) + 1
		base := int64(marker & 0x7fffffff)

		incomingGrams := big.NewInt(base + 11)
		prevBlocksValue := big.NewInt(base + 12)
		unpackedValue := big.NewInt(base + 13)
		duePayment := big.NewInt(base + 14)
		precompiledGas := big.NewInt(base + 15)
		inMsgValue := big.NewInt(base + 16)
		globalValue := big.NewInt(base + 17)
		balance := big.NewInt(base + 18)
		code := cell.BeginCell().MustStoreUInt(uint64(base)&0xff, 8).EndCell()

		c7, err := buildMessageEmulationC7(tonopsTestAddr, code, MessageEmulationConfig{
			Now:                 uint32(base) + 1,
			BlockLT:             base + 2,
			LogicalTime:         base + 3,
			RandSeed:            []byte{byte(base), byte(base >> 8), byte(base >> 16)},
			ConfigRoot:          cell.BeginCell().MustStoreUInt(uint64(base)&1, 1).EndCell(),
			IncomingValue:       tuple.NewTupleValue(incomingGrams, nil),
			StorageFees:         base + 4,
			PrevBlocks:          tuple.NewTupleValue(prevBlocksValue),
			UnpackedConfig:      tuple.NewTupleValue(unpackedValue),
			DuePayment:          duePayment,
			PrecompiledGasUsage: precompiledGas,
			InMsgParams:         tuple.NewTupleValue(inMsgValue),
			Globals: map[int]any{
				globalIndex: globalValue,
			},
		}, balance, version)
		if err != nil {
			t.Fatal(err)
		}

		incomingGrams.SetInt64(-1)
		prevBlocksValue.SetInt64(-1)
		unpackedValue.SetInt64(-1)
		duePayment.SetInt64(-1)
		precompiledGas.SetInt64(-1)
		inMsgValue.SetInt64(-1)
		globalValue.SetInt64(-1)
		balance.SetInt64(-1)

		if c7.Len() != globalIndex+1 {
			t.Fatalf("top c7 len version=%d = %d, want %d", version, c7.Len(), globalIndex+1)
		}
		assertMessageC7IntAt(t, c7, globalIndex, base+17, "global")

		inner := messageC7InnerForTest(t, c7)
		if inner.Len() != messageC7ExpectedLen(version) {
			t.Fatalf("inner c7 len version=%d = %d, want %d", version, inner.Len(), messageC7ExpectedLen(version))
		}

		assertMessageC7IntAt(t, inner, 3, int64(uint32(base)+1), "now")
		assertMessageC7IntAt(t, inner, 4, base+2, "block lt")
		assertMessageC7IntAt(t, inner, 5, base+3, "logical time")
		assertMessageC7TupleIntAt(t, inner, 7, 0, base+18, "balance")

		if version >= 4 {
			codeRaw, err := inner.RawIndex(10)
			if err != nil {
				t.Fatal(err)
			}
			if codeRaw.(*cell.Cell).HashKey() != code.HashKey() {
				t.Fatal("code cell mismatch")
			}
			assertMessageC7TupleIntAt(t, inner, 11, 0, base+11, "incoming value")
			assertMessageC7IntAt(t, inner, 12, base+4, "storage fees")
			assertMessageC7TupleIntAt(t, inner, 13, 0, base+12, "prev blocks")
		}
		if version >= 6 {
			assertMessageC7TupleIntAt(t, inner, 14, 0, base+13, "unpacked config")
			assertMessageC7IntAt(t, inner, 15, base+14, "due payment")
			assertMessageC7IntAt(t, inner, 16, base+15, "precompiled gas")
		}
		if version >= 11 {
			assertMessageC7TupleIntAt(t, inner, 17, 0, base+16, "in msg params")
		}
	})
}

func messageC7ExpectedLen(version uint32) int {
	if version >= 11 {
		return 18
	}
	if version >= 6 {
		return 17
	}
	if version >= 4 {
		return 14
	}
	return 10
}

func messageC7InnerForTest(t *testing.T, c7 tuple.Tuple) tuple.Tuple {
	t.Helper()

	raw, err := c7.RawIndex(0)
	if err != nil {
		t.Fatal(err)
	}
	return raw.(tuple.Tuple)
}

func assertMessageC7IntAt(t *testing.T, tup tuple.Tuple, idx int, want int64, name string) {
	t.Helper()

	raw, err := tup.RawIndex(idx)
	if err != nil {
		t.Fatalf("%s index %d: %v", name, idx, err)
	}
	if got := raw.(*big.Int).Int64(); got != want {
		t.Fatalf("%s index %d = %d, want %d", name, idx, got, want)
	}
}

func assertMessageC7TupleIntAt(t *testing.T, tup tuple.Tuple, idx, tupleIdx int, want int64, name string) {
	t.Helper()

	raw, err := tup.RawIndex(idx)
	if err != nil {
		t.Fatalf("%s tuple index %d: %v", name, idx, err)
	}
	inner := raw.(tuple.Tuple)
	assertMessageC7IntAt(t, inner, tupleIdx, want, name)
}

func TestBuildMessageEmulationC7RejectsReservedGlobalIndex(t *testing.T) {
	_, err := buildMessageEmulationC7(tonopsTestAddr, cell.BeginCell().EndCell(), MessageEmulationConfig{
		Globals: map[int]any{0: 1},
	}, big.NewInt(0), vmcore.DefaultGlobalVersion)
	if err == nil {
		t.Fatal("expected reserved global index to fail")
	}
}

func TestMessageExecutionGlobalVersionFallbackConfigRootAndErrors(t *testing.T) {
	got, err := messageExecutionGlobalVersion(MessageEmulationConfig{}, 6)
	if err != nil {
		t.Fatalf("fallback global version failed: %v", err)
	}
	if got != 6 {
		t.Fatalf("fallback global version = %d, want 6", got)
	}

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: 4})
	if err != nil {
		t.Fatalf("build global version cell: %v", err)
	}
	got, err = messageExecutionGlobalVersion(MessageEmulationConfig{
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion: versionCell,
		}),
	}, MaxSupportedGlobalVersion)
	if err != nil {
		t.Fatalf("config global version failed: %v", err)
	}
	if got != 4 {
		t.Fatalf("config global version = %d, want 4", got)
	}

	got, err = messageExecutionGlobalVersion(MessageEmulationConfig{
		ConfigRoot: messageExecutionGlobalVersionConfigRoot(t, 4),
	}, 3)
	if err != nil {
		t.Fatalf("config global version with fallback failed: %v", err)
	}
	if got != 4 {
		t.Fatalf("config global version with fallback = %d, want 4", got)
	}

	if _, err = messageExecutionGlobalVersion(MessageEmulationConfig{}, MaxSupportedGlobalVersion+1); err == nil {
		t.Fatal("unsupported fallback global version should fail")
	}

	unsupportedVersionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: uint32(MaxSupportedGlobalVersion + 1)})
	if err != nil {
		t.Fatalf("build unsupported global version cell: %v", err)
	}
	if _, err = messageExecutionGlobalVersion(MessageEmulationConfig{
		ConfigRoot: buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
			tlb.ConfigParamGlobalVersion: unsupportedVersionCell,
		}),
	}, 4); err == nil {
		t.Fatal("unsupported config global version should fail")
	}

	malformedRoot := buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: cell.BeginCell().MustStoreUInt(0, 8).EndCell(),
	})
	if _, err = messageExecutionGlobalVersion(MessageEmulationConfig{ConfigRoot: malformedRoot}, 4); err == nil {
		t.Fatal("malformed config global version should fail")
	}
}

func FuzzMessageExecutionGlobalVersionSelection(f *testing.F) {
	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		f.Add(uint16(version), uint16(MaxSupportedGlobalVersion-version), uint8(0))
		f.Add(uint16(MaxSupportedGlobalVersion+1), uint16(version), uint8(1))
		f.Add(uint16(version), uint16(version), uint8(2))
		f.Add(uint16(version), uint16(MaxSupportedGlobalVersion+1), uint8(3))
		f.Add(uint16(version), uint16(version), uint8(4))
	}

	f.Fuzz(func(t *testing.T, rawFallback, rawConfig uint16, rawCase uint8) {
		fallback := int(rawFallback % uint16(MaxSupportedGlobalVersion+3))
		cfg := MessageEmulationConfig{}

		var want int
		wantErr := false
		caseIdx := rawCase % 5
		switch caseIdx {
		case 0:
			want = fallback
			wantErr = fallback > MaxSupportedGlobalVersion
		case 1:
			cfg.ConfigRoot = buildTransactionConfigRoot(t, map[uint32]*cell.Cell{})
			if cfg.ConfigRoot == nil {
				want = fallback
				wantErr = fallback > MaxSupportedGlobalVersion
			} else {
				want = 0
			}
		case 2:
			version := tvmFuzzGlobalVersionUint32(uint32(rawConfig))
			cfg.ConfigRoot = messageExecutionGlobalVersionConfigRoot(t, version)
			want = int(version)
		case 3:
			version := uint32(MaxSupportedGlobalVersion + 1 + int(rawConfig%3))
			cfg.ConfigRoot = messageExecutionGlobalVersionConfigRoot(t, version)
			wantErr = true
		case 4:
			cfg.ConfigRoot = buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
				tlb.ConfigParamGlobalVersion: cell.BeginCell().MustStoreUInt(uint64(rawConfig&0xff), 8).EndCell(),
			})
			wantErr = true
		}

		got, err := messageExecutionGlobalVersion(cfg, fallback)
		if wantErr {
			if err == nil {
				t.Fatalf("case=%d fallback=%d config=%d got version %d, want error", caseIdx, fallback, rawConfig, got)
			}
			return
		}
		if err != nil {
			t.Fatalf("case=%d fallback=%d config=%d unexpected error: %v", caseIdx, fallback, rawConfig, err)
		}
		if got != want {
			t.Fatalf("case=%d fallback=%d config=%d version = %d, want %d", caseIdx, fallback, rawConfig, got, want)
		}
	})
}

func messageExecutionGlobalVersionConfigRoot(t *testing.T, version uint32) *cell.Cell {
	t.Helper()

	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{Version: version})
	if err != nil {
		t.Fatalf("build global version cell: %v", err)
	}
	return buildTransactionConfigRoot(t, map[uint32]*cell.Cell{
		tlb.ConfigParamGlobalVersion: versionCell,
	})
}
