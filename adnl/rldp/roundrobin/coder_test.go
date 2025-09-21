package roundrobin

import (
	"crypto/rand"
	mrand "math/rand"
	"testing"
	"time"
)

func genRandomBytes(t *testing.T, n int) []byte {
	t.Helper()
	if n == 0 {
		return nil
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return b
}

func TestNewEncoder_EmptyData(t *testing.T) {
	_, err := NewEncoder(nil, 256)
	if err == nil {
		t.Fatalf("expected error for empty data, got nil")
	}
}

func TestNewDecoder_BadParams(t *testing.T) {
	_, err := NewDecoder(256, 0)
	if err == nil {
		t.Fatalf("expected error for dataSize=0, got nil")
	}
}

func TestRoundTrip_ExactMultiple(t *testing.T) {
	const symbolSize = 256
	const dataSize = 4096
	data := genRandomBytes(t, dataSize)

	enc, err := NewEncoder(data, symbolSize)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	dec, err := NewDecoder(symbolSize, uint32(len(data)))
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}

	for i := uint32(0); i < enc.symbolsCount; i++ {
		s := enc.GenSymbol(i)
		if uint32(len(s)) != symbolSize {
			t.Fatalf("symbol len mismatch: got %d, want %d", len(s), symbolSize)
		}
	}

	ids := make([]uint32, enc.symbolsCount)
	for i := range ids {
		ids[i] = uint32(i)
	}
	r := mrand.New(mrand.NewSource(1))
	r.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })

	complete := false
	for idx, id := range ids {
		sym := enc.GenSymbol(id)
		done, err := dec.AddSymbol(id, sym)
		if err != nil {
			t.Fatalf("AddSymbol #%d (id=%d): %v", idx, id, err)
		}
		if done && idx != len(ids)-1 {
			t.Fatalf("completed too early at #%d", idx)
		}
		complete = done
	}
	if !complete {
		t.Fatalf("not completed after feeding all symbols")
	}

	ready, out, err := dec.Decode()
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if !ready {
		t.Fatalf("Decode not ready")
	}
	if len(out) != len(data) {
		t.Fatalf("decoded length mismatch: got %d, want %d", len(out), len(data))
	}
	if string(out) != string(data) {
		t.Fatalf("decoded data mismatch")
	}
}

func TestRoundTrip_WithTailRemainder(t *testing.T) {
	const symbolSize = 256
	const dataSize = 1000
	data := genRandomBytes(t, dataSize)

	enc, err := NewEncoder(data, symbolSize)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}

	lastID := enc.symbolsCount - 1
	last := enc.GenSymbol(lastID)
	if len(last) != symbolSize {
		t.Fatalf("last symbol len: got %d, want %d", len(last), symbolSize)
	}

	dec, err := NewDecoder(symbolSize, uint32(len(data)))
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}

	ids := make([]uint32, enc.symbolsCount)
	for i := range ids {
		ids[i] = uint32(i)
	}
	r := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })

	for _, id := range ids {
		sym := enc.GenSymbol(id)
		if _, err := dec.AddSymbol(id, sym); err != nil {
			t.Fatalf("AddSymbol id=%d: %v", id, err)
		}
		if _, err := dec.AddSymbol(id, sym); err != nil {
			t.Fatalf("AddSymbol duplicate id=%d: %v", id, err)
		}
	}

	ready, out, err := dec.Decode()
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if !ready {
		t.Fatalf("Decode not ready")
	}
	if len(out) != len(data) {
		t.Fatalf("decoded length mismatch: got %d, want %d", len(out), len(data))
	}
	if string(out) != string(data) {
		t.Fatalf("decoded data mismatch")
	}
}

func TestInvalidSymbolLength(t *testing.T) {
	const symbolSize = 128
	const dataSize = 1024
	data := genRandomBytes(t, dataSize)

	enc, err := NewEncoder(data, symbolSize)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	dec, err := NewDecoder(symbolSize, uint32(len(data)))
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}

	id := uint32(0)
	okSym := enc.GenSymbol(id)
	if len(okSym) != symbolSize {
		t.Fatalf("GenSymbol produced wrong length: %d", len(okSym))
	}

	badShort := okSym[:symbolSize-1]
	if _, err := dec.AddSymbol(id, badShort); err == nil {
		t.Fatalf("expected error for short symbol, got nil")
	}

	badLong := append(okSym, 0xFF)
	if _, err := dec.AddSymbol(id, badLong); err == nil {
		t.Fatalf("expected error for long symbol, got nil")
	}
}

func TestDecode_NotReady(t *testing.T) {
	const symbolSize = 64
	const dataSize = 1000
	data := genRandomBytes(t, dataSize)

	enc, err := NewEncoder(data, symbolSize)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	dec, err := NewDecoder(symbolSize, uint32(len(data)))
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}

	for id := uint32(0); id < enc.symbolsCount-1; id++ {
		if _, err := dec.AddSymbol(id, enc.GenSymbol(id)); err != nil {
			t.Fatalf("AddSymbol: %v", err)
		}
	}

	if ready, _, err := dec.Decode(); err == nil || ready {
		t.Fatalf("expected not ready error, got ready=%v err=%v", ready, err)
	}
}

func TestGenSymbol_ModuloBehavior(t *testing.T) {
	const symbolSize = 128
	const dataSize = 777
	data := genRandomBytes(t, dataSize)

	enc, err := NewEncoder(data, symbolSize)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}

	if enc.symbolsCount < 2 {
		t.Skip("need at least 2 symbols to test modulo behavior")
	}
	ref := enc.GenSymbol(1)
	same := enc.GenSymbol(enc.symbolsCount + 1)
	if string(ref) != string(same) {
		t.Fatalf("GenSymbol modulo mismatch: got different payloads for ids 1 and symbolsCount+1")
	}
}
