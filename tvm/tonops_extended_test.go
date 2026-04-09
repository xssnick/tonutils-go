package tvm

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func makeStoragePricesSlice(validSince uint32, bitPrice, cellPrice, mcBitPrice, mcCellPrice uint64) *cell.Slice {
	return cell.BeginCell().
		MustStoreUInt(0xCC, 8).
		MustStoreUInt(uint64(validSince), 32).
		MustStoreUInt(bitPrice, 64).
		MustStoreUInt(cellPrice, 64).
		MustStoreUInt(mcBitPrice, 64).
		MustStoreUInt(mcCellPrice, 64).
		ToSlice()
}

func makeGasPricesSlice(flatLimit, flatPrice, gasPrice, gasLimit, specialLimit, gasCredit, blockGasLimit, freezeDue, deleteDue uint64, ext bool) *cell.Slice {
	b := cell.BeginCell().
		MustStoreUInt(0xD1, 8).
		MustStoreUInt(flatLimit, 64).
		MustStoreUInt(flatPrice, 64)
	if ext {
		b.MustStoreUInt(0xDE, 8).
			MustStoreUInt(gasPrice, 64).
			MustStoreUInt(gasLimit, 64).
			MustStoreUInt(specialLimit, 64).
			MustStoreUInt(gasCredit, 64)
	} else {
		b.MustStoreUInt(0xDD, 8).
			MustStoreUInt(gasPrice, 64).
			MustStoreUInt(gasLimit, 64).
			MustStoreUInt(gasCredit, 64)
	}
	return b.
		MustStoreUInt(blockGasLimit, 64).
		MustStoreUInt(freezeDue, 64).
		MustStoreUInt(deleteDue, 64).
		ToSlice()
}

