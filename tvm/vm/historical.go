package vm

import "fmt"

// GasSchedule selects gas rules for historical global-version-zero executions.
// The profile names identify rule sets, not their activation dates.
type GasSchedule uint8

const (
	// GasScheduleModern preserves the default gas rules.
	GasScheduleModern GasSchedule = iota
	// GasSchedule2019 charges every cell load at full price and leaves implicit
	// jumps and returns uncharged.
	GasSchedule2019
	// GasScheduleEarly2020 discounts repeated cell loads and leaves implicit
	// jumps and returns uncharged.
	GasScheduleEarly2020
)

// HistoricalConfig selects rules that GlobalVersion alone cannot identify.
// Callers must supply explicit historical execution context; activation is
// never inferred from timestamps. The zero value preserves current behavior.
type HistoricalConfig struct {
	GasSchedule GasSchedule
	// PopC3Cell enables the temporary POP c3 auto-BLESS hook. It applies only
	// to the fixed POP c3 instruction at global version zero; other setters stay strict.
	PopC3Cell bool
	// NoPRNG disables RANDU256, RAND, SETRAND and ADDRAND at global version zero.
	NoPRNG bool
	// NoBLKDROP2 disables BLKDROP2 at global version zero.
	NoBLKDROP2 bool
	// NaNComparison makes binary comparisons return a finite left operand when
	// the right operand is NaN, including quiet comparisons. Requires version 0–3.
	NaNComparison bool
}

// Validate checks whether the historical rules apply to globalVersion.
func (h HistoricalConfig) Validate(globalVersion int) error {
	if h.GasSchedule > GasScheduleEarly2020 {
		return fmt.Errorf("unknown historical gas schedule: %d", h.GasSchedule)
	}
	if globalVersion != 0 && (h.GasSchedule != GasScheduleModern || h.PopC3Cell || h.NoPRNG || h.NoBLKDROP2) {
		return fmt.Errorf("historical configuration requires global version 0, got %d", globalVersion)
	}
	if h.NaNComparison && (globalVersion < 0 || globalVersion > 3) {
		return fmt.Errorf("historical NaN comparison requires global version 0 through 3, got %d", globalVersion)
	}

	return nil
}
