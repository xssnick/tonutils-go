package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	opcellslice "github.com/xssnick/tonutils-go/tvm/op/cellslice"
	opfuncs "github.com/xssnick/tonutils-go/tvm/op/funcs"
	opstack "github.com/xssnick/tonutils-go/tvm/op/stack"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

// TestTransactionCellLoadObserverProvesReadsWithoutARecordingTrace is the claim
// the load observer exists for, stated end to end: a subtree that reaches the
// machine with no recording trace on it is still proven, because the machine
// reports what it loads.
//
// The fixture splits the account in two. Its code keeps the trace, because the
// code root is converted outside gas accounting and so is never reported as a
// load — the traversal record is the only thing that can cover it, and without
// it there would be no executable proof to compare. Its data arrives with no
// trace at all, which makes the data subtree invisible to the record: it is in
// the proof because the observer put it there, or it is not in the proof.
//
// The control run at the end is the whole point. It repeats the transaction with
// the observer removed and requires the data to fall out of the proof and the
// replay to stop reproducing the transaction. Without that half, a proof that
// happened to contain the data would prove nothing about where the record came
// from.
func TestTransactionCellLoadObserverProvesReadsWithoutARecordingTrace(t *testing.T) {
	fixture := newCellLoadObserverFixture(t)

	observed := runCellLoadObserverTransaction(t, fixture, true)
	if !observed.result.Accepted {
		t.Fatal("fixture transaction was not accepted, so it never ran the code under test")
	}

	// Both cells the machine loaded out of the trace-less data subtree are in the
	// proof: the data root itself, and the one branch the program reads.
	provenAccount := cellLoadObserverProvenAccount(t, fixture, observed.proof)
	if provenAccount.StateInit.Data.GetType() == cell.PrunedCellType {
		t.Fatal("data root is pruned in a proof whose only source of data reads is the load observer")
	}
	if loaded := mustPeekRef(t, provenAccount.StateInit.Data, 0); loaded.GetType() == cell.PrunedCellType {
		t.Fatal("loaded data branch is pruned in a proof built from the load observer")
	}
	// And nothing else is: the observer reports reads, not the whole tree, so a
	// branch the program never touches must still prune away. A record that kept
	// everything would satisfy the checks above while proving nothing.
	if unused := mustPeekRef(t, provenAccount.StateInit.Data, 1); unused.GetType() != cell.PrunedCellType {
		t.Fatal("untouched data branch survived into the proof, so the record is not the read set")
	}

	// The proof is complete in the only sense that matters: the same transaction
	// replays on it and produces the identical transaction cell.
	replayed := replayCellLoadObserverTransaction(t, fixture, observed.proof)
	if replayed.err != nil {
		t.Fatalf("replay on the observer-built proof failed: %v", replayed.err)
	}
	if replayed.hash != observed.result.TransactionCell.HashKey() {
		t.Fatalf("replay on the observer-built proof produced transaction %x, want %x",
			replayed.hash, observed.result.TransactionCell.HashKey())
	}

	// Control: the identical run with no observer. The machine still has the
	// whole account, so the transaction is the same one — but the record now sees
	// only what the trace reached, and the data subtree was never on it.
	unobserved := runCellLoadObserverTransaction(t, fixture, false)
	if !unobserved.result.Accepted ||
		unobserved.result.TransactionCell.HashKey() != observed.result.TransactionCell.HashKey() {
		t.Fatal("control run produced another transaction, so the two proofs are not comparable")
	}
	controlAccount := cellLoadObserverProvenAccount(t, fixture, unobserved.proof)
	if controlAccount.StateInit.Code.GetType() == cell.PrunedCellType {
		t.Fatal("account code is pruned without the observer, so the control differs by more than the data subtree")
	}
	if controlAccount.StateInit.Data.GetType() != cell.PrunedCellType {
		t.Fatal("account data survived a proof built without the load observer; " +
			"the fixture is leaking a recording trace into the data and proves nothing")
	}
	controlReplay := replayCellLoadObserverTransaction(t, fixture, unobserved.proof)
	if controlReplay.err == nil && controlReplay.hash == observed.result.TransactionCell.HashKey() {
		t.Fatal("replay on the proof built without the observer reproduced the transaction anyway")
	}
}

