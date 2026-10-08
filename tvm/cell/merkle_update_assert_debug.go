//go:build cellassert

package cell

import "fmt"

// assertUntracedApplyParent enforces PreparedMerkleUpdate.ApplyTo's documented
// parent contract under the cellassert build tag.
//
// The contract: from must carry no Trace unless the caller means the walk's
// reads to be recorded into it. Reads recorded during an apply widen a ReadSet,
// and a ReadSet is what selects the cells a collated proof ships — so a trace
// that arrives here by accident changes produced block bytes, silently and only
// on the node that made the mistake. Every call site in this repository and in
// gton passes WithoutTrace.
//
// Why it is not on by default: it would be a behaviour change to a public API
// that documents the traced parent as the caller's choice, and ApplyTo cannot
// tell an intended trace from an accidental one. Off by default it is an
// assertion; on by default it would be a new rejection. Run the package with
// -tags cellassert to have it checked.
func assertUntracedApplyParent(from *Cell) error {
	if from != nil && from.Trace() != nil {
		return fmt.Errorf("merkle update apply: parent carries a trace; pass WithoutTrace")
	}
	return nil
}
