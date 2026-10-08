package overlay

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"reflect"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

var (
	publicKeyED25519TLID   = tl.CRC("pub.ed25519 key:int256 = PublicKey")
	fecRaptorQTLID         = tl.CRC("fec.raptorQ data_size:int symbol_size:int symbols_count:int = fec.Type")
	fecRoundRobinTLID      = tl.CRC("fec.roundRobin data_size:int symbol_size:int symbols_count:int = fec.Type")
	fecOnlineTLID          = tl.CRC("fec.online data_size:int symbol_size:int symbols_count:int = fec.Type")
	broadcastIDTLID        = tl.CRC("overlay.broadcast.id src:int256 data_hash:int256 flags:int = overlay.broadcast.Id")
	broadcastFECIDTLID     = tl.CRC("overlay.broadcastFec.id src:int256 type:int256 data_hash:int256 size:int flags:int = overlay.broadcastFec.Id")
	broadcastFECPartIDTLID = tl.CRC("overlay.broadcastFec.partId broadcast_hash:int256 data_hash:int256 seqno:int = overlay.broadcastFec.PartId")
	broadcastToSignTLID    = tl.CRC("overlay.broadcast.toSign hash:int256 date:int = overlay.broadcast.ToSign")
)

func broadcastSourceID(source any, flags int32) ([32]byte, error) {
	var src [32]byte
	if flags&BroadcastFlagAnySender != 0 {
		return src, nil
	}

	var key ed25519.PublicKey
	switch value := source.(type) {
	case keys.PublicKeyED25519:
		key = value.Key
	case *keys.PublicKeyED25519:
		if value == nil {
			return src, fmt.Errorf("failed to compute source key id: invalid signer key format")
		}
		key = value.Key
	default:
		return src, fmt.Errorf("failed to compute source key id: invalid signer key format")
	}
	if len(key) != ed25519.PublicKeySize {
		return src, fmt.Errorf("failed to compute source key id: invalid public key")
	}

	var wire [4 + ed25519.PublicKeySize]byte
	binary.LittleEndian.PutUint32(wire[:4], publicKeyED25519TLID)
	copy(wire[4:], key)
	return sha256.Sum256(wire[:]), nil
}

func broadcastFECTypeID(fec any) ([32]byte, error) {
	var (
		typeID       uint32
		dataSize     uint32
		symbolSize   uint32
		symbolsCount uint32
	)
	switch value := fec.(type) {
	case rldp.FECRaptorQ:
		typeID, dataSize, symbolSize, symbolsCount = fecRaptorQTLID, value.DataSize, value.SymbolSize, value.SymbolsCount
	case *rldp.FECRaptorQ:
		if value == nil {
			return [32]byte{}, fmt.Errorf("failed to compute fec type id: unsupported fec type %T", fec)
		}
		typeID, dataSize, symbolSize, symbolsCount = fecRaptorQTLID, value.DataSize, value.SymbolSize, value.SymbolsCount
	case rldp.FECRoundRobin:
		typeID, dataSize, symbolSize, symbolsCount = fecRoundRobinTLID, value.DataSize, value.SymbolSize, value.SymbolsCount
	case *rldp.FECRoundRobin:
		if value == nil {
			return [32]byte{}, fmt.Errorf("failed to compute fec type id: unsupported fec type %T", fec)
		}
		typeID, dataSize, symbolSize, symbolsCount = fecRoundRobinTLID, value.DataSize, value.SymbolSize, value.SymbolsCount
	case rldp.FECOnline:
		typeID, dataSize, symbolSize, symbolsCount = fecOnlineTLID, value.DataSize, value.SymbolSize, value.SymbolsCount
	case *rldp.FECOnline:
		if value == nil {
			return [32]byte{}, fmt.Errorf("failed to compute fec type id: unsupported fec type %T", fec)
		}
		typeID, dataSize, symbolSize, symbolsCount = fecOnlineTLID, value.DataSize, value.SymbolSize, value.SymbolsCount
	default:
		return [32]byte{}, fmt.Errorf("failed to compute fec type id: unsupported fec type %T", fec)
	}

	var wire [16]byte
	binary.LittleEndian.PutUint32(wire[0:4], typeID)
	binary.LittleEndian.PutUint32(wire[4:8], dataSize)
	binary.LittleEndian.PutUint32(wire[8:12], symbolSize)
	binary.LittleEndian.PutUint32(wire[12:16], symbolsCount)
	return sha256.Sum256(wire[:]), nil
}

