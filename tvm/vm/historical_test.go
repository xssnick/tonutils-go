package vm

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

func TestHistoricalConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  HistoricalConfig
		version int
		wantErr bool
	}{
		{name: "default zero", version: 0},
		{name: "default modern", version: MaxSupportedGlobalVersion},
		{name: "default unspecified", version: -1},
		{name: "2019", config: HistoricalConfig{GasSchedule: GasSchedule2019}},
		{name: "early 2020", config: HistoricalConfig{GasSchedule: GasScheduleEarly2020}},
		{name: "pop c3 cell", config: HistoricalConfig{PopC3Cell: true}},
		{name: "NaN comparison v0", config: HistoricalConfig{NaNComparison: true}, version: 0},
		{name: "NaN comparison v1", config: HistoricalConfig{NaNComparison: true}, version: 1},
		{name: "NaN comparison v2", config: HistoricalConfig{NaNComparison: true}, version: 2},
		{name: "NaN comparison v3", config: HistoricalConfig{NaNComparison: true}, version: 3},
		{name: "NaN comparison v4", config: HistoricalConfig{NaNComparison: true}, version: 4, wantErr: true},
		{name: "NaN comparison unspecified", config: HistoricalConfig{NaNComparison: true}, version: -1, wantErr: true},
		{name: "NaN comparison with v0 gas", config: HistoricalConfig{NaNComparison: true, GasSchedule: GasSchedule2019}, version: 1, wantErr: true},
		{name: "NaN comparison with v0 POP", config: HistoricalConfig{NaNComparison: true, PopC3Cell: true}, version: 2, wantErr: true},
		{name: "NaN comparison with v0 PRNG", config: HistoricalConfig{NaNComparison: true, NoPRNG: true}, version: 3, wantErr: true},
		{name: "NaN comparison with v0 BLKDROP2", config: HistoricalConfig{NaNComparison: true, NoBLKDROP2: true}, version: 3, wantErr: true},
		{name: "no PRNG", config: HistoricalConfig{NoPRNG: true}, version: 0},
		{name: "no BLKDROP2", config: HistoricalConfig{NoBLKDROP2: true}, version: 0},
		{name: "no PRNG at nonzero version", config: HistoricalConfig{NoPRNG: true}, version: 1, wantErr: true},
		{name: "no BLKDROP2 at nonzero version", config: HistoricalConfig{NoBLKDROP2: true}, version: 1, wantErr: true},
		{name: "combined", config: HistoricalConfig{GasSchedule: GasSchedule2019, PopC3Cell: true}},
		{name: "unknown gas", config: HistoricalConfig{GasSchedule: GasSchedule(255)}, wantErr: true},
		{name: "2019 at nonzero version", config: HistoricalConfig{GasSchedule: GasSchedule2019}, version: 1, wantErr: true},
		{name: "early 2020 at nonzero version", config: HistoricalConfig{GasSchedule: GasScheduleEarly2020}, version: 1, wantErr: true},
		{name: "pop c3 cell at nonzero version", config: HistoricalConfig{PopC3Cell: true}, version: 1, wantErr: true},
		{name: "unspecified historical version", config: HistoricalConfig{GasSchedule: GasSchedule2019}, version: -1, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.config.Validate(test.version); (err != nil) != test.wantErr {
				t.Fatalf("Validate(%d) = %v, want error %v", test.version, err, test.wantErr)
			}
		})
	}
}

