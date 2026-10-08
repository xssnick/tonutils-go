package overlay

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

func calcBroadcastTwoStepID(source any, flags int32, date uint32, sourceADNL, dataHash []byte, dataSize, partSize uint32, extra []byte) ([]byte, error) {
	sourceID, err := tl.Hash(source)
	if err != nil {
		return nil, fmt.Errorf("failed to compute source key id: %w", err)
	}
	return calcBroadcastTwoStepIDFromSourceID(sourceID, flags, date, sourceADNL, dataHash, dataSize, partSize, extra)
}

func calcBroadcastTwoStepIDFromSourceID(sourceID []byte, flags int32, date uint32, sourceADNL, dataHash []byte, dataSize, partSize uint32, extra []byte) ([]byte, error) {
	broadcastID, err := tl.Hash(&BroadcastTwoStepID{
		Flags:      flags,
		Date:       date,
		Source:     sourceID,
		SourceADNL: sourceADNL,
		DataHash:   dataHash,
		DataSize:   dataSize,
		PartSize:   partSize,
		Extra:      extra,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compute two-step broadcast id: %w", err)
	}
	return broadcastID, nil
}

func calcBroadcastTwoStepDataHash(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func serializeBroadcastTwoStepSimpleToSign(id, data []byte) ([]byte, error) {
	toSign, err := tl.Serialize(&BroadcastTwoStepSimpleToSign{
		ID:   id,
		Data: data,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize two-step simple for sign check: %w", err)
	}
	return toSign, nil
}

func serializeBroadcastTwoStepFECToSign(id []byte, seqno uint32, part []byte) ([]byte, error) {
	toSign, err := tl.Serialize(&BroadcastTwoStepFECToSign{
		ID:    id,
		Seqno: seqno,
		Part:  part,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize two-step fec for sign check: %w", err)
	}
	return toSign, nil
}

func verifyBroadcastTwoStepSignature(source any, toSign, signature []byte) error {
	sourceKey, ok := source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("invalid signer key format")
	}
	if !ed25519.Verify(sourceKey.Key, toSign, signature) {
		return fmt.Errorf("invalid broadcast signature")
	}
	return nil
}

func verifyBroadcastTwoStepSimpleSignature(source any, id, data, signature []byte) error {
	toSign, err := serializeBroadcastTwoStepSimpleToSign(id, data)
	if err != nil {
		return err
	}
	return verifyBroadcastTwoStepSignature(source, toSign, signature)
}

func signBroadcastTwoStepSimple(key ed25519.PrivateKey, id, data []byte) ([]byte, error) {
	return signBroadcastTwoStepSimpleWithSigner(ed25519BroadcastSigner(key), id, data)
}

func signBroadcastTwoStepSimpleWithSigner(signer BroadcastSigner, id, data []byte) ([]byte, error) {
	toSign, err := serializeBroadcastTwoStepSimpleToSign(id, data)
	if err != nil {
		return nil, err
	}

	return signBroadcastTwoStepPayload(signer, toSign)
}

func verifyBroadcastTwoStepFECSignature(source any, id []byte, seqno uint32, part, signature []byte) error {
	toSign, err := serializeBroadcastTwoStepFECToSign(id, seqno, part)
	if err != nil {
		return err
	}
	return verifyBroadcastTwoStepSignature(source, toSign, signature)
}

func signBroadcastTwoStepFEC(key ed25519.PrivateKey, id []byte, seqno uint32, part []byte) ([]byte, error) {
	return signBroadcastTwoStepFECWithSigner(ed25519BroadcastSigner(key), id, seqno, part)
}

func signBroadcastTwoStepFECWithSigner(signer BroadcastSigner, id []byte, seqno uint32, part []byte) ([]byte, error) {
	toSign, err := serializeBroadcastTwoStepFECToSign(id, seqno, part)
	if err != nil {
		return nil, err
	}

	return signBroadcastTwoStepPayload(signer, toSign)
}

func signBroadcastTwoStepPayload(signer BroadcastSigner, payload []byte) ([]byte, error) {
	signature, err := signer.Sign(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to sign broadcast: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("broadcast signature should be %d bytes", ed25519.SignatureSize)
	}

	return signature, nil
}

func checkBroadcastTwoStepSignKey(source any, key ed25519.PrivateKey) error {
	sourceKey, ok := source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("unsupported source type %s", reflect.TypeOf(source).String())
	}
	if !sourceKey.Key.Equal(key.Public()) {
		return fmt.Errorf("incorrect private key")
	}
	return nil
}

func (t *BroadcastTwoStepSimple) CalcID() ([]byte, error) {
	if uint64(len(t.Data)) > math.MaxUint32 {
		return nil, fmt.Errorf("data is too large")
	}

	dataHash := calcBroadcastTwoStepDataHash(t.Data)
	dataSize := uint32(len(t.Data))
	return calcBroadcastTwoStepID(t.Source, t.Flags, t.Date, t.SourceADNL, dataHash, dataSize, dataSize, t.Extra)
}

func (t *BroadcastTwoStepSimple) ToSign() (*BroadcastTwoStepSimpleToSign, error) {
	id, err := t.CalcID()
	if err != nil {
		return nil, err
	}
	return &BroadcastTwoStepSimpleToSign{
		ID:   id,
		Data: t.Data,
	}, nil
}

func (t *BroadcastTwoStepSimple) VerifySignature() error {
	id, err := t.CalcID()
	if err != nil {
		return err
	}
	return verifyBroadcastTwoStepSimpleSignature(t.Source, id, t.Data, t.Signature)
}

func (t *BroadcastTwoStepSimple) Sign(key ed25519.PrivateKey) error {
	if err := checkBroadcastTwoStepSignKey(t.Source, key); err != nil {
		return err
	}

	id, err := t.CalcID()
	if err != nil {
		return err
	}
	t.Signature, err = signBroadcastTwoStepSimple(key, id, t.Data)
	return nil
}

func (t *BroadcastTwoStepFEC) CalcID() ([]byte, error) {
	if uint64(len(t.Part)) > math.MaxUint32 {
		return nil, fmt.Errorf("part is too large")
	}
	return calcBroadcastTwoStepID(t.Source, t.Flags, t.Date, t.SourceADNL, t.DataHash, t.DataSize, uint32(len(t.Part)), t.Extra)
}

func (t *BroadcastTwoStepFEC) ToSign() (*BroadcastTwoStepFECToSign, error) {
	id, err := t.CalcID()
	if err != nil {
		return nil, err
	}
	return &BroadcastTwoStepFECToSign{
		ID:    id,
		Seqno: t.Seqno,
		Part:  t.Part,
	}, nil
}

func (t *BroadcastTwoStepFEC) VerifySignature() error {
	id, err := t.CalcID()
	if err != nil {
		return err
	}
	return verifyBroadcastTwoStepFECSignature(t.Source, id, t.Seqno, t.Part, t.Signature)
}

func (t *BroadcastTwoStepFEC) Sign(key ed25519.PrivateKey) error {
	if err := checkBroadcastTwoStepSignKey(t.Source, key); err != nil {
		return err
	}

	id, err := t.CalcID()
	if err != nil {
		return err
	}
	t.Signature, err = signBroadcastTwoStepFEC(key, id, t.Seqno, t.Part)
	return nil
}

func broadcastTwoStepPartFingerprint(id, sourceKey []byte, seqno uint32, part, signature []byte) [32]byte {
	hash := sha256.New()
	hash.Write(id)
	hash.Write(sourceKey)
	var sequence [4]byte
	binary.LittleEndian.PutUint32(sequence[:], seqno)
	hash.Write(sequence[:])
	hash.Write(part)
	hash.Write(signature)
	var result [32]byte
	hash.Sum(result[:0])
	return result
}
