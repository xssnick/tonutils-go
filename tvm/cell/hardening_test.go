package cell

import (
	"bytes"
	"errors"
	"math/big"
	"testing"
)

func TestParseCellsRejectsIndexedBoundaryOverflow(t *testing.T) {
	boc := BeginCell().MustStoreUInt(0xAB, 8).EndCell().ToBOCWithOptions(BOCOptions{WithIndex: true})
	if len(boc) < 12 {
		t.Fatalf("unexpected short indexed boc: %d", len(boc))
	}

	corrupted := append([]byte{}, boc...)
	corrupted[11] = 0xFF

	if _, err := FromBOC(corrupted); err == nil {
		t.Fatal("expected indexed boc with out-of-bounds offset to fail")
	}
	if _, err := FromBOCMultiRoot(corrupted); err == nil {
		t.Fatal("expected indexed boc with out-of-bounds offset to fail for multi-root parser too")
	}
}

func TestParseCellsRejectsIndexedCycles(t *testing.T) {
	boc := []byte{
		0xB5, 0xEE, 0x9C, 0x72,
		0x81, 0x01,
		0x02, 0x01, 0x00, 0x06,
		0x00,
		0x03, 0x06,
		0x01, 0x00, 0x01,
		0x01, 0x00, 0x00,
	}

	if _, err := FromBOC(boc); err == nil {
		t.Fatal("expected cyclic indexed boc to fail")
	}
}

func TestBuilderRejectsOutOfRangeValues(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "UIntWidth", err: BeginCell().StoreUInt(256, 8)},
		{name: "IntPositiveWidth", err: BeginCell().StoreInt(128, 8)},
		{name: "IntOneBitPositive", err: BeginCell().StoreInt(1, 1)},
		{name: "BigUIntWidth", err: BeginCell().StoreBigUInt(new(big.Int).Lsh(big.NewInt(1), 8), 8)},
		{name: "BigIntPositiveWidth", err: BeginCell().StoreBigInt(new(big.Int).Lsh(big.NewInt(1), 8), 8)},
		{name: "BigIntNegativeWidth", err: BeginCell().StoreBigInt(big.NewInt(-129), 8)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err != ErrTooBigValue {
				t.Fatalf("expected ErrTooBigValue, got %v", tc.err)
			}
		})
	}
}

func TestBuilderStoreBigValuesRejectNil(t *testing.T) {
	if err := BeginCell().StoreBigVarUInt(nil, 4); err != ErrNilBigInt {
		t.Fatalf("expected ErrNilBigInt from StoreBigVarUInt, got %v", err)
	}
	if err := BeginCell().StoreBigUInt(nil, 4); err != ErrNilBigInt {
		t.Fatalf("expected ErrNilBigInt from StoreBigUInt, got %v", err)
	}
	if err := BeginCell().StoreBigInt(nil, 4); err != ErrNilBigInt {
		t.Fatalf("expected ErrNilBigInt from StoreBigInt, got %v", err)
	}
}

func TestStoreBigIntDoesNotMutateInput(t *testing.T) {
	value := big.NewInt(-5)
	before := new(big.Int).Set(value)

	if err := BeginCell().StoreBigInt(value, 8); err != nil {
		t.Fatalf("failed to store signed big int: %v", err)
	}
	if value.Cmp(before) != 0 {
		t.Fatalf("StoreBigInt mutated input: got %v want %v", value, before)
	}
}

func TestStoreBigIntSupportsSigned257Boundary(t *testing.T) {
	min := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 256))

	cell := BeginCell().MustStoreBigInt(min, 257).EndCell()
	got, err := cell.BeginParse().LoadBigInt(257)
	if err != nil {
		t.Fatalf("failed to load signed 257-bit value: %v", err)
	}
	if got.Cmp(min) != 0 {
		t.Fatalf("unexpected signed 257-bit roundtrip: got %v want %v", got, min)
	}
}

func TestSliceRejectsNarrowLoadOverflow(t *testing.T) {
	tooBigUInt := BeginCell().MustStoreBigUInt(new(big.Int).Lsh(big.NewInt(1), 64), 65).EndCell().BeginParse()
	beforeUInt := tooBigUInt.BitsLeft()
	if _, err := tooBigUInt.LoadUInt(65); err != ErrTooBigValue {
		t.Fatalf("expected ErrTooBigValue from LoadUInt, got %v", err)
	}
	if tooBigUInt.BitsLeft() != beforeUInt {
		t.Fatalf("LoadUInt overflow advanced slice: before=%d after=%d", beforeUInt, tooBigUInt.BitsLeft())
	}
	if _, err := tooBigUInt.PreloadUInt(65); err != ErrTooBigValue {
		t.Fatalf("expected ErrTooBigValue from PreloadUInt, got %v", err)
	}

	tooBigInt := BeginCell().MustStoreBigInt(new(big.Int).Lsh(big.NewInt(1), 63), 65).EndCell().BeginParse()
	beforeInt := tooBigInt.BitsLeft()
	if _, err := tooBigInt.LoadInt(65); err != ErrTooBigValue {
		t.Fatalf("expected ErrTooBigValue from LoadInt, got %v", err)
	}
	if tooBigInt.BitsLeft() != beforeInt {
		t.Fatalf("LoadInt overflow advanced slice: before=%d after=%d", beforeInt, tooBigInt.BitsLeft())
	}

	tooBigCoins := BeginCell().MustStoreBigCoins(new(big.Int).Lsh(big.NewInt(1), 64)).EndCell().BeginParse()
	beforeCoins := tooBigCoins.BitsLeft()
	if _, err := tooBigCoins.LoadCoins(); err != ErrTooBigValue {
		t.Fatalf("expected ErrTooBigValue from LoadCoins, got %v", err)
	}
	if tooBigCoins.BitsLeft() != beforeCoins {
		t.Fatalf("LoadCoins overflow advanced slice: before=%d after=%d", beforeCoins, tooBigCoins.BitsLeft())
	}
}

