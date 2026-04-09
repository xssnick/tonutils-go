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
	"strconv"
	"unsafe"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type referenceGetMethodConfig struct {
	Address    *address.Address
	Now        uint32
	Balance    uint64
	RandSeed   []byte
	ConfigRoot *cell.Cell
	Libs       *cell.Cell
}

type referenceRunGetMethodJSON struct {
	Success    bool    `json:"success"`
	Error      string  `json:"error"`
	Stack      string  `json:"stack"`
	GasUsed    string  `json:"gas_used"`
	VMExitCode int32   `json:"vm_exit_code"`
	VMLog      string  `json:"vm_log"`
	MissingLib *string `json:"missing_library"`
}

func parseGasString(src string) (int64, error) {
	v, err := strconv.ParseInt(src, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse reference gas_used %q: %w", src, err)
	}
	return v, nil
}

func runReferenceCrossCodeViaEmulator(code, data *cell.Cell, stack *vm.Stack, cfg referenceGetMethodConfig) (*crossRunResult, error) {
	if cfg.Address == nil {
		return nil, fmt.Errorf("reference get method address is nil")
	}
	if cfg.Balance == 0 {
		cfg.Balance = uint64(tonopsTestBalance.Int64())
	}
	if len(cfg.RandSeed) == 0 {
		cfg.RandSeed = tonopsTestSeed
	}

	codeB64 := base64.StdEncoding.EncodeToString(code.ToBOC())
	dataB64 := base64.StdEncoding.EncodeToString(data.ToBOC())
	stackCell, err := stackToCell(stack)
	if err != nil {
		return nil, err
	}
	stackB64 := base64.StdEncoding.EncodeToString(stackCell.ToBOC())

	cCode := C.CString(codeB64)
	defer C.free(unsafe.Pointer(cCode))
	cData := C.CString(dataB64)
	defer C.free(unsafe.Pointer(cData))
	emulator := C.tvm_emulator_create(cCode, cData, 0)
	if emulator == nil {
		return nil, fmt.Errorf("failed to create reference tvm emulator")
	}
	defer C.tvm_emulator_destroy(emulator)

	if cfg.Libs != nil {
		cLibs := C.CString(base64.StdEncoding.EncodeToString(cfg.Libs.ToBOC()))
		defer C.free(unsafe.Pointer(cLibs))
		if !bool(C.tvm_emulator_set_libraries(emulator, cLibs)) {
			return nil, fmt.Errorf("failed to initialize reference libraries")
		}
	}

	var cConfig *C.char
	if cfg.ConfigRoot != nil {
		cConfig = C.CString(base64.StdEncoding.EncodeToString(cfg.ConfigRoot.ToBOC()))
		defer C.free(unsafe.Pointer(cConfig))
	}

	cAddr := C.CString(cfg.Address.StringRaw())
	defer C.free(unsafe.Pointer(cAddr))
	cSeed := C.CString(fmt.Sprintf("%x", cfg.RandSeed))
	defer C.free(unsafe.Pointer(cSeed))
	if !bool(C.tvm_emulator_set_c7(emulator, cAddr, C.uint32_t(cfg.Now), C.uint64_t(cfg.Balance), cSeed, cConfig)) {
		return nil, fmt.Errorf("failed to initialize reference c7")
	}

	cStack := C.CString(stackB64)
	defer C.free(unsafe.Pointer(cStack))
	resPtr := C.tvm_emulator_run_get_method(emulator, 0, cStack)
	if resPtr == nil {
		return nil, fmt.Errorf("reference run_get_method returned nil")
	}
	defer C.free(unsafe.Pointer(resPtr))

	var raw referenceRunGetMethodJSON
	if err = json.Unmarshal([]byte(C.GoString(resPtr)), &raw); err != nil {
		return nil, err
	}
	if !raw.Success {
		return nil, fmt.Errorf("reference emulator failed: %s", raw.Error)
	}

	stackOut, err := cellFromBocBase64(raw.Stack)
	if err != nil {
		return nil, fmt.Errorf("failed to decode reference stack: %w", err)
	}
	gasUsed, err := parseGasString(raw.GasUsed)
	if err != nil {
		return nil, err
	}

	return &crossRunResult{
		exitCode: raw.VMExitCode,
		gasUsed:  gasUsed,
		stack:    stackOut,
	}, nil
}
