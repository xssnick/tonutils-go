package tvm

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
)

const fatBlockReplayFixturePath = "testdata/tvm_replay_fat_block_66519406.json"

var benchmarkFatBlockReplayTransactions int

type fatBlockReplayFixture struct {
	Name                string                          `json:"name"`
	MasterSeqno         uint32                          `json:"master_seqno"`
	Block               fatBlockReplayBlockRef          `json:"block"`
	Accounts            int                             `json:"accounts"`
	Transactions        int                             `json:"transactions"`
	BlockBOCBase64      string                          `json:"block_boc_base64"`
	PreviousStateProofs []fatBlockReplayStateProof      `json:"previous_state_proofs"`
	PreviousAccounts    []fatBlockReplayAccountState    `json:"previous_accounts"`
	Config              fatBlockReplayConfig            `json:"config"`
	TransactionConfigs  []fatBlockReplayTransactionC7   `json:"transaction_configs"`
	Stats               fatBlockReplayFixtureBuildStats `json:"stats"`
}

type fatBlockReplayBlockRef struct {
	Workchain int32  `json:"workchain"`
	Shard     string `json:"shard"`
	Seqno     uint32 `json:"seqno"`
	RootHash  string `json:"root_hash"`
	FileHash  string `json:"file_hash"`
}

type fatBlockReplayStateProof struct {
	Block          fatBlockReplayBlockRef `json:"block"`
	RootHash       string                 `json:"root_hash"`
	ProofBOCBase64 string                 `json:"proof_boc_base64"`
}

type fatBlockReplayAccountState struct {
	Account               string `json:"account"`
	ShardAccountBOCBase64 string `json:"shard_account_boc_base64"`
	ShardAccountRootHash  string `json:"shard_account_root_hash"`
	AccountRootHash       string `json:"account_root_hash"`
}

type fatBlockReplayConfig struct {
	GlobalVersion                int      `json:"global_version"`
	ConfigRootBOCBase64          string   `json:"config_root_boc_base64"`
	PrevBlocksStackBOCBase64     string   `json:"prev_blocks_stack_boc_base64"`
	UnpackedConfigStackBOCBase64 string   `json:"unpacked_config_stack_boc_base64"`
	LibrariesBOCBase64           []string `json:"libraries_boc_base64"`
}

type fatBlockReplayTransactionC7 struct {
	Account                      string `json:"account"`
	LT                           uint64 `json:"lt"`
	Now                          uint32 `json:"now"`
	BlockLT                      int64  `json:"block_lt"`
	LogicalTime                  int64  `json:"logical_time"`
	RandSeedBase64               string `json:"rand_seed_base64"`
	IncomingValueStackBOCBase64  string `json:"incoming_value_stack_boc_base64"`
	StorageFees                  int64  `json:"storage_fees"`
	DuePaymentNano               string `json:"due_payment_nano"`
	PrecompiledGasStackBOCBase64 string `json:"precompiled_gas_stack_boc_base64"`
	InMsgParamsStackBOCBase64    string `json:"in_msg_params_stack_boc_base64"`
}

type fatBlockReplayFixtureBuildStats struct {
	BlockBOCBytes int `json:"block_boc_bytes"`
	ProofBOCBytes int `json:"proof_boc_bytes"`
}

type preparedFatBlockReplay struct {
	fixture  fatBlockReplayFixture
	machine  *TVM
	block    *tlb.Block
	accounts []fatBlockAccountWork
	config   fatBlockPreparedConfig
}

type fatBlockPreparedConfig struct {
	configRoot     *cell.Cell
	prevBlocks     tuple.Tuple
	unpackedConfig tuple.Tuple
	libraries      []*cell.Cell
}

type fatBlockAccountWork struct {
	account  []byte
	id       string
	expected []byte
	previous *tlb.ShardAccount
	txs      []fatBlockTransactionWork
}

type fatBlockTransactionWork struct {
	cell      *cell.Cell
	inMsgCell *cell.Cell
	parsed    *tlb.Transaction
	hash      []byte
	cfg       TransactionEmulationConfig
}

