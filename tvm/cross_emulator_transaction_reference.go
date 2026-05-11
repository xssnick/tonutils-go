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
	"unsafe"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type referenceTransactionJSON struct {
	Success      bool    `json:"success"`
	Error        string  `json:"error"`
	Transaction  string  `json:"transaction"`
	ShardAccount string  `json:"shard_account"`
	Actions      *string `json:"actions"`
	VMLog        string  `json:"vm_log"`
}

type referenceTransactionResult struct {
	exitCode    int64
	gasUsed     int64
	txCell      *cell.Cell
	shardCell   *cell.Cell
	actionsCell *cell.Cell
}

type referenceTransactionOptions struct {
	libs       *cell.Cell
	prevBlocks tuple.Tuple
}

func runReferenceOrdinaryTransaction(shard *tlb.ShardAccount, msgCell *cell.Cell, now uint32, lt uint64, randSeed []byte) (*referenceTransactionResult, error) {
	configB64, err := loadReferenceTransactionConfigB64()
	if err != nil {
		return nil, err
	}
	return runReferenceOrdinaryTransactionWithConfigB64(shard, msgCell, now, lt, randSeed, configB64)
}

func runReferenceOrdinaryTransactionWithConfigRoot(shard *tlb.ShardAccount, msgCell *cell.Cell, now uint32, lt uint64, randSeed []byte, configRoot *cell.Cell) (*referenceTransactionResult, error) {
	if configRoot == nil {
		return nil, fmt.Errorf("reference transaction config root is nil")
	}
	return runReferenceOrdinaryTransactionWithConfigRootAndOptions(shard, msgCell, now, lt, randSeed, configRoot, referenceTransactionOptions{})
}

func runReferenceOrdinaryTransactionWithConfigRootAndOptions(shard *tlb.ShardAccount, msgCell *cell.Cell, now uint32, lt uint64, randSeed []byte, configRoot *cell.Cell, opts referenceTransactionOptions) (*referenceTransactionResult, error) {
	if configRoot == nil {
		return nil, fmt.Errorf("reference transaction config root is nil")
	}
	return runReferenceOrdinaryTransactionWithConfigB64AndOptions(shard, msgCell, now, lt, randSeed, base64.StdEncoding.EncodeToString(configRoot.ToBOC()), opts)
}

func runReferenceOrdinaryTransactionWithConfigB64(shard *tlb.ShardAccount, msgCell *cell.Cell, now uint32, lt uint64, randSeed []byte, configB64 string) (*referenceTransactionResult, error) {
	return runReferenceOrdinaryTransactionWithConfigB64AndOptions(shard, msgCell, now, lt, randSeed, configB64, referenceTransactionOptions{})
}

func runReferenceOrdinaryTransactionWithConfigB64AndOptions(shard *tlb.ShardAccount, msgCell *cell.Cell, now uint32, lt uint64, randSeed []byte, configB64 string, opts referenceTransactionOptions) (*referenceTransactionResult, error) {
	shardCell, err := tlb.ToCell(shard)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize shard account: %w", err)
	}

	cConfig := C.CString(configB64)
	defer C.free(unsafe.Pointer(cConfig))
	emulator := C.transaction_emulator_create(cConfig, C.int(0))
	if emulator == nil {
		return nil, fmt.Errorf("failed to create reference transaction emulator")
	}
	defer C.transaction_emulator_destroy(emulator)

	if now != 0 && !bool(C.transaction_emulator_set_unixtime(emulator, C.uint32_t(now))) {
		return nil, fmt.Errorf("failed to set reference unixtime")
	}
	if lt != 0 && !bool(C.transaction_emulator_set_lt(emulator, C.uint64_t(lt))) {
		return nil, fmt.Errorf("failed to set reference logical time")
	}
	if len(randSeed) != 0 {
		seedHex := C.CString(fmt.Sprintf("%x", randSeed))
		defer C.free(unsafe.Pointer(seedHex))
		if !bool(C.transaction_emulator_set_rand_seed(emulator, seedHex)) {
			return nil, fmt.Errorf("failed to set reference rand seed")
		}
	}
	if opts.libs != nil {
		cLibs := C.CString(base64.StdEncoding.EncodeToString(opts.libs.ToBOC()))
		defer C.free(unsafe.Pointer(cLibs))
		if !bool(C.transaction_emulator_set_libs(emulator, cLibs)) {
			return nil, fmt.Errorf("failed to initialize reference libraries")
		}
	}
	if opts.prevBlocks.Len() > 0 {
		prevBlocksCell, err := stackValueToCell(opts.prevBlocks)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize reference prev blocks info: %w", err)
		}
		cPrevBlocks := C.CString(base64.StdEncoding.EncodeToString(prevBlocksCell.ToBOC()))
		defer C.free(unsafe.Pointer(cPrevBlocks))
		if !bool(C.transaction_emulator_set_prev_blocks_info(emulator, cPrevBlocks)) {
			return nil, fmt.Errorf("failed to initialize reference prev blocks info")
		}
	}

	cShard := C.CString(base64.StdEncoding.EncodeToString(shardCell.ToBOC()))
	defer C.free(unsafe.Pointer(cShard))
	cMsg := C.CString(base64.StdEncoding.EncodeToString(msgCell.ToBOC()))
	defer C.free(unsafe.Pointer(cMsg))

	resPtr := C.transaction_emulator_emulate_transaction(emulator, cShard, cMsg)
	if resPtr == nil {
		return nil, fmt.Errorf("reference transaction emulator returned nil")
	}
	defer C.free(unsafe.Pointer(resPtr))

	var raw referenceTransactionJSON
	if err = json.Unmarshal([]byte(C.GoString(resPtr)), &raw); err != nil {
		return nil, err
	}
	if !raw.Success {
		return nil, fmt.Errorf("reference transaction emulator failed: %s", raw.Error)
	}

	txCell, err := cellFromBocBase64(raw.Transaction)
	if err != nil {
		return nil, fmt.Errorf("failed to decode reference transaction: %w", err)
	}
	newShardCell, err := cellFromBocBase64(raw.ShardAccount)
	if err != nil {
		return nil, fmt.Errorf("failed to decode reference shard account: %w", err)
	}

	var actionsCell *cell.Cell
	if raw.Actions != nil {
		actionsCell, err = cellFromBocBase64(*raw.Actions)
		if err != nil {
			return nil, fmt.Errorf("failed to decode reference actions: %w", err)
		}
	}

	exitCode, gasUsed, err := referenceOrdinaryComputePhase(txCell)
	if err != nil {
		return nil, err
	}
	return &referenceTransactionResult{
		exitCode:    exitCode,
		gasUsed:     gasUsed,
		txCell:      txCell,
		shardCell:   newShardCell,
		actionsCell: actionsCell,
	}, nil
}

