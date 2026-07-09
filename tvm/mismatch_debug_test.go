package tvm

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

type debugMismatchReport struct {
	Accounts []debugMismatchAccount `json:"accounts"`
}

const debugMismatchBigMasterAccount = "-1:3333333333333333333333333333333333333333333333333333333333333333"

var benchmarkMismatchReplayTransactions int

type debugMismatchAccount struct {
	MasterSeqno               uint32               `json:"master_seqno"`
	Workchain                 int32                `json:"workchain"`
	Address                   string               `json:"address"`
	FromShardAccountBOCBase64 string               `json:"from_shard_account_boc_base64"`
	ToShardAccountBOCBase64   string               `json:"to_shard_account_boc_base64"`
	FirstTx                   debugMismatchFirstTx `json:"first_tx"`
}

type debugMismatchFirstTx struct {
	LT                  uint64 `json:"lt"`
	ExpectedTxHash      string `json:"expected_tx_hash"`
	GotTxHash           string `json:"got_tx_hash"`
	ExpectedTxBOCBase64 string `json:"expected_tx_boc_base64"`
	GotTxBOCBase64      string `json:"got_tx_boc_base64"`
	InMsgBOCBase64      string `json:"in_msg_boc_base64"`
	Config              struct {
		GlobalVersion                int      `json:"global_version"`
		Now                          uint32   `json:"now"`
		BlockLT                      int64    `json:"block_lt"`
		LogicalTime                  int64    `json:"logical_time"`
		RandSeedBase64               string   `json:"rand_seed_base64"`
		ConfigRootBOCBase64          string   `json:"config_root_boc_base64"`
		PrevBlocksStackBOCBase64     string   `json:"prev_blocks_stack_boc_base64"`
		UnpackedConfigStackBOCBase64 string   `json:"unpacked_config_stack_boc_base64"`
		PrecompiledGasStackBOCBase64 string   `json:"precompiled_gas_stack_boc_base64"`
		IncomingValueStackBOCBase64  string   `json:"incoming_value_stack_boc_base64"`
		StorageFees                  int64    `json:"storage_fees"`
		DuePaymentNano               string   `json:"due_payment_nano"`
		InMsgParamsStackBOCBase64    string   `json:"in_msg_params_stack_boc_base64"`
		LibrariesBOCBase64           []string `json:"libraries_boc_base64"`
	} `json:"config"`
}

func TestEmulateTransactionMismatchRegressionFixtures(t *testing.T) {
	report := debugMismatchReportFromFile(t, "testdata/tvm_replay_mismatch_regression.json")

	var bigAccountCases int
	for _, item := range report.Accounts {
		if debugMismatchIsBigMasterAccount(item) {
			bigAccountCases++
		}
		t.Run(fmt.Sprintf("%d/%s/%d", item.MasterSeqno, item.Address, item.FirstTx.LT), func(t *testing.T) {
			result := debugEmulateMismatch(t, item)
			expectedTx := debugMismatchCell(t, item.FirstTx.ExpectedTxBOCBase64)
			expectedAccountHash := debugMismatchExpectedAccountHash(t, expectedTx)

			if result == nil || result.TransactionCell == nil || testResultShardAccount(result) == nil {
				t.Fatal("emulation returned incomplete result")
			}
			if !bytes.Equal(result.TransactionCell.Hash(), expectedTx.Hash()) {
				t.Fatalf("transaction hash mismatch: got %x, want %x", result.TransactionCell.Hash(), expectedTx.Hash())
			}
			if !bytes.Equal(testResultShardAccount(result).Account.Hash(), expectedAccountHash) {
				t.Fatalf("account root hash mismatch: got %x, want %x", testResultShardAccount(result).Account.Hash(), expectedAccountHash)
			}
			if !bytes.Equal(testResultShardAccount(result).LastTransHash, expectedTx.Hash()) {
				t.Fatalf("last transaction hash mismatch: got %x, want %x", testResultShardAccount(result).LastTransHash, expectedTx.Hash())
			}
		})
	}
	if bigAccountCases == 0 {
		t.Fatalf("fixture has no %s cases", debugMismatchBigMasterAccount)
	}
}

