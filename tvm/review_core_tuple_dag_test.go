package tvm

import (
	"fmt"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

// Each DUP; TUPLE 2 adds one distinct tuple with two references to the
// preceding tuple. Its logical tree has 2^depth leaves, but the VM creates
// only depth+1 distinct tuple objects. Keep the final stack scalar so result
// serialization does not itself expand the tuple tree.
func reviewCoreTupleDAGCode(depth int, flow string) *cell.Cell {
	dag := cell.BeginCell().
		MustStoreUInt(0x89, 8).MustStoreRef(cell.BeginCell().MustStoreUInt(0xAB, 8).EndCell()).
		MustStoreUInt(0x6F01, 16) // PUSHREFSLICE; TUPLE 1
	for range depth {
		dag.MustStoreUInt(0x206F02, 24) // DUP; TUPLE 2
	}

	switch flow {
	case "local":
		return dag.MustStoreUInt(0x3070, 16).EndCell() // DROP; PUSHINT 0
	case "input":
		child := cell.BeginCell().MustStoreUInt(0x30, 8).EndCell()
		return dag.MustStoreUInt(0x7189, 16).MustStoreRef(child).
			MustStoreUInt(0xDB4000, 24).EndCell() // PUSHINT 1; PUSHREFSLICE; RUNVM 0
	case "output":
		return cell.BeginCell().MustStoreUInt(0x7089, 16).MustStoreRef(dag.EndCell()).
			MustStoreUInt(0xDB4000, 24).
			MustStoreUInt(0x303070, 24).EndCell() // DROP exit; DROP tuple; PUSHINT 0
	default:
		panic("unknown tuple DAG flow")
	}
}

func TestReviewCoreTupleDAGChildBoundaries(t *testing.T) {
	for _, flow := range []string{"local", "input", "output"} {
		t.Run(flow, func(t *testing.T) {
			for _, depth := range []int{0, 1, 8} {
				t.Run(fmt.Sprintf("depth_%d", depth), func(t *testing.T) {
					_, res, err := runRawCode(reviewCoreTupleDAGCode(depth, flow))
					if err != nil || res.ExitCode != 0 {
						t.Fatalf("execution: result=%+v err=%v", res, err)
					}
					baseGas := map[string]int64{"local": 186, "input": 383, "output": 419}[flow]
					if wantGas := baseGas + int64(depth)*46; res.GasUsed != wantGas {
						t.Fatalf("gas = %d, want %d", res.GasUsed, wantGas)
					}
					if res.Stack.Len() != 1 {
						t.Fatalf("stack depth = %d, want 1", res.Stack.Len())
					}
					got, err := res.Stack.PopIntFinite()
					if err != nil || got.Sign() != 0 {
						t.Fatalf("result = %v, err = %v, want zero", got, err)
					}
				})
			}
		})
	}
}

// RUNVM trace rebinding currently expands shared tuple DAGs recursively. This
// benchmark bounds the witness while retaining its growth curve for a future
// fix: moving a tuple between VMs should scale with distinct tuples, not leaves.
func BenchmarkReviewCoreTupleDAGChildBoundaries(b *testing.B) {
	for _, flow := range []string{"local", "input", "output"} {
		for _, depth := range []int{4, 8, 12, 16} {
			b.Run(fmt.Sprintf("%s/depth_%d", flow, depth), func(b *testing.B) {
				code := reviewCoreTupleDAGCode(depth, flow)
				machine := NewTVM()
				cfg := testExecutionConfig(b)
				data := cell.BeginCell().EndCell()
				b.ReportAllocs()
				var gas int64
				for b.Loop() {
					res, err := machine.Execute(code, data, tuple.Tuple{}, vm.GasWithLimit(10_000), vm.NewStack(), cfg)
					if err != nil || res.ExitCode != 0 {
						b.Fatalf("execution: result=%+v err=%v", res, err)
					}
					gas = res.GasUsed
				}
				b.ReportMetric(float64(gas), "gas/op")
			})
		}
	}
}
