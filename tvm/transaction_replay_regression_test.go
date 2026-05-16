package tvm

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

type replayRegressionReport struct {
	Config   replayRegressionSharedConfig `json:"config"`
	Accounts []replayRegressionAccount    `json:"accounts"`
}

type replayRegressionAccount struct {
	Address string             `json:"address"`
	From    string             `json:"from_shard_account_boc_base64"`
	To      string             `json:"to_shard_account_boc_base64"`
	FirstTx replayRegressionTx `json:"first_tx"`
}

type replayRegressionTx struct {
	LT                           uint64 `json:"lt"`
	ExpectedTxHash               string `json:"expected_tx_hash"`
	ExpectedTxBOCBase64          string `json:"expected_tx_boc_base64"`
	Now                          uint32 `json:"now"`
	BlockLT                      int64  `json:"block_lt"`
	LogicalTime                  int64  `json:"logical_time"`
	RandSeedBase64               string `json:"rand_seed_base64"`
	PrecompiledGasStackBOCBase64 string `json:"precompiled_gas_stack_boc_base64"`
	IncomingValueStackBOCBase64  string `json:"incoming_value_stack_boc_base64"`
	StorageFees                  int64  `json:"storage_fees"`
	DuePaymentNano               string `json:"due_payment_nano"`
	InMsgParamsStackBOCBase64    string `json:"in_msg_params_stack_boc_base64"`
}

type replayRegressionSharedConfig struct {
	GlobalVersion                int      `json:"global_version"`
	ConfigRootBOCBase64          string   `json:"config_root_boc_base64"`
	PrevBlocksStackBOCBase64     string   `json:"prev_blocks_stack_boc_base64"`
	UnpackedConfigStackBOCBase64 string   `json:"unpacked_config_stack_boc_base64"`
	LibrariesBOCBase64           []string `json:"libraries_boc_base64"`
}

func TestEmulateTransactionReplayRegressionFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/transaction_replay_regression.json")
	if err != nil {
		t.Fatal(err)
	}

	var report replayRegressionReport
	if err = json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}

	for _, account := range report.Accounts {
		t.Run(fmt.Sprintf("%s/%d", account.Address, account.FirstTx.LT), func(t *testing.T) {
			from := replayRegressionShardAccount(t, account.From)
			expectedTx := replayRegressionCell(t, account.FirstTx.ExpectedTxBOCBase64)
			expectedShardCell := replayRegressionCell(t, account.To)
			inMsg := replayRegressionInputMessage(t, expectedTx)

			var expectedShard tlb.ShardAccount
			if err := tlb.Parse(&expectedShard, expectedShardCell); err != nil {
				t.Fatal(err)
			}

			machine := NewTVM()
			if err := machine.SetGlobalVersion(report.Config.GlobalVersion); err != nil {
				t.Fatal(err)
			}

			result, err := machine.EmulateTransaction(from, inMsg, replayRegressionEmulationConfig(t, report.Config, account.FirstTx))
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || result.TransactionCell == nil || result.ShardAccount == nil {
				t.Fatal("emulation returned no transaction")
			}

			if !bytes.Equal(result.TransactionCell.Hash(), expectedTx.Hash()) {
				t.Fatalf("transaction hash mismatch: got %x, want %x", result.TransactionCell.Hash(), expectedTx.Hash())
			}
			if !bytes.Equal(result.ShardAccount.Account.Hash(), expectedShard.Account.Hash()) {
				t.Fatalf("account root hash mismatch: got %x, want %x", result.ShardAccount.Account.Hash(), expectedShard.Account.Hash())
			}
			if !bytes.Equal(result.ShardAccount.LastTransHash, expectedTx.Hash()) {
				t.Fatalf("last transaction hash mismatch: got %x, want %x", result.ShardAccount.LastTransHash, expectedTx.Hash())
			}
		})
	}
}