// cellLoadObserverFixture is an account whose data subtree is reachable only
// through the machine.
type cellLoadObserverFixture struct {
	addr *address.Address
	// accountRoot is the untouched source tree the read set is opened over.
	accountRoot *cell.Cell
	code        *cell.Cell
	data        *cell.Cell
	usage       transactionUsage
	// storageStat is the account's storage-stat dictionary. It is bound to every
	// run so the emulator answers state-size questions from the dictionary
	// instead of walking the account tree, which a proof with pruned branches
	// cannot serve — the same reason a collator ships one per account.
	storageStat *cell.Cell
	message     *PreparedMessage
}

func newCellLoadObserverFixture(t *testing.T) cellLoadObserverFixture {
	t.Helper()

	addr := address.MustParseRawAddr("0:" + "2b63f898590aa9e300fd0e696bc834b9ebe3ab75ec16dbc2a90955d960cc6a85")
	// Five drops clear the compute-phase stack, ACCEPT pays for an external
	// message, and the tail reads one branch of c4 so the data subtree is load
	// bearing rather than decoration.
	codeTail := codeFromBuilders(t,
		opstack.DROP().Serialize(),
		opstack.DROP().Serialize(),
		opstack.DROP().Serialize(),
		opstack.DROP().Serialize(),
		opstack.DROP().Serialize(),
		opfuncs.ACCEPT().Serialize(),
		opstack.PUSHCTR(4).Serialize(),
		opcellslice.CTOS().Serialize(),
		opcellslice.LDREFRTOS().Serialize(),
		opcellslice.LDU(8).Serialize(),
	)
	code := cell.BeginCell().MustStoreRef(codeTail).EndCell()
	data := cell.BeginCell().
		MustStoreRef(cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()).
		MustStoreRef(executionProofUnusedDataBranch()).
		EndCell()

	usage, storageStat, err := transactionComputeAccountStorageStat(storageStatProofAccountStorage(t, code, data), 0)
	if err != nil {
		t.Fatal(err)
	}

	messageCell, err := tlb.ToCell(&tlb.ExternalMessage{
		DstAddr: addr,
		Body:    cell.BeginCell().EndCell(),
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := PrepareMessage(messageCell)
	if err != nil {
		t.Fatal(err)
	}

	return cellLoadObserverFixture{
		addr:        addr,
		accountRoot: storageStatProofAccount(t, addr, code, data, usage, storageStat),
		code:        code,
		data:        data,
		usage:       usage,
		storageStat: storageStat,
		message:     message,
	}
}

type cellLoadObserverRun struct {
	proof  *cell.Cell
	result *TransactionExecutionResult
}

// runCellLoadObserverTransaction runs the fixture transaction against a read set
// opened over the account root, with the observer wired in or left out.
//
// The account the machine gets is assembled from the recorded code cell and the
// bare data cell. A trace is a wrapper and not part of the hash, so this is the
// same account by every measure the proof cares about — it just arrives at the
// machine half traced, which is the split the observer exists to cover.
func runCellLoadObserverTransaction(t *testing.T, fixture cellLoadObserverFixture, observe bool) cellLoadObserverRun {
	t.Helper()

	read := cell.NewReadSet(fixture.accountRoot)
	var traced tlb.AccountState
	if err := tlb.LoadFromCell(&traced, read.Root().MustBeginParse()); err != nil {
		t.Fatalf("decode account through the recording trace: %v", err)
	}
	// The traced decode takes code and data as references without opening them,
	// so the record holds the account root alone at this point. If that stopped
	// being true the test would pass for the wrong reason.
	if _, recorded := read.Contains(fixture.accountRoot.HashKey()); !recorded {
		t.Fatal("traced account decode did not record the account root")
	}
	if _, recorded := read.Contains(fixture.data.HashKey()); recorded {
		t.Fatal("traced account decode already recorded the data root, leaving nothing for the observer to prove")
	}

	machineRoot := storageStatProofAccount(t, fixture.addr,
		fixture.code.WithTrace(read.Trace()), fixture.data, fixture.usage, fixture.storageStat)
	if machineRoot.HashKey() != fixture.accountRoot.HashKey() {
		t.Fatal("half-traced account is a different account")
	}
	account, err := PrepareAccount(&tlb.ShardAccount{
		Account:       machineRoot,
		LastTransHash: make([]byte, 32),
	}, fixture.addr)
	if err != nil {
		t.Fatal(err)
	}
	if account.State().StateInit.Code.Trace() == nil {
		t.Fatal("fixture lost the code trace, so the control run has nothing to keep the code in the proof")
	}
	if account.State().StateInit.Data.Trace() != nil {
		t.Fatal("fixture handed the machine a data cell that still carries a recording trace")
	}

	opts := TransactionOptions{
		LogicalTime: transactionTestLogicalTime,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    walletSendTestGasMax,
			Credit: walletSendTestCredit,
		}),
	}
	if observe {
		opts.OnCellLoad = read.RecordUnbilled
	}
	result, err := cellLoadObserverEmulate(t, account, fixture.storageStat, fixture.message, opts)
	if err != nil {
		t.Fatalf("emulate fixture transaction: %v", err)
	}

	proof, err := read.Proof()
	if err != nil {
		t.Fatalf("build proof from the read set: %v", err)
	}
	return cellLoadObserverRun{proof: proof, result: result}
}

