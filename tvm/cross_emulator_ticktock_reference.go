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
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

type referenceTickTockJSON struct {
	Success      bool    `json:"success"`
	Error        string  `json:"error"`
	Transaction  string  `json:"transaction"`
	ShardAccount string  `json:"shard_account"`
	Actions      *string `json:"actions"`
	VMLog        string  `json:"vm_log"`
}

type referenceTickTockResult struct {
	exitCode int64
	gasUsed  int64
	accepted bool
	code     *cell.Cell
	data     *cell.Cell
	actions  *cell.Cell
	vmLog    string
}

var (
	referenceTransactionConfigOnce sync.Once
	referenceTransactionConfigB64  string
	referenceTransactionConfigErr  error
)

func runReferenceTickTock(code, data *cell.Cell, addr *address.Address, isTock bool, now uint32, balance uint64, randSeed []byte) (*referenceTickTockResult, error) {
	configB64, err := loadReferenceTransactionConfigB64()
	if err != nil {
		return nil, err
	}

	shardAccount, err := buildReferenceTickTockShardAccount(addr, code, data, balance)
	if err != nil {
		return nil, err
	}

	cConfig := C.CString(configB64)
	defer C.free(unsafe.Pointer(cConfig))
	emulator := C.transaction_emulator_create(cConfig, C.int(0))
	if emulator == nil {
		return nil, fmt.Errorf("failed to create reference transaction emulator")
	}
	defer C.transaction_emulator_destroy(emulator)

	if now != 0 && !bool(C.transaction_emulator_set_unixtime(emulator, C.uint32_t(now))) {
		return nil, fmt.Errorf("failed to set reference transaction emulator unixtime")
	}
	if len(randSeed) != 0 {
		seedHex := C.CString(fmt.Sprintf("%x", randSeed))
		defer C.free(unsafe.Pointer(seedHex))
		if !bool(C.transaction_emulator_set_rand_seed(emulator, seedHex)) {
			return nil, fmt.Errorf("failed to set reference transaction emulator rand seed")
		}
	}

	cShardAccount := C.CString(base64.StdEncoding.EncodeToString(shardAccount.ToBOC()))
	defer C.free(unsafe.Pointer(cShardAccount))

	resPtr := C.transaction_emulator_emulate_tick_tock_transaction(emulator, cShardAccount, C.bool(isTock))
	if resPtr == nil {
		return nil, fmt.Errorf("reference tick/tock emulator returned nil")
	}
	defer C.free(unsafe.Pointer(resPtr))

	var raw referenceTickTockJSON
	if err = json.Unmarshal([]byte(C.GoString(resPtr)), &raw); err != nil {
		return nil, err
	}
	if !raw.Success {
		return nil, fmt.Errorf("reference tick/tock emulator failed: %s", raw.Error)
	}

	txCell, err := cellFromBocBase64(raw.Transaction)
	if err != nil {
		return nil, fmt.Errorf("failed to decode reference transaction: %w", err)
	}
	shardCell, err := cellFromBocBase64(raw.ShardAccount)
	if err != nil {
		return nil, fmt.Errorf("failed to decode reference shard account: %w", err)
	}
	var actionsCell *cell.Cell
	if raw.Actions != nil {
		actionsCell, err = cellFromBocBase64(*raw.Actions)
		if err != nil {
			return nil, fmt.Errorf("failed to decode reference tick/tock actions: %w", err)
		}
	}

	exitCode, gasUsed, err := referenceTickTockComputePhase(txCell)
	if err != nil {
		return nil, err
	}
	codeCell, dataCell, err := referenceTickTockAccountState(shardCell)
	if err != nil {
		return nil, err
	}

	return &referenceTickTockResult{
		exitCode: exitCode,
		gasUsed:  gasUsed,
		accepted: true,
		code:     codeCell,
		data:     dataCell,
		actions:  actionsCell,
		vmLog:    raw.VMLog,
	}, nil
}