func makeMsgPricesSlice(lump, bitPrice, cellPrice uint64, ihrFactor uint32, firstFrac, nextFrac uint16) *cell.Slice {
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

func makeSizeLimitsSlice(maxMsgBits, maxMsgCells uint64) *cell.Slice {
	return cell.BeginCell().
		MustStoreUInt(0x01, 8).
		MustStoreUInt(maxMsgBits, 32).
		MustStoreUInt(maxMsgCells, 32).
		MustStoreUInt(1000, 32).
		MustStoreUInt(512, 16).
		MustStoreUInt(65535, 32).
		MustStoreUInt(512, 16).
		ToSlice()
}

func makeInMsgParamsTuple() tuple.Tuple {
	src := cell.BeginCell().MustStoreAddr(address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")).ToSlice()
	return *tuple.NewTuple(
		int64(1),
		int64(0),
		src,
		int64(11),
		int64(22),
		int64(33),
		*tuple.NewTuple(big.NewInt(44), nil),
		*tuple.NewTuple(big.NewInt(55), nil),
		nil,
		nil,
	)
}

func feeTestC7(t *testing.T) tuple.Tuple {
	t.Helper()
	unpacked := tuple.NewTupleSized(7)
	mustSetTupleValue(t, &unpacked, 0, makeStoragePricesSlice(100, 3, 5, 7, 11))
	mustSetTupleValue(t, &unpacked, 2, makeGasPricesSlice(100, 77, 200, 1000, 1200, 50, 2000, 3000, 4000, true))
	mustSetTupleValue(t, &unpacked, 3, makeGasPricesSlice(100, 55, 150, 900, 900, 40, 1800, 2800, 3800, true))
	mustSetTupleValue(t, &unpacked, 4, makeMsgPricesSlice(1000, 200, 300, 500, 1000, 2000))
	mustSetTupleValue(t, &unpacked, 5, makeMsgPricesSlice(900, 120, 220, 400, 800, 1200))
	mustSetTupleValue(t, &unpacked, 6, makeSizeLimitsSlice(1<<20, 128))

	return makeTonopsTestC7(t, tonopsTestC7Config{
		UnpackedConfig: unpacked,
		ExtraParams: map[int]any{
			13: *tuple.NewTuple(int64(111), int64(222), int64(333)),
			15: int64(444),
			16: int64(555),
			17: makeInMsgParamsTuple(),
		},
	})
}

func newTonopsState(t *testing.T, globalVersion int, c7 tuple.Tuple, values ...any) *vmcore.State {
	t.Helper()
	st := vmcore.NewStack()
	for _, value := range values {
		switch v := value.(type) {
		case int:
			if err := st.PushInt(big.NewInt(int64(v))); err != nil {
				t.Fatalf("failed to push int: %v", err)
			}
		case int64:
			if err := st.PushInt(big.NewInt(v)); err != nil {
				t.Fatalf("failed to push int64: %v", err)
			}
		case *big.Int:
			if err := st.PushInt(v); err != nil {
				t.Fatalf("failed to push big int: %v", err)
			}
		default:
			if err := st.PushAny(value); err != nil {
				t.Fatalf("failed to push value: %v", err)
			}
		}
	}
	state := &vmcore.State{
		GlobalVersion: globalVersion,
		Stack:         st,
		Gas:           vmcore.NewGas(),
		Reg:           vmcore.Register{D: [2]*cell.Cell{cell.BeginCell().EndCell(), cell.BeginCell().EndCell()}, C7: c7},
	}
	state.InitForExecution()
	return state
}

func TestTonOpsExtendedParamsAndPRNG(t *testing.T) {
	c7 := feeTestC7(t)

	t.Run("PrevBlocksAndInMsgFamilies", func(t *testing.T) {
		code := codeFromBuilders(t,
			funcsop.PREVBLOCKSINFOTUPLE().Serialize(),
			funcsop.PREVMCBLOCKS().Serialize(),
			funcsop.PREVKEYBLOCK().Serialize(),
			funcsop.PREVMCBLOCKS_100().Serialize(),
			funcsop.DUEPAYMENT().Serialize(),
			funcsop.GETPRECOMPILEDGAS().Serialize(),
		)
		st, res, err := runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		st = res.Stack
		gotPrecompiled, _ := st.PopIntFinite()
		if gotPrecompiled.Int64() != 555 {
			t.Fatalf("unexpected GETPRECOMPILEDGAS value: %s", gotPrecompiled.String())
		}
		gotDue, _ := st.PopIntFinite()
		if gotDue.Int64() != 444 {
			t.Fatalf("unexpected DUEPAYMENT value: %s", gotDue.String())
		}
		got100, _ := st.PopIntFinite()
		gotKey, _ := st.PopIntFinite()
		gotPrev, _ := st.PopIntFinite()
		if gotPrev.Int64() != 111 || gotKey.Int64() != 222 || got100.Int64() != 333 {
			t.Fatalf("unexpected prev blocks tuple values: %s %s %s", gotPrev.String(), gotKey.String(), got100.String())
		}
		tup, err := st.PopTuple()
		if err != nil {
			t.Fatalf("failed to pop PREVBLOCKSINFOTUPLE: %v", err)
		}
		if tup.Len() != 3 {
			t.Fatalf("unexpected prev blocks tuple len: %d", tup.Len())
		}
	})

	t.Run("InMsgAliases", func(t *testing.T) {
		code := codeFromBuilders(t, funcsop.INMSG_VALUE().Serialize())
		st, res, err := runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion)
		if err != nil {
			t.Fatalf("unexpected INMSG_VALUE error: %v", err)
		}
		val, err := res.Stack.PopTuple()
		if err != nil {
			t.Fatalf("failed to pop INMSG_VALUE: %v", err)
		}
		if val.Len() != 2 {
			t.Fatalf("unexpected INMSG_VALUE tuple len: %d", val.Len())
		}
		_, _ = st, val
	})

	t.Run("RandOpsUpdateSeed", func(t *testing.T) {
		state := newTonopsState(t, vmcore.DefaultGlobalVersion, c7)
		if err := funcsop.RANDU256().Interpret(state); err != nil {
			t.Fatalf("RANDU256 failed: %v", err)
		}
		got, err := state.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop randu256: %v", err)
		}
		sum := sha512.Sum512(tonopsTestSeed)
		if got.Cmp(new(big.Int).SetBytes(sum[32:])) != 0 {
			t.Fatalf("unexpected RANDU256 output")
		}
		seedAny, err := state.GetParam(6)
		if err != nil {
			t.Fatalf("failed to read updated seed: %v", err)
		}
		seedInt, ok := seedAny.(*big.Int)
		if !ok || seedInt.Cmp(new(big.Int).SetBytes(sum[:32])) != 0 {
			t.Fatalf("unexpected updated seed")
		}

		state = newTonopsState(t, vmcore.DefaultGlobalVersion, c7, big.NewInt(7))
		if err := funcsop.ADDRAND().Interpret(state); err != nil {
			t.Fatalf("ADDRAND failed: %v", err)
		}
		mix := make([]byte, 64)
		copy(mix, tonopsTestSeed)
		copy(mix[32:], cell.BeginCell().MustStoreBigUInt(big.NewInt(7), 256).EndCell().BeginParse().MustLoadSlice(256))
		wantMix := sha256.Sum256(mix)
		seedAny, err = state.GetParam(6)
		if err != nil {
			t.Fatalf("failed to read mixed seed: %v", err)
		}
		if seedAny.(*big.Int).Cmp(new(big.Int).SetBytes(wantMix[:])) != 0 {
			t.Fatalf("unexpected mixed seed")
		}
	})
}

