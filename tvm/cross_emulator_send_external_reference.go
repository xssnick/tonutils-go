//go:build cgo && tvm_cross_emulator

package tvm

/*
#cgo LDFLAGS: -L${SRCDIR}/vm/cross-emulate-test/lib -Wl,-rpath,${SRCDIR}/vm/cross-emulate-test/lib -lemulator
#include <stdbool.h>
#include <stdlib.h>
#include "vm/cross-emulate-test/lib/emulator-extern.h"
*/
import "C"

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"unsafe"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type referenceSendMessageResult struct {
	exitCode int64
	gasUsed  int64
	accepted bool
	code     *cell.Cell
	data     *cell.Cell
	actions  *cell.Cell
	vmLog    string
}

type referenceSendMessageConfig struct {
	address    *address.Address
	now        uint32
	balance    uint64
	randSeed   []byte
	configRoot *cell.Cell
	libs       *cell.Cell
}

type referenceSendMessageJSON struct {
	Success    bool    `json:"success"`
	Error      string  `json:"error"`
	GasUsed    string  `json:"gas_used"`
	VMExitCode int64   `json:"vm_exit_code"`
	Accepted   bool    `json:"accepted"`
	NewCode    string  `json:"new_code"`
	NewData    string  `json:"new_data"`
	Actions    *string `json:"actions"`
	VMLog      string  `json:"vm_log"`
}

func runReferenceSendExternal(code, data *cell.Cell, addr *address.Address, body *cell.Cell, now uint32) (*referenceSendMessageResult, error) {
	return runReferenceSendMessageWithConfig(code, data, body, 0, false, referenceSendMessageConfig{
		address:  addr,
		now:      now,
		balance:  walletSendTestBalance,
		randSeed: walletSendTestSeed,
	})
}

func runReferenceSendInternal(code, data *cell.Cell, addr *address.Address, body *cell.Cell, amount uint64, now uint32) (*referenceSendMessageResult, error) {
	return runReferenceSendMessageWithConfig(code, data, body, amount, true, referenceSendMessageConfig{
		address:  addr,
		now:      now,
		balance:  walletSendTestBalance,
		randSeed: walletSendTestSeed,
	})
}

func runReferenceSendMessageWithConfig(code, data, body *cell.Cell, amount uint64, internal bool, cfg referenceSendMessageConfig) (*referenceSendMessageResult, error) {
	codeB64 := base64.StdEncoding.EncodeToString(code.ToBOC())
	dataB64 := base64.StdEncoding.EncodeToString(data.ToBOC())
	bodyB64 := base64.StdEncoding.EncodeToString(body.ToBOC())
	if cfg.balance == 0 {
		cfg.balance = walletSendTestBalance
	}
	if len(cfg.randSeed) == 0 {
		cfg.randSeed = walletSendTestSeed
	}
	seedHex := fmt.Sprintf("%x", cfg.randSeed)

	cCode := C.CString(codeB64)
	defer C.free(unsafe.Pointer(cCode))
	cData := C.CString(dataB64)
	defer C.free(unsafe.Pointer(cData))
	if cfg.address == nil {
		return nil, fmt.Errorf("reference send_message address is nil")
	}
	cAddr := C.CString(cfg.address.StringRaw())
	defer C.free(unsafe.Pointer(cAddr))
	cSeed := C.CString(seedHex)
	defer C.free(unsafe.Pointer(cSeed))
	cBody := C.CString(bodyB64)
	defer C.free(unsafe.Pointer(cBody))

	verbosity := 0
	if os.Getenv("TVM_TRACE_WALLET_SEND_REF") != "" {
		verbosity = 3
	}
	emulator := C.tvm_emulator_create(cCode, cData, C.int(verbosity))
	if emulator == nil {
		return nil, fmt.Errorf("failed to create reference tvm emulator")
	}
	defer C.tvm_emulator_destroy(emulator)

	var cLibs *C.char
	if cfg.libs != nil {
		cLibs = C.CString(base64.StdEncoding.EncodeToString(cfg.libs.ToBOC()))
		defer C.free(unsafe.Pointer(cLibs))
		if !bool(C.tvm_emulator_set_libraries(emulator, cLibs)) {
			return nil, fmt.Errorf("failed to initialize reference libraries")
		}
	}

	var cConfig *C.char
	if cfg.configRoot != nil {
		cConfig = C.CString(base64.StdEncoding.EncodeToString(cfg.configRoot.ToBOC()))
		defer C.free(unsafe.Pointer(cConfig))
	}

	if !bool(C.tvm_emulator_set_c7(emulator, cAddr, C.uint32_t(cfg.now), C.uint64_t(cfg.balance), cSeed, cConfig)) {
		return nil, fmt.Errorf("failed to initialize reference c7")
	}

	var resPtr *C.char
	if internal {
		resPtr = C.tvm_emulator_send_internal_message(emulator, cBody, C.uint64_t(amount))
	} else {
		resPtr = C.tvm_emulator_send_external_message(emulator, cBody)
	}
	if resPtr == nil {
		return nil, fmt.Errorf("reference send_message returned nil")
	}
	defer C.free(unsafe.Pointer(resPtr))

	var raw referenceSendMessageJSON
	if err := json.Unmarshal([]byte(C.GoString(resPtr)), &raw); err != nil {
		return nil, err
	}
	if !raw.Success {
		return nil, fmt.Errorf("reference emulator failed: %s", raw.Error)
	}

	gasUsed, err := strconv.ParseInt(raw.GasUsed, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse reference gas_used %q: %w", raw.GasUsed, err)
	}

	codeCell, err := cellFromBocBase64(raw.NewCode)
	if err != nil {
		return nil, fmt.Errorf("failed to decode reference new_code: %w", err)
	}

	dataCell, err := cellFromBocBase64(raw.NewData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode reference new_data: %w", err)
	}

	var actionsCell *cell.Cell
	if raw.Actions != nil {
		actionsCell, err = cellFromBocBase64(*raw.Actions)
		if err != nil {
			return nil, fmt.Errorf("failed to decode reference actions: %w", err)
		}
	}

	return &referenceSendMessageResult{
		exitCode: raw.VMExitCode,
		gasUsed:  gasUsed,
		accepted: raw.Accepted,
		code:     codeCell,
		data:     dataCell,
		actions:  actionsCell,
		vmLog:    raw.VMLog,
	}, nil
}

func cellFromBocBase64(src string) (*cell.Cell, error) {
	raw, err := base64.StdEncoding.DecodeString(src)
	if err != nil {
		return nil, err
	}
	return cell.FromBOC(raw)
}