func TestTVMReplayFatBlockFixture(t *testing.T) {
	prepared := prepareFatBlockReplayFixture(t)
	txs, err := prepared.replay(true)
	if err != nil {
		t.Fatal(err)
	}
	if txs != prepared.fixture.Transactions {
		t.Fatalf("replayed %d transactions, want %d", txs, prepared.fixture.Transactions)
	}
}

func BenchmarkTVMReplayFatBlock(b *testing.B) {
	prepared := prepareFatBlockReplayFixture(b)

	b.ReportAllocs()
	b.ResetTimer()

	var txs int
	for i := 0; i < b.N; i++ {
		n, err := prepared.replay(true)
		if err != nil {
			b.Fatal(err)
		}
		txs += n
	}
	benchmarkFatBlockReplayTransactions = txs
	b.ReportMetric(float64(txs)/float64(b.N), "tx/op")
}

func BenchmarkTVMReplayFatBlockExecuteOnly(b *testing.B) {
	prepared := prepareFatBlockReplayFixture(b)

	b.ReportAllocs()
	b.ResetTimer()

	var txs int
	for i := 0; i < b.N; i++ {
		n, err := prepared.replay(false)
		if err != nil {
			b.Fatal(err)
		}
		txs += n
	}
	benchmarkFatBlockReplayTransactions = txs
	b.ReportMetric(float64(txs)/float64(b.N), "tx/op")
}

func prepareFatBlockReplayFixture(tb testing.TB) *preparedFatBlockReplay {
	tb.Helper()

	raw, err := os.ReadFile(fatBlockReplayFixturePath)
	if err != nil {
		tb.Fatal(err)
	}

	var fixture fatBlockReplayFixture
	if err = json.Unmarshal(raw, &fixture); err != nil {
		tb.Fatal(err)
	}

	blockRoot := fatBlockCell(tb, fixture.BlockBOCBase64)
	var block tlb.Block
	if err = tlb.Parse(&block, blockRoot); err != nil {
		tb.Fatal(err)
	}

	accounts, err := fatBlockAccountBlocks(&block)
	if err != nil {
		tb.Fatal(err)
	}
	if len(accounts) != fixture.Accounts {
		tb.Fatalf("loaded %d account blocks, want %d", len(accounts), fixture.Accounts)
	}
	if gotTxs := fatBlockCountTransactions(accounts); gotTxs != fixture.Transactions {
		tb.Fatalf("loaded %d transactions, want %d", gotTxs, fixture.Transactions)
	}

	for _, proof := range fixture.PreviousStateProofs {
		fatBlockStateProof(tb, proof)
	}

	accountByID := make(map[string]*tlb.ShardAccount, len(fixture.PreviousAccounts))
	for _, account := range fixture.PreviousAccounts {
		accountByID[account.Account] = fatBlockShardAccount(tb, account.ShardAccountBOCBase64)
	}
	fatBlockAttachPreviousAccounts(tb, fixture.Block.Workchain, accounts, accountByID)

	config := fatBlockPreparedConfig{
		configRoot:     fatBlockCell(tb, fixture.Config.ConfigRootBOCBase64),
		prevBlocks:     fatBlockTuple(tb, fixture.Config.PrevBlocksStackBOCBase64),
		unpackedConfig: fatBlockTuple(tb, fixture.Config.UnpackedConfigStackBOCBase64),
		libraries:      fatBlockCells(tb, fixture.Config.LibrariesBOCBase64),
	}
	txConfigByID := fatBlockPreparedTransactionConfigs(tb, fixture.TransactionConfigs, config)
	fatBlockAttachTransactionConfigs(tb, fixture.Block.Workchain, accounts, txConfigByID)

	machine := NewTVM()
	if err = machine.SetGlobalVersion(fixture.Config.GlobalVersion); err != nil {
		tb.Fatal(err)
	}

	return &preparedFatBlockReplay{
		fixture:  fixture,
		machine:  machine,
		block:    &block,
		accounts: accounts,
		config:   config,
	}
}

