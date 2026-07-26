package quic

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
)

var (
	// ErrIdentityNotFound is returned when a local identity is not registered.
	ErrIdentityNotFound = errors.New("quic: identity not found")
	// ErrDefaultIdentity is returned when removing the immutable default identity.
	ErrDefaultIdentity = errors.New("quic: default identity cannot be removed")
)

type identitySnapshot struct {
	defaultIdentity Identity
	identities      []Identity
	byID            map[adnlID]Identity
}

type identityRegistry struct {
	mu    sync.Mutex
	state atomic.Pointer[identitySnapshot]
}

func newIdentityRegistry(keys ...ed25519.PrivateKey) (*identityRegistry, error) {
	if len(keys) == 0 {
		return nil, errors.New("quic: at least one identity key is required")
	}

	identities := make([]Identity, 0, len(keys))
	byID := make(map[adnlID]Identity, len(keys))
	for _, key := range keys {
		identity, err := NewIdentity(key)
		if err != nil {
			return nil, err
		}

		if existing, ok := byID[identity.id]; ok {
			if !bytes.Equal(existing.key, identity.key) {
				return nil, fmt.Errorf("quic: identity %s is already registered with a different key", identity.id)
			}

			continue
		}

		identities = append(identities, identity)
		byID[identity.id] = identity
	}

	r := &identityRegistry{}
	r.state.Store(&identitySnapshot{
		defaultIdentity: identities[0],
		identities:      identities,
		byID:            byID,
	})

	return r, nil
}

func (r *identityRegistry) defaultIdentity() Identity {
	return r.state.Load().defaultIdentity
}

// identities returns an immutable snapshot owned by the registry.
// Package-internal callers must not mutate the slice or its elements.
func (r *identityRegistry) identities() []Identity {
	return r.state.Load().identities
}

func (r *identityRegistry) get(id adnlID) (Identity, error) {
	identity, ok := r.state.Load().byID[id]
	if !ok {
		return Identity{}, fmt.Errorf("%w: %s", ErrIdentityNotFound, id)
	}

	return identity, nil
}

func (r *identityRegistry) has(id adnlID) bool {
	_, ok := r.state.Load().byID[id]
	return ok
}

func (r *identityRegistry) add(key ed25519.PrivateKey) (Identity, error) {
	identity, err := NewIdentity(key)
	if err != nil {
		return Identity{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	current := r.state.Load()
	if existing, ok := current.byID[identity.id]; ok {
		if !bytes.Equal(existing.key, identity.key) {
			return Identity{}, fmt.Errorf("quic: identity %s is already registered with a different key", identity.id)
		}

		return existing, nil
	}

	next := &identitySnapshot{
		defaultIdentity: current.defaultIdentity,
		identities:      append(slices.Clone(current.identities), identity),
		byID:            maps.Clone(current.byID),
	}
	next.byID[identity.id] = identity
	r.state.Store(next)

	return identity, nil
}

func (r *identityRegistry) remove(id adnlID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	current := r.state.Load()
	if id == current.defaultIdentity.id {
		return fmt.Errorf("%w: %s", ErrDefaultIdentity, id)
	}
	if _, ok := current.byID[id]; !ok {
		return fmt.Errorf("%w: %s", ErrIdentityNotFound, id)
	}

	next := &identitySnapshot{
		defaultIdentity: current.defaultIdentity,
		identities:      make([]Identity, 0, len(current.identities)-1),
		byID:            maps.Clone(current.byID),
	}
	for _, identity := range current.identities {
		if identity.id != id {
			next.identities = append(next.identities, identity)
		}
	}
	delete(next.byID, id)
	r.state.Store(next)

	return nil
}
