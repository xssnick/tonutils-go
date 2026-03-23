//go:build darwin && cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type crossRunResult struct {
	exitCode int32
	stack    *cell.Cell
}

var (
	crossTestAddr      = address.MustParseAddr("EQAYqo4u7VF0fa4DPAebk4g9lBytj2VFny7pzXR0trjtXQaO")
	crossTestTime      = time.Unix(1_710_000_000, 0).UTC()
	crossTestSeed      = make([]byte, 32)
	crossTestBalance   = big.NewInt(10_000_000)
	crossTestMaxGas    = int64(1_000_000_000)
	crossTestWalletPub = mustBytesFromHex("f7a26c623ca2429ae80fbb17786b0b523ba71c1dbb13fbcdb8ded762a71cd867")
	crossTestWalletSeq = uint32(42)
	crossTestSubwallet = uint32(698983191)
	crossTestWalletV1  = mustCellFromHex("B5EE9C7241010101005F0000BAFF0020DD2082014C97BA218201339CBAB19C71B0ED44D0D31FD70BFFE304E0A4F260810200D71820D70B1FED44D0D31FD3FFD15112BAF2A122F901541044F910F2A2F80001D31F3120D74A96D307D402FB00DED1A4C8CB1FCBFFC9ED54B5B86E42")
	crossTestWalletV2  = mustCellFromHex("B5EE9C724101010100630000C2FF0020DD2082014C97BA218201339CBAB19C71B0ED44D0D31FD70BFFE304E0A4F2608308D71820D31FD31F01F823BBF263ED44D0D31FD3FFD15131BAF2A103F901541042F910F2A2F800029320D74A96D307D402FB00E8D1A4C8CB1FCBFFC9ED54044CD7A1")
	crossTestWalletV3  = mustCellFromHex("B5EE9C724101010100710000DEFF0020DD2082014C97BA218201339CBAB19F71B0ED44D0D31FD31F31D70BFFE304E0A4F2608308D71820D31FD31FD31FF82313BBF263ED44D0D31FD31FD3FFD15132BAF2A15144BAF2A204F901541055F910F2A3F8009320D74A96D307D402FB00E8D101A4C8CB1FCB1FCBFFC9ED5410BD6DAD")
	crossTestWalletV4  = mustCellFromHex("B5EE9C72410214010002D4000114FF00F4A413F4BCF2C80B010201200203020148040504F8F28308D71820D31FD31FD31F02F823BBF264ED44D0D31FD31FD3FFF404D15143BAF2A15151BAF2A205F901541064F910F2A3F80024A4C8CB1F5240CB1F5230CBFF5210F400C9ED54F80F01D30721C0009F6C519320D74A96D307D402FB00E830E021C001E30021C002E30001C0039130E30D03A4C8CB1F12CB1FCBFF1011121302E6D001D0D3032171B0925F04E022D749C120925F04E002D31F218210706C7567BD22821064737472BDB0925F05E003FA403020FA4401C8CA07CBFFC9D0ED44D0810140D721F404305C810108F40A6FA131B3925F07E005D33FC8258210706C7567BA923830E30D03821064737472BA925F06E30D06070201200809007801FA00F40430F8276F2230500AA121BEF2E0508210706C7567831EB17080185004CB0526CF1658FA0219F400CB6917CB1F5260CB3F20C98040FB0006008A5004810108F45930ED44D0810140D720C801CF16F400C9ED540172B08E23821064737472831EB17080185005CB055003CF1623FA0213CB6ACB1FCB3FC98040FB00925F03E20201200A0B0059BD242B6F6A2684080A06B90FA0218470D4080847A4937D29910CE6903E9FF9837812801B7810148987159F31840201580C0D0011B8C97ED44D0D70B1F8003DB29DFB513420405035C87D010C00B23281F2FFF274006040423D029BE84C600201200E0F0019ADCE76A26840206B90EB85FFC00019AF1DF6A26840106B90EB858FC0006ED207FA00D4D422F90005C8CA0715CBFFC9D077748018C8CB05CB0222CF165005FA0214CB6B12CCCCC973FB00C84014810108F451F2A7020070810108D718FA00D33FC8542047810108F451F2A782106E6F746570748018C8CB05CB025006CF165004FA0214CB6A12CB1FCB3FC973FB0002006C810108D718FA00D33F305224810108F459F2A782106473747270748018C8CB05CB025005CF165003FA0213CB6ACB1F12CB3FC973FB00000AF400C9ED54696225E5")
)

