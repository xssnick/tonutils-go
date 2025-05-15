package wallet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"strings"
	"time"
)

type TonConnectProof struct {
	Timestamp int64 `json:"timestamp"`
	Domain    struct {
		LengthBytes uint32 `json:"lengthBytes"`
		Value       string `json:"value"`
	} `json:"domain"`
	Signature []byte `json:"signature"`
	Payload   string `json:"payload"`
}

type TonConnectVerifier struct {
	domain   string
	ttlRange time.Duration
	client   ton.APIClientWrapped
}

func NewTonConnectVerifier(domain string, ttlRange time.Duration, client ton.APIClientWrapped) *TonConnectVerifier {
	return &TonConnectVerifier{
		domain:   domain,
		ttlRange: ttlRange,
		client:   client,
	}
}

func (v *TonConnectVerifier) VerifyProof(ctx context.Context, addr *address.Address, proof TonConnectProof, expectedPayload string, stateInit []byte) error {
	if !strings.EqualFold(proof.Domain.Value, v.domain) {
		return errors.New("invalid domain in proof")
	}

	now := timeNow()

	if skew := now.Sub(time.Unix(proof.Timestamp, 0)); skew > v.ttlRange || skew < -v.ttlRange {
		return errors.New("timestamp out of allowed range")
	}

	if proof.Payload != expectedPayload {
		return errors.New("invalid payload in proof")
	}

	msg, err := buildMessage(addr, proof)
	if err != nil {
		return err
	}

	msgHash := sha256.Sum256(msg)

	var full bytes.Buffer
	full.Write([]byte{0xff, 0xff})
	full.WriteString("ton-connect")
	full.Write(msgHash[:])
	fullHash := sha256.Sum256(full.Bytes())

	if len(proof.Signature) != ed25519.SignatureSize {
		return errors.New("signature length != 64")
	}

	pubKey, err := v.getPubKey(ctx, addr, stateInit)
	if err != nil {
		return fmt.Errorf("failed to get public key: %w", err)
	}

	if !ed25519.Verify(pubKey, fullHash[:], proof.Signature) {
		return errors.New("signature verification failed")
	}

	return nil
}

const tonProofPrefix = "ton-proof-item-v2/"

// utf8("ton-proof-item-v2/") ++ workchain(BE) ++ hash ++ domainLen(LE) ++ domain ++ timestamp(LE) ++ payload
func buildMessage(addr *address.Address, proof TonConnectProof) ([]byte, error) {
	var msg bytes.Buffer
	msg.WriteString(tonProofPrefix)

	if err := binary.Write(&msg, binary.BigEndian, addr.Workchain()); err != nil {
		return nil, err
	}
	msg.Write(addr.Data())

	if proof.Domain.LengthBytes > 2048 || len(proof.Domain.Value) > 2048 {
		return nil, errors.New("domain length too big")
	}

	if err := binary.Write(&msg, binary.LittleEndian, proof.Domain.LengthBytes); err != nil {
		return nil, err
	}
	msg.WriteString(proof.Domain.Value)

	if err := binary.Write(&msg, binary.LittleEndian, proof.Timestamp); err != nil {
		return nil, err
	}

	msg.WriteString(proof.Payload)
	return msg.Bytes(), nil
}

func (v *TonConnectVerifier) getPubKey(ctx context.Context, addr *address.Address, stateInit []byte) (ed25519.PublicKey, error) {
	key, err := GetPublicKey(ctx, v.client, addr)
	if err != nil {
		var cErr ton.ContractExecError
		if !errors.As(err, &cErr) || cErr.Code != ton.ErrCodeContractNotInitialized {
			return nil, fmt.Errorf("failed to get public key: %w", err)
		}

		if len(stateInit) == 0 {
			return nil, errors.New("wallet is not initialized and state init is empty")
		}

		siCell, err := cell.FromBOC(stateInit)
		if err != nil {
			return nil, fmt.Errorf("failed to parse state init boc: %w", err)
		}

		if !bytes.Equal(siCell.Hash(), addr.Data()) {
			return nil, errors.New("state init hash does not match address")
		}

		var si tlb.StateInit
		if err = tlb.LoadFromCell(&si, siCell.BeginParse()); err != nil {
			return nil, fmt.Errorf("failed to parse state init: %w", err)
		}

		if si.Code == nil || si.Data == nil {
			return nil, errors.New("state init has no code or data")
		}

		ver, ok := walletVersionByCodeHash[string(si.Code.Hash())]
		if !ok {
			return nil, errors.New("state init has unknown code")
		}

		key, err = ParsePubKeyFromData(ver, si.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key from code: %w", err)
		}
	}

	return key, nil
}
