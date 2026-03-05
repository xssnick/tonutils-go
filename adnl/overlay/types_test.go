package overlay

import (
	"bytes"
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/adnl/rldp"
	"github.com/xssnick/tonutils-go/tl"
)

func keyPairFromSeed(seedByte byte) (ed25519.PublicKey, ed25519.PrivateKey) {
	seed := bytes.Repeat([]byte{seedByte}, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	return priv.Public().(ed25519.PublicKey), priv
}

func mustSignTL(t *testing.T, obj any, priv ed25519.PrivateKey) []byte {
	t.Helper()
	raw, err := tl.Serialize(obj, true)
	if err != nil {
		t.Fatalf("serialize for signature failed: %v", err)
	}
	return ed25519.Sign(priv, raw)
}

func TestCertificateCheck(t *testing.T) {
	issuerPub, issuerPriv := keyPairFromSeed(1)
	overlayID := bytes.Repeat([]byte{0x55}, 32)
	issuedToID := bytes.Repeat([]byte{0x44}, 32)
	exp := uint32(time.Now().Add(time.Hour).Unix())

	sig := mustSignTL(t, CertificateId{
		OverlayID: overlayID,
		Node:      issuedToID,
		ExpireAt:  exp,
		MaxSize:   32,
	}, issuerPriv)

	cert := Certificate{
		IssuedBy:  keys.PublicKeyED25519{Key: issuerPub},
		ExpireAt:  exp,
		MaxSize:   32,
		Signature: sig,
	}

	res, err := cert.Check(issuedToID, overlayID, 10, false)
	if err != nil || res != CertCheckResultTrusted {
		t.Fatalf("expected trusted cert, got res=%v err=%v", res, err)
	}

	res, err = cert.Check(issuedToID, overlayID, 33, false)
	if err != nil || res != CertCheckResultForbidden {
		t.Fatalf("expected forbidden on oversize, got res=%v err=%v", res, err)
	}

	expired := cert
	expired.ExpireAt = uint32(time.Now().Add(-time.Second).Unix())
	res, err = expired.Check(issuedToID, overlayID, 1, false)
	if err != nil || res != CertCheckResultForbidden {
		t.Fatalf("expected forbidden on expired cert, got res=%v err=%v", res, err)
	}

	unsupported := cert
	unsupported.IssuedBy = keys.PublicKeyAES{Key: bytes.Repeat([]byte{1}, 32)}
	res, err = unsupported.Check(issuedToID, overlayID, 1, false)
	if err == nil || !strings.Contains(err.Error(), "unsupported issuer key format") || res != CertCheckResultForbidden {
		t.Fatalf("expected unsupported issuer error, got res=%v err=%v", res, err)
	}

	broken := cert
	broken.Signature = bytes.Repeat([]byte{0x99}, ed25519.SignatureSize)
	res, err = broken.Check(issuedToID, overlayID, 1, false)
	if err == nil || !strings.Contains(err.Error(), "incorrect cert signature") || res != CertCheckResultForbidden {
		t.Fatalf("expected incorrect signature error, got res=%v err=%v", res, err)
	}
}

func TestCertificateV2Check(t *testing.T) {
	issuerPub, issuerPriv := keyPairFromSeed(2)
	overlayID := bytes.Repeat([]byte{0x77}, 32)
	issuedToID := bytes.Repeat([]byte{0x88}, 32)
	exp := uint32(time.Now().Add(time.Hour).Unix())

	buildCert := func(flags int32) CertificateV2 {
		return CertificateV2{
			IssuedBy: keys.PublicKeyED25519{Key: issuerPub},
			ExpireAt: exp,
			MaxSize:  16,
			Flags:    flags,
			Signature: mustSignTL(t, CertificateIdV2{
				OverlayID: overlayID,
				Node:      issuedToID,
				ExpireAt:  exp,
				MaxSize:   16,
				Flags:     flags,
			}, issuerPriv),
		}
	}

	cert := buildCert(0)
	res, err := cert.Check(issuedToID, overlayID, 8, false)
	if err != nil || res != CertCheckResultNeedCheck {
		t.Fatalf("expected need-check cert, got res=%v err=%v", res, err)
	}

	res, err = cert.Check(issuedToID, overlayID, 8, true)
	if err != nil || res != CertCheckResultForbidden {
		t.Fatalf("expected forbidden fec for flag-less cert, got res=%v err=%v", res, err)
	}

	trusted := buildCert(_CertFlagTrusted | _CertFlagAllowFEC)
	res, err = trusted.Check(issuedToID, overlayID, 8, true)
	if err != nil || res != CertCheckResultTrusted {
		t.Fatalf("expected trusted cert, got res=%v err=%v", res, err)
	}

	broken := trusted
	broken.Signature = bytes.Repeat([]byte{0xAB}, ed25519.SignatureSize)
	res, err = broken.Check(issuedToID, overlayID, 8, false)
	if err == nil || !strings.Contains(err.Error(), "incorrect cert signature") || res != CertCheckResultForbidden {
		t.Fatalf("expected incorrect signature error, got res=%v err=%v", res, err)
	}
}

func TestBroadcastFECCalcID(t *testing.T) {
	pub1, _ := keyPairFromSeed(11)
	pub2, _ := keyPairFromSeed(12)

	base := BroadcastFEC{
		Source:   keys.PublicKeyED25519{Key: pub1},
		DataHash: bytes.Repeat([]byte{0xAF}, 32),
		DataSize: 128,
		Flags:    0,
		FEC:      rldp.FECRaptorQ{DataSize: 128, SymbolSize: 32, SymbolsCount: 8},
	}

	id1, err := base.CalcID()
	if err != nil {
		t.Fatalf("calc id failed: %v", err)
	}

	base.Source = keys.PublicKeyED25519{Key: pub2}
	id2, err := base.CalcID()
	if err != nil {
		t.Fatalf("calc id with second source failed: %v", err)
	}
	if bytes.Equal(id1, id2) {
		t.Fatalf("expected different hash for different sources")
	}

	base.Flags = _BroadcastFlagAnySender
	base.Source = keys.PublicKeyED25519{Key: pub1}
	id3, err := base.CalcID()
	if err != nil {
		t.Fatalf("calc id any-sender failed: %v", err)
	}

	base.Source = keys.PublicKeyED25519{Key: pub2}
	id4, err := base.CalcID()
	if err != nil {
		t.Fatalf("calc id any-sender with second source failed: %v", err)
	}
	if !bytes.Equal(id3, id4) {
		t.Fatalf("expected same hash when any-sender flag is set")
	}
}

func TestNodeSignCheckAndNewNode(t *testing.T) {
	pub, priv := keyPairFromSeed(21)
	overlayID := bytes.Repeat([]byte{0x31}, 32)

	n := &Node{ID: keys.PublicKeyED25519{Key: pub}, Overlay: overlayID, Version: 7}
	if err := n.Sign(priv); err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	if err := n.CheckSignature(); err != nil {
		t.Fatalf("signature check failed: %v", err)
	}

	n.Signature[0] ^= 0xFF
	if err := n.CheckSignature(); err == nil || !strings.Contains(err.Error(), "bad signature") {
		t.Fatalf("expected bad signature, got err=%v", err)
	}

	otherPub, otherPriv := keyPairFromSeed(22)
	n2 := &Node{ID: keys.PublicKeyED25519{Key: otherPub}, Overlay: overlayID, Version: 1}
	if err := n2.Sign(priv); err == nil || !strings.Contains(err.Error(), "incorrect private key") {
		t.Fatalf("expected incorrect private key error, got: %v", err)
	}

	n3 := &Node{ID: keys.PublicKeyAES{Key: bytes.Repeat([]byte{1}, 32)}, Overlay: overlayID, Version: 1}
	if err := n3.CheckSignature(); err == nil || !strings.Contains(err.Error(), "unsupported id type") {
		t.Fatalf("expected unsupported id type error, got: %v", err)
	}

	created, err := NewNode(overlayID, otherPriv)
	if err != nil {
		t.Fatalf("NewNode failed: %v", err)
	}
	if err = created.CheckSignature(); err != nil {
		t.Fatalf("new node signature must be valid: %v", err)
	}

	expectedOverlay, err := tl.Hash(keys.PublicKeyOverlay{Key: overlayID})
	if err != nil {
		t.Fatalf("hash overlay failed: %v", err)
	}
	if !bytes.Equal(created.Overlay, expectedOverlay) {
		t.Fatalf("unexpected overlay hash in new node")
	}
}
