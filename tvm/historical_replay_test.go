package tvm

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

type historicalBlockID struct {
	Workchain int32
	Shard     int64
	SeqNo     uint32
	RootHash  []byte
	FileHash  []byte
}

type historicalBlockPointer struct {
	ID                   historicalBlockID
	InclusionMasterSeqno uint32
}

type historicalBlockSource struct {
	Pointer historicalBlockPointer
	BOC     []byte
}

type historicalFixtureConfig struct {
	GlobalVersion uint32   `json:"global_version"`
	Root          []byte   `json:"config_root_boc_base64"`
	PrevBlocks    []byte   `json:"prev_blocks_stack_boc_base64"`
	Libraries     [][]byte `json:"libraries_boc_base64"`
}

type historicalAccountFixture struct {
	Source  historicalBlockSource   `json:"source"`
	Master  historicalBlockID       `json:"context_master"`
	Account string                  `json:"account"`
	Before  []byte                  `json:"before_boc"`
	Config  historicalFixtureConfig `json:"config"`
}

type historicalTransactionWitness struct {
	Cell *cell.Cell
	Tx   tlb.Transaction
}

type historicalAccountWitness struct {
	Account     []byte
	Workchain   int32
	InitialHash []byte
	FinalHash   []byte
	Txs         []historicalTransactionWitness
}

func historicalReadFile(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "historical-replay", name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(name, ".gz") {
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()

		raw, err = io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
	}

	return raw
}

func historicalParseJSON(t *testing.T, raw []byte, target any) {
	t.Helper()

	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}

func historicalParseCell(t *testing.T, raw []byte) *cell.Cell {
	t.Helper()

	root, err := cell.FromBOC(raw)
	if err != nil {
		t.Fatal(err)
	}

	return root
}

func historicalParseTLB(t *testing.T, target any, root *cell.Cell) {
	t.Helper()

	if err := tlb.Parse(target, root); err != nil {
		t.Fatal(err)
	}
}

func historicalGenesisState(t *testing.T) (historicalBlockID, *tlb.ShardStateUnsplit) {
	t.Helper()

	var id historicalBlockID
	historicalParseJSON(t, historicalReadFile(t, "genesis/zerostate.json"), &id)
	raw := historicalReadFile(t, "genesis/zerostate.boc")
	root := historicalParseCell(t, raw)
	digest := sha256.Sum256(raw)
	if !bytes.Equal(digest[:], id.FileHash) || !bytes.Equal(root.Hash(0), id.RootHash) {
		t.Fatal("zerostate commitment mismatch")
	}
	var state tlb.ShardStateUnsplit
	historicalParseTLB(t, &state, root)
	if id.SeqNo != 0 || state.Seqno != 0 || id.Workchain != -1 {
		t.Fatal("wrong zerostate identity")
	}

	return id, &state
}

func historicalLoadBlock(t *testing.T, source historicalBlockSource) *tlb.Block {
	t.Helper()

	root := historicalParseCell(t, source.BOC)
	// The block fixtures prune state_update, so authenticate the original
	// root through the Merkle proof instead of hashing the proof's BOC bytes.
	root, err := cell.UnwrapProof(root, source.Pointer.ID.RootHash)
	if err != nil {
		t.Fatal(err)
	}

	var block tlb.Block
	historicalParseTLB(t, &block, root)
	if block.BlockInfo.SeqNo != source.Pointer.ID.SeqNo ||
		block.BlockInfo.Shard.WorkchainID != source.Pointer.ID.Workchain ||
		uint64(block.BlockInfo.Shard.GetShardID()) != uint64(source.Pointer.ID.Shard) {
		t.Fatal("wrong block identity")
	}

	return &block
}

func historicalVerifyParent(t *testing.T, block *tlb.Block, master historicalBlockID) {
	t.Helper()

	parent := block.BlockInfo.PrevRef.Prev1
	if block.BlockInfo.Shard.WorkchainID != -1 {
		if block.BlockInfo.MasterRef == nil {
			t.Fatal("shard fixture has no MasterRef")
		}
		parent = *block.BlockInfo.MasterRef
	}
	if master.Workchain != -1 || master.Shard != -1<<63 || parent.SeqNo != master.SeqNo ||
		!bytes.Equal(parent.RootHash, master.RootHash) || !bytes.Equal(parent.FileHash, master.FileHash) {
		t.Fatal("execution context does not match the block's master reference")
	}
}

