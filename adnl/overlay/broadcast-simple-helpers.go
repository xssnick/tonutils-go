package overlay

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"reflect"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

func calcBroadcastDataHash(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func calcBroadcastIDFromDataHash(source any, flags int32, dataHash []byte) ([]byte, error) {
	src, err := broadcastSourceID(source, flags)
	if err != nil {
		return nil, err
	}

	broadcastHash, err := tl.Hash(&BroadcastID{
		Source:   src,
		DataHash: dataHash,
		Flags:    flags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compute hash id of the broadcast: %w", err)
	}
	return broadcastHash, nil
}

func calcBroadcastID(source any, flags int32, data []byte) ([]byte, []byte, error) {
	dataHash := calcBroadcastDataHash(data)
	broadcastHash, err := calcBroadcastIDFromDataHash(source, flags, dataHash)
	if err != nil {
		return nil, nil, err
	}
	return broadcastHash, dataHash, nil
}

func serializeBroadcastToSign(hash []byte, date uint32) ([]byte, error) {
	toSign, err := tl.Serialize(&BroadcastToSign{
		Hash: hash,
		Date: date,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize broadcast for sign check: %w", err)
	}
	return toSign, nil
}

func signBroadcast(key ed25519.PrivateKey, hash []byte, date uint32) ([]byte, error) {
	toSign, err := serializeBroadcastToSign(hash, date)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(key, toSign), nil
}

func verifyBroadcastSignature(source any, hash []byte, date uint32, signature []byte) error {
	sourceKey, ok := source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("invalid signer key format")
	}

	toSign, err := serializeBroadcastToSign(hash, date)
	if err != nil {
		return err
	}

	if !ed25519.Verify(sourceKey.Key, toSign, signature) {
		return fmt.Errorf("invalid broadcast signature")
	}
	return nil
}

func (t *Broadcast) CalcID() ([]byte, error) {
	broadcastHash, _, err := calcBroadcastID(t.Source, t.Flags, t.Data)
	return broadcastHash, err
}

func (t *Broadcast) ToSign() (*BroadcastToSign, error) {
	if t.Date < 0 {
		return nil, fmt.Errorf("invalid broadcast date")
	}
	broadcastHash, err := t.CalcID()
	if err != nil {
		return nil, err
	}
	return &BroadcastToSign{
		Hash: broadcastHash,
		Date: uint32(t.Date),
	}, nil
}

func (t *Broadcast) VerifySignature() error {
	if t.Date < 0 {
		return fmt.Errorf("invalid broadcast date")
	}
	broadcastHash, err := t.CalcID()
	if err != nil {
		return err
	}
	return verifyBroadcastSignature(t.Source, broadcastHash, uint32(t.Date), t.Signature)
}

func (t *Broadcast) Sign(key ed25519.PrivateKey) error {
	source, ok := t.Source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("unsupported source type %s", reflect.TypeOf(t.Source).String())
	}

	if !source.Key.Equal(key.Public()) {
		return fmt.Errorf("incorrect private key")
	}
	if t.Date < 0 {
		return fmt.Errorf("invalid broadcast date")
	}

	broadcastHash, err := t.CalcID()
	if err != nil {
		return err
	}

	signature, err := signBroadcast(key, broadcastHash, uint32(t.Date))
	if err != nil {
		return err
	}
	t.Signature = signature
	return nil
}
