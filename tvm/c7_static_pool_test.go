package tvm

import (
	"math/big"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	funcsop "github.com/xssnick/tonutils-go/tvm/op/funcs"
	mathop "github.com/xssnick/tonutils-go/tvm/op/math"
	stackop "github.com/xssnick/tonutils-go/tvm/op/stack"
	tupleop "github.com/xssnick/tonutils-go/tvm/op/tuple"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	vmcore "github.com/xssnick/tonutils-go/tvm/vm"
)

// The pooled range exported through vm.StaticInt (vm/stack.go stackStaticIntMin/Max).
const (
	c7StaticPoolMin = -5
	c7StaticPoolMax = 10
)

// snapshotStaticIntPool records the pooled instances and their values so a test
// can prove none of them was mutated.
func snapshotStaticIntPool(t *testing.T) map[int64]*big.Int {
	t.Helper()
	pool := map[int64]*big.Int{}
	for v := int64(c7StaticPoolMin); v <= c7StaticPoolMax; v++ {
		shared := vmcore.StaticInt(v)
		if shared == nil {
			t.Fatalf("vm.StaticInt(%d) = nil, want a pooled instance", v)
		}
		if shared.Int64() != v {
			t.Fatalf("pool is already corrupted before the test: StaticInt(%d) = %s", v, shared)
		}
		pool[v] = shared
	}
	return pool
}

func assertStaticIntPoolIntact(t *testing.T, pool map[int64]*big.Int, stage string) {
	t.Helper()
	for v, shared := range pool {
		if got := vmcore.StaticInt(v); got != shared {
			t.Fatalf("%s: vm.StaticInt(%d) instance was replaced", stage, v)
		}
		if shared.Int64() != v {
			t.Fatalf("%s: shared VM integer %d was mutated in place, now %s", stage, v, shared)
		}
	}
}

// plantedInMsgParams is an in_msg_params tuple built entirely out of pooled
// instances. Planting them explicitly keeps the test honest even if the c7
// builders (messageTupleBool/messageTupleInt/transactionSharedBigOrZero) stop
// using the pool: the invariant under test is "a shared instance reachable from
// c7 cannot be mutated", not "the builders happen to share".
func plantedInMsgParams(t *testing.T) tuple.Tuple {
	t.Helper()
	vals := make([]any, 10)
	for i := range vals {
		v := int64(i - 1) // -1, 0, 1, 2 ... 8: all inside the pooled range
		shared := vmcore.StaticInt(v)
		if shared == nil {
			t.Fatalf("vm.StaticInt(%d) = nil", v)
		}
		vals[i] = shared
	}
	return tuple.NewTupleValue(vals...)
}

