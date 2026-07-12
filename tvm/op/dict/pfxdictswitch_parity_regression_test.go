package dict

import (
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestPFXDICTSWITCHDeserializeDoesNotLoadRoot(t *testing.T) {
	root := cell.BeginCell().MustStoreUInt(0, 2).EndCell()
	code := PFXDICTSWITCH(root, 1).Serialize().EndCell()

	loads := 0
	trace := cell.NewTrace(cell.TraceHooks{
		OnLoad: func(*cell.Cell) {
			loads++
		},
	})
	codeSlice, err := code.BeginParseWithTrace(trace)
	if err != nil {
		t.Fatalf("parse code: %v", err)
	}
	loads = 0

	if err = PFXDICTSWITCH(nil).Deserialize(codeSlice); err != nil {
		t.Fatalf("deserialize PFXDICTSWITCH: %v", err)
	}
	if loads != 0 {
		t.Fatalf("deserialize loaded dictionary root %d time(s), want 0", loads)
	}
}