func TestTonOpsExtendedFeesAndHashing(t *testing.T) {
	c7 := feeTestC7(t)

	t.Run("FeeHelpers", func(t *testing.T) {
		tests := []struct {
			name string
			code *cell.Cell
			args []any
			want int64
		}{
			{"GETGASFEE", codeFromBuilders(t, funcsop.GETGASFEE().Serialize()), []any{int64(250), int64(0)}, 56},
			{"GETSTORAGEFEE", codeFromBuilders(t, funcsop.GETSTORAGEFEE().Serialize()), []any{int64(2), int64(3), int64(10), int64(0)}, 1},
			{"GETFORWARDFEE", codeFromBuilders(t, funcsop.GETFORWARDFEE().Serialize()), []any{int64(2), int64(8), int64(0)}, 901},
			{"GETORIGINALFWDFEE", codeFromBuilders(t, funcsop.GETORIGINALFWDFEE().Serialize()), []any{big.NewInt(3200), int64(0)}, 3239},
			{"GETGASFEESIMPLE", codeFromBuilders(t, funcsop.GETGASFEESIMPLE().Serialize()), []any{int64(250), int64(0)}, 1},
			{"GETFORWARDFEESIMPLE", codeFromBuilders(t, funcsop.GETFORWARDFEESIMPLE().Serialize()), []any{int64(2), int64(8), int64(0)}, 1},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, res, err := runRawCodeWithEnv(t, tt.code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, tt.args...)
				if err != nil {
					t.Fatalf("unexpected %s error: %v", tt.name, err)
				}
				got, err := res.Stack.PopIntFinite()
				if err != nil {
					t.Fatalf("failed to pop %s result: %v", tt.name, err)
				}
				if got.Int64() != tt.want {
					t.Fatalf("unexpected %s: %s", tt.name, got.String())
				}
			})
		}

		_, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.GETGASFEE().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			int64(-1),
			int64(0),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected negative GETGASFEE exit code: %d", code)
		}

		_, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.GETGASFEESIMPLE().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			int64(-1),
			int64(0),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected negative GETGASFEESIMPLE exit code: %d", code)
		}
	})

	t.Run("SHA256UAndHASHEXT", func(t *testing.T) {
		data := []byte("hello world")
		wantSHA := sha256.Sum256(data)
		_, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.SHA256U().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			cell.BeginCell().MustStoreSlice(data, uint(len(data)*8)).ToSlice(),
		)
		if err != nil {
			t.Fatalf("unexpected SHA256U error: %v", err)
		}
		gotSHA, _ := res.Stack.PopIntFinite()
		if gotSHA.Cmp(new(big.Int).SetBytes(wantSHA[:])) != 0 {
			t.Fatalf("unexpected SHA256U")
		}

		_, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.HASHEXT(0).Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			cell.BeginCell().MustStoreSlice(data, uint(len(data)*8)).ToSlice(),
			int64(1),
		)
		if err != nil {
			t.Fatalf("unexpected HASHEXT error: %v", err)
		}
		gotHashExt, _ := res.Stack.PopIntFinite()
		if gotHashExt.Cmp(new(big.Int).SetBytes(wantSHA[:])) != 0 {
			t.Fatalf("unexpected HASHEXT")
		}
	})

	t.Run("HASHBU", func(t *testing.T) {
		builder := cell.BeginCell().MustStoreUInt(0xAB, 8)
		state := newTonopsState(t, vmcore.DefaultGlobalVersion, c7, builder)
		if err := funcsop.HASHBU().Interpret(state); err != nil {
			t.Fatalf("HASHBU failed: %v", err)
		}
		got, _ := state.Stack.PopIntFinite()
		want := builder.EndCell().Hash()
		if got.Cmp(new(big.Int).SetBytes(want)) != 0 {
			t.Fatalf("unexpected HASHBU")
		}
	})

	t.Run("GETEXTRABALANCEUsesBalanceExtraDict", func(t *testing.T) {
		extraDict := cell.NewDict(32)
		if _, err := extraDict.SetBuilderWithMode(
			cell.BeginCell().MustStoreUInt(7, 32).EndCell(),
			cell.BeginCell().MustStoreVarUInt(12345, 32),
			cell.DictSetModeSet,
		); err != nil {
			t.Fatalf("failed to seed extra balance dict: %v", err)
		}

		c7WithExtra := makeTonopsTestC7(t, tonopsTestC7Config{
			Balance: *tuple.NewTuple(new(big.Int).Set(tonopsTestBalance), extraDict.AsCell()),
		})

		_, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			cell.BeginCell().EndCell(),
			c7WithExtra,
			10,
			int64(7),
		)
		if err != nil {
			t.Fatalf("unexpected GETEXTRABALANCE error: %v", err)
		}
		got, _ := res.Stack.PopIntFinite()
		if got.Int64() != 12345 {
			t.Fatalf("unexpected GETEXTRABALANCE value: %s", got.String())
		}

		_, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			cell.BeginCell().EndCell(),
			c7WithExtra,
			10,
			int64(9),
		)
		if err != nil {
			t.Fatalf("unexpected GETEXTRABALANCE miss error: %v", err)
		}
		got, _ = res.Stack.PopIntFinite()
		if got.Sign() != 0 {
			t.Fatalf("expected zero for missing extra balance, got %s", got.String())
		}

		_, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.GETEXTRABALANCE().Serialize()),
			cell.BeginCell().EndCell(),
			c7WithExtra,
			10,
			int64(-1),
		)
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected negative GETEXTRABALANCE exit code: %d", code)
		}
	})
}