type cellLoadObserverReplay struct {
	hash cell.Hash
	err  error
}

// replayCellLoadObserverTransaction reruns the transaction on the virtualized
// proof, which is what a validator does with the cells a candidate ships. A
// proof that covers the reads reproduces the transaction; one that does not
// walks into a pruned branch instead.
func replayCellLoadObserverTransaction(t *testing.T, fixture cellLoadObserverFixture, proof *cell.Cell) cellLoadObserverReplay {
	t.Helper()

	virtual, err := cell.UnwrapProofVirtualized(proof, fixture.accountRoot.Hash())
	if err != nil {
		return cellLoadObserverReplay{err: err}
	}
	account, err := PrepareAccount(&tlb.ShardAccount{
		Account:       virtual,
		LastTransHash: make([]byte, 32),
	}, fixture.addr)
	if err != nil {
		return cellLoadObserverReplay{err: err}
	}
	result, err := cellLoadObserverEmulate(t, account, fixture.storageStat, fixture.message, TransactionOptions{
		LogicalTime: transactionTestLogicalTime,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    walletSendTestGasMax,
			Credit: walletSendTestCredit,
		}),
	})
	if err != nil {
		return cellLoadObserverReplay{err: err}
	}
	if !result.Accepted {
		return cellLoadObserverReplay{err: errCellLoadObserverReplayRejected}
	}
	return cellLoadObserverReplay{hash: result.TransactionCell.HashKey()}
}

var errCellLoadObserverReplayRejected = cellLoadObserverError("replayed external message was not accepted")

type cellLoadObserverError string

func (e cellLoadObserverError) Error() string { return string(e) }

// cellLoadObserverEmulate runs one transaction in a block context of its own.
// The storage stat is bound per context, so a shared one would carry a binding
// made for another account object into the next run.
func cellLoadObserverEmulate(
	t *testing.T,
	account *PreparedAccount,
	storageStat *cell.Cell,
	message *PreparedMessage,
	opts TransactionOptions,
) (*TransactionExecutionResult, error) {
	t.Helper()

	block, err := transactionTestConfigWithGlobalVersion(t, uint32(vmcore.MaxSupportedGlobalVersion)).NewBlockContext(BlockOptions{
		Now:      uint32(tonopsTestTime.Unix()),
		BlockLT:  transactionTestLogicalTime,
		RandSeed: append([]byte(nil), tonopsTestSeed...),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = block.BindAccountStorageStat(account, storageStat); err != nil {
		return nil, err
	}
	return NewTVM().EmulateTransaction(block, account, message, opts)
}

func cellLoadObserverProvenAccount(t *testing.T, fixture cellLoadObserverFixture, proof *cell.Cell) tlb.AccountState {
	t.Helper()

	body, err := cell.UnwrapProof(proof, fixture.accountRoot.Hash())
	if err != nil {
		t.Fatalf("unwrap account proof: %v", err)
	}
	var account tlb.AccountState
	if err = tlb.LoadFromCell(&account, body.MustBeginParse()); err != nil {
		t.Fatalf("decode proven account: %v", err)
	}
	if account.StateInit == nil || account.StateInit.Code == nil || account.StateInit.Data == nil {
		t.Fatal("proven account lost its state init")
	}
	return account
}

func mustPeekRef(t *testing.T, c *cell.Cell, index int) *cell.Cell {
	t.Helper()

	ref, err := c.PeekRef(index)
	if err != nil {
		t.Fatalf("peek ref %d: %v", index, err)
	}
	return ref
}
