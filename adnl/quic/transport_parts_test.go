package quic

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestWriteBoxedObjectPartsMatchesContiguousPayload(t *testing.T) {
	for _, size := range []int{0, 31, 1024, directWriteObjectThreshold + 17} {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			prefix := bytes.Repeat([]byte{0x31}, size/3)
			body := bytes.Repeat([]byte{0x72}, size-len(prefix))
			payload := append(append([]byte(nil), prefix...), body...)

			var want bytes.Buffer
			if err := writeBoxedObjectTo(&want, idQuicMessage, payload); err != nil {
				t.Fatalf("write contiguous: %v", err)
			}
			var got bytes.Buffer
			if err := writeBoxedObjectPartsTo(&got, idQuicMessage, prefix, body); err != nil {
				t.Fatalf("write parts: %v", err)
			}
			if !bytes.Equal(got.Bytes(), want.Bytes()) {
				t.Fatalf("wire differs for payload size %d", size)
			}
		})
	}
}

type retainingErrorWriter struct {
	data []byte
	n    int
	err  error
}

func (w *retainingErrorWriter) Write(data []byte) (int, error) {
	w.data = data
	return w.n, w.err
}

func TestWriteSmallBoxedObjectDoesNotRecycleBufferAfterWriteError(t *testing.T) {
	allocations := 0
	buffers := sync.Pool{
		New: func() any {
			allocations++
			buf := make([]byte, directWriteObjectThreshold+16)
			return &buf
		},
	}

	errWrite := errors.New("retained write failed")
	w := &retainingErrorWriter{n: 1, err: errWrite}
	payload := []byte("payload")
	if err := writeSmallBoxedObject(w, idQuicMessage, nil, payload, len(payload), &buffers); !errors.Is(err, errWrite) {
		t.Fatalf("write error = %v, want %v", err, errWrite)
	}
	if len(w.data) == 0 {
		t.Fatal("writer did not retain the wire buffer")
	}

	// A failed SendStream write may retain data until the stream is torn down.
	// A second Get must allocate instead of handing that backing array to
	// another stream while the failed writer still owns it.
	_ = buffers.Get().(*[]byte)
	if allocations != 2 {
		t.Fatalf("wire buffer allocations = %d, want 2; failed write buffer was recycled", allocations)
	}
}
