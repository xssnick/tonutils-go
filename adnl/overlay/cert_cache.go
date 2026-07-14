package overlay

import (
	"crypto/ed25519"
	"crypto/sha256"
	"sync"
)

// Certificate signature checks run for every FEC part of every broadcast; the
// signature is identical across all parts of a stream, so a bounded cache of
// already-verified (issuer, payload, signature) triples removes an Ed25519
// verify from the per-part hot path. Only valid signatures are cached:
// invalid payloads can be mutated endlessly, so caching failures would not
// bound attacker work anyway.
const certSignatureCacheLimit = 8192

var (
	certSignatureCacheMx sync.RWMutex
	certSignatureCache   = map[[32]byte]struct{}{}
)

// verifyCertSignatureCached reports whether signature is a valid issuer
// signature over toSign, consulting the verified-signature cache first. The
// cache key concatenation is unambiguous: issuer and signature have fixed
// lengths, only toSign varies.
func verifyCertSignatureCached(issuer ed25519.PublicKey, toSign []byte, signature []byte) bool {
	if len(issuer) != ed25519.PublicKeySize {
		return false
	}

	hasher := sha256.New()
	hasher.Write(issuer)
	hasher.Write(signature)
	hasher.Write(toSign)
	var key [32]byte
	hasher.Sum(key[:0])

	certSignatureCacheMx.RLock()
	_, ok := certSignatureCache[key]
	certSignatureCacheMx.RUnlock()
	if ok {
		return true
	}

	if !ed25519.Verify(issuer, toSign, signature) {
		return false
	}

	certSignatureCacheMx.Lock()
	if len(certSignatureCache) >= certSignatureCacheLimit {
		// Drop a slice of entries; randomized map iteration is a good enough
		// eviction policy at this size.
		drop := certSignatureCacheLimit / 8
		for evict := range certSignatureCache {
			delete(certSignatureCache, evict)
			drop--
			if drop <= 0 {
				break
			}
		}
	}
	certSignatureCache[key] = struct{}{}
	certSignatureCacheMx.Unlock()
	return true
}
