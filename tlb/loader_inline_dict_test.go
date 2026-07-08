package tlb

import (
	"math/big"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// TestInlineDictRefusesEmpty documents that `dict inline N` correctly REFUSES
// to serialize empty dictionaries: the inline form maps to TLB Hashmap (non-E)
// whose hm_edge constructor always carries a node — only HashmapE has the
// hme_empty variant. This regularly surprised users during config
// serialization, so the error must state the reason clearly.
func TestInlineDictRefusesEmpty(t *testing.T) {
	type inlineDictHolder struct {
		Dict *cell.Dictionary `tlb:"dict inline 32"`
	}

	_, err := ToCell(inlineDictHolder{Dict: cell.NewDict(32)})
	if err == nil {
		t.Fatal("expected empty inline dict serialization to fail")
	}
	if !strings.Contains(err.Error(), "cannot be empty") ||
		!strings.Contains(err.Error(), "no empty representation") {
		t.Fatalf("error must explain the TLB reason, got: %v", err)
	}

	// nil dict must fail the same way
	if _, err = ToCell(inlineDictHolder{}); err == nil {
		t.Fatal("expected nil inline dict serialization to fail")
	}

	// non-empty inline dict round-trips
	d := cell.NewDict(32)
	if err = d.SetIntKey(big.NewInt(7), cell.BeginCell().MustStoreUInt(1, 8).EndCell()); err != nil {
		t.Fatal(err)
	}
	c, err := ToCell(inlineDictHolder{Dict: d})
	if err != nil {
		t.Fatalf("non-empty inline dict must serialize: %v", err)
	}

	var parsed inlineDictHolder
	if err = LoadFromCell(&parsed, c.MustBeginParse()); err != nil {
		t.Fatalf("failed to load inline dict: %v", err)
	}
	v, err := parsed.Dict.LoadValueByIntKey(big.NewInt(7))
	if err != nil {
		t.Fatal(err)
	}
	if got := v.MustLoadUInt(8); got != 1 {
		t.Fatalf("inline dict value mismatch: %d", got)
	}
}
