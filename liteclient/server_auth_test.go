package liteclient

import (
	"context"
	"crypto/ed25519"
	"testing"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

func TestServerAuthenticatesTrustedClient(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	trustedID := serverAuthTestKeyID(t, publicKey)
	server := NewServerWithTrustedClients(nil, [][32]byte{trustedID})
	client := newServerAuthTestClient()
	if !client.beginAuthentication([]byte("client nonce")) {
		t.Fatal("authentication challenge was rejected")
	}
	releaseAuthTestChallenge(client)

	complete := TCPAuthenticationComplete{
		PublicKey: keys.PublicKeyED25519{Key: publicKey},
		Signature: ed25519.Sign(privateKey, client.authPayload),
	}
	if !server.completeAuthentication(client, complete) {
		t.Fatal("trusted client was rejected")
	}

	wantID := trustedID
	gotID, authenticated := client.ClientID()
	if !authenticated {
		t.Fatal("client is not marked authenticated")
	}
	if gotID != wantID {
		t.Fatalf("unexpected client id %x", gotID)
	}
}

func TestServerRejectsClientOutsideAllowlist(t *testing.T) {
	trustedPublicKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	trustedID := serverAuthTestKeyID(t, trustedPublicKey)
	server := NewServerWithTrustedClients(nil, [][32]byte{trustedID})
	client := newServerAuthTestClient()
	if !client.beginAuthentication([]byte("client nonce")) {
		t.Fatal("authentication challenge was rejected")
	}
	releaseAuthTestChallenge(client)

	complete := TCPAuthenticationComplete{
		PublicKey: keys.PublicKeyED25519{Key: publicKey},
		Signature: ed25519.Sign(privateKey, client.authPayload),
	}
	if server.completeAuthentication(client, complete) {
		t.Fatal("client outside the allowlist was accepted")
	}
	if _, authenticated := client.ClientID(); authenticated {
		t.Fatal("rejected client is marked authenticated")
	}
}

func TestServerRejectsInvalidClientSignature(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, otherPrivateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	trustedID := serverAuthTestKeyID(t, publicKey)
	server := NewServerWithTrustedClients(nil, [][32]byte{trustedID})
	client := newServerAuthTestClient()
	if !client.beginAuthentication([]byte("client nonce")) {
		t.Fatal("authentication challenge was rejected")
	}
	releaseAuthTestChallenge(client)

	complete := TCPAuthenticationComplete{
		PublicKey: keys.PublicKeyED25519{Key: publicKey},
		Signature: ed25519.Sign(otherPrivateKey, client.authPayload),
	}
	if server.completeAuthentication(client, complete) {
		t.Fatal("invalid signature was accepted")
	}
}

func newServerAuthTestClient() *ServerClient {
	return &ServerClient{
		ctx:       context.Background(),
		sendQueue: make(chan packetBuffer, 1),
	}
}

func releaseAuthTestChallenge(client *ServerClient) {
	packet := <-client.sendQueue
	packet.release()
}

func serverAuthTestKeyID(t *testing.T, publicKey ed25519.PublicKey) [32]byte {
	t.Helper()

	hash, err := tl.Hash(keys.PublicKeyED25519{Key: publicKey})
	if err != nil {
		t.Fatal(err)
	}

	var id [32]byte
	copy(id[:], hash)

	return id
}