// TestC7SharedIntsSurviveHostReads covers the direct host path: Go code inside
// the VM and the transaction runtime reads c7 slots through State.GetParam /
// Tuple.Index and then mutates the result in place (getRandSeed, and everything
// downstream of sendMsgTupleAmount, do exactly this shape of read).
//
// It FAILS if tuple.Tuple.Index stops cloning *big.Int leaves.
func TestC7SharedIntsSurviveHostReads(t *testing.T) {
	pool := snapshotStaticIntPool(t)

	params := plantedInMsgParams(t)
	c7 := tuple.NewTupleValue(tuple.NewTupleValue(
		vmcore.StaticInt(0),
		vmcore.StaticInt(-1),
		vmcore.StaticInt(7),
		params,
	))
	st := vmcore.NewExecutionState(vmcore.MaxSupportedGlobalVersion, vmcore.NewGas(), nil, c7, vmcore.NewStack())

	// flat slots
	for idx := 0; idx < 3; idx++ {
		v, err := st.GetParam(idx)
		if err != nil {
			t.Fatalf("GetParam(%d): %v", idx, err)
		}
		got, ok := v.(*big.Int)
		if !ok {
			t.Fatalf("GetParam(%d) = %T, want *big.Int", idx, v)
		}
		got.Add(got, big.NewInt(1_000_003))
		got.Neg(got)
		got.SetInt64(-424242)
	}

	// nested tuple slot, read the way sendMsgTupleAmount reads the balance
	nestedAny, err := st.GetParam(3)
	if err != nil {
		t.Fatalf("GetParam(3): %v", err)
	}
	nested, ok := nestedAny.(tuple.Tuple)
	if !ok {
		t.Fatalf("GetParam(3) = %T, want tuple.Tuple", nestedAny)
	}
	for i := 0; i < nested.Len(); i++ {
		leafAny, err := nested.Index(i)
		if err != nil {
			t.Fatalf("nested.Index(%d): %v", i, err)
		}
		leaf, ok := leafAny.(*big.Int)
		if !ok {
			t.Fatalf("nested.Index(%d) = %T, want *big.Int", i, leafAny)
		}
		leaf.Mul(leaf, big.NewInt(7))
		leaf.Sub(leaf, big.NewInt(11))
	}

	// GetGlobal takes the same Index path over the outer tuple
	globalAny, err := st.GetGlobal(0)
	if err != nil {
		t.Fatalf("GetGlobal(0): %v", err)
	}
	if _, ok := globalAny.(tuple.Tuple); !ok {
		t.Fatalf("GetGlobal(0) = %T, want tuple.Tuple", globalAny)
	}

	assertStaticIntPoolIntact(t, pool, "host reads")

	// c7 itself must still hold the original values
	for idx, want := range map[int]int64{0: 0, 1: -1, 2: 7} {
		v, err := st.GetParam(idx)
		if err != nil {
			t.Fatalf("GetParam(%d) after mutation: %v", idx, err)
		}
		if got := v.(*big.Int); got.Int64() != want {
			t.Fatalf("c7[%d] = %s after host mutation, want %d", idx, got, want)
		}
	}
}