func BenchmarkTVMReplayMismatchBigAccount(b *testing.B) {
	report := debugMismatchReportFromFile(b, "testdata/tvm_replay_mismatch_regression.json")

	prepared := make([]debugPreparedMismatchReplay, 0, len(report.Accounts))
	for _, item := range report.Accounts {
		if debugMismatchIsBigMasterAccount(item) {
			prepared = append(prepared, debugPrepareMismatchReplay(b, item))
		}
	}
	if len(prepared) == 0 {
		b.Fatalf("fixture has no %s cases", debugMismatchBigMasterAccount)
	}

	machine := NewTVM()
	b.ReportAllocs()
	b.ResetTimer()

	var txs int
	for i := 0; i < b.N; i++ {
		for _, replay := range prepared {
			result, err := testEmulateTransaction(machine, replay.from, replay.inMsg, replay.config)
			if err != nil {
				b.Fatal(err)
			}
			if result == nil || result.TransactionCell == nil || testResultShardAccount(result) == nil {
				b.Fatal("emulation returned incomplete result")
			}
			if !bytes.Equal(result.TransactionCell.Hash(), replay.txHash) {
				b.Fatalf("transaction hash mismatch: got %x, want %x", result.TransactionCell.Hash(), replay.txHash)
			}
			if !bytes.Equal(testResultShardAccount(result).Account.Hash(), replay.accountHash) {
				b.Fatalf("account root hash mismatch: got %x, want %x", testResultShardAccount(result).Account.Hash(), replay.accountHash)
			}
			txs++
		}
	}
	benchmarkMismatchReplayTransactions = txs
	b.ReportMetric(float64(txs)/float64(b.N), "tx/op")
}

func TestDebugReplayMismatch(t *testing.T) {
	path := os.Getenv("TVM_MISMATCH_JSON")
	if path == "" {
		t.Skip("set TVM_MISMATCH_JSON")
	}
	idx := 0
	if rawIdx := os.Getenv("TVM_MISMATCH_INDEX"); rawIdx != "" {
		parsed, err := strconv.Atoi(rawIdx)
		if err != nil {
			t.Fatal(err)
		}
		idx = parsed
	}

	report := debugMismatchReportFromFile(t, path)
	if idx >= len(report.Accounts) {
		t.Fatalf("sample %d is out of range", idx)
	}
	item := report.Accounts[idx]
	tx := item.FirstTx
	wantCell := debugMismatchCell(t, tx.ExpectedTxBOCBase64)
	gotCell := debugMismatchCell(t, tx.GotTxBOCBase64)

	var wantTx, gotTx tlb.Transaction
	if err := tlb.Parse(&wantTx, wantCell); err != nil {
		t.Fatal(err)
	}
	if err := tlb.Parse(&gotTx, gotCell); err != nil {
		t.Fatal(err)
	}

	t.Logf("case master=%d wc=%d account=%s lt=%d", item.MasterSeqno, item.Workchain, item.Address, tx.LT)
	t.Logf("hash want=%x got=%x", wantCell.Hash(), gotCell.Hash())
	t.Logf("repacked want=%x got=%x", wantCell.MustBeginParse().MustToCell().Hash(), gotCell.MustBeginParse().MustToCell().Hash())
	if packedWant, err := wantTx.ToCell(); err == nil {
		t.Logf("tlb packed want=%x", packedWant.Hash())
	}
	if packedGot, err := gotTx.ToCell(); err == nil {
		t.Logf("tlb packed got=%x", packedGot.Hash())
	}
	t.Logf("root refs: %s", debugCellRefsSummary(wantCell, gotCell))
	t.Logf("cell diff: %s", debugFirstCellDiff(wantCell, gotCell, "tx", 0))
	t.Logf("want: %s", debugTxSummary(&wantTx))
	t.Logf("got:  %s", debugTxSummary(&gotTx))
	t.Logf("want desc: %s", debugDescSummary(wantTx.Description))
	t.Logf("got desc:  %s", debugDescSummary(gotTx.Description))
	t.Logf("want io: %s", debugIOSummary(wantTx.IO.Out))
	t.Logf("got io:  %s", debugIOSummary(gotTx.IO.Out))
	t.Logf("want raw io: %s", debugRawIOSummary(wantCell))
	t.Logf("got raw io:  %s", debugRawIOSummary(gotCell))
	t.Logf("description equal=%v type want=%T got=%T", reflect.DeepEqual(wantTx.Description, gotTx.Description), wantTx.Description, gotTx.Description)
	t.Logf("total fees equal=%v state update equal=%v io equal=%v", reflect.DeepEqual(wantTx.TotalFees, gotTx.TotalFees), reflect.DeepEqual(wantTx.StateUpdate, gotTx.StateUpdate), reflect.DeepEqual(wantTx.IO, gotTx.IO))
	debugReplayMismatch(t, item)
}

