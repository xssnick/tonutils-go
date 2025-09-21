package main

// #cgo LDFLAGS: -L ./lib -Wl,-rpath,./tvm/vm/cross-emulate-test/lib -lemulator
// #include <stdlib.h>
// #include <stdbool.h>
// #include "lib/emulator-extern.h"
import "C"

// a native library can be downloaded from ton releases for your OS https://github.com/ton-blockchain/ton/releases
// put binary to ./lib directory and go run tvm/vm/cross-emulate-test/main.go, from a project root
import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
	"log"
	"math/big"
	"time"
	"unsafe"
)

type MethodConfig struct {
	C7   *cell.Cell `tlb:"^"`
	Libs *cell.Cell `tlb:"^"`
}

type RunMethodParams struct {
	Code     *cell.Cell   `tlb:"^"`
	Data     *cell.Cell   `tlb:"^"`
	Stack    *cell.Cell   `tlb:"^"`
	Params   MethodConfig `tlb:"^"`
	MethodID int32        `tlb:"## 32"`
}

type RunResult struct {
	ExitCode int32      `tlb:"## 32"`
	GasUsed  int64      `tlb:"## 64"`
	Stack    *cell.Cell `tlb:"^"`
}

func init() {
	C.emulator_set_verbosity_level(10)
}

func RunGetMethod(params RunMethodParams, maxGas int64) (*RunResult, error) {
	req, err := tlb.ToCell(params)
	if err != nil {
		return nil, err
	}

	boc := req.ToBOCWithFlags(false)

	cReq := C.CBytes(boc)
	defer C.free(unsafe.Pointer(cReq))

	res := unsafe.Pointer(C.tvm_emulator_emulate_run_method(C.uint32_t(len(boc)), (*C.char)(cReq), C.int64_t(maxGas)))
	if res == nil {
		return nil, fmt.Errorf("failed to execute tvm, req: %s, %s", hex.EncodeToString(boc), req.Dump())
	}
	defer C.free(res)

	sz := *(*C.uint32_t)(res)
	data := C.GoBytes(unsafe.Pointer(uintptr(res)+4), C.int(sz))
	c, err := cell.FromBOC(data)
	if err != nil {
		return nil, err
	}

	var result RunResult
	if err := tlb.LoadFromCell(&result, c.BeginParse()); err != nil {
		return nil, err
	}
	return &result, nil
}

func PrepareC7(addr *address.Address, tm time.Time, seed []byte, balance *big.Int, cfg *cell.Dictionary, code *cell.Cell) ([]any, error) {
	if len(seed) != 32 {
		return nil, fmt.Errorf("seed len is not 32")
	}

	var tuple = make([]any, 0, 14)
	tuple = append(tuple, uint32(0x076ef1ea))
	tuple = append(tuple, uint8(0))
	tuple = append(tuple, uint8(0))
	tuple = append(tuple, uint32(tm.Unix()))
	tuple = append(tuple, uint8(0))
	tuple = append(tuple, uint8(0))
	tuple = append(tuple, new(big.Int).SetBytes(seed))
	tuple = append(tuple, []any{balance, nil})
	tuple = append(tuple, cell.BeginCell().MustStoreAddr(addr).ToSlice())
	if cfg != nil {
		tuple = append(tuple, cfg.AsCell())
		tuple = append(tuple, code)
		tuple = append(tuple, []any{0, nil}) // storage fees
		tuple = append(tuple, uint8(0))
		tuple = append(tuple, nil) // prev blocks
	} else {
		tuple = append(tuple, nil)
	}

	return []any{tuple}, nil
}

var MainContractCode = func() *cell.Cell {
	contractCodeBytes, _ := hex.DecodeString("b5ee9c724102180100019a000114ff00f4a413f4bcf2c80b0102016202070202ce0306020120040500691b088831c02456f8007434c0cc1caa42644c383c0074c7f4cfcc4060841fa1d93beea6f4c7cc3e1080683e18bc00b80c2103fcbc20001d3b513434c7c07e1874c7c07e18b460001d4c8f84101cb1ff84201cb1fc9ed548020120080f020120090e0201200a0b000db7203e003f08300201200c0d0047b2e9c160c235c61981fe44407e08efac3ca600fe800c1480aed4cc6eec3ca696284068200057b057bb68bb7efb507b50fb513b517b51e556dc76cc4c3b59fb597b593b58fb5864e936cc7b507b7c407cbfe0002fb829a708e10709320c1059402a402a4e830a420c204e630802012010110099bbfe470ed41ed43ed44ed45ed47935b8064ed67ed65ed64ed63ed618e28758e237a9320c2008e18a5738e1202a420c20524c200b092f237de02a520c101e630e830fe00e431ed41edf101f2ff8020148121702012013140021ad1cbacdb84b80d200d2106102731872400201201516000caad0f001f8420022aacd759c709320c1059401a401a4e830e4000db3aedd646939209e25fbf5")
	code, _ := cell.FromBOC(contractCodeBytes)
	return code
}()

func main() {
	name := "simpleRepeatWhile"
	vmx := tvm.NewTVM()
	s := vm.NewStack()
	_ = s.PushInt(big.NewInt(2))

	ts, err := tlb.NewStackFromVM(s)
	if err != nil {
		panic(err.Error())
	}
	cc, _ := ts.ToCell()

	c7tuple, err := PrepareC7(address.MustParseAddr("EQAYqo4u7VF0fa4DPAebk4g9lBytj2VFny7pzXR0trjtXQaO"), time.Now(), make([]byte, 32), big.NewInt(10000000), nil, MainContractCode)
	if err != nil {
		panic(1)
	}

	stack := tlb.NewStack()
	stack.Push(c7tuple)
	c7cell, err := stack.ToCell()
	if err != nil {
		panic(2)
	}

	res, err := RunGetMethod(RunMethodParams{
		Code:  MainContractCode,
		Data:  cell.BeginCell().EndCell(),
		Stack: cc,
		Params: MethodConfig{
			C7:   c7cell,
			Libs: cell.BeginCell().EndCell(),
		},
		MethodID: int32(tlb.MethodNameHash(name)),
	}, 1000000000)
	if err != nil {
		panic(3)
	}

	var resStack tlb.Stack
	err = resStack.LoadFromCell(res.Stack.BeginParse())
	if err != nil {
		panic(4)
	}

	rst, err := resStack.ToCell()
	if err != nil {
		panic(5)
	}
	log.Println("C CALL COMPLETED")

	_ = s.PushInt(big.NewInt(int64(tlb.MethodNameHash(name))))
	err = vmx.Execute(MainContractCode, cell.BeginCell().EndCell(), tuple.Tuple{}, vm.Gas{}, s)
	if err != nil {
		panic(err.Error())
	}

	f, err := tlb.NewStackFromVM(s)
	if err != nil {
		panic(err.Error())
	}
	log.Println("GO CALL COMPLETED")

	lst, err := f.ToCell()
	if err != nil {
		panic(5)
	}

	if bytes.Equal(lst.Hash(), rst.Hash()) {
		log.Println("OK, SAME", f.Depth())
		return
	}

	log.Println("CPP")
	for resStack.Depth() > 0 {
		v, err := resStack.Pop()
		if err != nil {
			panic(5)
		}
		log.Println(v)
	}

	log.Println()
	log.Println("GO")
	for f.Depth() > 0 {
		v, err := f.Pop()
		if err != nil {
			panic(5)
		}
		log.Println(v)
	}

}