func (p *preparedFatBlockReplay) replay(validate bool) (int, error) {
	var txs int
	for _, account := range p.accounts {
		current := account.previous

		var accountStorageStat *cell.Cell
		for _, tx := range account.txs {
			txs++

			cfg := tx.cfg
			cfg.AccountStorageStat = accountStorageStat

			var res *TransactionExecutionResult
			var err error
			if tx.parsed.IO.In == nil || tx.inMsgCell == nil {
				desc, ok := tx.parsed.Description.(tlb.TransactionDescriptionTickTock)
				if !ok {
					return txs, fmt.Errorf("transaction %s lt=%d has no input message", account.id, tx.parsed.LT)
				}
				res, err = p.machine.EmulateTickTockTransaction(current, desc.IsTock, cfg)
			} else {
				res, err = p.machine.EmulateTransaction(current, tx.inMsgCell, cfg)
			}
			if err != nil {
				return txs, fmt.Errorf("emulate transaction %s lt=%d: %w", account.id, tx.parsed.LT, err)
			}
			if res == nil || res.TransactionCell == nil || res.ShardAccount == nil {
				return txs, fmt.Errorf("emulate transaction %s lt=%d returned incomplete result", account.id, tx.parsed.LT)
			}
			if validate {
				gotHash := res.TransactionCell.Hash()
				if !bytes.Equal(gotHash, tx.hash) {
					currentHash := []byte(nil)
					if current != nil && current.Account != nil {
						currentHash = current.Account.Hash()
					}
					return txs, fmt.Errorf("transaction hash mismatch %s lt=%d: got=%x want=%x current_account=%x block_account_new=%x tx_old=%x tx_new=%x res_account=%x", account.id, tx.parsed.LT, gotHash, tx.hash, currentHash, account.expected, tx.parsed.StateUpdate.OldHash, tx.parsed.StateUpdate.NewHash, res.ShardAccount.Account.Hash())
				}
			}

			current = res.ShardAccount
			accountStorageStat = res.AccountStorageStat
		}

		if current == nil || current.Account == nil {
			return txs, fmt.Errorf("missing final account %s", account.id)
		}
		if validate {
			gotHash := current.Account.Hash()
			if !bytes.Equal(gotHash, account.expected) {
				return txs, fmt.Errorf("account hash mismatch %s: got=%x want=%x", account.id, gotHash, account.expected)
			}
		}
	}
	return txs, nil
}

func fatBlockTransactionConfig(raw fatBlockReplayTransactionC7, config fatBlockPreparedConfig) (TransactionEmulationConfig, error) {
	randSeed, err := base64.StdEncoding.DecodeString(raw.RandSeedBase64)
	if err != nil {
		return TransactionEmulationConfig{}, err
	}

	var due any
	if raw.DuePaymentNano != "" {
		duePayment, ok := new(big.Int).SetString(raw.DuePaymentNano, 10)
		if !ok {
			return TransactionEmulationConfig{}, fmt.Errorf("invalid due payment %q", raw.DuePaymentNano)
		}
		due = duePayment
	}

	return TransactionEmulationConfig{
		Now:                 raw.Now,
		BlockLT:             raw.BlockLT,
		LogicalTime:         raw.LogicalTime,
		RandSeed:            randSeed,
		ConfigRoot:          config.configRoot,
		PrevBlocks:          config.prevBlocks,
		UnpackedConfig:      config.unpackedConfig,
		IncomingValue:       fatBlockTupleFromStack(raw.IncomingValueStackBOCBase64),
		StorageFees:         raw.StorageFees,
		DuePayment:          due,
		PrecompiledGasUsage: fatBlockBigIntFromStack(raw.PrecompiledGasStackBOCBase64),
		InMsgParams:         fatBlockTupleFromStack(raw.InMsgParamsStackBOCBase64),
		Libraries:           config.libraries,
	}, nil
}

func fatBlockPreparedTransactionConfigs(tb testing.TB, raw []fatBlockReplayTransactionC7, config fatBlockPreparedConfig) map[string]TransactionEmulationConfig {
	tb.Helper()

	out := make(map[string]TransactionEmulationConfig, len(raw))
	for _, item := range raw {
		cfg, err := fatBlockTransactionConfig(item, config)
		if err != nil {
			tb.Fatal(err)
		}
		out[fatBlockTxConfigKey(item.Account, item.LT)] = cfg
	}
	return out
}

