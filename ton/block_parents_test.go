package ton

import (
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
)

// TestGetParentBlocksRefusesPrunedPrev ensures parent extraction fails loudly
// when the prev-block references were pruned away in the proof the header was
// parsed from, instead of returning zero-hash parent ids.
func TestGetParentBlocksRefusesPrunedPrev(t *testing.T) {
	var header tlb.BlockHeader
	header.PrevRef.Pruned = true

	if _, err := GetParentBlocks(&header); err == nil {
		t.Fatal("expected pruned prev refs to be rejected")
	} else if !strings.Contains(err.Error(), "pruned") {
		t.Fatalf("expected a pruned-specific error, got: %v", err)
	}
}