func TestDebugReplayAllMismatches(t *testing.T) {
	path := os.Getenv("TVM_MISMATCH_ALL_JSON")
	if path == "" {
		t.Skip("set TVM_MISMATCH_ALL_JSON")
	}

	report := debugMismatchReportFromFile(t, path)
	for i, item := range report.Accounts {
		t.Run(fmt.Sprintf("%03d/%d/%s/%d", i, item.MasterSeqno, item.Address, item.FirstTx.LT), func(t *testing.T) {
			result := debugEmulateMismatch(t, item)
			expectedTx := debugMismatchCell(t, item.FirstTx.ExpectedTxBOCBase64)
			expectedAccountHash := debugMismatchExpectedAccountHash(t, expectedTx)

			if result == nil || result.TransactionCell == nil || testResultShardAccount(result) == nil {
				t.Fatal("emulation returned incomplete result")
			}
			if !bytes.Equal(result.TransactionCell.Hash(), expectedTx.Hash()) {
				t.Fatalf("transaction hash mismatch: got %x, want %x", result.TransactionCell.Hash(), expectedTx.Hash())
			}
			if !bytes.Equal(testResultShardAccount(result).Account.Hash(), expectedAccountHash) {
				t.Fatalf("account root hash mismatch: got %x, want %x", testResultShardAccount(result).Account.Hash(), expectedAccountHash)
			}
			if !bytes.Equal(testResultShardAccount(result).LastTransHash, expectedTx.Hash()) {
				t.Fatalf("last transaction hash mismatch: got %x, want %x", testResultShardAccount(result).LastTransHash, expectedTx.Hash())
			}
		})
	}
}

func debugMismatchReportFromFile(tb testing.TB, path string) debugMismatchReport {
	tb.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		tb.Fatal(err)
	}
	var report debugMismatchReport
	if err = json.Unmarshal(raw, &report); err != nil {
		tb.Fatal(err)
	}
	return report
}

func debugReplayMismatch(t *testing.T, item debugMismatchAccount) {
	t.Helper()

	tx := item.FirstTx
	inMsg := debugMismatchCell(t, tx.InMsgBOCBase64)
	expectedRawIn := debugRawInputCell(t, debugMismatchCell(t, tx.ExpectedTxBOCBase64))
	res := debugEmulateMismatch(t, item)
	replayCell := res.TransactionCell
	t.Logf("replay hash=%x want=%s equal=%v", replayCell.Hash(), tx.ExpectedTxHash, fmt.Sprintf("%x", replayCell.Hash()) == tx.ExpectedTxHash)
	t.Logf("replay diff: %s", debugFirstCellDiff(debugMismatchCell(t, tx.ExpectedTxBOCBase64), replayCell, "tx", 0))
	t.Logf("replay desc: %s", debugDescSummary(testResultTransaction(t, res).Description))
	t.Logf("replay io: %s", debugIOSummary(testResultTransaction(t, res).IO.Out))
	t.Logf("replay raw io: %s", debugRawIOSummary(replayCell))
	t.Logf("replay gas=%d action_phase=%s actions=%s", res.GasUsed, debugActionPhase(debugResultActionPhase(res)), debugActionsSummary(res.Actions))
	if expectedRawIn != nil && !bytes.Equal(inMsg.Hash(), expectedRawIn.Hash()) {
		resRaw := debugEmulateMismatchWithInput(t, item, expectedRawIn)
		rawCell := resRaw.TransactionCell
		t.Logf("raw-in replay hash=%x want=%s equal=%v", rawCell.Hash(), tx.ExpectedTxHash, fmt.Sprintf("%x", rawCell.Hash()) == tx.ExpectedTxHash)
	}
}