func loadReferenceTransactionConfigB64() (string, error) {
	referenceTransactionConfigOnce.Do(func() {
		_, currentFile, _, _ := runtime.Caller(0)
		src, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), "..", "cppnode", "ton", "emulator", "test", "emulator-tests.cpp"))
		if err != nil {
			referenceTransactionConfigErr = err
			return
		}

		text := string(src)
		start := strings.Index(text, "const char *config_boc =")
		if start < 0 {
			referenceTransactionConfigErr = fmt.Errorf("config_boc fixture not found")
			return
		}
		text = text[start:]
		end := strings.Index(text, ";")
		if end < 0 {
			referenceTransactionConfigErr = fmt.Errorf("config_boc fixture terminator not found")
			return
		}

		var b strings.Builder
		for _, match := range regexp.MustCompile(`"(?:[^"\\\\]|\\\\.)*"`).FindAllString(text[:end], -1) {
			part, unquoteErr := strconv.Unquote(match)
			if unquoteErr != nil {
				referenceTransactionConfigErr = unquoteErr
				return
			}
			b.WriteString(part)
		}
		referenceTransactionConfigB64 = b.String()
		if referenceTransactionConfigB64 == "" {
			referenceTransactionConfigErr = fmt.Errorf("config_boc fixture is empty")
		}
	})
	return referenceTransactionConfigB64, referenceTransactionConfigErr
}

func buildReferenceTickTockShardAccount(addr *address.Address, code, data *cell.Cell, balance uint64) (*cell.Cell, error) {
	storageInfoCell, err := tlb.ToCell(&tlb.StorageInfo{
		StorageUsed: tlb.StorageUsed{
			CellsUsed: big.NewInt(0),
			BitsUsed:  big.NewInt(0),
		},
		StorageExtra: tlb.StorageExtraNone{},
		LastPaid:     0,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize storage info: %w", err)
	}

	stateInitCell, err := tlb.ToCell(&tlb.StateInit{
		TickTock: &tlb.TickTock{Tick: true, Tock: true},
		Code:     code,
		Data:     data,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to serialize state init: %w", err)
	}

	accountCell := cell.BeginCell().
		MustStoreBoolBit(true).
		MustStoreAddr(addr).
		MustStoreBuilder(storageInfoCell.ToBuilder()).
		MustStoreUInt(0, 64).
		MustStoreBigCoins(new(big.Int).SetUint64(balance)).
		MustStoreDict(nil).
		MustStoreBoolBit(true).
		MustStoreBuilder(stateInitCell.ToBuilder()).
		EndCell()

	return tlb.ToCell(&tlb.ShardAccount{
		Account:       accountCell,
		LastTransHash: make([]byte, 32),
		LastTransLT:   0,
	})
}

func referenceTickTockComputePhase(txCell *cell.Cell) (int64, int64, error) {
	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.BeginParse()); err != nil {
		return 0, 0, fmt.Errorf("failed to decode reference tick/tock transaction: %w", err)
	}

	desc, ok := tx.Description.(tlb.TransactionDescriptionTickTock)
	if !ok {
		return 0, 0, fmt.Errorf("unexpected reference transaction description type %T", tx.Description)
	}

	vmPhase, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseVM)
	if !ok {
		return 0, 0, fmt.Errorf("unexpected reference tick/tock compute phase type %T", desc.ComputePhase.Phase)
	}

	return int64(vmPhase.Details.ExitCode), vmPhase.Details.GasUsed.Int64(), nil
}

func referenceTickTockAccountState(shardCell *cell.Cell) (*cell.Cell, *cell.Cell, error) {
	var shard tlb.ShardAccount
	if err := tlb.LoadFromCell(&shard, shardCell.BeginParse()); err != nil {
		return nil, nil, fmt.Errorf("failed to decode reference shard account: %w", err)
	}

	var acc tlb.AccountState
	if err := tlb.LoadFromCell(&acc, shard.Account.BeginParse()); err != nil {
		return nil, nil, fmt.Errorf("failed to decode reference account state: %w", err)
	}
	if !acc.IsValid || acc.Status != tlb.AccountStatusActive || acc.StateInit == nil {
		return nil, nil, fmt.Errorf("reference shard account is not active after tick/tock")
	}
	return acc.StateInit.Code, acc.StateInit.Data, nil
}
