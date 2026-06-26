//go:build cgo && tvm_cross_emulator

package tvm

import (
	"math/big"
	"os"
	"strconv"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	cellsliceop "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	execop "github.com/xssnick/tonutils-go/tvm/op/exec"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
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

func TestTVMCrossEmulatorInternalMessageAllGlobalVersions(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, version := range crossEmulatorVersionAuditVersions(t, "TVM_INTERNAL_MESSAGE_VERSION_AUDIT") {
		for program := uint8(0); program < internalMessageVersionProgramCount; program++ {
			t.Run("global_v"+strconv.Itoa(version)+"_"+internalMessageVersionProgramName(program), func(t *testing.T) {
				assertInternalMessageVersionParity(t, version, program, uint16(0xCAFE+uint16(program)))
			})
		}
	}
}

func FuzzTVMCrossEmulatorInternalMessageGlobalVersion(f *testing.F) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		f.Skipf("reference emulator library is unavailable: %v", err)
	}

	for version := MinSupportedGlobalVersion; version <= MaxSupportedGlobalVersion; version++ {
		for program := uint8(0); program < internalMessageVersionProgramCount; program++ {
			f.Add(uint8(version), program, uint16(0xCA00+uint16(version)*internalMessageVersionProgramCount+uint16(program)))
		}
	}
	f.Add(uint8(255), uint8(255), uint16(0xffff))

	f.Fuzz(func(t *testing.T, rawVersion, rawProgram uint8, bodyTag uint16) {
		version := tvmFuzzGlobalVersionByte(rawVersion)
		assertInternalMessageVersionParity(t, version, rawProgram, bodyTag)
	})
}

const internalMessageVersionProgramCount = 15

func assertInternalMessageVersionParity(t *testing.T, version int, rawProgram uint8, bodyTag uint16) {
	t.Helper()

	now := uint32(tonopsTestTime.Unix())
	configRoot := referenceTransactionConfigRootWithGlobalVersion(t, mustReferenceTransactionConfigRoot(t), uint32(version))
	origData := cell.BeginCell().MustStoreUInt(0xAAAA, 16).EndCell()
	newData := cell.BeginCell().MustStoreUInt(0xBEEF, 16).EndCell()
	body := cell.BeginCell().MustStoreUInt(uint64(bodyTag), 16).EndCell()
	code := makeInternalMessageVersionCode(t, rawProgram, newData, body)

	balance := new(big.Int).SetUint64(referenceDefaultWalletSendBalance)
	goRes, err := NewTVM().EmulateInternalMessage(code, origData, body, internalMessageTestAmount, EmulateInternalMessageConfig{
		Address:    tonopsTestAddr,
		Now:        now,
		Balance:    balance,
		RandSeed:   append([]byte(nil), referenceDefaultWalletSendSeed...),
		ConfigRoot: configRoot,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:   DefaultInternalMessageGasMax,
			Limit: int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
		}),
	})
	if err != nil {
		t.Fatalf("go send_internal v%d program=%s failed: %v", version, internalMessageVersionProgramName(rawProgram), err)
	}

	refRes, err := runReferenceSendMessageWithConfig(code, origData, body, internalMessageTestAmount, true, referenceSendMessageConfig{
		address:    tonopsTestAddr,
		now:        now,
		balance:    referenceDefaultWalletSendBalance,
		randSeed:   referenceDefaultWalletSendSeed,
		configRoot: configRoot,
	})
	if err != nil {
		t.Fatalf("reference send_internal v%d program=%s failed: %v", version, internalMessageVersionProgramName(rawProgram), err)
	}

	assertMessageSendMatchesReference(t, goRes, refRes)
}

