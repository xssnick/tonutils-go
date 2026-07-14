//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestTVMCrossEmulatorSENDMSGIntegerAmountParity(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	prices := tlb.ConfigMsgForwardPrices{
		LumpPrice: 1,
		BitPrice:  1 << 16,
		CellPrice: 3 << 16,
		FirstFrac: 1 << 15,
	}
	configRoot := tonopsCrossSendMsgConfig(t, referenceRawRunGlobalVersion, prices)
	unpacked := tonopsCrossSendMsgUnpackedConfig(t, prices)

	inlineBody := cell.BeginCell().MustStoreUInt(0xA5, 8).EndCell()
	referencedBody := cell.BeginCell().MustStoreSlice(bytes.Repeat([]byte{0xA5}, 125), 1000).EndCell()
	maxTVMInt := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	tests := []struct {
		name      string
		body      *cell.Cell
		bodyInRef bool
		mode      int64
		balance   tuple.Tuple
		incoming  tuple.Tuple
		shortAddr bool
		stateInit *tlb.StateInit
		initInRef bool
	}{
		{
			name:      "balance inline body",
			body:      inlineBody,
			bodyInRef: false,
			mode:      1024 | 128,
			balance:   tuple.NewTupleValue(vm.NaN{}, nil),
		},
		{
			name:      "balance inline state init and body",
			body:      inlineBody,
			bodyInRef: false,
			mode:      1024 | 128,
			balance:   tuple.NewTupleValue(vm.NaN{}, nil),
			stateInit: &tlb.StateInit{},
			initInRef: false,
		},
		{
			name:      "incoming inline body",
			body:      inlineBody,
			bodyInRef: false,
			mode:      1024 | 64,
			incoming:  tuple.NewTupleValue(vm.NaN{}, nil),
		},
		{
			name:      "balance referenced body",
			body:      referencedBody,
			bodyInRef: true,
			mode:      1024 | 128,
			balance:   tuple.NewTupleValue(vm.NaN{}, nil),
		},
		{
			name:      "incoming referenced body",
			body:      referencedBody,
			bodyInRef: true,
			mode:      1024 | 64,
			incoming:  tuple.NewTupleValue(vm.NaN{}, nil),
		},
		{
			name:      "negative balance forces body reference",
			body:      inlineBody,
			bodyInRef: false,
			mode:      1024 | 128,
			balance:   tuple.NewTupleValue(big.NewInt(-1), nil),
		},
		{
			name:      "incoming maximum value keeps finite bit size",
			body:      inlineBody,
			bodyInRef: false,
			mode:      1024 | 64,
			incoming:  tuple.NewTupleValue(maxTVMInt, nil),
			shortAddr: true,
		},
		{
			name:      "negative incoming sum forces body reference",
			body:      inlineBody,
			bodyInRef: false,
			mode:      1024 | 64,
			incoming:  tuple.NewTupleValue(big.NewInt(-101), nil),
		},
		{
			name:      "negative incoming operand sums to zero",
			body:      inlineBody,
			bodyInRef: false,
			mode:      1024 | 64,
			incoming:  tuple.NewTupleValue(big.NewInt(-100), nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			myAddr := tonopsTestAddr
			dstAddr := tonopsTestAddr
			if tt.shortAddr {
				myAddr = address.NewAddressVar(0, 0, 1, []byte{0})
				dstAddr = myAddr
			}
			msg, err := tlb.ToCell(&tlb.InternalMessage{
				IHRDisabled: true,
				SrcAddr:     tonopsTestAddr,
				DstAddr:     dstAddr,
				Amount:      tlb.FromNanoTONU(100),
				StateInit:   tt.stateInit,
				Body:        tt.body,
			})
			if err != nil {
				t.Fatalf("failed to build SENDMSG fixture: %v", err)
			}

			var relaxed tlb.MessageRelaxed
			if err = tlb.LoadFromCell(&relaxed, msg.MustBeginParse()); err != nil {
				t.Fatalf("failed to parse SENDMSG fixture: %v", err)
			}
			if relaxed.Body.InRef != tt.bodyInRef {
				t.Fatalf("fixture body InRef = %v, want %v", relaxed.Body.InRef, tt.bodyInRef)
			}
			if relaxed.Init.Exists != (tt.stateInit != nil) || relaxed.Init.InRef != tt.initInRef {
				t.Fatalf("fixture state init = (exists=%v, InRef=%v), want (exists=%v, InRef=%v)", relaxed.Init.Exists, relaxed.Init.InRef, tt.stateInit != nil, tt.initInRef)
			}

			c7 := makeTonopsTestC7(t, tonopsTestC7Config{
				ConfigRoot:     configRoot,
				UnpackedConfig: unpacked,
				Balance:        tt.balance,
				IncomingValue:  tt.incoming,
				ExtraParams: map[int]any{
					8: cell.BeginCell().MustStoreAddr(myAddr).ToSlice(),
				},
			})
			code := prependRawMethodDrop(codeFromBuilders(t, funcsop.SENDMSG().Serialize()))
			runTonOpsEdgeParityCase(t, code, []any{msg, tt.mode}, c7, 0, 0)
		})
	}

	t.Run("legacy balance NaN with extra slot", func(t *testing.T) {
		legacyConfig := tonopsCrossSendMsgConfig(t, 9, prices)
		legacyC7 := makeTonopsTestC7(t, tonopsTestC7Config{
			ConfigRoot:     legacyConfig,
			UnpackedConfig: unpacked,
			Balance:        tuple.NewTupleValue(vm.NaN{}, nil),
		})
		msg, err := tlb.ToCell(&tlb.InternalMessage{
			IHRDisabled: true,
			SrcAddr:     tonopsTestAddr,
			DstAddr:     tonopsTestAddr,
			Amount:      tlb.FromNanoTONU(100),
			Body:        inlineBody,
		})
		if err != nil {
			t.Fatalf("failed to build legacy SENDMSG fixture: %v", err)
		}

		code := codeFromBuilders(t,
			execop.POPCTR(7).Serialize(),
			funcsop.SENDMSG().Serialize(),
		)
		runTonOpsEdgeVersionedParityCase(
			t,
			code,
			[]any{msg, int64(1024 | 128), legacyC7},
			tuple.Tuple{},
			9,
			0,
			0,
		)
	})
}
