package tvm

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

var benchmarkExecutionResult *ExecutionResult

func BenchmarkTVMRunGetMethodRealistic(b *testing.B) {
	pubKey := benchmarkMustBytesFromHex("f7a26c623ca2429ae80fbb17786b0b523ba71c1dbb13fbcdb8ded762a71cd867")
	walletV3 := benchmarkMustCellFromHex("B5EE9C724101010100710000DEFF0020DD2082014C97BA218201339CBAB19F71B0ED44D0D31FD31F31D70BFFE304E0A4F2608308D71820D31FD31FD31FF82313BBF263ED44D0D31FD31FD3FFD15132BAF2A15144BAF2A204F901541055F910F2A3F8009320D74A96D307D402FB00E8D101A4C8CB1FCB1FCBFFC9ED5410BD6DAD")
	walletV4 := benchmarkMustCellFromHex("B5EE9C72410214010002D4000114FF00F4A413F4BCF2C80B010201200203020148040504F8F28308D71820D31FD31FD31F02F823BBF264ED44D0D31FD31FD3FFF404D15143BAF2A15151BAF2A205F901541064F910F2A3F80024A4C8CB1F5240CB1F5230CBFF5210F400C9ED54F80F01D30721C0009F6C519320D74A96D307D402FB00E830E021C001E30021C002E30001C0039130E30D03A4C8CB1F12CB1FCBFF1011121302E6D001D0D3032171B0925F04E022D749C120925F04E002D31F218210706C7567BD22821064737472BDB0925F05E003FA403020FA4401C8CA07CBFFC9D0ED44D0810140D721F404305C810108F40A6FA131B3925F07E005D33FC8258210706C7567BA923830E30D03821064737472BA925F06E30D06070201200809007801FA00F40430F8276F2230500AA121BEF2E0508210706C7567831EB17080185004CB0526CF1658FA0219F400CB6917CB1F5260CB3F20C98040FB0006008A5004810108F45930ED44D0810140D720C801CF16F400C9ED540172B08E23821064737472831EB17080185005CB055003CF1623FA0213CB6ACB1FCB3FC98040FB00925F03E20201200A0B0059BD242B6F6A2684080A06B90FA0218470D4080847A4937D29910CE6903E9FF9837812801B7810148987159F31840201580C0D0011B8C97ED44D0D70B1F8003DB29DFB513420405035C87D010C00B23281F2FFF274006040423D029BE84C600201200E0F0019ADCE76A26840206B90EB85FFC00019AF1DF6A26840106B90EB858FC0006ED207FA00D4D422F90005C8CA0715CBFFC9D077748018C8CB05CB0222CF165005FA0214CB6B12CCCCC973FB00C84014810108F451F2A7020070810108D718FA00D33FC8542047810108F451F2A782106E6F746570748018C8CB05CB025006CF165004FA0214CB6A12CB1FCB3FC973FB0002006C810108D718FA00D33F305224810108F459F2A782106473747270748018C8CB05CB025005CF165003FA0213CB6ACB1F12CB3FC973FB00000AF400C9ED54696225E5")
	emptyData := cell.BeginCell().EndCell()
	walletV3Data := benchmarkWalletV3Data(42, 698983191, pubKey)
	walletV4Data := benchmarkWalletV4Data(42, 698983191, pubKey)

	tests := []struct {
		name   string
		code   *cell.Cell
		data   *cell.Cell
		method string
		args   []int64
	}{
		{
			name:   "wallet_v3_seqno",
			code:   walletV3,
			data:   walletV3Data,
			method: "seqno",
		},
		{
			name:   "wallet_v3_get_public_key",
			code:   walletV3,
			data:   walletV3Data,
			method: "get_public_key",
		},
		{
			name:   "wallet_v4_seqno",
			code:   walletV4,
			data:   walletV4Data,
			method: "seqno",
		},
		{
			name:   "main_simple_until_while",
			code:   MainContractCode,
			data:   emptyData,
			method: "simpleUntilWhile",
			args:   []int64{2},
		},
		{
			name:   "main_try_repeat_while_until",
			code:   MainContractCode,
			data:   emptyData,
			method: "tryRepeatWhileUntil",
			args:   []int64{0},
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			machine := NewTVM()
			c7 := benchmarkC7(tt.code)
			methodID := big.NewInt(int64(tlb.MethodNameHash(tt.method)))
			args := make([]*big.Int, len(tt.args))
			for i, arg := range tt.args {
				args[i] = big.NewInt(arg)
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				stack := vm.NewStack()
				for _, arg := range args {
					if err := stack.PushInt(arg); err != nil {
						b.Fatal(err)
					}
				}
				if err := stack.PushInt(methodID); err != nil {
					b.Fatal(err)
				}

				res, err := machine.ExecuteDetailed(tt.code, tt.data, c7, vm.GasWithLimit(1_000_000_000), stack)
				if err != nil {
					b.Fatal(err)
				}
				if res.ExitCode != 0 && res.ExitCode != 1 {
					b.Fatalf("unexpected exit code %d", res.ExitCode)
				}
				benchmarkExecutionResult = res
			}
		})
	}
}

func benchmarkC7(code *cell.Cell) tuple.Tuple {
	inner := tuple.NewTupleValue(
		uint32(0x076ef1ea),
		uint8(0),
		uint8(0),
		uint32(time.Unix(1_710_000_000, 0).Unix()),
		uint8(0),
		uint8(0),
		new(big.Int).SetBytes(bytes.Repeat([]byte{0x11}, 32)),
		tuple.NewTupleValue(big.NewInt(10_000_000), nil),
		cell.BeginCell().MustStoreAddr(address.MustParseAddr("EQAYqo4u7VF0fa4DPAebk4g9lBytj2VFny7pzXR0trjtXQaO")).ToSlice(),
		nil,
	)
	inner.Append(code)
	return tuple.NewTupleValue(inner)
}

func benchmarkWalletV3Data(seqno, subwallet uint32, pubKey []byte) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(uint64(seqno), 32).
		MustStoreUInt(uint64(subwallet), 32).
		MustStoreSlice(pubKey, 256).
		EndCell()
}

func benchmarkWalletV4Data(seqno, subwallet uint32, pubKey []byte) *cell.Cell {
	return cell.BeginCell().
		MustStoreUInt(uint64(seqno), 32).
		MustStoreUInt(uint64(subwallet), 32).
		MustStoreSlice(pubKey, 256).
		MustStoreDict(nil).
		EndCell()
}

func benchmarkMustCellFromHex(src string) *cell.Cell {
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

func benchmarkMustBytesFromHex(src string) []byte {
	data, err := hex.DecodeString(src)
	if err != nil {
		panic(err)
	}
	return data
}
