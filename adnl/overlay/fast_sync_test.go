package overlay

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

type fastSyncWireGoldenCase struct {
	name     string
	value    any
	expected string
}

type fastSyncMemberCertificateValidationCase struct {
	name        string
	certificate MemberCertificate
	nodeID      []byte
	maxSlots    int32
	errorPart   string
}

type fastSyncNodeSignatureCase struct {
	name  string
	flags uint32
}

func TestFastSyncWireGolden(t *testing.T) {
	publicKey := keys.PublicKeyED25519{Key: bytes.Repeat([]byte{0x11}, ed25519.PublicKeySize)}
	memberCertificate := MemberCertificate{
		IssuedBy:  publicKey,
		Flags:     0x01020304,
		Slot:      -2,
		ExpireAt:  0x11223344,
		Signature: []byte{0xAA, 0xBB},
	}
	node := NodeV2{
		ID:          publicKey,
		Overlay:     bytes.Repeat([]byte{0x22}, 32),
		Flags:       0x01020304,
		Version:     0x11223344,
		Signature:   []byte{0xAA, 0xBB},
		Certificate: EmptyMemberCertificate{},
	}

	tests := []fastSyncWireGoldenCase{
		{
			name: "member certificate id",
			value: MemberCertificateID{
				Node:     bytes.Repeat([]byte{0x11}, 32),
				Flags:    0x01020304,
				Slot:     -2,
				ExpireAt: 0x11223344,
			},
			expected: "32dc872c" +
				strings.Repeat("11", 32) +
				"04030201" +
				"feffffff" +
				"44332211",
		},
		{
			name:  "member certificate",
			value: memberCertificate,
			expected: "598c00c2" +
				"c6b41348" + strings.Repeat("11", 32) +
				"04030201" +
				"feffffff" +
				"44332211" +
				"02aabb00",
		},
		{
			name:     "empty member certificate",
			value:    EmptyMemberCertificate{},
			expected: "e24124c0",
		},
		{
			name: "node to sign ex",
			value: NodeToSignEx{
				ID:      bytes.Repeat([]byte{0x11}, 32),
				Overlay: bytes.Repeat([]byte{0x22}, 32),
				Flags:   0x01020304,
				Version: 0x11223344,
			},
			expected: "e9677a84" +
				strings.Repeat("11", 32) +
				strings.Repeat("22", 32) +
				"04030201" +
				"44332211",
		},
		{
			name:  "node v2",
			value: node,
			expected: "a7ad9bbd" +
				"c6b41348" + strings.Repeat("11", 32) +
				strings.Repeat("22", 32) +
				"04030201" +
				"44332211" +
				"02aabb00" +
				"e24124c0",
		},
		{
			name:     "nodes v2",
			value:    NodesV2{Nodes: []NodeV2{}},
			expected: "421807e4" + "00000000",
		},
		{
			name:     "get random peers v2",
			value:    GetRandomPeersV2{Peers: NodesV2{Nodes: []NodeV2{}}},
			expected: "cc7e8ea5" + "00000000",
		},
		{
			name:     "message extra",
			value:    MessageExtra{},
			expected: "a5e8d82e" + "00000000",
		},
		{
			name: "message with extra",
			value: MessageWithExtra{
				Overlay: bytes.Repeat([]byte{0x44}, 32),
				Extra:   MessageExtra{},
			},
			expected: "3d2332a2" +
				strings.Repeat("44", 32) +
				"00000000",
		},
		{
			name: "query with extra",
			value: QueryWithExtra{
				Overlay: bytes.Repeat([]byte{0x44}, 32),
				Extra:   MessageExtra{},
			},
			expected: "e9c3ff94" +
				strings.Repeat("44", 32) +
				"00000000",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := tl.Serialize(test.value, true)
			if err != nil {
				t.Fatalf("serialize: %v", err)
			}

			if actual := hex.EncodeToString(encoded); actual != test.expected {
				t.Fatalf("serialized bytes = %s, want %s", actual, test.expected)
			}
		})
	}
}