func calcBroadcastFECID(source any, flags int32, dataHash []byte, dataSize uint32, fec any) ([]byte, error) {
	if len(dataHash) != sha256.Size {
		return nil, fmt.Errorf("failed to compute hash id of the broadcast: data hash should be %d bytes", sha256.Size)
	}

	typeID, err := broadcastFECTypeID(fec)
	if err != nil {
		return nil, err
	}

	src, err := broadcastSourceID(source, flags)
	if err != nil {
		return nil, err
	}

	var wire [4 + 32 + 32 + 32 + 4 + 4]byte
	binary.LittleEndian.PutUint32(wire[0:4], broadcastFECIDTLID)
	copy(wire[4:36], src[:])
	copy(wire[36:68], typeID[:])
	copy(wire[68:100], dataHash)
	binary.LittleEndian.PutUint32(wire[100:104], dataSize)
	binary.LittleEndian.PutUint32(wire[104:108], uint32(flags))
	broadcastHash := sha256.Sum256(wire[:])
	return broadcastHash[:], nil
}

func calcBroadcastFECPartID(broadcastHash, partDataHash []byte, seqno uint32) ([]byte, error) {
	if len(broadcastHash) != sha256.Size || len(partDataHash) != sha256.Size {
		return nil, fmt.Errorf("failed to compute hash id of the part: hashes should be %d bytes", sha256.Size)
	}

	var wire [4 + 32 + 32 + 4]byte
	binary.LittleEndian.PutUint32(wire[0:4], broadcastFECPartIDTLID)
	copy(wire[4:36], broadcastHash)
	copy(wire[36:68], partDataHash)
	binary.LittleEndian.PutUint32(wire[68:72], seqno)
	partHash := sha256.Sum256(wire[:])
	return partHash[:], nil
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
	var wire [4 + 32 + 4]byte
	if err := fillBroadcastToSign(&wire, partHash, date); err != nil {
		return nil, err
	}
	return append([]byte(nil), wire[:]...), nil
}

func fillBroadcastToSign(wire *[4 + 32 + 4]byte, partHash []byte, date uint32) error {
	if len(partHash) != sha256.Size {
		return fmt.Errorf("failed to serialize broadcast for sign check: hash should be %d bytes", sha256.Size)
	}

	binary.LittleEndian.PutUint32(wire[0:4], broadcastToSignTLID)
	copy(wire[4:36], partHash)
	binary.LittleEndian.PutUint32(wire[36:40], date)
	return nil
}

func signBroadcastFECPart(key ed25519.PrivateKey, partHash []byte, date uint32) ([]byte, error) {
	var toSign [4 + 32 + 4]byte
	if err := fillBroadcastToSign(&toSign, partHash, date); err != nil {
		return nil, err
	}
	return ed25519.Sign(key, toSign[:]), nil
}

func verifyBroadcastFECPartSignature(source any, partHash []byte, date uint32, signature []byte) error {
	sourceKey, ok := source.(keys.PublicKeyED25519)
	if !ok {
		return fmt.Errorf("invalid signer key format")
	}

	var toSign [4 + 32 + 4]byte
	if err := fillBroadcastToSign(&toSign, partHash, date); err != nil {
		return err
	}

	if !ed25519.Verify(sourceKey.Key, toSign[:], signature) {
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