func fatBlockAttachPreviousAccounts(tb testing.TB, workchain int32, accounts []fatBlockAccountWork, previous map[string]*tlb.ShardAccount) {
	tb.Helper()

	for idx := range accounts {
		accountID := fatBlockAccountRaw(workchain, accounts[idx].account)
		shard, ok := previous[accountID]
		if !ok {
			tb.Fatalf("previous account fixture is missing for %s", accountID)
		}
		accounts[idx].id = accountID
		accounts[idx].previous = shard
	}
}

func fatBlockAttachTransactionConfigs(tb testing.TB, workchain int32, accounts []fatBlockAccountWork, configs map[string]TransactionEmulationConfig) {
	tb.Helper()

	for accountIdx := range accounts {
		accountID := accounts[accountIdx].id
		if accountID == "" {
			accountID = fatBlockAccountRaw(workchain, accounts[accountIdx].account)
		}
		for txIdx := range accounts[accountIdx].txs {
			tx := &accounts[accountIdx].txs[txIdx]
			cfg, ok := configs[fatBlockTxConfigKey(accountID, tx.parsed.LT)]
			if !ok {
				tb.Fatalf("transaction config not found for %s lt=%d", accountID, tx.parsed.LT)
			}
			tx.cfg = cfg
		}
	}
}

func fatBlockStateProof(tb testing.TB, proof fatBlockReplayStateProof) {
	tb.Helper()

	rootHash, err := hex.DecodeString(proof.RootHash)
	if err != nil {
		tb.Fatal(err)
	}
	proofRoot := fatBlockCell(tb, proof.ProofBOCBase64)
	root, err := cell.UnwrapProof(proofRoot, rootHash)
	if err != nil {
		tb.Fatal(err)
	}

	var state tlb.ShardStateUnsplit
	if err = tlb.Parse(&state, root); err != nil {
		tb.Fatal(err)
	}
}

func fatBlockAccountBlocks(block *tlb.Block) ([]fatBlockAccountWork, error) {
	if block == nil || block.Extra == nil || block.Extra.ShardAccountBlocks == nil {
		return nil, fmt.Errorf("block has no shard account blocks")
	}

	accounts, err := block.Extra.ShardAccountBlocks.BeginParse()
	if err != nil {
		return nil, err
	}
	hasAccounts, err := accounts.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if !hasAccounts {
		return nil, nil
	}
	root, err := accounts.LoadRefCell()
	if err != nil {
		return nil, err
	}
	items, err := root.AsDict(256).LoadAll()
	if err != nil {
		return nil, err
	}

	out := make([]fatBlockAccountWork, 0, len(items))
	for _, item := range items {
		if err = fatBlockSkipCurrencyCollectionBoundary(item.Value); err != nil {
			return nil, err
		}

		var accountBlock tlb.AccountBlock
		if err = tlb.LoadFromCell(&accountBlock, item.Value); err != nil {
			return nil, err
		}

		var update tlb.HashUpdate
		if accountBlock.StateUpdate != nil {
			if err = tlb.Parse(&update, accountBlock.StateUpdate); err != nil {
				return nil, err
			}
		}

		work := fatBlockAccountWork{
			account:  append([]byte(nil), accountBlock.Addr...),
			expected: append([]byte(nil), update.NewHash...),
		}
		if accountBlock.Transactions != nil && !accountBlock.Transactions.IsEmpty() {
			txItems, err := accountBlock.Transactions.AsCell().AsDict(64).LoadAll()
			if err != nil {
				return nil, err
			}
			work.txs = make([]fatBlockTransactionWork, 0, len(txItems))
			for _, txItem := range txItems {
				if err = fatBlockSkipCurrencyCollectionBoundary(txItem.Value); err != nil {
					return nil, err
				}
				txCell, err := txItem.Value.LoadRefCell()
				if err != nil {
					return nil, err
				}

				var tx tlb.Transaction
				if err = tlb.Parse(&tx, txCell); err != nil {
					return nil, err
				}
				inMsgCell, err := fatBlockInputMessageCell(txCell)
				if err != nil {
					return nil, err
				}
				hash := txCell.Hash()
				work.txs = append(work.txs, fatBlockTransactionWork{
					cell:      txCell,
					inMsgCell: inMsgCell,
					parsed:    &tx,
					hash:      append([]byte(nil), hash...),
				})
			}
			sort.Slice(work.txs, func(i, j int) bool {
				return work.txs[i].parsed.LT < work.txs[j].parsed.LT
			})
		}
		out = append(out, work)
	}
	return out, nil
}