func TestHistoricalCellLoadGas(t *testing.T) {
	tests := []struct {
		name      string
		schedule  GasSchedule
		version   int
		reloadGas int64
	}{
		{name: "default version zero", version: 0, reloadGas: 25},
		{name: "default current version", version: MaxSupportedGlobalVersion, reloadGas: 25},
		{name: "2019", schedule: GasSchedule2019, reloadGas: 100},
		{name: "early 2020", schedule: GasScheduleEarly2020, reloadGas: 25},
		{name: "2019 does not affect other versions", schedule: GasSchedule2019, version: 1, reloadGas: 25},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, method := range []string{"cell", "hash key"} {
				t.Run(method, func(t *testing.T) {
					state := NewExecutionState(test.version, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
					state.Historical.GasSchedule = test.schedule
					state.InitForExecution()
					observed := map[cell.Hash]int{}
					state.OnCellLoad = func(cl *cell.Cell) { observed[cl.HashKey()]++ }

					first := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
					sameHash := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
					second := cell.BeginCell().MustStoreUInt(2, 8).EndCell()
					for _, cl := range []*cell.Cell{first, sameHash, second} {
						var err error
						if method == "cell" {
							err = state.Cells.RegisterCellLoad(cl)
						} else {
							err = state.Cells.RegisterCellLoadKey(cl.HashKey())
						}
						if err != nil {
							t.Fatal(err)
						}
					}

					if got, want := state.Gas.Used(), 200+test.reloadGas; got != want {
						t.Fatalf("gas used = %d, want %d", got, want)
					}
					if !state.Cells.IsCellLoaded(first) || !state.Cells.IsCellLoaded(second) {
						t.Fatal("loaded cells were not tracked")
					}
					if method == "cell" {
						if observed[first.HashKey()] != 1 || observed[second.HashKey()] != 1 {
							t.Fatalf("observer counts = %v, want one per cell", observed)
						}
					} else if len(observed) != 0 {
						t.Fatal("hash-only loads must not notify the cell observer")
					}
				})
			}
		})
	}
}

func TestHistoricalCellLoadOutOfGas(t *testing.T) {
	for _, method := range []string{"cell", "hash key", "trace"} {
		t.Run(method, func(t *testing.T) {
			state := NewExecutionState(0, GasWithLimit(199), nil, tuple.Tuple{}, NewStack())
			state.Historical.GasSchedule = GasSchedule2019
			state.InitForExecution()
			cl := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
			var observed int
			state.OnCellLoad = func(*cell.Cell) { observed++ }

			for i := 0; i < 2; i++ {
				var err error
				switch method {
				case "cell":
					err = state.Cells.RegisterCellLoad(cl)
				case "hash key":
					err = state.Cells.RegisterCellLoadKey(cl.HashKey())
				case "trace":
					_, err = cl.WithTrace(state.Cells.Trace()).BeginParse()
				}
				if err != nil {
					t.Fatalf("global version zero should defer the gas check: %v", err)
				}
				if i == 0 {
					if err = state.CheckGas(); err != nil {
						t.Fatalf("first load exhausted gas: %v", err)
					}
				}
			}

			if got := state.Gas.Used(); got != 200 {
				t.Fatalf("gas used = %d, want 200", got)
			}
			assertVMErrorCode(t, state.CheckGas(), vmerr.CodeOutOfGas)
			if !state.Cells.IsCellLoaded(cl) {
				t.Fatal("out-of-gas cell load was not tracked")
			}
			if method != "hash key" && observed != 1 {
				t.Fatalf("observer calls = %d, want 1", observed)
			}
		})
	}
}

func TestHistoricalCellTraceGas(t *testing.T) {
	tests := []struct {
		name     string
		schedule GasSchedule
		wantGas  int64
	}{
		{name: "2019", schedule: GasSchedule2019, wantGas: 400},
		{name: "early 2020", schedule: GasScheduleEarly2020, wantGas: 250},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := NewExecutionState(0, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
			state.Historical.GasSchedule = test.schedule
			state.InitForExecution()
			observed := map[cell.Hash]int{}
			state.OnCellLoad = func(cl *cell.Cell) { observed[cl.HashKey()]++ }
			child := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
			root := cell.BeginCell().MustStoreRef(child).EndCell()

			for i := 0; i < 2; i++ {
				sl, err := state.Cells.BeginParse(root)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = state.Cells.LoadRef(sl); err != nil {
					t.Fatal(err)
				}
			}
			if got := state.Gas.Used(); got != test.wantGas {
				t.Fatalf("gas used = %d, want %d", got, test.wantGas)
			}
			if observed[root.HashKey()] != 1 || observed[child.HashKey()] != 1 {
				t.Fatalf("observer counts = %v, want one per cell", observed)
			}
		})
	}
}

