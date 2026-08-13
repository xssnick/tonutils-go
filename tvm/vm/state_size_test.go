package vm

import (
	"strconv"
	"testing"
	"unsafe"
)

func TestStateStaysInCompactAllocatorClass(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("allocator class guard is defined for 64-bit targets")
	}

	// Pointer-bearing objects need room for the runtime allocation header in
	// the 640-byte class on current 64-bit Go targets.
	const maxStateSize = 632
	size := unsafe.Sizeof(State{})
	t.Logf("State size: %d bytes", size)
	t.Logf("State library offsets: loads=%d sandbox=%d depth=%d stack=%d", unsafe.Offsetof(State{}.libraryLoads), unsafe.Offsetof(State{}.libraryLookupSandboxed), unsafe.Offsetof(State{}.maxDataDepth), unsafe.Offsetof(State{}.Stack))
	if size > maxStateSize {
		t.Fatalf("State size = %d bytes, want at most %d", size, maxStateSize)
	}
}