func historicalSkipCurrency(t *testing.T, s *cell.Slice) {
	t.Helper()

	if _, err := s.LoadBigCoins(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadMaybeRef(); err != nil {
		t.Fatal(err)
	}
}

func historicalBlockAccounts(t *testing.T, block *tlb.Block) []historicalAccountWitness {
	t.Helper()

	s := block.Extra.ShardAccountBlocks.MustBeginParse()
	if !s.MustLoadBoolBit() {
		t.Fatal("fixture contains no account blocks")
	}
	entries, err := s.MustLoadRef().MustToCell().AsDict(256).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	var result []historicalAccountWitness
	for _, entry := range entries {
		historicalSkipCurrency(t, entry.Value)
		var ab tlb.AccountBlock
		if err := tlb.LoadFromCell(&ab, entry.Value); err != nil {
			t.Fatal(err)
		}
		var update tlb.HashUpdate
		historicalParseTLB(t, &update, ab.StateUpdate)
		account := historicalAccountWitness{
			Account: ab.Addr, Workchain: block.BlockInfo.Shard.WorkchainID,
			InitialHash: update.OldHash, FinalHash: update.NewHash,
		}
		txs, err := ab.Transactions.AsCell().AsDict(64).LoadAll()
		if err != nil {
			t.Fatal(err)
		}
		for _, txEntry := range txs {
			historicalSkipCurrency(t, txEntry.Value)
			txCell, err := txEntry.Value.LoadRefCell()
			if err != nil {
				t.Fatal(err)
			}
			w := historicalTransactionWitness{Cell: txCell}
			historicalParseTLB(t, &w.Tx, txCell)
			account.Txs = append(account.Txs, w)
		}
		slices.SortFunc(account.Txs, func(a, b historicalTransactionWitness) int { return cmp.Compare(a.Tx.LT, b.Tx.LT) })
		result = append(result, account)
	}

	return result
}

func historicalBlockTuple(id historicalBlockID) tuple.Tuple {
	return tuple.NewTupleValue(big.NewInt(int64(id.Workchain)), new(big.Int).SetUint64(uint64(id.Shard)),
		new(big.Int).SetUint64(uint64(id.SeqNo)), new(big.Int).SetBytes(id.RootHash), new(big.Int).SetBytes(id.FileHash))
}

func historicalFixtureBlockContext(t *testing.T, f historicalAccountFixture, block *tlb.Block) *BlockContext {
	t.Helper()

	config, err := PrepareBlockchainConfig(historicalParseCell(t, f.Config.Root))
	if err != nil {
		t.Fatal(err)
	}
	if config.GlobalVersion() != f.Config.GlobalVersion {
		t.Fatalf("config global version = %d, fixture declares %d", config.GlobalVersion(), f.Config.GlobalVersion)
	}
	value, err := tlb.ParseStackValue(historicalParseCell(t, f.Config.PrevBlocks).MustBeginParse())
	if err != nil {
		t.Fatal(err)
	}
	previous := replayRegressionTupleValue(value).(tuple.Tuple)
	var libraries []*cell.Cell
	for _, raw := range f.Config.Libraries {
		libraries = append(libraries, historicalParseCell(t, raw))
	}
	ctx, err := config.NewBlockContext(BlockOptions{Now: block.BlockInfo.GenUtime,
		BlockLT: int64(block.BlockInfo.StartLt), RandSeed: block.Extra.RandSeed, PrevBlocks: previous, Libraries: libraries})
	if err != nil {
		t.Fatal(err)
	}

	return ctx
}

func historicalPrepareFirstTransaction(t *testing.T, account historicalAccountWitness, before []byte) (*PreparedAccount, *PreparedMessage) {
	t.Helper()

	var shard tlb.ShardAccount
	historicalParseTLB(t, &shard, historicalParseCell(t, before))
	current, err := PrepareAccount(&shard, address.NewAddress(0, byte(account.Workchain), account.Account))
	if err != nil {
		t.Fatal(err)
	}
	message, err := PrepareMessage(replayRegressionInputMessage(t, account.Txs[0].Cell))
	if err != nil {
		t.Fatal(err)
	}

	return current, message
}

func historicalReplayAccount(t *testing.T, ctx *BlockContext, account historicalAccountWitness, before []byte, options TransactionOptions) {
	t.Helper()

	var initial tlb.ShardAccount
	historicalParseTLB(t, &initial, historicalParseCell(t, before))
	if !bytes.Equal(initial.Account.Hash(0), account.InitialHash) {
		t.Fatal("initial account-block hash mismatch")
	}

	current, err := PrepareAccount(&initial, address.NewAddress(0, byte(account.Workchain), account.Account))
	if err != nil {
		t.Fatal(err)
	}
	previous := &initial
	var storageStat *cell.Cell
	for _, witness := range account.Txs {
		tx := witness.Tx
		if !bytes.Equal(previous.Account.Hash(0), tx.StateUpdate.OldHash) ||
			previous.LastTransLT != tx.PrevTxLT || !bytes.Equal(previous.LastTransHash, tx.PrevTxHash) {
			t.Fatalf("invalid pre-state or previous transaction chain at LT %d", tx.LT)
		}
		options.LogicalTime = int64(tx.LT)
		options.AccountStorageStat = storageStat
		var result *TransactionExecutionResult
		var compute tlb.ComputePhase
		switch description := tx.Description.(type) {
		case tlb.TransactionDescriptionOrdinary:
			compute = description.ComputePhase
			io := witness.Cell.MustBeginParse().MustLoadRef()
			if !io.MustLoadBoolBit() {
				t.Fatal("ordinary transaction has no inbound message")
			}
			msgCell, err := io.LoadRefCell()
			if err != nil {
				t.Fatal(err)
			}
			msg, err := PrepareMessage(msgCell)
			if err != nil {
				t.Fatal(err)
			}
			result, err = NewTVM().EmulateTransaction(ctx, current, msg, options)
			if err != nil {
				t.Fatal(err)
			}
		case tlb.TransactionDescriptionTickTock:
			compute = description.ComputePhase
			result, err = NewTVM().EmulateTickTockTransaction(ctx, current, description.IsTock, options)
			if err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unsupported fixture transaction %T", description)
		}
		if result.TransactionCell == nil || result.NextAccount == nil ||
			(tx.IO.In != nil && tx.IO.In.MsgType == tlb.MsgTypeExternalIn && !result.Accepted) {
			t.Fatalf("historical replay rejected LT %d before producing a transaction: accepted=%t exit=%d", tx.LT, result.Accepted, result.ExitCode)
		}

		actual, err := result.ParseTransaction()
		if err != nil {
			t.Fatal(err)
		}
		if actual.TotalFees.Coins.Nano().Cmp(tx.TotalFees.Coins.Nano()) != 0 || actual.OutMsgCount != tx.OutMsgCount {
			t.Fatalf("LT %d outputs: fees=%s messages=%d, want fees=%s messages=%d",
				tx.LT, actual.TotalFees.Coins.Nano(), actual.OutMsgCount, tx.TotalFees.Coins.Nano(), tx.OutMsgCount)
		}

		if phase, ok := compute.Phase.(tlb.ComputePhaseVM); ok {
			details := phase.Details
			actualPhase, ok := historicalComputePhase(t, actual).Phase.(tlb.ComputePhaseVM)
			if !ok {
				t.Fatalf("LT %d compute phase was skipped", tx.LT)
			}
			actualCredit, credit := actualPhase.Details.GasCredit, details.GasCredit
			if actualPhase.Details.GasLimit.Cmp(details.GasLimit) != 0 ||
				(actualCredit == nil) != (credit == nil) || (actualCredit != nil && actualCredit.Cmp(credit) != 0) ||
				actualPhase.GasFees.Nano().Cmp(phase.GasFees.Nano()) != 0 {
				t.Fatalf("LT %d gas accounting: limit=%s credit=%v fees=%s, want limit=%s credit=%v fees=%s",
					tx.LT, actualPhase.Details.GasLimit, actualCredit, actualPhase.GasFees.Nano(), details.GasLimit, credit, phase.GasFees.Nano())
			}
			if result.GasUsed != details.GasUsed.Int64() || result.Steps != uint64(details.VMSteps) || result.ExitCode != int64(details.ExitCode) {
				t.Fatalf("LT %d compute: gas=%d steps=%d exit=%d, want gas=%s steps=%d exit=%d",
					tx.LT, result.GasUsed, result.Steps, result.ExitCode, details.GasUsed, details.VMSteps, details.ExitCode)
			}
		}

		next := result.NextAccount.ShardAccount()
		t.Logf("LT=%d gas=%d steps=%d exit=%d tx=%x", tx.LT, result.GasUsed, result.Steps, result.ExitCode, result.TransactionCell.Hash(0))
		if !bytes.Equal(result.TransactionCell.Hash(0), witness.Cell.Hash(0)) ||
			!bytes.Equal(next.Account.Hash(0), tx.StateUpdate.NewHash) ||
			next.LastTransLT != tx.LT || !bytes.Equal(next.LastTransHash, witness.Cell.Hash(0)) {
			t.Fatalf("historical replay mismatch LT=%d: transaction got=%x want=%x; account got=%x want=%x",
				tx.LT, result.TransactionCell.Hash(0), witness.Cell.Hash(0), next.Account.Hash(0), tx.StateUpdate.NewHash)
		}
		current, previous, storageStat = result.NextAccount, next, result.AccountStorageStat
	}
	if !bytes.Equal(previous.Account.Hash(0), account.FinalHash) {
		t.Fatal("final account-block hash mismatch")
	}
}

func historicalComputePhase(t *testing.T, tx *tlb.Transaction) tlb.ComputePhase {
	t.Helper()

	switch description := tx.Description.(type) {
	case tlb.TransactionDescriptionOrdinary:
		return description.ComputePhase
	case tlb.TransactionDescriptionTickTock:
		return description.ComputePhase
	default:
		t.Fatalf("unsupported fixture transaction %T", description)
		return tlb.ComputePhase{}
	}
}

func TestHistoricalReplayGenesis(t *testing.T) {
	var pointer historicalBlockPointer
	historicalParseJSON(t, historicalReadFile(t, "genesis/block.json"), &pointer)
	block := historicalLoadBlock(t, historicalBlockSource{Pointer: pointer, BOC: historicalReadFile(t, "genesis/block.boc")})
	zero, state := historicalGenesisState(t)
	historicalVerifyParent(t, block, zero)

	stats := state.Stats.MustBeginParse()
	stats.MustLoadUInt(64)
	stats.MustLoadUInt(64)
	historicalSkipCurrency(t, stats)
	historicalSkipCurrency(t, stats)
	libs, err := stats.LoadDict(256)
	if err != nil {
		t.Fatal(err)
	}
	var libraries []*cell.Cell
	if !libs.IsEmpty() {
		libraries = append(libraries, libs.AsCell())
	}
	previous := tuple.NewTupleValue(tuple.NewTupleValue(historicalBlockTuple(zero)), historicalBlockTuple(zero), tuple.NewTupleValue(historicalBlockTuple(zero)))
	var extra tlb.McStateExtra
	historicalParseTLB(t, &extra, state.McStateExtra)
	config, err := PrepareBlockchainConfig(extra.ConfigParams.Config.Params.AsCell())
	if err != nil {
		t.Fatal(err)
	}
	if config.GlobalVersion() != 0 {
		t.Fatal("expected global version zero")
	}

	ctx, err := config.NewBlockContext(BlockOptions{Now: block.BlockInfo.GenUtime,
		BlockLT: int64(block.BlockInfo.StartLt), RandSeed: block.Extra.RandSeed, PrevBlocks: previous, Libraries: libraries})
	if err != nil {
		t.Fatal(err)
	}

	accounts := historicalBlockAccounts(t, block)
	total := 0
	for _, account := range accounts {
		total += len(account.Txs)
	}
	if len(accounts) != 5 || total != 9 {
		t.Fatalf("genesis inventory: %d accounts / %d transactions", len(accounts), total)
	}
	for _, account := range accounts {
		t.Run(hex.EncodeToString(account.Account), func(t *testing.T) {
			before := historicalReadFile(t, fmt.Sprintf("genesis/before-%x.boc", account.Account))
			historicalReplayAccount(t, ctx, account, before, TransactionOptions{Historical: vm.HistoricalConfig{GasSchedule: vm.GasSchedule2019}})
		})
	}
}

func TestHistoricalReplayConfig94558(t *testing.T) {
	var fixture historicalAccountFixture
	historicalParseJSON(t, historicalReadFile(t, "config-94558.json"), &fixture)
	block := historicalLoadBlock(t, fixture.Source)
	historicalVerifyParent(t, block, fixture.Master)
	if fixture.Master.SeqNo != 94557 || block.BlockInfo.SeqNo != 94558 {
		t.Fatal("wrong config fixture block")
	}
	ctx := historicalFixtureBlockContext(t, fixture, block)
	accountID, err := hex.DecodeString(fixture.Account[len(fixture.Account)-64:])
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range historicalBlockAccounts(t, block) {
		if !bytes.Equal(account.Account, accountID) {
			continue
		}
		if len(account.Txs) != 2 || account.Txs[0].Tx.LT != 104990000001 {
			t.Fatal("wrong config transaction inventory")
		}
		if hex.EncodeToString(account.Txs[0].Cell.Hash(0)) != "3f75f750162d4c9defbea07eb840e9fe38bc60d526d5a7abfff72e17b542422e" {
			t.Fatal("wrong config transaction commitment")
		}
		historicalReplayAccount(t, ctx, account, fixture.Before, TransactionOptions{Historical: vm.HistoricalConfig{GasSchedule: vm.GasSchedule2019, PopC3Cell: true}})
		return
	}
	t.Fatal("config account is missing")
}