func TestRunChildVMInheritsHistoricalConfig(t *testing.T) {
	tests := []struct {
		name      string
		schedule  GasSchedule
		reloadGas int64
	}{
		{name: "modern", reloadGas: 25},
		{name: "2019", schedule: GasSchedule2019, reloadGas: 100},
		{name: "early 2020", schedule: GasScheduleEarly2020, reloadGas: 25},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, isolation := range []struct {
				name    string
				isolate bool
			}{
				{name: "shared loads"},
				{name: "isolated loads", isolate: true},
			} {
				t.Run(isolation.name, func(t *testing.T) {
					parent := NewExecutionState(0, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
					parent.Historical = HistoricalConfig{GasSchedule: test.schedule, PopC3Cell: true, NoPRNG: true, NoBLKDROP2: true, NaNComparison: true}
					parent.InitForExecution()
					first := cell.BeginCell().MustStoreUInt(1, 8).EndCell()
					second := cell.BeginCell().MustStoreUInt(2, 8).EndCell()
					if err := parent.Cells.RegisterCellLoad(first); err != nil {
						t.Fatal(err)
					}

					wantGas := 100 + 2*test.reloadGas
					if isolation.isolate {
						wantGas = 200 + test.reloadGas
					}
					parent.SetChildRunner(func(child *State) (int64, error) {
						if child.Historical != parent.Historical || child.GlobalVersion != 0 {
							t.Fatalf("child historical context = %+v, version %d", child.Historical, child.GlobalVersion)
						}
						for _, cl := range []*cell.Cell{first, second, second} {
							if err := child.Cells.RegisterCellLoad(cl); err != nil {
								return 0, err
							}
						}
						if got := child.Gas.Used(); got != wantGas {
							t.Fatalf("child gas used = %d, want %d", got, wantGas)
						}
						return 0, nil
					})
					if err := parent.RunChildVM(ChildVMConfig{
						Code:       cell.BeginCell().EndCell().MustBeginParse(),
						Gas:        GasWithLimit(500),
						IsolateGas: isolation.isolate,
						ReturnGas:  true,
					}); err != nil {
						t.Fatal(err)
					}

					if got := parent.Gas.Used(); got != 100+wantGas {
						t.Fatalf("parent gas used = %d, want %d", got, 100+wantGas)
					}
					if got := mustPopInt64(t, parent.Stack); got != wantGas {
						t.Fatalf("returned child gas = %d, want %d", got, wantGas)
					}
					if got := parent.Cells.IsCellLoaded(second); got == isolation.isolate {
						t.Fatalf("parent tracks child load = %v, isolation = %v", got, isolation.isolate)
					}
				})
			}
		})
	}
}

func TestRunChildInheritsHistoricalConfig(t *testing.T) {
	parent := NewExecutionState(0, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
	parent.Historical = HistoricalConfig{GasSchedule: GasSchedule2019, PopC3Cell: true, NoPRNG: true, NoBLKDROP2: true, NaNComparison: true}
	parent.SetChildRunner(func(child *State) (int64, error) {
		if child.Historical != parent.Historical || child.GlobalVersion != 0 {
			t.Fatalf("child historical context = %+v, version %d", child.Historical, child.GlobalVersion)
		}
		return 0, nil
	})
	child := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
	if _, err := parent.RunChild(child); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalNaNComparisonChildInheritance(t *testing.T) {
	for _, mode := range []string{"RunChild", "RunChildVM", "isolated RunChildVM"} {
		t.Run(mode, func(t *testing.T) {
			parent := NewExecutionState(3, GasWithLimit(1_000), nil, tuple.Tuple{}, NewStack())
			parent.Historical.NaNComparison = true
			var called bool
			parent.SetChildRunner(func(child *State) (int64, error) {
				called = true
				if child.GlobalVersion != 3 || child.Historical != parent.Historical {
					t.Fatalf("child context: version=%d, historical=%+v", child.GlobalVersion, child.Historical)
				}
				return 0, nil
			})
			var err error
			if mode == "RunChild" {
				child := NewExecutionState(MaxSupportedGlobalVersion, GasWithLimit(500), nil, tuple.Tuple{}, NewStack())
				_, err = parent.RunChild(child)
			} else {
				err = parent.RunChildVM(ChildVMConfig{
					Code:       cell.BeginCell().EndCell().MustBeginParse(),
					Gas:        GasWithLimit(500),
					IsolateGas: mode == "isolated RunChildVM",
				})
			}
			if err != nil || !called {
				t.Fatalf("child runner called=%t, err=%v", called, err)
			}
		})
	}
}
