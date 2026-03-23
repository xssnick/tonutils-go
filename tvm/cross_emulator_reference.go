//go:build darwin && cgo && tvm_cross_emulator

package tvm

/*
#cgo LDFLAGS: -L${SRCDIR}/vm/cross-emulate-test/lib -Wl,-rpath,${SRCDIR}/vm/cross-emulate-test/lib -lemulator
#include <stdbool.h>
#include <stdlib.h>
#include "vm/cross-emulate-test/lib/emulator-extern.h"
*/
import "C"

import (
	"fmt"
	"math/big"
	"unsafe"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type referenceMethodConfig struct {
	C7   *cell.Cell `tlb:"^"`
	Libs *cell.Cell `tlb:"^"`
}

type referenceRunMethodParams struct {
	Code     *cell.Cell            `tlb:"^"`
	Data     *cell.Cell            `tlb:"^"`
	Stack    *cell.Cell            `tlb:"^"`
	Params   referenceMethodConfig `tlb:"^"`
	MethodID int32                 `tlb:"## 32"`
}

type referenceRunResult struct {
	ExitCode int32      `tlb:"## 32"`
	GasUsed  int64      `tlb:"## 64"`
	Stack    *cell.Cell `tlb:"^"`
}

func init() {
	C.emulator_set_verbosity_level(0)
}

func runReferenceCrossMethod(code, data *cell.Cell, c7 tuple.Tuple, method string, args ...int64) (*crossRunResult, error) {
	stack := vm.NewStack()
	for _, arg := range args {
		if err := stack.PushInt(big.NewInt(arg)); err != nil {
			return nil, err
		}
	}

	stackCell, err := stackToCell(stack)
	if err != nil {
		return nil, err
	}

	c7Cell, err := tupleToStackCell(c7)
	if err != nil {
		return nil, err
	}

	paramsCell, err := tlb.ToCell(referenceRunMethodParams{
		Code:  code,
		Data:  data,
		Stack: stackCell,
		Params: referenceMethodConfig{
			C7:   c7Cell,
			Libs: cell.BeginCell().EndCell(),
		},
		MethodID: int32(tlb.MethodNameHash(method)),
	})
	if err != nil {
		return nil, err
	}

	boc := paramsCell.ToBOCWithFlags(false)
	cReq := C.CBytes(boc)
	defer C.free(cReq)

	resPtr := unsafe.Pointer(C.tvm_emulator_emulate_run_method(C.uint32_t(len(boc)), (*C.char)(cReq), C.int64_t(crossTestMaxGas)))
	if resPtr == nil {
		return nil, fmt.Errorf("reference emulator returned nil for method %s", method)
	}
	defer C.free(resPtr)

	resSize := *(*C.uint32_t)(resPtr)
	resData := C.GoBytes(unsafe.Pointer(uintptr(resPtr)+4), C.int(resSize))

	resCell, err := cell.FromBOC(resData)
	if err != nil {
		return nil, err
	}

	var result referenceRunResult
	if err = tlb.LoadFromCell(&result, resCell.BeginParse()); err != nil {
		return nil, err
	}

	return &crossRunResult{
		exitCode: result.ExitCode,
		stack:    result.Stack,
	}, nil
}