func TestFastSyncWireRoundTripUsesValues(t *testing.T) {
	publicKey := keys.PublicKeyED25519{Key: bytes.Repeat([]byte{0x31}, ed25519.PublicKeySize)}
	certificate := MemberCertificate{
		IssuedBy:  publicKey,
		Flags:     7,
		Slot:      2,
		ExpireAt:  1_900_000_000,
		Signature: bytes.Repeat([]byte{0x41}, ed25519.SignatureSize),
	}
	node := NodeV2{
		ID:          publicKey,
		Overlay:     bytes.Repeat([]byte{0x51}, 32),
		Flags:       3,
		Version:     42,
		Signature:   bytes.Repeat([]byte{0x61}, ed25519.SignatureSize),
		Certificate: certificate,
	}

	decodedPeers := fastSyncRoundTrip(t, GetRandomPeersV2{
		Peers: NodesV2{Nodes: []NodeV2{node}},
	})
	if len(decodedPeers.Peers.Nodes) != 1 {
		t.Fatalf("decoded node count = %d, want 1", len(decodedPeers.Peers.Nodes))
	}
	if _, ok := decodedPeers.Peers.Nodes[0].ID.(keys.PublicKeyED25519); !ok {
		t.Fatalf("decoded node id type = %T, want keys.PublicKeyED25519 value", decodedPeers.Peers.Nodes[0].ID)
	}
	decodedCertificate, ok := decodedPeers.Peers.Nodes[0].Certificate.(MemberCertificate)
	if !ok {
		t.Fatalf(
			"decoded node certificate type = %T, want overlay.MemberCertificate value",
			decodedPeers.Peers.Nodes[0].Certificate,
		)
	}
	if _, ok := decodedCertificate.IssuedBy.(keys.PublicKeyED25519); !ok {
		t.Fatalf(
			"decoded certificate issuer type = %T, want keys.PublicKeyED25519 value",
			decodedCertificate.IssuedBy,
		)
	}

	decodedQuery := fastSyncRoundTrip(t, QueryWithExtra{
		Overlay: bytes.Repeat([]byte{0x71}, 32),
		Extra: MessageExtra{
			Flags:       1,
			Certificate: certificate,
		},
	})
	if _, ok := decodedQuery.Extra.Certificate.(MemberCertificate); !ok {
		t.Fatalf(
			"decoded query certificate type = %T, want overlay.MemberCertificate value",
			decodedQuery.Extra.Certificate,
		)
	}

	decodedMessage := fastSyncRoundTrip(t, MessageWithExtra{
		Overlay: bytes.Repeat([]byte{0x81}, 32),
		Extra: MessageExtra{
			Flags:       1,
			Certificate: EmptyMemberCertificate{},
		},
	})
	if _, ok := decodedMessage.Extra.Certificate.(EmptyMemberCertificate); !ok {
		t.Fatalf(
			"decoded message certificate type = %T, want overlay.EmptyMemberCertificate value",
			decodedMessage.Extra.Certificate,
		)
	}
}

