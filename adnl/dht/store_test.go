package dht

import (
	"testing"
	"time"
)

func TestMemoryValueStore_ForEachDeleteInCallbackDoesNotDeadlock(t *testing.T) {
	store := NewMemoryValueStore(16)
	keyID := []byte("key")
	if err := store.Put(keyID, &Value{TTL: int32(time.Now().Unix())}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- store.ForEach(func(keyID []byte, value *Value) error {
			return store.Delete(keyID)
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("foreach delete deadlocked")
	}

	value, err := store.Get(keyID)
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		t.Fatal("value was not deleted")
	}
}

func TestMemoryValueStore_EvictsOldestKeys(t *testing.T) {
	store := NewMemoryValueStore(2)

	if err := store.Put([]byte("a"), &Value{TTL: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put([]byte("b"), &Value{TTL: 2}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put([]byte("c"), &Value{TTL: 3}); err != nil {
		t.Fatal(err)
	}

	value, err := store.Get([]byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		t.Fatal("oldest key was not evicted")
	}

	value, err = store.Get([]byte("b"))
	if err != nil {
		t.Fatal(err)
	}
	if value == nil || value.TTL != 2 {
		t.Fatal("key b was unexpectedly evicted")
	}

	value, err = store.Get([]byte("c"))
	if err != nil {
		t.Fatal(err)
	}
	if value == nil || value.TTL != 3 {
		t.Fatal("key c was unexpectedly evicted")
	}
}

func TestMemoryValueStore_UpdateRefreshesEvictionOrder(t *testing.T) {
	store := NewMemoryValueStore(2)

	if err := store.Put([]byte("a"), &Value{TTL: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put([]byte("b"), &Value{TTL: 2}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put([]byte("a"), &Value{TTL: 10}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put([]byte("c"), &Value{TTL: 3}); err != nil {
		t.Fatal(err)
	}

	value, err := store.Get([]byte("b"))
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		t.Fatal("key b should have been evicted after key a refresh")
	}

	value, err = store.Get([]byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	if value == nil || value.TTL != 10 {
		t.Fatal("key a was not refreshed correctly")
	}
}
