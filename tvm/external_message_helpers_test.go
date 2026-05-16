package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
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

		if got := messageUnpackedConfig(MessageEmulationConfig{}); got != nil {
			t.Fatalf("zero config should not synthesize unpacked config, got %T", got)
		}

		cfgTuple := tuple.NewTupleValue("cfg")
		if got := messageUnpackedConfig(MessageEmulationConfig{UnpackedConfig: cfgTuple}); func() int {
			tup := got.(tuple.Tuple)
			return tup.Len()
		}() != 1 {
			t.Fatalf("explicit unpacked config should pass through, got %T", got)
		}

		synth := messageUnpackedConfig(MessageEmulationConfig{GlobalID: 0x11223344}).(tuple.Tuple)
		if synth.Len() != 2 {
			t.Fatalf("unexpected synthesized unpacked config len: %d", synth.Len())
		}
		raw, err := synth.RawIndex(1)
		if err != nil {
			t.Fatal(err)
		}
		if got := raw.(*cell.Slice).MustLoadUInt(32); got != 0x11223344 {
			t.Fatalf("unexpected synthesized global id: %x", got)
		}

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

		orig := big.NewInt(55)
		cp := normalizeMessageTupleValue(orig).(*big.Int)
		orig.SetInt64(99)
		if cp.Int64() != 55 {
			t.Fatalf("big.Int normalization should copy input, got %d", cp.Int64())
		}
		origValue := *big.NewInt(77)
		cp = normalizeMessageTupleValue(origValue).(*big.Int)
		origValue.SetInt64(99)
		if cp.Int64() != 77 {
			t.Fatalf("big.Int value normalization should copy input, got %d", cp.Int64())
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
	c7, err := buildMessageEmulationC7(tonopsTestAddr, code, cfg, balance)
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
	c7, err := buildMessageEmulationC7(tonopsTestAddr, cell.BeginCell().EndCell(), MessageEmulationConfig{}, big.NewInt(0))
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

	c7, err := buildMessageEmulationC7(tonopsTestAddr, cell.BeginCell().EndCell(), cfg, big.NewInt(0))
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

func TestBuildMessageEmulationC7RejectsReservedGlobalIndex(t *testing.T) {
	_, err := buildMessageEmulationC7(tonopsTestAddr, cell.BeginCell().EndCell(), MessageEmulationConfig{
		Globals: map[int]any{0: 1},
	}, big.NewInt(0))
	if err == nil {
		t.Fatal("expected reserved global index to fail")
	}
}
