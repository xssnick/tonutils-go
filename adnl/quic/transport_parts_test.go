package quic

import (
	"bytes"
	"fmt"
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