func replayRegressionEmulationConfig(t *testing.T, shared replayRegressionSharedConfig, tx replayRegressionTx) TransactionEmulationConfig {
	t.Helper()

	randSeed, err := base64.StdEncoding.DecodeString(tx.RandSeedBase64)
	if err != nil {
		t.Fatal(err)
	}

	var due any
	if tx.DuePaymentNano != "" {
		duePayment, ok := new(big.Int).SetString(tx.DuePaymentNano, 10)
		if !ok {
			t.Fatalf("invalid due payment %q", tx.DuePaymentNano)
		}
		due = duePayment
	}

	return TransactionEmulationConfig{
		Now:                 tx.Now,
		BlockLT:             tx.BlockLT,
		LogicalTime:         tx.LogicalTime,
		RandSeed:            randSeed,
		ConfigRoot:          replayRegressionCell(t, shared.ConfigRootBOCBase64),
		PrevBlocks:          replayRegressionTuple(t, shared.PrevBlocksStackBOCBase64),
		UnpackedConfig:      replayRegressionTuple(t, shared.UnpackedConfigStackBOCBase64),
		PrecompiledGasUsage: replayRegressionBigInt(t, tx.PrecompiledGasStackBOCBase64),
		IncomingValue:       replayRegressionTuple(t, tx.IncomingValueStackBOCBase64),
		StorageFees:         tx.StorageFees,
		DuePayment:          due,
		InMsgParams:         replayRegressionTuple(t, tx.InMsgParamsStackBOCBase64),
		Libraries:           replayRegressionCells(t, shared.LibrariesBOCBase64),
	}
}

func replayRegressionShardAccount(t *testing.T, boc string) *tlb.ShardAccount {
	t.Helper()

	root := replayRegressionCell(t, boc)
	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, root); err != nil {
		t.Fatal(err)
	}
	return &shard
}

func replayRegressionInputMessage(t *testing.T, txCell *cell.Cell) *cell.Cell {
	t.Helper()

	ioCell, err := txCell.MustBeginParse().LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	ioLoader := ioCell.MustBeginParse()
	if !ioLoader.MustLoadBoolBit() {
		t.Fatal("transaction has no input message")
	}
	inMsg, err := ioLoader.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	return inMsg
}

func replayRegressionTuple(t *testing.T, boc string) tuple.Tuple {
	t.Helper()

	value := replayRegressionStackValue(t, boc)
	if value == nil {
		return tuple.Tuple{}
	}
	tup, ok := value.(tuple.Tuple)
	if !ok {
		t.Fatalf("stack value has type %T, want tuple.Tuple", value)
	}
	return tup
}

func replayRegressionBigInt(t *testing.T, boc string) *big.Int {
	t.Helper()

	value := replayRegressionStackValue(t, boc)
	if value == nil {
		return nil
	}
	num, ok := value.(*big.Int)
	if !ok {
		t.Fatalf("stack value has type %T, want *big.Int", value)
	}
	return num
}

func replayRegressionStackValue(t *testing.T, boc string) any {
	t.Helper()

	if boc == "" {
		return nil
	}
	value, err := tlb.ParseStackValue(replayRegressionCell(t, boc).MustBeginParse())
	if err != nil {
		t.Fatal(err)
	}
	return replayRegressionTupleValue(value)
}

func replayRegressionTupleValue(value any) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}

	normalized := make([]any, len(items))
	for i, item := range items {
		normalized[i] = replayRegressionTupleValue(item)
	}
	return tuple.NewTupleValue(normalized...)
}

func replayRegressionCells(t *testing.T, bocs []string) []*cell.Cell {
	t.Helper()

	if len(bocs) == 0 {
		return nil
	}
	cells := make([]*cell.Cell, 0, len(bocs))
	for _, boc := range bocs {
		cells = append(cells, replayRegressionCell(t, boc))
	}
	return cells
}

func replayRegressionCell(t *testing.T, boc string) *cell.Cell {
	t.Helper()

	raw, err := base64.StdEncoding.DecodeString(boc)
	if err != nil {
		t.Fatal(err)
	}
	root, err := cell.FromBOC(raw)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
