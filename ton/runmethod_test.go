package ton

import (
	"bytes"
	"testing"
)

func Test_methodNameHash(t *testing.T) {
	hash := methodNameHash("seqno")
	if !bytes.Equal([]byte{0x97, 0x4c, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00}, hash) {
		t.Fatal("bad name hash")
	}
}
