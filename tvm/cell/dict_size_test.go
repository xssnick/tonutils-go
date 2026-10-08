package cell

import (
	"strconv"
	"testing"
	"unsafe"
)

func TestDictionaryViewsStayInCompactAllocatorClass(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("allocator class guard is defined for 64-bit targets")
	}

	const maxDictionarySize = 24
	dictSize := unsafe.Sizeof(Dictionary{})
	prefixSize := unsafe.Sizeof(PrefixDictionary{})
	t.Logf("Dictionary sizes: plain=%d prefix=%d bytes", dictSize, prefixSize)
	if size := dictSize; size > maxDictionarySize {
		t.Fatalf("Dictionary size = %d bytes, want at most %d", size, maxDictionarySize)
	}
	if size := prefixSize; size > maxDictionarySize {
		t.Fatalf("PrefixDictionary size = %d bytes, want at most %d", size, maxDictionarySize)
	}

	const maxIteratorSize = 432
	iteratorSize := unsafe.Sizeof(DictIterator{})
	t.Logf("DictIterator size: %d bytes", iteratorSize)
	if size := iteratorSize; size > maxIteratorSize {
		t.Fatalf("DictIterator size = %d bytes, want at most %d", size, maxIteratorSize)
	}
}