func fatBlockSkipCurrencyCollectionBoundary(loader *cell.Slice) error {
	if _, err := loader.LoadBigCoins(); err != nil {
		return err
	}
	_, err := loader.LoadMaybeRef()
	return err
}

func fatBlockInputMessageCell(txCell *cell.Cell) (*cell.Cell, error) {
	loader, err := txCell.BeginParse()
	if err != nil {
		return nil, err
	}
	ioCell, err := loader.LoadRefCell()
	if err != nil {
		return nil, err
	}

	io := ioCell.MustBeginParse()
	hasInput, err := io.LoadBoolBit()
	if err != nil {
		return nil, err
	}
	if !hasInput {
		return nil, nil
	}
	return io.LoadRefCell()
}

func fatBlockCell(tb testing.TB, boc string) *cell.Cell {
	tb.Helper()

	raw, err := base64.StdEncoding.DecodeString(boc)
	if err != nil {
		tb.Fatal(err)
	}
	root, err := cell.FromBOC(raw)
	if err != nil {
		tb.Fatal(err)
	}
	return root
}

func fatBlockCells(tb testing.TB, bocs []string) []*cell.Cell {
	tb.Helper()

	if len(bocs) == 0 {
		return nil
	}
	out := make([]*cell.Cell, 0, len(bocs))
	for _, boc := range bocs {
		out = append(out, fatBlockCell(tb, boc))
	}
	return out
}

func fatBlockShardAccount(tb testing.TB, boc string) *tlb.ShardAccount {
	tb.Helper()

	root := fatBlockCell(tb, boc)
	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, root); err != nil {
		tb.Fatal(err)
	}
	return &shard
}

func fatBlockTuple(tb testing.TB, boc string) tuple.Tuple {
	tb.Helper()

	value := fatBlockStackValue(tb, boc)
	if value == nil {
		return tuple.Tuple{}
	}
	tup, ok := value.(tuple.Tuple)
	if !ok {
		tb.Fatalf("stack value has type %T, want tuple.Tuple", value)
	}
	return tup
}

func fatBlockTupleFromStack(boc string) tuple.Tuple {
	value := fatBlockStackValueNoFatal(boc)
	if value == nil {
		return tuple.Tuple{}
	}
	tup, _ := value.(tuple.Tuple)
	return tup
}

func fatBlockBigIntFromStack(boc string) *big.Int {
	value := fatBlockStackValueNoFatal(boc)
	if value == nil {
		return nil
	}
	num, _ := value.(*big.Int)
	return num
}

func fatBlockStackValue(tb testing.TB, boc string) any {
	tb.Helper()

	value, err := fatBlockParseStackValue(boc)
	if err != nil {
		tb.Fatal(err)
	}
	return value
}

func fatBlockStackValueNoFatal(boc string) any {
	value, err := fatBlockParseStackValue(boc)
	if err != nil {
		panic(err)
	}
	return value
}

func fatBlockParseStackValue(boc string) (any, error) {
	if boc == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(boc)
	if err != nil {
		return nil, err
	}
	root, err := cell.FromBOC(raw)
	if err != nil {
		return nil, err
	}
	value, err := tlb.ParseStackValue(root.MustBeginParse())
	if err != nil {
		return nil, err
	}
	return fatBlockTupleValue(value), nil
}

func fatBlockTupleValue(value any) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}

	normalized := make([]any, len(items))
	for i, item := range items {
		normalized[i] = fatBlockTupleValue(item)
	}
	return tuple.NewTupleValue(normalized...)
}

func fatBlockCountTransactions(accounts []fatBlockAccountWork) int {
	var count int
	for _, account := range accounts {
		count += len(account.txs)
	}
	return count
}

func fatBlockAccountRaw(workchain int32, account []byte) string {
	return fmt.Sprintf("%d:%x", workchain, account)
}

func fatBlockTxConfigKey(account string, lt uint64) string {
	return fmt.Sprintf("%s:%d", account, lt)
}
