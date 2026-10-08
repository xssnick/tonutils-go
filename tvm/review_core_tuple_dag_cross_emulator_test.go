//go:build cgo && tvm_cross_emulator

package tvm

import (
	"fmt"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/tuple"
	"github.com/xssnick/tonutils-go/tvm/vm"
)

func TestReviewCoreTupleDAGCrossEmulator(t *testing.T) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		t.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, flow := range []string{"local", "input", "output"} {
		for _, depth := range []int{0, 1, 8, 12} {
			t.Run(fmt.Sprintf("%s/depth_%d", flow, depth), func(t *testing.T) {
				runContVersionedParityCase(t, reviewCoreTupleDAGCode(depth, flow), nil, 13, 0, "", nil)
			})
		}
	}
}

func BenchmarkReviewCoreTupleDAGReference(b *testing.B) {
	if _, err := os.Stat("vm/cross-emulate-test/lib/libemulator.dylib"); err != nil {
		b.Skipf("reference emulator library is unavailable: %v", err)
	}

	for _, flow := range []string{"local", "input", "output"} {
		for _, depth := range []int{4, 8, 12, 16} {
			b.Run(fmt.Sprintf("%s/depth_%d", flow, depth), func(b *testing.B) {
				code := prependRawMethodDrop(reviewCoreTupleDAGCode(depth, flow))
				data := cell.BeginCell().EndCell()
				stack := vm.NewStack()
				var gas int64
				for b.Loop() {
					res, err := runReferenceCrossCode(code, data, tuple.Tuple{}, stack)
					if err != nil || res.exitCode != 0 {
						b.Fatalf("execution: result=%+v err=%v", res, err)
					}
					gas = res.gasUsed
				}
				b.ReportMetric(float64(gas), "gas/op")
			})
		}
	}
}