func TestSliceLoadBigIntZeroBits(t *testing.T) {
	s := BeginCell().MustStoreUInt(0xAB, 8).EndCell().BeginParse()

	before := s.BitsLeft()
	got, err := s.LoadBigInt(0)
	if err != nil {
		t.Fatalf("failed to load zero-bit big int: %v", err)
	}
	if got.Sign() != 0 {
		t.Fatalf("expected zero-bit big int to be zero, got %v", got)
	}
	if s.BitsLeft() != before {
		t.Fatalf("zero-bit LoadBigInt advanced slice: before=%d after=%d", before, s.BitsLeft())
	}

	got, err = s.PreloadBigInt(0)
	if err != nil {
		t.Fatalf("failed to preload zero-bit big int: %v", err)
	}
	if got.Sign() != 0 {
		t.Fatalf("expected zero-bit preloaded big int to be zero, got %v", got)
	}
	if s.BitsLeft() != before {
		t.Fatalf("zero-bit PreloadBigInt advanced slice: before=%d after=%d", before, s.BitsLeft())
	}
}

func TestSliceLoadVarUIntRejectsInvalidLengthEncoding(t *testing.T) {
	s := BeginCell().MustStoreUInt(10, 4).EndCell().BeginParse()
	if _, err := s.LoadVarUInt(10); err != ErrTooBigValue {
		t.Fatalf("expected ErrTooBigValue from invalid varuint length, got %v", err)
	}
}

func TestToBOCWithTopHashChangesEncoding(t *testing.T) {
	root := BeginCell().MustStoreUInt(0xAB, 8).EndCell()

	plain := root.ToBOCWithOptions(BOCOptions{WithIndex: true})
	withTopHash := root.ToBOCWithOptions(BOCOptions{
		WithIndex:   true,
		WithTopHash: true,
	})

	if bytes.Equal(plain, withTopHash) {
		t.Fatal("expected top-hash mode to change serialized boc")
	}

	parsedPlain, err := FromBOC(plain)
	if err != nil {
		t.Fatalf("failed to parse plain boc: %v", err)
	}
	parsedWithTopHash, err := FromBOC(withTopHash)
	if err != nil {
		t.Fatalf("failed to parse top-hash boc: %v", err)
	}
	if !bytes.Equal(parsedPlain.Hash(), parsedWithTopHash.Hash()) {
		t.Fatalf("top-hash mode changed root hash: plain=%x top=%x", parsedPlain.Hash(), parsedWithTopHash.Hash())
	}
}

func TestCalculateHashesSafeRejectsTooDeepCells(t *testing.T) {
	root := &Cell{}
	for i := 0; i < maxDepth; i++ {
		parent := &Cell{}
		parent.setRef(0, root)
		parent.setRefsCount(1)
		parent.setLevelMask(ordinaryLevelMask(parent.rawRefs()))
		root = parent
	}

	if err := root.calculateHashesSafe(); !errors.Is(err, ErrCellDepthLimit) {
		t.Fatalf("expected ErrCellDepthLimit, got %v", err)
	}
}

func TestFromBOCRejectsConfiguredRootLimit(t *testing.T) {
	prev := MaxBOCRoots
	MaxBOCRoots = 1
	defer func() {
		MaxBOCRoots = prev
	}()

	roots := []*Cell{
		BeginCell().MustStoreUInt(0xAA, 8).EndCell(),
		BeginCell().MustStoreUInt(0xBB, 8).EndCell(),
	}

	if _, err := FromBOCMultiRoot(ToBOCWithOptions(roots, BOCOptions{})); err == nil {
		t.Fatal("expected root-limit violation to fail")
	}
}

func TestFromBOCRejectsConfiguredCellLimit(t *testing.T) {
	prev := MaxBOCCells
	MaxBOCCells = 1
	defer func() {
		MaxBOCCells = prev
	}()

	root := BeginCell().
		MustStoreUInt(0xAB, 8).
		MustStoreRef(BeginCell().MustStoreUInt(0xCD, 8).EndCell()).
		EndCell()

	if _, err := FromBOC(root.ToBOCWithOptions(BOCOptions{})); err == nil {
		t.Fatal("expected cell-limit violation to fail")
	}
}

func TestFromBOCRejectsTrailingDataAfterPayload(t *testing.T) {
	boc := BeginCell().MustStoreUInt(0xAB, 8).EndCell().ToBOCWithOptions(BOCOptions{})
	boc = append(boc, 0x00, 0x01)

	if _, err := FromBOC(boc); err == nil {
		t.Fatal("expected trailing bytes after payload to fail")
	}
}

func TestStoreBinarySnakeRejectsDepthOverflow(t *testing.T) {
	data := bytes.Repeat([]byte{0xAB}, 127*1024+1)

	if err := BeginCell().StoreBinarySnake(data); !errors.Is(err, ErrCellDepthLimit) {
		t.Fatalf("expected ErrCellDepthLimit, got %v", err)
	}
}