func TestTVMCrossEmulatorReference(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	type testCase struct {
		name   string
		code   *cell.Cell
		data   *cell.Cell
		method string
		args   []int64
		c7     tuple.Tuple
	}

	mainC7 := prepareCrossTestC7(nil, MainContractCode)
	walletV1Data := makeWalletV1V2Data(crossTestWalletSeq, crossTestWalletPub)
	walletV2Data := makeWalletV1V2Data(crossTestWalletSeq, crossTestWalletPub)
	walletV3Data := makeWalletV3Data(crossTestWalletSeq, crossTestSubwallet, crossTestWalletPub)
	walletV4Data := makeWalletV4Data(crossTestWalletSeq, crossTestSubwallet, crossTestWalletPub)

	tests := []testCase{
		{
			name:   "main_simple_repeat",
			code:   MainContractCode,
			data:   cell.BeginCell().EndCell(),
			method: "simpleRepeat",
			args:   []int64{2},
			c7:     mainC7,
		},
		{
			name:   "main_simple_repeat_while",
			code:   MainContractCode,
			data:   cell.BeginCell().EndCell(),
			method: "simpleRepeatWhile",
			args:   []int64{2},
			c7:     mainC7,
		},
		{
			name:   "main_simple_until_while",
			code:   MainContractCode,
			data:   cell.BeginCell().EndCell(),
			method: "simpleUntilWhile",
			args:   []int64{2},
			c7:     mainC7,
		},
		{
			name:   "main_simple_repeat_until",
			code:   MainContractCode,
			data:   cell.BeginCell().EndCell(),
			method: "simpleRepeatUntil",
			args:   []int64{2},
			c7:     mainC7,
		},
		{
			name:   "main_try_repeat_while_until_success",
			code:   MainContractCode,
			data:   cell.BeginCell().EndCell(),
			method: "tryRepeatWhileUntil",
			args:   []int64{0},
			c7:     mainC7,
		},
		{
			name:   "main_try_repeat_while_until_catch",
			code:   MainContractCode,
			data:   cell.BeginCell().EndCell(),
			method: "tryRepeatWhileUntil",
			args:   []int64{1},
			c7:     mainC7,
		},
		{
			name:   "wallet_v1r3_seqno",
			code:   crossTestWalletV1,
			data:   walletV1Data,
			method: "seqno",
			c7:     prepareCrossTestC7(nil, crossTestWalletV1),
		},
		{
			name:   "wallet_v2r2_seqno",
			code:   crossTestWalletV2,
			data:   walletV2Data,
			method: "seqno",
			c7:     prepareCrossTestC7(nil, crossTestWalletV2),
		},
		{
			name:   "wallet_v3r2_seqno",
			code:   crossTestWalletV3,
			data:   walletV3Data,
			method: "seqno",
			c7:     prepareCrossTestC7(nil, crossTestWalletV3),
		},
		{
			name:   "wallet_v3r2_get_public_key",
			code:   crossTestWalletV3,
			data:   walletV3Data,
			method: "get_public_key",
			c7:     prepareCrossTestC7(nil, crossTestWalletV3),
		},
		{
			name:   "wallet_v4r2_seqno",
			code:   crossTestWalletV4,
			data:   walletV4Data,
			method: "seqno",
			c7:     prepareCrossTestC7(nil, crossTestWalletV4),
		},
		{
			name:   "wallet_v4r2_get_public_key",
			code:   crossTestWalletV4,
			data:   walletV4Data,
			method: "get_public_key",
			c7:     prepareCrossTestC7(nil, crossTestWalletV4),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goRes, err := runGoCrossMethod(tt.code, tt.data, tt.c7, tt.method, tt.args...)
			if err != nil {
				t.Fatalf("go tvm execution failed: %v", err)
			}

			refRes, err := runReferenceCrossMethod(tt.code, tt.data, tt.c7, tt.method, tt.args...)
			if err != nil {
				t.Fatalf("reference tvm execution failed: %v", err)
			}

			if goRes.exitCode != refRes.exitCode {
				t.Fatalf("exit code mismatch: go=%d reference=%d", goRes.exitCode, refRes.exitCode)
			}

			if !bytes.Equal(goRes.stack.Hash(), refRes.stack.Hash()) {
				t.Fatalf("stack mismatch:\ngo=%s\nreference=%s", goRes.stack.Dump(), refRes.stack.Dump())
			}
		})
	}
}