func TestMemberCertificateValidation(t *testing.T) {
	issuerPublic, issuerPrivate := fastSyncKeyPair(0x11)
	nodeID := bytes.Repeat([]byte{0x22}, 32)
	now := time.Unix(1_800_000_000, 0)
	certificate := MemberCertificate{
		IssuedBy: issuerPublic,
		Flags:    5,
		Slot:     3,
		ExpireAt: int32(now.Add(time.Minute).Unix()),
	}
	certificate.Signature = fastSyncMemberCertificateSignature(t, certificate, nodeID, issuerPrivate)

	if err := certificate.Validate(nodeID, now, 4); err != nil {
		t.Fatalf("validate certificate: %v", err)
	}

	expectedIssuerID, err := tl.Hash(issuerPublic)
	if err != nil {
		t.Fatalf("hash issuer: %v", err)
	}
	actualIssuerID, err := certificate.IssuerID()
	if err != nil {
		t.Fatalf("issuer id: %v", err)
	}
	if !bytes.Equal(actualIssuerID, expectedIssuerID) {
		t.Fatalf("issuer id = %x, want %x", actualIssuerID, expectedIssuerID)
	}

	atGraceBoundary := certificate
	atGraceBoundary.ExpireAt = int32(now.Add(-memberCertificateExpiryGrace).Unix())
	if atGraceBoundary.IsExpired(now) {
		t.Fatal("certificate expired at the three-second grace boundary")
	}
	if !atGraceBoundary.IsExpired(now.Add(time.Nanosecond)) {
		t.Fatal("certificate remained valid after the three-second grace boundary")
	}

	tests := []fastSyncMemberCertificateValidationCase{
		{
			name:        "expired",
			certificate: fastSyncCertificateWithExpiry(certificate, int32(now.Add(-memberCertificateExpiryGrace-time.Second).Unix())),
			nodeID:      nodeID,
			maxSlots:    4,
			errorPart:   "expired",
		},
		{
			name:        "negative slot",
			certificate: fastSyncCertificateWithSlot(certificate, -1),
			nodeID:      nodeID,
			maxSlots:    4,
			errorPart:   "outside",
		},
		{
			name:        "slot equals limit",
			certificate: fastSyncCertificateWithSlot(certificate, 4),
			nodeID:      nodeID,
			maxSlots:    4,
			errorPart:   "outside",
		},
		{
			name:        "bad signature",
			certificate: fastSyncCertificateWithSignature(certificate, bytes.Repeat([]byte{0x99}, ed25519.SignatureSize)),
			nodeID:      nodeID,
			maxSlots:    4,
			errorPart:   "signature",
		},
		{
			name: "unsupported issuer",
			certificate: fastSyncCertificateWithIssuer(
				certificate,
				keys.PublicKeyAES{Key: bytes.Repeat([]byte{0x55}, 32)},
			),
			nodeID:    nodeID,
			maxSlots:  4,
			errorPart: "issuer type",
		},
		{
			name:        "short node id",
			certificate: certificate,
			nodeID:      nodeID[:31],
			maxSlots:    4,
			errorPart:   "node id size",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.certificate.Validate(test.nodeID, now, test.maxSlots)
			if err == nil || !strings.Contains(err.Error(), test.errorPart) {
				t.Fatalf("error = %v, want error containing %q", err, test.errorPart)
			}
		})
	}
}

func TestNodeV2Signature(t *testing.T) {
	publicKey, privateKey := fastSyncKeyPair(0x71)
	overlayID := bytes.Repeat([]byte{0x81}, 32)

	tests := []fastSyncNodeSignatureCase{
		{name: "legacy payload for zero flags", flags: 0},
		{name: "extended payload for nonzero flags", flags: 7},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := NodeV2{
				ID:          publicKey,
				Overlay:     overlayID,
				Flags:       test.flags,
				Version:     123,
				Certificate: EmptyMemberCertificate{},
			}
			if err := node.Sign(privateKey); err != nil {
				t.Fatalf("sign: %v", err)
			}
			if err := node.CheckSignature(); err != nil {
				t.Fatalf("check signature: %v", err)
			}

			shortID, err := tl.Hash(publicKey)
			if err != nil {
				t.Fatalf("hash public key: %v", err)
			}

			var toSign []byte
			if test.flags == 0 {
				toSign, err = tl.Serialize(NodeToSign{
					ID:      shortID,
					Overlay: overlayID,
					Version: node.Version,
				}, true)
			} else {
				toSign, err = tl.Serialize(NodeToSignEx{
					ID:      shortID,
					Overlay: overlayID,
					Flags:   test.flags,
					Version: node.Version,
				}, true)
			}
			if err != nil {
				t.Fatalf("serialize expected signature payload: %v", err)
			}

			expectedSignature := ed25519.Sign(privateKey, toSign)
			if !bytes.Equal(node.Signature, expectedSignature) {
				t.Fatalf("signature = %x, want %x", node.Signature, expectedSignature)
			}

			node.Version++
			if err := node.CheckSignature(); err == nil || !strings.Contains(err.Error(), "signature") {
				t.Fatalf("tampered node error = %v, want signature error", err)
			}
		})
	}

	_, wrongPrivateKey := fastSyncKeyPair(0x72)
	wrongKeyNode := NodeV2{
		ID:          publicKey,
		Overlay:     overlayID,
		Certificate: EmptyMemberCertificate{},
	}
	if err := wrongKeyNode.Sign(wrongPrivateKey); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong private key error = %v, want mismatch error", err)
	}

	unsupportedNode := NodeV2{
		ID:          keys.PublicKeyAES{Key: bytes.Repeat([]byte{0x91}, 32)},
		Overlay:     overlayID,
		Signature:   bytes.Repeat([]byte{0xA1}, ed25519.SignatureSize),
		Certificate: EmptyMemberCertificate{},
	}
	if err := unsupportedNode.CheckSignature(); err == nil || !strings.Contains(err.Error(), "id type") {
		t.Fatalf("unsupported id error = %v, want id type error", err)
	}
}

