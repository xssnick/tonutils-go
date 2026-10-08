//go:build !cellassert

package cell

// assertUntracedApplyParent is the release form of the ApplyTo parent contract:
// nothing. See merkle_update_assert_debug.go for what the check is and why it
// is not on by default.
//
// It compiles to nothing at all — an empty function returning a nil interface
// value is inlined away — so the contract costs the hot path zero.
func assertUntracedApplyParent(*Cell) error { return nil }
