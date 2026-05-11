package main

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestMainContractCodeIsLoaded(t *testing.T) {
	if MainContractCode == nil {
		t.Fatal("MainContractCode is nil")
	}
	if MainContractCode.BitsSize() == 0 {
		t.Fatal("MainContractCode should not be empty")
	}
}

func TestPrepareC7RejectsInvalidSeedLength(t *testing.T) {
	addr := address.MustParseAddr("EQAYqo4u7VF0fa4DPAebk4g9lBytj2VFny7pzXR0trjtXQaO")

	if _, err := PrepareC7(addr, time.Unix(123, 0), make([]byte, 31), big.NewInt(1), nil, MainContractCode); err == nil {
		t.Fatal("PrepareC7 should reject non-32-byte seeds")
	}
}

func TestPrepareC7Layouts(t *testing.T) {
	addr := address.MustParseAddr("EQAYqo4u7VF0fa4DPAebk4g9lBytj2VFny7pzXR0trjtXQaO")
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}

	t.Run("WithoutConfig", func(t *testing.T) {
		out, err := PrepareC7(addr, time.Unix(456, 0), seed, big.NewInt(77), nil, MainContractCode)
		if err != nil {
			t.Fatalf("PrepareC7 failed: %v", err)
		}
		if len(out) != 1 {
			t.Fatalf("unexpected outer tuple len: %d", len(out))
		}

		tuple, ok := out[0].([]any)
		if !ok {
			t.Fatalf("unexpected tuple type: %T", out[0])
		}
		if len(tuple) != 10 {
			t.Fatalf("unexpected tuple len without config: %d", len(tuple))
		}
		if magic, ok := tuple[0].(uint32); !ok || magic != 0x076ef1ea {
			t.Fatalf("unexpected c7 magic: %v", tuple[0])
		}
		if ts, ok := tuple[3].(uint32); !ok || ts != 456 {
			t.Fatalf("unexpected unix time: %v", tuple[3])
		}
		if seedInt, ok := tuple[6].(*big.Int); !ok || seedInt.Cmp(new(big.Int).SetBytes(seed)) != 0 {
			t.Fatalf("unexpected seed tuple item: %v", tuple[6])
		}
		balanceTuple, ok := tuple[7].([]any)
		if !ok || len(balanceTuple) != 2 {
			t.Fatalf("unexpected balance tuple: %#v", tuple[7])
		}
		if balance, ok := balanceTuple[0].(*big.Int); !ok || balance.Int64() != 77 {
			t.Fatalf("unexpected balance value: %v", balanceTuple[0])
		}
		if tuple[9] != nil {
			t.Fatalf("expected nil config slot without config, got %T", tuple[9])
		}
	})

	t.Run("WithConfig", func(t *testing.T) {
		cfg := cell.NewDict(32)
		if err := cfg.SetIntKey(big.NewInt(1), cell.BeginCell().MustStoreUInt(7, 8).EndCell()); err != nil {
			t.Fatalf("seed config dict: %v", err)
		}

		out, err := PrepareC7(addr, time.Unix(789, 0), seed, big.NewInt(99), cfg, MainContractCode)
		if err != nil {
			t.Fatalf("PrepareC7 failed: %v", err)
		}
		tuple, ok := out[0].([]any)
		if !ok {
			t.Fatalf("unexpected tuple type: %T", out[0])
		}
		if len(tuple) != 14 {
			t.Fatalf("unexpected tuple len with config: %d", len(tuple))
		}

		cfgCell, ok := tuple[9].(*cell.Cell)
		if !ok || cfgCell == nil {
			t.Fatalf("unexpected config cell: %T", tuple[9])
		}
		if codeCell, ok := tuple[10].(*cell.Cell); !ok || codeCell == nil || !bytes.Equal(codeCell.Hash(), MainContractCode.Hash()) {
			t.Fatalf("unexpected code cell: %v", tuple[10])
		}
		storageFees, ok := tuple[11].([]any)
		if !ok || len(storageFees) != 2 || storageFees[0] != 0 || storageFees[1] != nil {
			t.Fatalf("unexpected storage fees tuple: %#v", tuple[11])
		}
		if wc, ok := tuple[12].(uint8); !ok || wc != 0 {
			t.Fatalf("unexpected workchain marker: %v", tuple[12])
		}
		if tuple[13] != nil {
			t.Fatalf("expected nil prev-blocks slot, got %T", tuple[13])
		}
	})
}

func TestRunGetMethodFailsBeforeNativeCallOnInvalidParams(t *testing.T) {
	_, err := RunGetMethod(RunMethodParams{}, 1)
	if err == nil {
		t.Fatal("RunGetMethod should fail on zero-value params")
	}
}

func TestMainRoundTripDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("main panicked: %v", r)
		}
	}()

	main()
}
