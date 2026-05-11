//go:build cgo && tvm_cross_emulator

package tvm

import (
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
)

func TestTVMCrossEmulatorInternalMessage(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	t.Run("SuccessMatchesReference", func(t *testing.T) {
		origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
		newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
		wantMsg, err := buildInternalMessageForEmulation(tonopsTestAddr, body, internalMessageTestAmount)
		if err != nil {
			t.Fatalf("failed to build expected internal message: %v", err)
		}
		code := makeInternalMessageSuccessCode(t, newData, wantMsg)

		goRes, err := emulateInternalForTest(t, code, origData, body)
		if err != nil {
			t.Fatalf("go send_internal failed: %v", err)
		}
		refRes, err := runReferenceSendInternal(code, origData, tonopsTestAddr, body, internalMessageTestAmount, uint32(tonopsTestTime.Unix()))
		if err != nil {
			t.Fatalf("reference send_internal failed: %v", err)
		}

		assertMessageSendMatchesReference(t, goRes, refRes)
	})

	t.Run("FailureMatchesReference", func(t *testing.T) {
		origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
		newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
		body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
		code := makeInternalMessageFailureCode(t, newData)

		goRes, err := emulateInternalForTest(t, code, origData, body)
		if err != nil {
			t.Fatalf("go failing send_internal failed: %v", err)
		}
		refRes, err := runReferenceSendInternal(code, origData, tonopsTestAddr, body, internalMessageTestAmount, uint32(tonopsTestTime.Unix()))
		if err != nil {
			t.Fatalf("reference failing send_internal failed: %v", err)
		}

		assertMessageSendMatchesReference(t, goRes, refRes)
	})

	t.Run("InMsgParamsMatchReferenceC7", func(t *testing.T) {
		body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
		origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
		tests := []struct {
			name string
			code *cell.Cell
		}{
			{
				name: "inmsg_bounce_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSG_BOUNCE().Serialize(),
					mathop.ABS().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STU(1).Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsg_value_grams_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSG_VALUE().Serialize(),
					tupleop.INDEX(0).Serialize(),
					cellsliceop.NEWC().Serialize(),
					stackop.XCHG0(1).Serialize(),
					cellsliceop.STGRAMS().Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsg_src_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSG_SRC().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STSLICE().Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsg_params_len_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSGPARAMS().Serialize(),
					tupleop.QTLEN().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STU(8).Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsg_bounced_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSG_BOUNCED().Serialize(),
					mathop.ABS().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STU(1).Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsg_fwdfee_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSG_FWDFEE().Serialize(),
					cellsliceop.NEWC().Serialize(),
					stackop.XCHG0(1).Serialize(),
					cellsliceop.STGRAMS().Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsg_lt_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSG_LT().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STU(64).Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsg_utime_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSG_UTIME().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STU(32).Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsg_origvalue_grams_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSG_ORIGVALUE().Serialize(),
					tupleop.INDEX(0).Serialize(),
					cellsliceop.NEWC().Serialize(),
					stackop.XCHG0(1).Serialize(),
					cellsliceop.STGRAMS().Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsg_valueextra_null_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSG_VALUEEXTRA().Serialize(),
					tupleop.ISNULL().Serialize(),
					mathop.ABS().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STU(1).Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsg_stateinit_null_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSG_STATEINIT().Serialize(),
					tupleop.ISNULL().Serialize(),
					mathop.ABS().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STU(1).Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsgparam_src_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSGPARAM(2).Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STSLICE().Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
			{
				name: "inmsgparam_stateinit_null_to_data",
				code: codeFromBuilders(t,
					funcsop.INMSGPARAM(9).Serialize(),
					tupleop.ISNULL().Serialize(),
					mathop.ABS().Serialize(),
					cellsliceop.NEWC().Serialize(),
					cellsliceop.STU(1).Serialize(),
					cellsliceop.ENDC().Serialize(),
					execop.POPCTR(4).Serialize(),
				),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				goRes, err := emulateInternalForTest(t, tt.code, origData, body)
				if err != nil {
					t.Fatalf("go send_internal failed: %v", err)
				}
				refRes, err := runReferenceSendInternal(tt.code, origData, tonopsTestAddr, body, internalMessageTestAmount, uint32(tonopsTestTime.Unix()))
				if err != nil {
					t.Fatalf("reference send_internal failed: %v", err)
				}

				assertMessageSendMatchesReference(t, goRes, refRes)
			})
		}
	})
}