func referenceOrdinaryComputePhase(txCell *cell.Cell) (int64, int64, error) {
	var tx tlb.Transaction
	if err := tlb.LoadFromCell(&tx, txCell.BeginParse()); err != nil {
		return 0, 0, fmt.Errorf("failed to decode reference transaction: %w", err)
	}
	desc, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		return 0, 0, fmt.Errorf("unexpected reference transaction description type %T", tx.Description)
	}
	vmPhase, ok := desc.ComputePhase.Phase.(tlb.ComputePhaseVM)
	if !ok {
		if _, skipped := desc.ComputePhase.Phase.(tlb.ComputePhaseSkipped); skipped {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("unexpected reference compute phase type %T", desc.ComputePhase.Phase)
	}
	return int64(vmPhase.Details.ExitCode), vmPhase.Details.GasUsed.Int64(), nil
}

func mustReferenceTransactionConfigRoot(t testingT) *cell.Cell {
	b64, err := loadReferenceTransactionConfigB64()
	if err != nil {
		t.Fatalf("failed to load reference config fixture: %v", err)
	}
	root, err := cellFromBocBase64(b64)
	if err != nil {
		t.Fatalf("failed to decode reference config fixture: %v", err)
	}
	return referenceTransactionConfigRootWithGlobalVersion(t, root, vm.DefaultGlobalVersion)
}

func referenceTransactionConfigRootWithGlobalVersion(t testingT, base *cell.Cell, version uint32) *cell.Cell {
	params := tlb.BlockchainConfig{Root: base}.All()
	if params == nil {
		t.Fatalf("failed to load reference config params")
	}

	globalVersion, err := tlb.BlockchainConfig{Root: base}.GetGlobalVersion()
	if err != nil {
		t.Fatalf("failed to load reference global version: %v", err)
	}
	versionCell, err := tlb.ToCell(&tlb.GlobalVersion{
		Version:      version,
		Capabilities: globalVersion.Capabilities,
	})
	if err != nil {
		t.Fatalf("failed to build global version config: %v", err)
	}
	params[int32(tlb.ConfigParamGlobalVersion)] = versionCell
	return referenceTransactionConfigRootWithOverridesMap(t, params)
}

func referenceTransactionConfigRootWithOverrides(t testingT, base *cell.Cell, overrides map[int32]*cell.Cell) *cell.Cell {
	params := tlb.BlockchainConfig{Root: base}.All()
	if params == nil {
		t.Fatalf("failed to load reference config params")
	}
	for id, param := range overrides {
		params[id] = param
	}
	return referenceTransactionConfigRootWithOverridesMap(t, params)
}

func referenceTransactionConfigRootWithOverridesMap(t testingT, params map[int32]*cell.Cell) *cell.Cell {
	dict := cell.NewDict(32)
	for id, param := range params {
		value := cell.BeginCell().MustStoreRef(param).EndCell()
		if err := dict.SetIntKey(big.NewInt(int64(id)), value); err != nil {
			t.Fatalf("failed to store config param %d: %v", id, err)
		}
	}
	return dict.AsCell()
}

type testingT interface {
	Fatalf(format string, args ...any)
}