func makeInternalMessageVersionCode(t *testing.T, rawProgram uint8, newData, body *cell.Cell) *cell.Cell {
	t.Helper()

	switch rawProgram % internalMessageVersionProgramCount {
	case 0:
		wantMsg, err := buildInternalMessageForEmulation(tonopsTestAddr, body, internalMessageTestAmount)
		if err != nil {
			t.Fatalf("failed to build expected internal message: %v", err)
		}
		return makeInternalMessageSuccessCode(t, newData, wantMsg)
	case 1:
		return makeInternalMessageFailureCode(t, newData)
	case 2:
		return codeFromBuilders(t,
			funcsop.INMSG_BOUNCE().Serialize(),
			mathop.ABS().Serialize(),
			cellsliceop.NEWC().Serialize(),
			cellsliceop.STU(1).Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	case 3:
		return codeFromBuilders(t,
			funcsop.INMSG_VALUE().Serialize(),
			tupleop.INDEX(0).Serialize(),
			cellsliceop.NEWC().Serialize(),
			stackop.XCHG0(1).Serialize(),
			cellsliceop.STGRAMS().Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	case 4:
		return codeFromBuilders(t,
			funcsop.INMSG_SRC().Serialize(),
			cellsliceop.NEWC().Serialize(),
			cellsliceop.STSLICE().Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	case 5:
		return codeFromBuilders(t,
			funcsop.INMSGPARAMS().Serialize(),
			tupleop.QTLEN().Serialize(),
			cellsliceop.NEWC().Serialize(),
			cellsliceop.STU(8).Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	case 6:
		return codeFromBuilders(t,
			funcsop.INMSG_BOUNCED().Serialize(),
			mathop.ABS().Serialize(),
			cellsliceop.NEWC().Serialize(),
			cellsliceop.STU(1).Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	case 7:
		return codeFromBuilders(t,
			funcsop.INMSG_FWDFEE().Serialize(),
			cellsliceop.NEWC().Serialize(),
			stackop.XCHG0(1).Serialize(),
			cellsliceop.STGRAMS().Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	case 8:
		return codeFromBuilders(t,
			funcsop.INMSG_LT().Serialize(),
			cellsliceop.NEWC().Serialize(),
			cellsliceop.STU(64).Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	case 9:
		return codeFromBuilders(t,
			funcsop.INMSG_UTIME().Serialize(),
			cellsliceop.NEWC().Serialize(),
			cellsliceop.STU(32).Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	case 10:
		return codeFromBuilders(t,
			funcsop.INMSG_ORIGVALUE().Serialize(),
			tupleop.INDEX(0).Serialize(),
			cellsliceop.NEWC().Serialize(),
			stackop.XCHG0(1).Serialize(),
			cellsliceop.STGRAMS().Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	case 11:
		return codeFromBuilders(t,
			funcsop.INMSG_VALUEEXTRA().Serialize(),
			tupleop.ISNULL().Serialize(),
			mathop.ABS().Serialize(),
			cellsliceop.NEWC().Serialize(),
			cellsliceop.STU(1).Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	case 12:
		return codeFromBuilders(t,
			funcsop.INMSG_STATEINIT().Serialize(),
			tupleop.ISNULL().Serialize(),
			mathop.ABS().Serialize(),
			cellsliceop.NEWC().Serialize(),
			cellsliceop.STU(1).Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	case 13:
		return codeFromBuilders(t,
			funcsop.INMSGPARAM(2).Serialize(),
			cellsliceop.NEWC().Serialize(),
			cellsliceop.STSLICE().Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	default:
		return codeFromBuilders(t,
			funcsop.INMSGPARAM(9).Serialize(),
			tupleop.ISNULL().Serialize(),
			mathop.ABS().Serialize(),
			cellsliceop.NEWC().Serialize(),
			cellsliceop.STU(1).Serialize(),
			cellsliceop.ENDC().Serialize(),
			execop.POPCTR(4).Serialize(),
		)
	}
}

func internalMessageVersionProgramName(rawProgram uint8) string {
	switch rawProgram % internalMessageVersionProgramCount {
	case 0:
		return "success"
	case 1:
		return "failure"
	case 2:
		return "inmsg_bounce"
	case 3:
		return "inmsg_value_grams"
	case 4:
		return "inmsg_src"
	case 5:
		return "inmsg_params_len"
	case 6:
		return "inmsg_bounced"
	case 7:
		return "inmsg_fwdfee"
	case 8:
		return "inmsg_lt"
	case 9:
		return "inmsg_utime"
	case 10:
		return "inmsg_origvalue_grams"
	case 11:
		return "inmsg_valueextra_null"
	case 12:
		return "inmsg_stateinit_null"
	case 13:
		return "inmsgparam_src"
	default:
		return "inmsgparam_stateinit_null"
	}
}