func debugEmulateMismatch(t testing.TB, item debugMismatchAccount) *TransactionExecutionResult {
	t.Helper()

	return debugEmulateMismatchWithInput(t, item, debugMismatchCell(t, item.FirstTx.InMsgBOCBase64))
}

func debugEmulateMismatchWithInput(t testing.TB, item debugMismatchAccount, inMsg *cell.Cell) *TransactionExecutionResult {
	t.Helper()

	tx := item.FirstTx
	from := debugMismatchShardAccount(t, item.FromShardAccountBOCBase64)
	cfg := debugMismatchEmulationConfig(t, tx)

	machine := NewTVM()
	res, err := testEmulateTransaction(machine, from, inMsg, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

type debugPreparedMismatchReplay struct {
	globalVersion int
	from          *tlb.ShardAccount
	inMsg         *cell.Cell
	config        testTxParams
	txHash        []byte
	accountHash   []byte
}

func debugPrepareMismatchReplay(tb testing.TB, item debugMismatchAccount) debugPreparedMismatchReplay {
	tb.Helper()

	expectedTx := debugMismatchCell(tb, item.FirstTx.ExpectedTxBOCBase64)

	return debugPreparedMismatchReplay{
		globalVersion: item.FirstTx.Config.GlobalVersion,
		from:          debugMismatchShardAccount(tb, item.FromShardAccountBOCBase64),
		inMsg:         debugMismatchCell(tb, item.FirstTx.InMsgBOCBase64),
		config:        debugMismatchEmulationConfig(tb, item.FirstTx),
		txHash:        append([]byte(nil), expectedTx.Hash()...),
		accountHash:   debugMismatchExpectedAccountHash(tb, expectedTx),
	}
}

func debugMismatchIsBigMasterAccount(item debugMismatchAccount) bool {
	return item.Address == debugMismatchBigMasterAccount
}

func debugMismatchExpectedAccountHash(tb testing.TB, expectedTx *cell.Cell) []byte {
	tb.Helper()

	var tx tlb.Transaction
	if err := tlb.Parse(&tx, expectedTx); err != nil {
		tb.Fatal(err)
	}
	return append([]byte(nil), tx.StateUpdate.NewHash...)
}

func debugMismatchEmulationConfig(t testing.TB, tx debugMismatchFirstTx) testTxParams {
	t.Helper()

	randSeed, err := base64.StdEncoding.DecodeString(tx.Config.RandSeedBase64)
	if err != nil {
		t.Fatal(err)
	}

	return testTxParams{
		Now:             tx.Config.Now,
		BlockLT:         tx.Config.BlockLT,
		LogicalTime:     tx.Config.LogicalTime,
		AccountRandSeed: randSeed,
		ConfigRoot:      debugMismatchCell(t, tx.Config.ConfigRootBOCBase64),
		PrevBlocks:      fatBlockTuple(t, tx.Config.PrevBlocksStackBOCBase64),
		Libraries:       fatBlockCells(t, tx.Config.LibrariesBOCBase64),
	}
}

func debugMismatchShardAccount(t testing.TB, encoded string) *tlb.ShardAccount {
	t.Helper()

	root := debugMismatchCell(t, encoded)
	var shard tlb.ShardAccount
	if err := tlb.Parse(&shard, root); err != nil {
		t.Fatal(err)
	}
	return &shard
}

func debugResultActionPhase(res *TransactionExecutionResult) *tlb.ActionPhase {
	if res == nil {
		return nil
	}
	tx, err := res.ParseTransaction()
	if err != nil {
		return nil
	}
	switch d := tx.Description.(type) {
	case tlb.TransactionDescriptionOrdinary:
		return d.ActionPhase
	case tlb.TransactionDescriptionTickTock:
		return d.ActionPhase
	default:
		return nil
	}
}

func debugActionsSummary(root *cell.Cell) string {
	if root == nil {
		return "nil"
	}
	loaded, err := transactionLoadActions(root, vmcore.MaxSupportedGlobalVersion)
	if err != nil {
		return err.Error()
	}
	out := fmt.Sprintf("hash=%x total=%d skipped=%d load_code=%d", root.Hash(), loaded.totalActions, loaded.skippedActions, loaded.resultCode)
	for i, action := range loaded.actions {
		out += fmt.Sprintf(" #%d skipped=%v type=%T", i, action.skipped, action.action)
		switch act := action.action.(type) {
		case tlb.ActionSendMsg:
			out += fmt.Sprintf(" mode=%d msg=%s relaxed=%s", act.Mode, debugMessageSummary(act.Msg), debugRelaxedActionMessageDetails(act.Msg))
		case tlb.ActionReserveCurrency:
			out += fmt.Sprintf(" mode=%d grams=%s", act.Mode, act.Currency.Coins.Nano())
		case tlb.ActionChangeLibrary:
			out += fmt.Sprintf(" mode=%d lib=%T", act.Mode, act.LibRef)
		case tlb.ActionSetCode:
			if act.NewCode != nil {
				out += fmt.Sprintf(" code=%x", act.NewCode.Hash())
			}
		}
	}
	return out
}

func debugRelaxedActionMessageDetails(root *cell.Cell) string {
	if root == nil {
		return "nil"
	}
	var msg tlb.MessageRelaxed
	if err := tlb.Parse(&msg, root); err != nil {
		return "parse=" + err.Error()
	}
	extra := "nil"
	if msg.Info.ExtraCurrencies != nil {
		extra = fmt.Sprintf("empty=%v cell=%s", msg.Info.ExtraCurrencies.IsEmpty(), debugCellShape(msg.Info.ExtraCurrencies.AsCell()))
		if items, err := msg.Info.ExtraCurrencies.LoadAll(); err != nil {
			extra += " load=" + err.Error()
		} else {
			extra += fmt.Sprintf(" items=%d", len(items))
		}
	}
	return fmt.Sprintf("type=%s ihr_disabled=%v bounce=%v bounced=%v src=%s dst=%s amount=%s extra_present=%v extra=%s extra_flags=%s fwd=%s created_lt=%d created_at=%d init=%v init_ref=%s body_in_ref=%v body=%s",
		msg.MsgType, msg.Info.IHRDisabled, msg.Info.Bounce, msg.Info.Bounced, msg.Info.SrcAddr, msg.Info.DstAddr,
		transactionBigOrZero(msg.Info.Amount), msg.Info.ExtraPresent, extra, transactionBigOrZero(msg.Info.ExtraFlags),
		transactionBigOrZero(msg.Info.FwdFee), msg.Info.CreatedLT, msg.Info.CreatedAt, msg.Init.Exists, debugCellShape(msg.Init.Ref), msg.Body.InRef, debugCellShape(msg.Body.Ref))
}

func debugMessageSummary(root *cell.Cell) string {
	if root == nil {
		return "nil"
	}
	rootSummary := fmt.Sprintf("hash=%x bits=%d refs=%d", root.Hash(), root.BitsSize(), root.RefsNum())
	var msg tlb.Message
	if err := tlb.Parse(&msg, root); err != nil {
		var relaxed tlb.MessageRelaxed
		if relaxedErr := tlb.Parse(&relaxed, root); relaxedErr != nil {
			return fmt.Sprintf("%s parse=%v relaxed=%v", rootSummary, err, relaxedErr)
		}
		return fmt.Sprintf("%s parse=%v relaxed=%s", rootSummary, err, debugRelaxedMessageSummary(&relaxed))
	}
	switch msg.MsgType {
	case tlb.MsgTypeInternal:
		in := msg.AsInternal()
		return fmt.Sprintf("%s internal src=%s dst=%s amount=%s ihr=%s fwd=%s init=%v body=%s", rootSummary, in.SrcAddr, in.DstAddr, in.Amount.Nano(), in.IHRFee.Nano(), in.FwdFee.Nano(), in.StateInit != nil, debugCellShape(in.Body))
	case tlb.MsgTypeExternalOut:
		out := msg.AsExternalOut()
		return fmt.Sprintf("%s external src=%s dst=%s init=%v body=%s", rootSummary, out.SrcAddr, out.DstAddr, out.StateInit != nil, debugCellShape(out.Body))
	default:
		return fmt.Sprintf("%s type=%s", rootSummary, msg.MsgType)
	}
}

func debugIOSummary(list *tlb.MessagesList) string {
	if list == nil || list.List == nil {
		return "nil"
	}
	kvs, err := list.List.LoadAll()
	if err != nil {
		return "load=" + err.Error()
	}
	out := fmt.Sprintf("messages=%d", len(kvs))
	for i, kv := range kvs {
		msgSlice, err := kv.Value.LoadRef()
		if err != nil {
			out += fmt.Sprintf(" #%d load_ref=%v", i, err)
			continue
		}
		msgCell := msgSlice.MustToCell()
		out += fmt.Sprintf(" #%d key=%x %s", i, kv.Key.MustToCell().Hash(), debugMessageSummary(msgCell))
	}
	return out
}

func debugRawIOSummary(txCell *cell.Cell) string {
	if txCell == nil {
		return "nil"
	}
	txSlice := txCell.MustBeginParse()
	ioCell, err := txSlice.LoadRefCell()
	if err != nil {
		return "load_io=" + err.Error()
	}
	io := ioCell.MustBeginParse()
	hasIn, err := io.LoadBoolBit()
	if err != nil {
		return "load_in=" + err.Error()
	}
	inCell := (*cell.Cell)(nil)
	if hasIn {
		inCell, err = io.LoadRefCell()
		if err != nil {
			return "load_in_ref=" + err.Error()
		}
	}
	dict, err := io.LoadDict(15)
	if err != nil {
		return "load_dict=" + err.Error()
	}
	dictCell := (*cell.Cell)(nil)
	if dict != nil && !dict.IsEmpty() {
		dictCell = dict.AsCell()
	}
	return fmt.Sprintf("io=%s in=%s dict=%s", debugCellShape(ioCell), debugMessageSummary(inCell), debugCellShape(dictCell))
}

func debugRawInputCell(t *testing.T, txCell *cell.Cell) *cell.Cell {
	t.Helper()

	txSlice := txCell.MustBeginParse()
	ioCell, err := txSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	io := ioCell.MustBeginParse()
	hasIn, err := io.LoadBoolBit()
	if err != nil {
		t.Fatal(err)
	}
	if !hasIn {
		return nil
	}
	in, err := io.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func debugRelaxedMessageSummary(msg *tlb.MessageRelaxed) string {
	if msg == nil {
		return "nil"
	}
	info := msg.Info
	return fmt.Sprintf("type=%s src=%s dst=%s amount=%s extra=%v extra_flags=%s fwd=%s created_lt=%d created_at=%d init={exists=%v in_ref=%v ref=%s} body={in_ref=%v ref=%s}",
		msg.MsgType, info.SrcAddr, info.DstAddr, transactionBigOrZero(info.Amount), info.ExtraPresent, transactionBigOrZero(info.ExtraFlags), transactionBigOrZero(info.FwdFee), info.CreatedLT, info.CreatedAt,
		msg.Init.Exists, msg.Init.InRef, debugCellHash(msg.Init.Ref), msg.Body.InRef, debugCellHash(msg.Body.Ref))
}

func debugCellHash(root *cell.Cell) string {
	if root == nil {
		return "nil"
	}
	return fmt.Sprintf("%x", root.Hash())
}

func debugCellShape(root *cell.Cell) string {
	if root == nil {
		return "nil"
	}
	return fmt.Sprintf("hash=%x bits=%d refs=%d", root.Hash(), root.BitsSize(), root.RefsNum())
}

func debugCellRefsSummary(want, got *cell.Cell) string {
	wantSlice := want.MustBeginParse()
	gotSlice := got.MustBeginParse()
	out := fmt.Sprintf("root want={level=%d mask=%v depth=%d h0=%x h=%x refs=%d} got={level=%d mask=%v depth=%d h0=%x h=%x refs=%d}",
		want.Level(), want.LevelMask(), want.Depth(), want.Hash(0), want.Hash(), wantSlice.RefsNum(),
		got.Level(), got.LevelMask(), got.Depth(), got.Hash(0), got.Hash(), gotSlice.RefsNum())
	refCnt := wantSlice.RefsNum()
	if gotSlice.RefsNum() < refCnt {
		refCnt = gotSlice.RefsNum()
	}
	for i := 0; i < refCnt; i++ {
		w, _ := wantSlice.LoadRefCell()
		g, _ := gotSlice.LoadRefCell()
		out += fmt.Sprintf(" ref%d want={level=%d mask=%v depth=%d h0=%x h=%x} got={level=%d mask=%v depth=%d h0=%x h=%x}",
			i, w.Level(), w.LevelMask(), w.Depth(), w.Hash(0), w.Hash(),
			g.Level(), g.LevelMask(), g.Depth(), g.Hash(0), g.Hash())
	}
	return out
}

func debugMismatchCell(t testing.TB, encoded string) *cell.Cell {
	t.Helper()

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	root, err := cell.FromBOC(raw)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func debugTxSummary(tx *tlb.Transaction) string {
	return fmt.Sprintf("out=%d orig=%v end=%v fees=%s old=%x new=%x desc=%#v", tx.OutMsgCount, tx.OrigStatus, tx.EndStatus, tx.TotalFees.Coins.Nano().String(), tx.StateUpdate.OldHash, tx.StateUpdate.NewHash, tx.Description)
}

func debugDescSummary(desc any) string {
	switch d := desc.(type) {
	case tlb.TransactionDescriptionOrdinary:
		return fmt.Sprintf("ordinary credit_first=%v aborted=%v destroyed=%v storage=%s compute=%s action=%s bounce=%#v", d.CreditFirst, d.Aborted, d.Destroyed, debugStoragePhase(d.StoragePhase), debugComputePhase(d.ComputePhase), debugActionPhase(d.ActionPhase), d.BouncePhase)
	case tlb.TransactionDescriptionTickTock:
		return fmt.Sprintf("ticktock tock=%v aborted=%v destroyed=%v storage=%s compute=%s action=%s", d.IsTock, d.Aborted, d.Destroyed, debugStoragePhase(&d.StoragePhase), debugComputePhase(d.ComputePhase), debugActionPhase(d.ActionPhase))
	default:
		return fmt.Sprintf("%#v", desc)
	}
}

func debugStoragePhase(phase *tlb.StoragePhase) string {
	if phase == nil {
		return "nil"
	}
	due := "nil"
	if phase.StorageFeesDue != nil {
		due = phase.StorageFeesDue.Nano().String()
	}
	return fmt.Sprintf("{fees=%s due=%s status=%v}", phase.StorageFeesCollected.Nano().String(), due, phase.StatusChange)
}

func debugComputePhase(phase tlb.ComputePhase) string {
	switch p := phase.Phase.(type) {
	case tlb.ComputePhaseVM:
		return fmt.Sprintf("{vm success=%v msg_state=%v activated=%v gas_fees=%s gas_used=%s gas_limit=%s gas_credit=%v mode=%d exit=%d arg=%v steps=%d init=%x final=%x}",
			p.Success, p.MsgStateUsed, p.AccountActivated, p.GasFees.Nano().String(), p.Details.GasUsed, p.Details.GasLimit, p.Details.GasCredit, p.Details.Mode, p.Details.ExitCode, p.Details.ExitArg, p.Details.VMSteps, p.Details.VMInitStateHash, p.Details.VMFinalStateHash)
	case tlb.ComputePhaseSkipped:
		return fmt.Sprintf("{skipped reason=%v}", p.Reason)
	default:
		return fmt.Sprintf("%#v", phase.Phase)
	}
}

func debugActionPhase(phase *tlb.ActionPhase) string {
	if phase == nil {
		return "nil"
	}
	fwd := "nil"
	if phase.TotalFwdFees != nil {
		fwd = phase.TotalFwdFees.Nano().String()
	}
	act := "nil"
	if phase.TotalActionFees != nil {
		act = phase.TotalActionFees.Nano().String()
	}
	return fmt.Sprintf("{success=%v valid=%v no_funds=%v status=%v fwd=%s action=%s result=%d arg=%v total=%d spec=%d skipped=%d created=%d list=%x cells=%s bits=%s}",
		phase.Success, phase.Valid, phase.NoFunds, phase.StatusChange, fwd, act, phase.ResultCode, phase.ResultArg, phase.TotalActions, phase.SpecActions, phase.SkippedActions, phase.MessagesCreated, phase.ActionListHash, phase.TotalMsgSize.Cells, phase.TotalMsgSize.Bits)
}

func debugFirstCellDiff(want, got *cell.Cell, path string, depth int) string {
	if want == nil || got == nil {
		if want == got {
			return ""
		}
		return fmt.Sprintf("%s nil mismatch", path)
	}
	if debugCellMetaEqual(want, got) {
		return ""
	}
	if depth >= 16 {
		return fmt.Sprintf("%s hash want=%x got=%x", path, want.Hash(), got.Hash())
	}

	wantSlice := want.MustBeginParse()
	gotSlice := got.MustBeginParse()
	wantBits, wantData, err := wantSlice.Copy().RestBits()
	if err != nil {
		return fmt.Sprintf("%s want rest bits: %v", path, err)
	}
	gotBits, gotData, err := gotSlice.Copy().RestBits()
	if err != nil {
		return fmt.Sprintf("%s got rest bits: %v", path, err)
	}
	if wantBits != gotBits || !bytes.Equal(wantData, gotData) {
		return fmt.Sprintf("%s bits want=%d:%x got=%d:%x", path, wantBits, wantData, gotBits, gotData)
	}
	if wantSlice.RefsNum() != gotSlice.RefsNum() {
		return fmt.Sprintf("%s refs want=%d got=%d", path, wantSlice.RefsNum(), gotSlice.RefsNum())
	}
	refCnt := wantSlice.RefsNum()
	for i := 0; i < refCnt; i++ {
		wantRef, err := wantSlice.LoadRefCell()
		if err != nil {
			return fmt.Sprintf("%s want ref %d: %v", path, i, err)
		}
		gotRef, err := gotSlice.LoadRefCell()
		if err != nil {
			return fmt.Sprintf("%s got ref %d: %v", path, i, err)
		}
		if diff := debugFirstCellDiff(wantRef, gotRef, fmt.Sprintf("%s.%d", path, i), depth+1); diff != "" {
			return diff
		}
	}
	if want.LevelMask() != got.LevelMask() || want.Depth() != got.Depth() || want.IsSpecial() != got.IsSpecial() || want.GetType() != got.GetType() {
		return fmt.Sprintf("%s meta want={level=%d mask=%v depth=%d special=%v type=%v h0=%x h=%x} got={level=%d mask=%v depth=%d special=%v type=%v h0=%x h=%x}",
			path, want.Level(), want.LevelMask(), want.Depth(), want.IsSpecial(), want.GetType(), want.Hash(0), want.Hash(),
			got.Level(), got.LevelMask(), got.Depth(), got.IsSpecial(), got.GetType(), got.Hash(0), got.Hash())
	}
	for level := 0; level <= 3; level++ {
		if !bytes.Equal(want.Hash(level), got.Hash(level)) || want.Depth(level) != got.Depth(level) {
			return fmt.Sprintf("%s level %d want={depth=%d hash=%x} got={depth=%d hash=%x}", path, level, want.Depth(level), want.Hash(level), got.Depth(level), got.Hash(level))
		}
	}
	return fmt.Sprintf("%s unknown hash want=%x got=%x", path, want.Hash(), got.Hash())
}

func debugCellMetaEqual(a, b *cell.Cell) bool {
	if a.LevelMask() != b.LevelMask() || a.Depth() != b.Depth() || a.IsSpecial() != b.IsSpecial() || a.GetType() != b.GetType() {
		return false
	}
	for level := 0; level <= 3; level++ {
		if !bytes.Equal(a.Hash(level), b.Hash(level)) || a.Depth(level) != b.Depth(level) {
			return false
		}
	}
	return true
}
