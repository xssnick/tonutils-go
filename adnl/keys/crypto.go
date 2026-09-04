package keys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"sync"

	"filippo.io/edwards25519" // lib from core golang developer, based on go source with extended features
)

// SharedKey - Generate encryption key based on our and server key, ECDH algorithm
func SharedKey(ourKey ed25519.PrivateKey, serverKey ed25519.PublicKey) ([]byte, error) {
	privateKey, err := ecdh.X25519().NewPrivateKey(Ed25519PrivateToX25519(ourKey))
	if err != nil {
		return nil, err
	}

	pubX, err := Ed25519PubToX25519(serverKey)
	if err != nil {
		return nil, err
	}

	pubKey, err := ecdh.X25519().NewPublicKey(pubX)
	if err != nil {
		return nil, err
	}
	return privateKey.ECDH(pubKey)
}

func SharedKeyWithPeerX25519(ourKey ed25519.PrivateKey, serverKeyX25519 []byte) ([]byte, error) {
	privateKey, err := ecdh.X25519().NewPrivateKey(Ed25519PrivateToX25519(ourKey))
	if err != nil {
		return nil, err
	}

	pubKey, err := ecdh.X25519().NewPublicKey(serverKeyX25519)
	if err != nil {
		return nil, err
	}
	return privateKey.ECDH(pubKey)
}

var ErrShortCipherInput = errors.New("shared cipher needs a 32 byte key and a 32 byte checksum")

// deriveSharedKIV expands the ADNL packet key material into the AES-256 key
// (first 32 bytes) and the CTR IV (last 16). The layout is dictated by the
// wire: every peer derives the same schedule from the packet checksum, so
// nothing here may change.
func deriveSharedKIV(kiv *[48]byte, key, checksum []byte) error {
	if len(key) < 32 || len(checksum) < 32 {
		return ErrShortCipherInput
	}

	// key
	copy(kiv[:16], key[:16])
	copy(kiv[16:32], checksum[16:32])

	// iv
	copy(kiv[32:36], checksum[:4])
	copy(kiv[36:48], key[20:32])
	return nil
}

func BuildSharedCipher(key []byte, checksum []byte) (cipher.Stream, error) {
	var kiv [48]byte
	if err := deriveSharedKIV(&kiv, key, checksum); err != nil {
		return nil, err
	}

	ctr, err := NewCipherCtr(kiv[:32], kiv[32:])
	if err != nil {
		return nil, err
	}

	return ctr, nil
}

// ctrDirectMaxSize is the body size up to which XORSharedStream runs the
// counter itself instead of asking crypto/cipher for a CTR stream.
//
// The AES key of an ADNL packet is derived from that packet's own checksum, so
// a fresh key schedule per packet is unavoidable; what is avoidable is the
// second heap object crypto/cipher.NewCTR allocates on top of it, since it
// copies the whole 488-byte expanded block into a *aes.CTR. The direct loop
// pays for that with the 8-block pipelined assembly it gives up, so the cut is
// where the two are still level plus the band where the direct loop is at most
// ~10% slower -- it always halves the allocation, and the packets in that band
// (acks, nops, plumtree IHAVEs) are the numerous ones. Measured on
// darwin/arm64 with BenchmarkSharedStreamPath{Direct,Pipelined}: even at 256
// bytes (286 vs 286 ns), +7% at 320, +20% at 384, +33% at 512. Full-MTU data
// packets stay on the pipelined path.
const ctrDirectMaxSize = 320

// ctrScratch is the per-packet working set of XORSharedStream. Every field is
// here rather than on the stack because each of them is handed to a function
// that crypto marks as leaking its argument (aes.NewCipher, cipher.NewCTR and
// the cipher.Block.Encrypt interface call), so as locals they would trade the
// allocation we are removing for three smaller ones. A scratch belongs
// exclusively to the goroutine that took it out of the pool until it is put
// back, so it needs no other synchronisation; it keeps one packet's key
// material alive until the next Get overwrites it, which is no more exposure
// than the heap block the old per-packet kiv left behind.
type ctrScratch struct {
	kiv [48]byte
	ctr [aes.BlockSize]byte
	ks  [ctrDirectMaxSize]byte
}

var ctrScratchPool = sync.Pool{New: func() any { return new(ctrScratch) }}

// XORSharedStream applies the ADNL packet stream cipher to src and writes the
// result to dst (they may be the same slice). It replaces
// BuildSharedCipher(...).XORKeyStream(...) on the per-packet path: the returned
// cipher.Stream forced the expanded AES block, the CTR state and the derived
// key material onto the heap for the lifetime of a single call.
func XORSharedStream(dst, src, key, checksum []byte) error {
	s := ctrScratchPool.Get().(*ctrScratch)
	defer ctrScratchPool.Put(s)

	if err := deriveSharedKIV(&s.kiv, key, checksum); err != nil {
		return err
	}

	block, err := aes.NewCipher(s.kiv[:32])
	if err != nil {
		return err
	}

	if len(src) <= ctrDirectMaxSize {
		xorCTRDirect(block, s, dst, src)
		return nil
	}

	// NewCTR reads the IV into its counter limbs and keeps no reference to it,
	// so the scratch may go back to the pool as soon as this returns.
	cipher.NewCTR(block, s.kiv[32:]).XORKeyStream(dst, src)
	return nil
}

// xorCTRDirect is CTR mode over a single block cipher: the counter is the IV
// taken as one big-endian 128-bit integer and incremented per block, which is
// exactly what crypto/internal/fips140/aes.CTR does, so the keystream is
// byte-identical (asserted in TestXORSharedStreamMatchesCipherStream).
func xorCTRDirect(block cipher.Block, s *ctrScratch, dst, src []byte) {
	hi := binary.BigEndian.Uint64(s.kiv[32:40])
	lo := binary.BigEndian.Uint64(s.kiv[40:48])
	binary.BigEndian.PutUint64(s.ctr[0:8], hi)

	for len(src) > 0 {
		n := 0
		for n < len(s.ks) && n < len(src) {
			binary.BigEndian.PutUint64(s.ctr[8:16], lo)
			block.Encrypt(s.ks[n:n+aes.BlockSize], s.ctr[:])
			n += aes.BlockSize

			lo++
			if lo == 0 {
				// only a wrap of the low limb touches the high one
				hi++
				binary.BigEndian.PutUint64(s.ctr[0:8], hi)
			}
		}

		m := subtle.XORBytes(dst, src, s.ks[:n])
		dst, src = dst[m:], src[m:]
	}
}

func NewCipherCtr(key, iv []byte) (cipher.Stream, error) {
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return cipher.NewCTR(c, iv), nil
}

func Ed25519PrivateToX25519(edPrivate ed25519.PrivateKey) []byte {
	h := sha512.Sum512(edPrivate.Seed())
	h[0] &= 248
	h[31] &= 127
	h[31] |= 64
	return h[:32]
}

func Ed25519PubToX25519(edPub ed25519.PublicKey) ([]byte, error) {
	// convert ed pub key to ec pub key
	point := new(edwards25519.Point)
	_, err := point.SetBytes(edPub)
	if err != nil {
		return nil, err
	}
	return point.BytesMontgomery(), nil
}