// TestC7SharedIntsSurviveOpcodeReads covers the opcode path: INMSGPARAMS/INDEX
// push a c7 leaf onto the stack, then ADDINT and NEGATE mutate their popped
// operand in place (tvm/op/math/addconst.go x.Add(x, arg),
// tvm/op/math/negate.go).
//
// It FAILS if Stack.PopInt stops copying pooled instances: canonicalStackInt
// maps 0/-1/1 back onto the pool on every push, so those three constants sit on
// the stack as shared instances no matter what the host handed in, and PUSHINT
// -5..10 pushes the pooled instance directly (see PushSmallInt).
func TestC7SharedIntsSurviveOpcodeReads(t *testing.T) {
	pool := snapshotStaticIntPool(t)

	params := plantedInMsgParams(t)

	var builders []*cell.Builder
	for i := 0; i < params.Len(); i++ {
		builders = append(builders,
			funcsop.INMSGPARAMS().Serialize(),
			tupleop.INDEX(uint8(i)).Serialize(),
			mathop.ADDCONST(11).Serialize(),
			mathop.NEGATE().Serialize(),
			stackop.DROP().Serialize(),
		)
	}
	code := codeFromBuilders(t, builders...)

	body := cell.BeginCell().MustStoreUInt(0xCAFE, 16).EndCell()
	res, err := NewTVM().EmulateInternalMessage(code, cell.BeginCell().EndCell(), body, internalMessageTestAmount, EmulateInternalMessageConfig{
		Address:     tonopsTestAddr,
		Now:         uint32(tonopsTestTime.Unix()),
		Balance:     new(big.Int).Set(tonopsTestBalance),
		RandSeed:    append([]byte(nil), tonopsTestSeed...),
		Config:      transactionTestConfigWithGlobalVersion(t, uint32(vmcore.MaxSupportedGlobalVersion)),
		InMsgParams: params,
		Gas: vmcore.NewGas(vmcore.GasConfig{
			Max:    DefaultInternalMessageGasMax,
			Limit:  int64(internalMessageTestAmount) * InternalMessageGasAmountFactor,
			Credit: 0,
		}),
	})
	if err != nil {
		t.Fatalf("emulate internal failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("unexpected exit code %d", res.ExitCode)
	}

	assertStaticIntPoolIntact(t, pool, "opcode reads")

	// PUSHINT -5..10 pushes the pooled instance directly (PushSmallInt), so the
	// same mutate-the-operand shape must also leave the pool intact when the
	// value never came from c7 at all.
	var pushBuilders []*cell.Builder
	for v := int64(c7StaticPoolMin); v <= c7StaticPoolMax; v++ {
		pushBuilders = append(pushBuilders,
			stackop.PUSHINT(big.NewInt(v)).Serialize(),
			mathop.ADDCONST(11).Serialize(),
			mathop.NEGATE().Serialize(),
			stackop.DROP().Serialize(),
		)
	}
	if _, pushRes, err := runRawCodeWithGas(codeFromBuilders(t, pushBuilders...), 1_000_000); err != nil {
		t.Fatalf("PUSHINT mutation run failed: %v", err)
	} else if pushRes.ExitCode != 0 {
		t.Fatalf("PUSHINT mutation run exit code %d", pushRes.ExitCode)
	}
	assertStaticIntPoolIntact(t, pool, "PUSHINT operands")

	// the planted tuple must be unchanged too
	for i := 0; i < params.Len(); i++ {
		leaf, err := params.RawIndex(i)
		if err != nil {
			t.Fatalf("params.RawIndex(%d): %v", i, err)
		}
		if got := leaf.(*big.Int).Int64(); got != int64(i-1) {
			t.Fatalf("planted in_msg_params[%d] = %d, wanted %d", i, got, i-1)
		}
	}
}

// TestC7TupleBuildersShareStaticInts pins the premise of the optimization: the
// c7 tuple builders hand out pooled instances rather than fresh allocations.
// Without this the safety tests above would still pass, but vacuously.
func TestC7TupleBuildersShareStaticInts(t *testing.T) {
	pool := snapshotStaticIntPool(t)

	if got := messageTupleBool(true); got != pool[-1] {
		t.Fatalf("messageTupleBool(true) = %s (%p), want the shared -1 (%p)", got, got, pool[-1])
	}
	if got := messageTupleBool(false); got != pool[0] {
		t.Fatalf("messageTupleBool(false) = %s (%p), want the shared 0 (%p)", got, got, pool[0])
	}
	for v := int64(c7StaticPoolMin); v <= c7StaticPoolMax; v++ {
		if got := messageTupleInt(v); got != pool[v] {
			t.Fatalf("messageTupleInt(%d) is not the shared instance", v)
		}
		if v < 0 {
			continue
		}
		if got := messageTupleUint(uint64(v)); got != pool[v] {
			t.Fatalf("messageTupleUint(%d) is not the shared instance", v)
		}
		if got := transactionSharedBigOrZero(big.NewInt(v)); got != pool[v] {
			t.Fatalf("transactionSharedBigOrZero(%d) is not the shared instance", v)
		}
	}
	if got := transactionSharedBigOrZero(nil); got != pool[0] {
		t.Fatalf("transactionSharedBigOrZero(nil) is not the shared 0")
	}

	// out-of-range values must still be freshly allocated and owned
	big11 := messageTupleInt(c7StaticPoolMax + 1)
	big11.Add(big11, big.NewInt(1))
	uint11 := messageTupleUint(uint64(c7StaticPoolMax) + 1)
	uint11.Add(uint11, big.NewInt(1))
	shared11 := transactionSharedBigOrZero(big.NewInt(c7StaticPoolMax + 1))
	shared11.Add(shared11, big.NewInt(1))

	// transactionBigOrZero must keep allocating: its results are mutated in
	// place by transactionCurrencyBalance.copy callers.
	owned := transactionBigOrZero(nil)
	if owned == pool[0] {
		t.Fatal("transactionBigOrZero(nil) returned the shared 0; its callers mutate the result in place")
	}
	owned.Add(owned, big.NewInt(5))
	ownedFromValue := transactionBigOrZero(big.NewInt(3))
	if ownedFromValue == pool[3] {
		t.Fatal("transactionBigOrZero(3) returned the shared 3; its callers mutate the result in place")
	}
	ownedFromValue.Sub(ownedFromValue, big.NewInt(9))

	assertStaticIntPoolIntact(t, pool, "builders")
}
