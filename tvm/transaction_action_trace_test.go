package tvm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTransactionLoadActionsTracesReusedList(t *testing.T) {
	actions := buildTransactionActionList(t,
		tlb.ActionSetCode{NewCode: cell.BeginCell().MustStoreUInt(0xa1, 8).EndCell()},
		tlb.ActionSetCode{NewCode: cell.BeginCell().MustStoreUInt(0xa2, 8).EndCell()},
	)
	discarded := cell.BeginCell().MustStoreRef(
		cell.BeginCell().MustStoreUInt(0xdd, 8).EndCell(),
	).EndCell()
	predecessor := cell.BeginCell().MustStoreRef(actions).MustStoreRef(discarded).EndCell()

	read := cell.NewReadSet(predecessor)
	predecessorSlice := read.Root().MustBeginParse()
	tracedActions, err := predecessorSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := transactionLoadActions(tracedActions, 14)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.resultCode != 0 || loaded.totalActions != 2 {
		t.Fatalf("loaded actions = %+v", loaded)
	}

	proof, err := read.Proof()
	if err != nil {
		t.Fatal(err)
	}
	proven, err := cell.UnwrapProof(proof, predecessor.Hash())
	if err != nil {
		t.Fatal(err)
	}
	provenSlice := proven.MustBeginParse()
	list, err := provenSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if list.GetType() == cell.PrunedCellType {
			t.Fatalf("action node %d is pruned", i)
		}
		list, err = list.MustBeginParse().LoadRefCell()
		if err != nil {
			t.Fatalf("load previous action node %d: %v", i, err)
		}
	}
	if list.GetType() == cell.PrunedCellType || list.BitsSize() != 0 || list.RefsNum() != 0 {
		t.Fatalf("action-list terminator is not materialized: type=%v bits=%d refs=%d", list.GetType(), list.BitsSize(), list.RefsNum())
	}

	provenDiscarded, err := provenSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if provenDiscarded.GetType() != cell.PrunedCellType {
		t.Fatal("unrelated predecessor subtree is materialized")
	}
}

func TestTransactionLoadActionsMaterializesLazyListNodesBeforeSpecialCheck(t *testing.T) {
	actions := make([]any, 30)
	for i := range actions {
		actions[i] = tlb.ActionSetCode{
			NewCode: cell.BeginCell().MustStoreUInt(uint64(i), 8).EndCell(),
		}
	}
	root := buildTransactionActionList(t, actions...)
	lazyRoot, err := cell.FromBOCWithOptions(
		root.ToBOCWithOptions(cell.BOCSerializeOptions{WithIndex: true}),
		cell.BOCParseOptions{Lazy: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := transactionLoadActions(lazyRoot, 14)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.resultCode != 0 || loaded.totalActions != 30 || len(loaded.actions) != 30 {
		t.Fatalf("loaded lazy actions = code %d arg %v total %d entries %d", loaded.resultCode, loaded.resultArg, loaded.totalActions, len(loaded.actions))
	}

	exotic, err := root.CreateProof(cell.CreateProofSkeleton())
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = transactionLoadActions(exotic, 14)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.resultCode != 32 || loaded.resultArg != nil {
		t.Fatalf("exotic action root = code %d arg %v, want code 32 without arg", loaded.resultCode, loaded.resultArg)
	}
}

func TestTransactionMessageStatsTracesReusedOutboundSubtree(t *testing.T) {
	deep := cell.BeginCell().MustStoreUInt(0x3a, 8).EndCell()
	reused := cell.BeginCell().MustStoreRef(
		cell.BeginCell().MustStoreRef(deep).EndCell(),
	).EndCell()
	discarded := cell.BeginCell().MustStoreRef(
		cell.BeginCell().MustStoreUInt(0xac, 8).EndCell(),
	).EndCell()
	predecessor := cell.BeginCell().MustStoreRef(reused).MustStoreRef(discarded).EndCell()

	read := cell.NewReadSet(predecessor)
	predecessorSlice := read.Root().MustBeginParse()
	tracedReused, err := predecessorSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}

	// Message normalization builds a fresh root while retaining StateInit and
	// body references. Size accounting must traverse those predecessor refs.
	normalized := cell.BeginCell().MustStoreRef(tracedReused).EndCell()
	if _, err = transactionMessageStats(normalized); err != nil {
		t.Fatal(err)
	}

	proof, err := read.Proof()
	if err != nil {
		t.Fatal(err)
	}
	proven, err := cell.UnwrapProof(proof, predecessor.Hash())
	if err != nil {
		t.Fatal(err)
	}
	provenSlice := proven.MustBeginParse()
	provenReused, err := provenSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	levelOne, err := provenReused.MustBeginParse().LoadRefCell()
	if err != nil {
		t.Fatalf("load reused outbound subtree: %v", err)
	}
	if _, err = levelOne.MustBeginParse().LoadRefCell(); err != nil {
		t.Fatalf("load deep reused outbound cell: %v", err)
	}

	provenDiscarded, err := provenSlice.LoadRefCell()
	if err != nil {
		t.Fatal(err)
	}
	if provenDiscarded.GetType() != cell.PrunedCellType {
		t.Fatal("unrelated predecessor subtree is materialized")
	}
}