func runGoCrossMethod(code, data *cell.Cell, c7 tuple.Tuple, method string, args ...int64) (*crossRunResult, error) {
	stack := vm.NewStack()
	for _, arg := range args {
		if err := stack.PushInt(big.NewInt(arg)); err != nil {
			return nil, err
		}
	}

	if err := stack.PushInt(big.NewInt(int64(tlb.MethodNameHash(method)))); err != nil {
		return nil, err
	}

	err := NewTVM().Execute(code, data, c7, vm.Gas{}, stack)
	exitCode := int32(0)
	if err != nil {
		var vmErr vmerr.VMError
		if !errors.As(err, &vmErr) {
			return nil, err
		}
		exitCode = int32(vmErr.Code)
	}

	stackCell, err := stackToCell(stack)
	if err != nil {
		return nil, err
	}

	return &crossRunResult{
		exitCode: exitCode,
		stack:    stackCell,
	}, nil
}

func prepareCrossTestC7(cfg *cell.Dictionary, code *cell.Cell) tuple.Tuple {
	inner := *tuple.NewTuple(
		uint32(0x076ef1ea),
		uint8(0),
		uint8(0),
		uint32(crossTestTime.Unix()),
		uint8(0),
		uint8(0),
		new(big.Int).SetBytes(crossTestSeed),
		*tuple.NewTuple(new(big.Int).Set(crossTestBalance), nil),
		cell.BeginCell().MustStoreAddr(crossTestAddr).ToSlice(),
	)

	if cfg != nil {
		inner.Append(cfg.AsCell())
		inner.Append(code)
		inner.Append(*tuple.NewTuple(int64(0), nil))
		inner.Append(uint8(0))
		inner.Append(nil)
	} else {
		inner.Append(nil)
	}

	return *tuple.NewTuple(inner)
}

func tupleToStackCell(v tuple.Tuple) (*cell.Cell, error) {
	stack := tlb.NewStack()
	stack.Push(tupleToAny(v))
	return stack.ToCell()
}

func tupleToAny(v tuple.Tuple) []any {
	res := make([]any, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		val, err := v.Index(i)
		if err != nil {
			panic(err)
		}

		if nested, ok := val.(tuple.Tuple); ok {
			res = append(res, tupleToAny(nested))
			continue
		}

		res = append(res, val)
	}
	return res
}

func stackToCell(stack *vm.Stack) (*cell.Cell, error) {
	tlbStack, err := tlb.NewStackFromVM(stack)
	if err != nil {
		return nil, err
	}
	return tlbStack.ToCell()
}

func mustCellFromHex(src string) *cell.Cell {
	data, err := hex.DecodeString(src)
	if err != nil {
		panic(err)
	}

	cl, err := cell.FromBOC(data)
	if err != nil {
		panic(err)
	}
	return cl
}

func mustBytesFromHex(src string) []byte {
	data, err := hex.DecodeString(src)
	if err != nil {
		panic(err)
	}
	return data
}

func makeWalletV1V2Data(seqno uint32, pubKey []byte) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(uint64(seqno), 32).
		MustStoreSlice(pubKey, 256).
		EndCell()
}

func makeWalletV3Data(seqno, subwallet uint32, pubKey []byte) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(uint64(seqno), 32).
		MustStoreUInt(uint64(subwallet), 32).
		MustStoreSlice(pubKey, 256).
		EndCell()
}

func makeWalletV4Data(seqno, subwallet uint32, pubKey []byte) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(uint64(seqno), 32).
		MustStoreUInt(uint64(subwallet), 32).
		MustStoreSlice(pubKey, 256).
		MustStoreDict(nil).
		EndCell()
}
