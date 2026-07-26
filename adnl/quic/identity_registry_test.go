package quic

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestNewIdentityOwnsAndValidatesPrivateKey(t *testing.T) {
	key := mustIdentityRegistryKey(t)
	expectedKey := append(ed25519.PrivateKey(nil), key...)
	expectedPublicKey := append(ed25519.PublicKey(nil), key[ed25519.SeedSize:]...)

	identity, err := NewIdentity(key)
	if err != nil {
		t.Fatal(err)
	}

	clear(key)
	if !bytes.Equal(identity.key, expectedKey) {
		t.Fatal("identity private key changed after caller key mutation")
	}
	if !bytes.Equal(identity.PublicKey(), expectedPublicKey) {
		t.Fatal("identity public key changed after caller key mutation")
	}

	malformed := append(ed25519.PrivateKey(nil), expectedKey...)
	malformed[0] ^= 1
	if _, err = NewIdentity(malformed); err == nil {
		t.Fatal("expected inconsistent private key to be rejected")
	}
}

func TestNewIdentityRegistryRequiresDefaultIdentity(t *testing.T) {
	if _, err := newIdentityRegistry(); err == nil {
		t.Fatal("expected empty identity registry to fail")
	}
}

func TestIdentityRegistryDefaultAndLookup(t *testing.T) {
	defaultKey := mustIdentityRegistryKey(t)
	secondKey := mustIdentityRegistryKey(t)

	registry, err := newIdentityRegistry(defaultKey, secondKey)
	if err != nil {
		t.Fatal(err)
	}

	defaultIdentity := registry.defaultIdentity()
	if !bytes.Equal(defaultIdentity.PublicKey(), defaultKey[ed25519.SeedSize:]) {
		t.Fatal("unexpected default identity")
	}

	identities := registry.identities()
	if len(identities) != 2 {
		t.Fatalf("identities = %d, want 2", len(identities))
	}
	if identities[0].id != defaultIdentity.id {
		t.Fatal("default identity is not first")
	}

	secondID := adnlIDFromKey(ed25519.PublicKey(secondKey[ed25519.SeedSize:]))
	secondIdentity, err := registry.get(secondID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(secondIdentity.PublicKey(), secondKey[ed25519.SeedSize:]) {
		t.Fatal("unexpected second identity")
	}
	if !registry.has(secondID) {
		t.Fatal("second identity is not registered")
	}
}

func TestNewIdentityRegistryDeduplicatesStartupKeys(t *testing.T) {
	defaultKey := mustIdentityRegistryKey(t)
	secondKey := mustIdentityRegistryKey(t)

	registry, err := newIdentityRegistry(defaultKey, secondKey, defaultKey, secondKey)
	if err != nil {
		t.Fatal(err)
	}

	identities := registry.identities()
	if len(identities) != 2 {
		t.Fatalf("identities = %d, want 2", len(identities))
	}
	if identities[0].id != adnlIDFromKey(ed25519.PublicKey(defaultKey[ed25519.SeedSize:])) {
		t.Fatal("default identity changed while deduplicating startup keys")
	}
	if identities[1].id != adnlIDFromKey(ed25519.PublicKey(secondKey[ed25519.SeedSize:])) {
		t.Fatal("identity order changed while deduplicating startup keys")
	}
}

func TestNewIdentityRegistryValidatesEveryStartupKey(t *testing.T) {
	validKey := mustIdentityRegistryKey(t)
	invalidKey := append(ed25519.PrivateKey(nil), validKey...)
	invalidKey[0] ^= 1

	if _, err := newIdentityRegistry(validKey, validKey, invalidKey); err == nil {
		t.Fatal("expected invalid key after a duplicate to be rejected")
	}
}

func TestIdentityRegistryAddIsIdempotent(t *testing.T) {
	defaultKey := mustIdentityRegistryKey(t)
	addedKey := mustIdentityRegistryKey(t)

	registry, err := newIdentityRegistry(defaultKey)
	if err != nil {
		t.Fatal(err)
	}

	first, err := registry.add(addedKey)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.add(addedKey)
	if err != nil {
		t.Fatal(err)
	}

	if first.id != second.id {
		t.Fatal("idempotent add returned different identities")
	}
	if len(registry.identities()) != 2 {
		t.Fatalf("identities = %d, want 2", len(registry.identities()))
	}
}

func TestIdentityRegistryRemove(t *testing.T) {
	defaultKey := mustIdentityRegistryKey(t)
	removedKey := mustIdentityRegistryKey(t)

	registry, err := newIdentityRegistry(defaultKey, removedKey)
	if err != nil {
		t.Fatal(err)
	}

	removedID := adnlIDFromKey(ed25519.PublicKey(removedKey[ed25519.SeedSize:]))
	if err = registry.remove(removedID); err != nil {
		t.Fatal(err)
	}
	if registry.has(removedID) {
		t.Fatal("removed identity is still registered")
	}
	if _, err = registry.get(removedID); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("get removed identity error = %v, want %v", err, ErrIdentityNotFound)
	}
	if err = registry.remove(removedID); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("second remove error = %v, want %v", err, ErrIdentityNotFound)
	}

	defaultID := registry.defaultIdentity().id
	if err = registry.remove(defaultID); !errors.Is(err, ErrDefaultIdentity) {
		t.Fatalf("remove default error = %v, want %v", err, ErrDefaultIdentity)
	}
	if !registry.has(defaultID) {
		t.Fatal("default identity was removed")
	}
}

