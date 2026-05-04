package overlay

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"reflect"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

func broadcastFECSourceID(source any, flags int32) ([]byte, error) {
	src := make([]byte, 32)
	if flags&BroadcastFlagAnySender != 0 {
		return src, nil
	}

	src, err := tl.Hash(source)
	if err != nil {
		return nil, fmt.Errorf("failed to compute source key id: %w", err)
	}
	return src, nil
}

func calcBroadcastFECID(source any, flags int32, dataHash []byte, dataSize uint32, fec any) ([]byte, error) {
	typeID, err := tl.Hash(fec)
	if err != nil {
		return nil, fmt.Errorf("failed to compute fec type id: %w", err)
	}

	src, err := broadcastFECSourceID(source, flags)
	if err != nil {
		return nil, err
	}

	broadcastHash, err := tl.Hash(&BroadcastFECID{
		Source:   src,
		Type:     typeID,
		DataHash: dataHash,
		Size:     dataSize,
		Flags:    flags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compute hash id of the broadcast: %w", err)
	}
	return broadcastHash, nil
}

func calcBroadcastFECPartID(broadcastHash, partDataHash []byte, seqno uint32) ([]byte, error) {
	partHash, err := tl.Hash(&BroadcastFECPartID{
		BroadcastHash: broadcastHash,
		DataHash:      partDataHash,
		Seqno:         seqno,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compute hash id of the part: %w", err)
	}
	return partHash, nil
}

func calcBroadcastFECPartData(broadcastHash, data []byte, seqno uint32) (partHash []byte, partDataHash []byte, err error) {
	partDataHash = calcBroadcastFECPartDataHash(data)
	partHash, err = calcBroadcastFECPartID(broadcastHash, partDataHash, seqno)
	if err != nil {
		return nil, nil, err
	}
	return partHash, partDataHash, nil
}

func serializeBroadcastFECToSign(partHash []byte, date uint32) ([]byte, error) {
	toSign, err := tl.Serialize(&BroadcastToSign{
		Hash: partHash,
		Date: date,
	}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize broadcast for sign check: %w", err)
	}
	return toSign, nil
}

func signBroadcastFECPart(key ed25519.PrivateKey, partHash []byte, date uint32) ([]byte, error) {
	toSign, err := serializeBroadcastFECToSign(partHash, date)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(key, toSign), nil
}

func verifyBroadcastFECPartSignature(source any, partHash []byte, date uint32, signature []byte) error {
	sourceKey, ok := source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("invalid signer key format")
	}

	toSign, err := serializeBroadcastFECToSign(partHash, date)
	if err != nil {
		return err
	}

	if !ed25519.Verify(sourceKey.Key, toSign, signature) {
		return fmt.Errorf("invalid broadcast signature")
	}
	return nil
}

func calcBroadcastFECPartDataHash(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func (t *BroadcastFEC) PartDataHash() []byte {
	return calcBroadcastFECPartDataHash(t.Data)
}

func (t *BroadcastFEC) CalcPartID() ([]byte, error) {
	broadcastHash, err := t.CalcID()
	if err != nil {
		return nil, err
	}
	return calcBroadcastFECPartID(broadcastHash, t.PartDataHash(), t.Seqno)
}

func (t *BroadcastFEC) ToSign() (*BroadcastToSign, error) {
	partHash, err := t.CalcPartID()
	if err != nil {
		return nil, err
	}
	return &BroadcastToSign{
		Hash: partHash,
		Date: t.Date,
	}, nil
}

func (t *BroadcastFEC) VerifySignature() error {
	broadcastHash, err := t.CalcID()
	if err != nil {
		return err
	}
	partHash, _, err := calcBroadcastFECPartData(broadcastHash, t.Data, t.Seqno)
	if err != nil {
		return err
	}
	return verifyBroadcastFECPartSignature(t.Source, partHash, t.Date, t.Signature)
}

func (t *BroadcastFEC) Sign(key ed25519.PrivateKey) error {
	source, ok := t.Source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("unsupported source type %s", reflect.TypeOf(t.Source).String())
	}

	if !source.Key.Equal(key.Public()) {
		return fmt.Errorf("incorrect private key")
	}

	broadcastHash, err := t.CalcID()
	if err != nil {
		return err
	}
	partHash, _, err := calcBroadcastFECPartData(broadcastHash, t.Data, t.Seqno)
	if err != nil {
		return err
	}

	signature, err := signBroadcastFECPart(key, partHash, t.Date)
	if err != nil {
		return err
	}
	t.Signature = signature
	return nil
}

func (t *BroadcastFEC) Short() (*BroadcastFECShort, error) {
	broadcastHash, err := t.CalcID()
	if err != nil {
		return nil, err
	}
	_, partDataHash, err := calcBroadcastFECPartData(broadcastHash, t.Data, t.Seqno)
	if err != nil {
		return nil, err
	}

	return &BroadcastFECShort{
		Source:        t.Source,
		Certificate:   t.Certificate,
		BroadcastHash: broadcastHash,
		PartDataHash:  partDataHash,
		Seqno:         int32(t.Seqno),
		Signature:     append([]byte(nil), t.Signature...),
	}, nil
}

func (t *BroadcastFECShort) CalcPartID() ([]byte, error) {
	if t.Seqno < 0 {
		return nil, fmt.Errorf("invalid seqno")
	}
	return calcBroadcastFECPartID(t.BroadcastHash, t.PartDataHash, uint32(t.Seqno))
}

func (t *BroadcastFECShort) ToSign(date uint32) (*BroadcastToSign, error) {
	partHash, err := t.CalcPartID()
	if err != nil {
		return nil, err
	}
	return &BroadcastToSign{
		Hash: partHash,
		Date: date,
	}, nil
}

func (t *BroadcastFECShort) VerifySignature(date uint32) error {
	partHash, err := t.CalcPartID()
	if err != nil {
		return err
	}
	return verifyBroadcastFECPartSignature(t.Source, partHash, date, t.Signature)
}
