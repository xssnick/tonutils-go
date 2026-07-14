//go:build cgo && tvm_cross_emulator

package tvm

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func assertCrossSkippedGoStack(t *testing.T, got *cell.Cell, want []any) {
	t.Helper()

	if want == nil {
		return
	}

	wantStack, err := buildCrossStack(want...)
	if err != nil {
		t.Fatalf("failed to build expected go stack: %v", err)
	}
	wantStackCell, err := stackToCell(wantStack)
	if err != nil {
		t.Fatalf("failed to serialize expected go stack: %v", err)
	}
	gotStackCell, err := normalizeStackCell(got)
	if err != nil {
		t.Fatalf("failed to normalize go stack: %v", err)
	}
	wantStackCell, err = normalizeStackCell(wantStackCell)
	if err != nil {
		t.Fatalf("failed to normalize expected go stack: %v", err)
	}
	if !bytes.Equal(gotStackCell.Hash(), wantStackCell.Hash()) {
		t.Fatalf("go stack mismatch:\ngo=%s\nwant=%s", gotStackCell.Dump(), wantStackCell.Dump())
	}
}