func TestIdentityRegistrySnapshotsRemainImmutable(t *testing.T) {
	defaultKey := mustIdentityRegistryKey(t)
	addedKey := mustIdentityRegistryKey(t)

	registry, err := newIdentityRegistry(defaultKey)
	if err != nil {
		t.Fatal(err)
	}
	beforeAdd := registry.state.Load()

	added, err := registry.add(addedKey)
	if err != nil {
		t.Fatal(err)
	}
	afterAdd := registry.state.Load()

	if len(beforeAdd.identities) != 1 || beforeAdd.byID[added.id].key != nil {
		t.Fatal("add mutated the previous snapshot")
	}
	if len(afterAdd.identities) != 2 {
		t.Fatal("new snapshot does not contain added identity")
	}

	if err = registry.remove(added.id); err != nil {
		t.Fatal(err)
	}
	afterRemove := registry.state.Load()
	if len(afterAdd.identities) != 2 || afterAdd.byID[added.id].key == nil {
		t.Fatal("remove mutated the previous snapshot")
	}
	if len(afterRemove.identities) != 1 {
		t.Fatal("new snapshot still contains removed identity")
	}
}

func TestIdentityRegistryConcurrentReadersAndWriters(t *testing.T) {
	defaultKey := mustIdentityRegistryKey(t)
	dynamicKey := mustIdentityRegistryKey(t)

	registry, err := newIdentityRegistry(defaultKey)
	if err != nil {
		t.Fatal(err)
	}
	dynamicID := adnlIDFromKey(ed25519.PublicKey(dynamicKey[ed25519.SeedSize:]))

	const iterations = 1000
	var wg sync.WaitGroup
	errs := make(chan error, 9)

	for range 8 {
		wg.Go(func() {
			for range iterations {
				if registry.defaultIdentity().id == (adnlID{}) {
					errs <- errors.New("zero default identity")
					return
				}

				_ = registry.has(dynamicID)
				_, err := registry.get(dynamicID)
				if err != nil && !errors.Is(err, ErrIdentityNotFound) {
					errs <- err
					return
				}
				if len(registry.identities()) == 0 {
					errs <- errors.New("empty identity snapshot")
					return
				}
			}
		})
	}

	wg.Go(func() {
		for range iterations {
			if _, err := registry.add(dynamicKey); err != nil {
				errs <- err
				return
			}
			if err := registry.remove(dynamicID); err != nil {
				errs <- err
				return
			}
		}
	})

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func BenchmarkNewIdentityRegistry(b *testing.B) {
	for _, size := range []int{1, 16, 256} {
		keys := make([]ed25519.PrivateKey, size)
		for i := range keys {
			keys[i] = mustIdentityRegistryBenchmarkKey(b)
		}

		b.Run(fmt.Sprintf("keys=%d", size), func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				registry, err := newIdentityRegistry(keys...)
				if err != nil {
					b.Fatal(err)
				}
				if len(registry.identities()) != size {
					b.Fatalf("identities = %d, want %d", len(registry.identities()), size)
				}
			}
		})
	}
}

func mustIdentityRegistryKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustIdentityRegistryBenchmarkKey(b *testing.B) ed25519.PrivateKey {
	b.Helper()

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	return key
}