func TestTonOpsExtendedDataSizeVarintAddressAndMessages(t *testing.T) {
	c7 := feeTestC7(t)

	t.Run("DataSizeCountsUniqueCellsAndQuietBound", func(t *testing.T) {
		leaf := cell.BeginCell().MustStoreUInt(1, 1).EndCell()
		root := cell.BeginCell().MustStoreRef(leaf).MustStoreRef(leaf).EndCell()
		code := codeFromBuilders(t, funcsop.CDATASIZE().Serialize())
		_, res, err := runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, root, int64(10))
		if err != nil {
			t.Fatalf("unexpected CDATASIZE error: %v", err)
		}
		st := res.Stack
		refs, _ := st.PopIntFinite()
		bits, _ := st.PopIntFinite()
		cellsCnt, _ := st.PopIntFinite()
		if cellsCnt.Int64() != 2 || bits.Int64() != 1 || refs.Int64() != 2 {
			t.Fatalf("unexpected datasize values: cells=%s bits=%s refs=%s", cellsCnt.String(), bits.String(), refs.String())
		}

		code = codeFromBuilders(t, funcsop.CDATASIZEQ().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, root, int64(1))
		if err != nil {
			t.Fatalf("unexpected CDATASIZEQ error: %v", err)
		}
		ok, _ := res.Stack.PopBool()
		if ok {
			t.Fatalf("expected CDATASIZEQ bound failure")
		}
	})

	t.Run("VarIntAndAddrOps", func(t *testing.T) {
		addr := address.MustParseAddr("EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c")
		msgAddr := cell.BeginCell().MustStoreAddr(addr).ToSlice()

		_, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.LDVARINT16().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			cell.BeginCell().MustStoreUInt(1, 4).MustStoreInt(-17, 8).ToSlice(),
		)
		if err != nil {
			t.Fatalf("unexpected LDVARINT16 error: %v", err)
		}
		loadedRest, _ := res.Stack.PopSlice()
		loadedVal, _ := res.Stack.PopIntFinite()
		if loadedVal.Int64() != -17 || loadedRest.BitsLeft() != 0 {
			t.Fatalf("unexpected LDVARINT16 result")
		}

		_, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.LDVARINT16().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			cell.BeginCell().MustStoreUInt(2, 4).MustStoreInt(-1, 16).ToSlice(),
		)
		if err != nil {
			t.Fatalf("unexpected non-canonical LDVARINT16 error: %v", err)
		}
		loadedRest, _ = res.Stack.PopSlice()
		loadedVal, _ = res.Stack.PopIntFinite()
		if loadedVal.Int64() != -1 || loadedRest.BitsLeft() != 0 {
			t.Fatalf("unexpected non-canonical LDVARINT16 result")
		}

		_, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.LDMSGADDR().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			msgAddr,
		)
		if err != nil {
			t.Fatalf("unexpected LDMSGADDR error: %v", err)
		}
		loadedRest, err = res.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop LDMSGADDR rest: %v", err)
		}
		loadedAddr, err := res.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop LDMSGADDR addr: %v", err)
		}
		if loadedRest.BitsLeft() != 0 || loadedAddr.BitsLeft() == 0 {
			t.Fatalf("unexpected LDMSGADDR result")
		}

		_, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.PARSEMSGADDR().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			msgAddr,
		)
		if err != nil {
			t.Fatalf("unexpected PARSEMSGADDR error: %v", err)
		}
		parsed, err := res.Stack.PopTuple()
		if err != nil {
			t.Fatalf("failed to pop parsed addr tuple: %v", err)
		}
		if parsed.Len() != 4 {
			t.Fatalf("unexpected PARSEMSGADDR tuple len: %d", parsed.Len())
		}

		_, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.REWRITESTDADDR().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			msgAddr,
		)
		if err != nil {
			t.Fatalf("unexpected REWRITESTDADDR error: %v", err)
		}
		st := res.Stack
		rwAddr, _ := st.PopIntFinite()
		rwWC, _ := st.PopIntFinite()
		if rwWC.Int64() != int64(addr.Workchain()) || rwAddr.Cmp(new(big.Int).SetBytes(addr.Data())) != 0 {
			t.Fatalf("unexpected REWRITESTDADDR result")
		}

		addrNoneTail := cell.BeginCell().MustStoreUInt(0, 2).MustStoreUInt(0xA, 4).ToSlice()
		_, res, err = runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.LDOPTSTDADDR().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			12,
			addrNoneTail,
		)
		if err != nil {
			t.Fatalf("unexpected LDOPTSTDADDR addr_none error: %v", err)
		}
		rest, err := res.Stack.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop LDOPTSTDADDR rest: %v", err)
		}
		rawNil, err := res.Stack.PopAny()
		if err != nil {
			t.Fatalf("failed to pop LDOPTSTDADDR null value: %v", err)
		}
		if rawNil != nil || rest.BitsLeft() != 4 {
			t.Fatalf("unexpected LDOPTSTDADDR addr_none result")
		}
	})

	t.Run("AddressCornerCases", func(t *testing.T) {
		invalidAnycast := cell.BeginCell().
			MustStoreUInt(0b10, 2).
			MustStoreBoolBit(true).
			MustStoreUInt(31, 5).
			MustStoreSlice(bytes.Repeat([]byte{0xFF}, 4), 31).
			MustStoreInt(int64(tonopsTestAddr.Workchain()), 8).
			MustStoreSlice(tonopsTestAddr.Data(), 256).
			ToSlice()

		_, res, err := runRawCodeWithEnv(t,
			codeFromBuilders(t, funcsop.PARSEMSGADDRQ().Serialize()),
			cell.BeginCell().EndCell(),
			c7,
			9,
			invalidAnycast,
		)
		if err != nil {
			t.Fatalf("unexpected PARSEMSGADDRQ anycast error: %v", err)
		}
		ok, err := res.Stack.PopBool()
		if err != nil {
			t.Fatalf("failed to pop PARSEMSGADDRQ flag: %v", err)
		}
		if ok {
			t.Fatalf("expected PARSEMSGADDRQ to reject anycast depth 31")
		}
	})

	t.Run("QuietAddrFailureAndMessageActions", func(t *testing.T) {
		invalid := cell.BeginCell().MustStoreUInt(0b11, 2).ToSlice()
		code := codeFromBuilders(t, funcsop.LDMSGADDRQ().Serialize())
		_, res, err := runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, invalid)
		if err != nil {
			t.Fatalf("unexpected LDMSGADDRQ error: %v", err)
		}
		st := res.Stack
		ok, _ := st.PopBool()
		if ok {
			t.Fatalf("expected LDMSGADDRQ failure")
		}

		addrNoneTail := cell.BeginCell().MustStoreUInt(0, 2).MustStoreUInt(0xA, 4).ToSlice()
		code = codeFromBuilders(t, funcsop.LDSTDADDRQ().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, addrNoneTail)
		if err != nil {
			t.Fatalf("unexpected LDSTDADDRQ error: %v", err)
		}
		st = res.Stack
		ok, _ = st.PopBool()
		if ok {
			t.Fatalf("expected LDSTDADDRQ failure for addr_none")
		}
		restoredLDSTD, err := st.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop LDSTDADDRQ restored slice: %v", err)
		}
		if !bytes.Equal(restoredLDSTD.MustLoadSlice(restoredLDSTD.BitsLeft()), addrNoneTail.MustLoadSlice(addrNoneTail.BitsLeft())) {
			t.Fatalf("unexpected LDSTDADDRQ restored slice")
		}

		shortSlice := cell.BeginCell().MustStoreUInt(1, 1).ToSlice()
		code = codeFromBuilders(t, funcsop.LDOPTSTDADDRQ().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, shortSlice)
		if err != nil {
			t.Fatalf("unexpected LDOPTSTDADDRQ short error: %v", err)
		}
		st = res.Stack
		ok, _ = st.PopBool()
		if ok {
			t.Fatalf("expected LDOPTSTDADDRQ short failure")
		}
		restoredShort, err := st.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop LDOPTSTDADDRQ restored slice: %v", err)
		}
		if restoredShort.BitsLeft() != shortSlice.BitsLeft() {
			t.Fatalf("unexpected LDOPTSTDADDRQ restored slice")
		}

		code = codeFromBuilders(t, funcsop.REWRITESTDADDRQ().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, addrNoneTail)
		if err != nil {
			t.Fatalf("unexpected REWRITESTDADDRQ error: %v", err)
		}
		st = res.Stack
		ok, _ = st.PopBool()
		if ok {
			t.Fatalf("expected REWRITESTDADDRQ failure for addr_none")
		}

		code = codeFromBuilders(t, funcsop.STSTDADDRQ().Serialize())
		_, res, err = runRawCodeWithEnv(t,
			code,
			cell.BeginCell().EndCell(),
			c7,
			vmcore.DefaultGlobalVersion,
			addrNoneTail,
			cell.BeginCell(),
		)
		if err != nil {
			t.Fatalf("unexpected STSTDADDRQ quiet invalid-address error: %v", err)
		}
		st = res.Stack
		ok, _ = st.PopBool()
		if !ok {
			t.Fatalf("expected STSTDADDRQ quiet failure flag")
		}
		builder, err := st.PopBuilder()
		if err != nil {
			t.Fatalf("failed to pop STSTDADDRQ builder: %v", err)
		}
		restoredStd, err := st.PopSlice()
		if err != nil {
			t.Fatalf("failed to pop STSTDADDRQ restored slice: %v", err)
		}
		if builder.BitsUsed() != 0 || builder.RefsUsed() != 0 || restoredStd.BitsLeft() != addrNoneTail.BitsLeft() {
			t.Fatalf("unexpected STSTDADDRQ restored values")
		}

		code = codeFromBuilders(t, funcsop.STOPTSTDADDRQ().Serialize())
		_, res, err = runRawCodeWithEnv(t,
			code,
			cell.BeginCell().EndCell(),
			c7,
			12,
			int64(7),
			cell.BeginCell(),
		)
		if err != nil {
			t.Fatalf("unexpected STOPTSTDADDRQ quiet type mismatch error: %v", err)
		}
		st = res.Stack
		ok, _ = st.PopBool()
		if !ok {
			t.Fatalf("expected STOPTSTDADDRQ quiet failure flag")
		}
		builder, err = st.PopBuilder()
		if err != nil {
			t.Fatalf("failed to pop STOPTSTDADDRQ builder: %v", err)
		}
		raw, err := st.PopAny()
		if err != nil {
			t.Fatalf("failed to pop STOPTSTDADDRQ restored value: %v", err)
		}
		if builder.BitsUsed() != 0 || builder.RefsUsed() != 0 || raw != nil {
			t.Fatalf("unexpected STOPTSTDADDRQ restored values")
		}

		msg := &tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     tonopsTestAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.MustFromNano(big.NewInt(1000), 9),
			IHRFee:      tlb.MustFromNano(big.NewInt(0), 9),
			FwdFee:      tlb.MustFromNano(big.NewInt(0), 9),
			Body:        cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell(),
		}
		msgCell, err := tlb.ToCell(msg)
		if err != nil {
			t.Fatalf("failed to build message: %v", err)
		}

		code = codeFromBuilders(t, funcsop.SENDMSG().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, msgCell, int64(1024))
		if err != nil {
			t.Fatalf("unexpected SENDMSG fee-only error: %v", err)
		}
		fee, _ := res.Stack.PopIntFinite()
		if fee.Sign() <= 0 {
			t.Fatalf("expected positive SENDMSG fee")
		}
		emptyActions := cell.BeginCell().EndCell()
		if res.Actions == nil || !bytes.Equal(res.Actions.Hash(), emptyActions.Hash()) {
			t.Fatalf("expected SENDMSG fee-only path to leave actions unchanged")
		}

		code = codeFromBuilders(t, funcsop.SETCODE().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, msgCell)
		if err != nil {
			t.Fatalf("unexpected SETCODE error: %v", err)
		}
		wantSetCode := cell.BeginCell().
			MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreUInt(0xAD4DE08E, 32).
			MustStoreRef(msgCell).
			EndCell()
		if !bytes.Equal(res.Actions.Hash(), wantSetCode.Hash()) {
			t.Fatalf("unexpected SETCODE action")
		}

		code = codeFromBuilders(t, funcsop.RAWRESERVEX().Serialize())
		extra := cell.BeginCell().MustStoreUInt(0xCC, 8).EndCell()
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, big.NewInt(777), extra, int64(3))
		if err != nil {
			t.Fatalf("unexpected RAWRESERVEX error: %v", err)
		}
		wantReserveX := cell.BeginCell().
			MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreUInt(0x36e6b809, 32).
			MustStoreUInt(3, 8).
			MustStoreBigCoins(big.NewInt(777)).
			MustStoreMaybeRef(extra).
			EndCell()
		if !bytes.Equal(res.Actions.Hash(), wantReserveX.Hash()) {
			t.Fatalf("unexpected RAWRESERVEX action")
		}

		code = codeFromBuilders(t, funcsop.RAWRESERVE().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, int64(-1), int64(0))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected RAWRESERVE negative exit code: %d", code)
		}

		code = codeFromBuilders(t, funcsop.RAWRESERVEX().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, int64(-1), extra, int64(0))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected RAWRESERVEX negative exit code: %d", code)
		}

		code = codeFromBuilders(t, funcsop.SETLIBCODE().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, msgCell, int64(1))
		if err != nil {
			t.Fatalf("unexpected SETLIBCODE error: %v", err)
		}
		wantSetLibCode := cell.BeginCell().
			MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreUInt(0x26FA1DD4, 32).
			MustStoreUInt(3, 8).
			MustStoreRef(msgCell).
			EndCell()
		if !bytes.Equal(res.Actions.Hash(), wantSetLibCode.Hash()) {
			t.Fatalf("unexpected SETLIBCODE action")
		}

		code = codeFromBuilders(t, funcsop.SETLIBCODE().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, msgCell, int64(4))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected SETLIBCODE invalid-mode exit code: %d", code)
		}

		code = codeFromBuilders(t, funcsop.CHANGELIB().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, big.NewInt(1), int64(1))
		if err != nil {
			t.Fatalf("unexpected CHANGELIB error: %v", err)
		}
		wantChangeLib := cell.BeginCell().
			MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreUInt(0x26FA1DD4, 32).
			MustStoreUInt(2, 8).
			MustStoreBigUInt(big.NewInt(1), 256).
			EndCell()
		if !bytes.Equal(res.Actions.Hash(), wantChangeLib.Hash()) {
			t.Fatalf("unexpected CHANGELIB action")
		}

		code = codeFromBuilders(t, funcsop.CHANGELIB().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, big.NewInt(-1), int64(1))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected CHANGELIB negative-hash exit code: %d", code)
		}

		code = codeFromBuilders(t, funcsop.SENDMSG().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, msgCell, int64(1))
		if err != nil {
			t.Fatalf("unexpected SENDMSG send error: %v", err)
		}
		sendFee, err := res.Stack.PopIntFinite()
		if err != nil {
			t.Fatalf("failed to pop SENDMSG send fee: %v", err)
		}
		if sendFee.Sign() <= 0 {
			t.Fatalf("expected positive SENDMSG send fee")
		}
		wantSendMsg := cell.BeginCell().
			MustStoreRef(cell.BeginCell().EndCell()).
			MustStoreUInt(0x0EC3C86D, 32).
			MustStoreUInt(1, 8).
			MustStoreRef(msgCell).
			EndCell()
		if res.Actions == nil || !bytes.Equal(res.Actions.Hash(), wantSendMsg.Hash()) {
			t.Fatalf("unexpected SENDMSG action")
		}

		code = codeFromBuilders(t, funcsop.SENDMSG().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, msgCell, int64(256))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeRangeCheck {
			t.Fatalf("unexpected SENDMSG invalid-mode exit code: %d", code)
		}

		invalidMsgCell := cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()
		code = codeFromBuilders(t, funcsop.SENDMSG().Serialize())
		_, res, err = runRawCodeWithEnv(t, code, cell.BeginCell().EndCell(), c7, vmcore.DefaultGlobalVersion, invalidMsgCell, int64(1))
		if code := exitCodeFromResult(res, err); code != vmerr.CodeUnknown {
			t.Fatalf("unexpected SENDMSG invalid-message exit code: %d", code)
		}
	})
}