func fastSyncRoundTrip[T any](t *testing.T, value T) T {
	t.Helper()

	encoded, err := tl.Serialize(value, true)
	if err != nil {
		t.Fatalf("serialize %T: %v", value, err)
	}

	var decoded T
	trailing, err := tl.Parse(&decoded, encoded, true)
	if err != nil {
		t.Fatalf("parse %T: %v", value, err)
	}
	if len(trailing) != 0 {
		t.Fatalf("parse %T left %d trailing bytes", value, len(trailing))
	}

	withTrailing := append(bytes.Clone(encoded), 0xDE, 0xAD)
	var decodedWithTrailing T
	trailing, err = tl.Parse(&decodedWithTrailing, withTrailing, true)
	if err != nil {
		t.Fatalf("parse %T with trailing bytes: %v", value, err)
	}
	if !bytes.Equal(trailing, []byte{0xDE, 0xAD}) {
		t.Fatalf("parse %T trailing bytes = %x, want dead", value, trailing)
	}

	reencoded, err := tl.Serialize(decoded, true)
	if err != nil {
		t.Fatalf("re-serialize %T: %v", value, err)
	}
	if !bytes.Equal(reencoded, encoded) {
		t.Fatalf("round-trip %T bytes differ", value)
	}

	return decoded
}

func fastSyncKeyPair(seed byte) (keys.PublicKeyED25519, ed25519.PrivateKey) {
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	publicKey := privateKey.Public().(ed25519.PublicKey)
	return keys.PublicKeyED25519{Key: publicKey}, privateKey
}

func fastSyncMemberCertificateSignature(
	t *testing.T,
	certificate MemberCertificate,
	nodeID []byte,
	privateKey ed25519.PrivateKey,
) []byte {
	t.Helper()

	toSign, err := tl.Serialize(MemberCertificateID{
		Node:     nodeID,
		Flags:    certificate.Flags,
		Slot:     certificate.Slot,
		ExpireAt: certificate.ExpireAt,
	}, true)
	if err != nil {
		t.Fatalf("serialize member certificate id: %v", err)
	}

	return ed25519.Sign(privateKey, toSign)
}

func fastSyncCertificateWithExpiry(certificate MemberCertificate, expireAt int32) MemberCertificate {
	certificate.ExpireAt = expireAt
	return certificate
}

func fastSyncCertificateWithSlot(certificate MemberCertificate, slot int32) MemberCertificate {
	certificate.Slot = slot
	return certificate
}

func fastSyncCertificateWithSignature(certificate MemberCertificate, signature []byte) MemberCertificate {
	certificate.Signature = signature
	return certificate
}

func fastSyncCertificateWithIssuer(certificate MemberCertificate, issuer any) MemberCertificate {
	certificate.IssuedBy = issuer
	return certificate
}
