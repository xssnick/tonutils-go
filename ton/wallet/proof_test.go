package wallet

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"github.com/xssnick/tonutils-go/address"
	"strings"
	"testing"
	"time"
)

const testData = `{"address":"0:960ab627408d5472d9d125b667cbe00ce17eeaa44e9dc6a86e93cdfef2c480d5","proof":{"payload":"1747303885:","signature":"OXNmXOa6Ml5mgx13CXVunl5TjQiIl2oYq8dn9MUcBiS6OjZcuJYccqqVMluy0duyOXkoJYHg4VJCyycwkjdJBA==","timestamp":1747303893,"domain":{"value":"ton-connect.github.io","lengthBytes":21}},"state_init":"te6cckECFgEAAwQAAgE0AgEAUQAAAAApqaMXp6kMOCJ4z0QdKdtKvLvoflXmJvj/gmw26izx4ImGnYtAART/APSkE/S88sgLAwIBIAgEBPjygwjXGCDTH9Mf0x8C+CO78mTtRNDTH9Mf0//0BNFRQ7ryoVFRuvKiBfkBVBBk+RDyo/gAJKTIyx9SQMsfUjDL/1IQ9ADJ7VT4DwHTByHAAJ9sUZMg10qW0wfUAvsA6DDgIcAB4wAhwALjAAHAA5Ew4w0DpMjLHxLLH8v/BwYQBQAK9ADJ7VQAcIEBCNcY+gDTP8hUIEeBAQj0UfKnghBub3RlcHSAGMjLBcsCUAbPFlAE+gIUy2oSyx/LP8lz+wACAG7SB/oA1NQi+QAFyMoHFcv/ydB3dIAYyMsFywIizxZQBfoCFMtrEszMyXP7AMhAFIEBCPRR8qcCAgFIDQkCASALCgBZvSQrb2omhAgKBrkPoCGEcNQICEekk30pkQzmkD6f+YN4EoAbeBAUiYcVnzGEAgEgEQwAEbjJftRNDXCx+ALm0AHQ0wMhcbCSXwTgItdJwSCSXwTgAtMfIYIQcGx1Z70ighBkc3RyvbCSXwXgA/pAMCD6RAHIygfL/8nQ7UTQgQFA1yH0BDBcgQEI9ApvoTGzkl8H4AXTP8glghBwbHVnupI4MOMNA4IQZHN0crqSXwbjDQ8OAIpQBIEBCPRZMO1E0IEBQNcgyAHPFvQAye1UAXKwjiOCEGRzdHKDHrFwgBhQBcsFUAPPFiP6AhPLassfyz/JgED7AJJfA+IAeAH6APQEMPgnbyIwUAqhIb7y4FCCEHBsdWeDHrFwgBhQBMsFJs8WWPoCGfQAy2kXyx9SYMs/IMmAQPsABgBsgQEI1xj6ANM/MFIkgQEI9Fnyp4IQZHN0cnB0gBjIywXLAlAFzxZQA/oCE8tqyx8Syz/Jc/sAAgFYFRICASAUEwAZrx32omhAEGuQ64WPwAAZrc52omhAIGuQ64X/wAA9sp37UTQgQFA1yH0BDACyMoHy//J0AGBAQj0Cm+hMYHrKDBA="}`

func TestTonConnectVerifier_VerifyProof(t *testing.T) {
	type TestData struct {
		Address   string          `json:"address"`
		Proof     TonConnectProof `json:"proof"`
		StateInit []byte          `json:"state_init"`
	}

	var data TestData
	err := json.Unmarshal([]byte(testData), &data)
	if err != nil {
		t.Fatalf("failed to unmarshal testData: %v", err)
	}

	previousTimeNow := timeNow
	t.Cleanup(func() { timeNow = previousTimeNow })
	timeNow = func() time.Time {
		return time.Unix(1747303900, 0)
	}

	vi := NewTonConnectVerifier("ton-connect.github.io", 30*time.Minute, nil)

	addr := address.MustParseRawAddr(data.Address)

	err = vi.VerifyProof(context.Background(), addr, data.Proof, "1747303885:", data.StateInit)
	if err != nil {
		t.Fatalf("failed to verify proof: %v", err)
	}

}

func TestTonConnectVerifier_CheckPayload(t *testing.T) {
	secret := "test_secret"

	// Test case: Valid payload
	t.Run("ValidPayload", func(t *testing.T) {
		ttl := 5 * time.Second
		payload, err := GeneratePayload(secret, ttl)
		if err != nil {
			t.Fatalf("GeneratePayload failed: %v", err)
		}

		err = CheckPayload(payload, secret)
		if err != nil {
			t.Errorf("CheckPayload failed for valid payload: %v", err)
		}
	})

	// Test case: Expired payload
	t.Run("ExpiredPayload", func(t *testing.T) {
		ttl := -5 * time.Second // Already expired
		payload, err := GeneratePayload(secret, ttl)
		if err != nil {
			t.Fatalf("GeneratePayload failed: %v", err)
		}

		err = CheckPayload(payload, secret)
		if err == nil || err.Error() != "payload expired" {
			t.Errorf("Expected 'payload expired', got: %v", err)
		}
	})

	// Test case: Tampered payload
	t.Run("TamperedPayload", func(t *testing.T) {
		ttl := 5 * time.Second
		payload, err := GeneratePayload(secret, ttl)
		if err != nil {
			t.Fatalf("GeneratePayload failed: %v", err)
		}

		// Tamper with the payload
		last := byte('0')
		if payload[len(payload)-1] == last {
			last = '1'
		}
		tamperedPayload := payload[:len(payload)-1] + string(last)

		err = CheckPayload(tamperedPayload, secret)
		if err == nil || err.Error() != "invalid payload signature" {
			t.Errorf("Expected 'invalid payload signature', got: %v", err)
		}
	})

	// Test case: Invalid payload length
	t.Run("InvalidPayloadLength", func(t *testing.T) {
		invalidPayload := "abcd" // Too short to be valid

		err := CheckPayload(invalidPayload, secret)
		if err == nil || err.Error() != "invalid payload length" {
			t.Errorf("Expected 'invalid payload length', got: %v", err)
		}
	})
}

func TestTonConnectVerifier_ProofValidation(t *testing.T) {
	previousTimeNow := timeNow
	t.Cleanup(func() { timeNow = previousTimeNow })
	now := time.Unix(1747303900, 0)
	timeNow = func() time.Time { return now }

	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	si, err := GetStateInit(key.Public().(ed25519.PublicKey), V4R2, DefaultSubwallet)
	if err != nil {
		t.Fatal(err)
	}
	addr, stateInit := proofTestStateInit(t, si.Code, si.Data)
	callbackErr := errors.New("challenge is not available")

	tests := []struct {
		name          string
		change        func(*TonConnectProof)
		domain        string
		invalidInit   bool
		corruptSign   bool
		callbackErr   error
		wantErr       string
		wantCallbacks int
	}{
		{name: "valid", wantCallbacks: 1},
		{name: "UTF8 domain", domain: "тест.example", change: func(p *TonConnectProof) {
			p.Domain.Value = "тест.example"
			p.Domain.LengthBytes = uint32(len(p.Domain.Value))
		}, wantCallbacks: 1},
		{name: "domain case mismatch", change: func(p *TonConnectProof) { p.Domain.Value = "Example.com" },
			wantErr: "invalid domain"},
		{name: "zero domain length", change: func(p *TonConnectProof) { p.Domain.LengthBytes = 0 },
			wantErr: "domain length does not match"},
		{name: "short domain length", change: func(p *TonConnectProof) { p.Domain.LengthBytes-- },
			wantErr: "domain length does not match"},
		{name: "long domain length", change: func(p *TonConnectProof) { p.Domain.LengthBytes++ },
			wantErr: "domain length does not match"},
		{name: "UTF8 character count", domain: "тест.example", change: func(p *TonConnectProof) {
			p.Domain.Value = "тест.example"
			p.Domain.LengthBytes = uint32(len([]rune(p.Domain.Value)))
		}, wantErr: "domain length does not match"},
		{name: "domain length over limit", change: func(p *TonConnectProof) { p.Domain.LengthBytes = 2049 },
			wantErr: "domain length too big"},
		{name: "domain value over limit", domain: strings.Repeat("a", 2049), change: func(p *TonConnectProof) {
			p.Domain.Value = strings.Repeat("a", 2049)
			p.Domain.LengthBytes = uint32(len(p.Domain.Value))
		}, wantErr: "domain length too big"},
		{name: "expired", change: func(p *TonConnectProof) { p.Timestamp -= 61 }, wantErr: "timestamp"},
		{name: "future", change: func(p *TonConnectProof) { p.Timestamp += 61 }, wantErr: "timestamp"},
		{name: "past boundary", change: func(p *TonConnectProof) { p.Timestamp -= 60 }, wantCallbacks: 1},
		{name: "future boundary", change: func(p *TonConnectProof) { p.Timestamp += 60 }, wantCallbacks: 1},
		{name: "invalid signature", corruptSign: true, wantErr: "signature verification failed"},
		{name: "invalid StateInit", invalidInit: true, wantErr: "failed to parse state init boc"},
		{name: "callback error", callbackErr: callbackErr, wantErr: "payload check failed", wantCallbacks: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proof := proofTestSign(addr, key)
			if tt.change != nil {
				tt.change(&proof)
				proof = proofTestSignMessage(addr, key, proof)
			}
			if tt.corruptSign {
				proof.Signature[0] ^= 1
			}
			init := stateInit
			if tt.invalidInit {
				init = []byte{1, 2, 3}
			}
			domain := tt.domain
			if domain == "" {
				domain = "example.com"
			}
			verifier := NewTonConnectVerifier(domain, time.Minute, nil)
			calls := 0
			err := verifier.VerifyProofHandlePayload(context.Background(), addr, proof, init,
				func(payload, secret string) error {
					calls++
					if payload != "challenge" || secret != "session secret" {
						t.Fatal("callback received incorrect payload or secret")
					}
					return tt.callbackErr
				}, "session secret")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("VerifyProofHandlePayload failed: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
			if tt.callbackErr != nil && !errors.Is(err, tt.callbackErr) {
				t.Fatalf("callback error was not preserved: %v", err)
			}
			if calls != tt.wantCallbacks {
				t.Fatalf("callback calls = %d, want %d", calls, tt.wantCallbacks)
			}
		})
	}

	verifier := NewTonConnectVerifier("example.com", time.Minute, nil)
	proof := proofTestSign(addr, key)
	t.Run("short signature", func(t *testing.T) {
		proof.Signature = proof.Signature[:ed25519.SignatureSize-1]
		err := verifier.VerifyProof(context.Background(), addr, proof, "challenge", stateInit)
		if err == nil || !strings.Contains(err.Error(), "signature length") {
			t.Fatalf("expected invalid signature length, got %v", err)
		}
	})
	t.Run("payload mismatch", func(t *testing.T) {
		err := verifier.VerifyProof(context.Background(), addr, proofTestSign(addr, key), "another challenge", stateInit)
		if err == nil || !strings.Contains(err.Error(), "invalid payload in proof") {
			t.Fatalf("expected payload mismatch, got %v", err)
		}
	})
}

func TestTonConnectVerifier_PayloadConsumedAfterVerification(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	si, err := GetStateInit(key.Public().(ed25519.PublicKey), V4R2, DefaultSubwallet)
	if err != nil {
		t.Fatal(err)
	}
	addr, stateInit := proofTestStateInit(t, si.Code, si.Data)
	verifier := NewTonConnectVerifier("example.com", time.Minute, nil)
	proof := proofTestSign(addr, key)
	used := false
	consumedErr := errors.New("challenge already consumed")
	consume := func(payload, secret string) error {
		if used {
			return consumedErr
		}
		used = true
		return nil
	}

	invalid := proof
	invalid.Signature = append([]byte(nil), proof.Signature...)
	invalid.Signature[0] ^= 1
	if err = verifier.VerifyProofHandlePayload(context.Background(), addr, invalid, stateInit, consume, ""); err == nil {
		t.Fatal("invalid signature was accepted")
	}
	if used {
		t.Fatal("invalid proof consumed the challenge")
	}
	if err = verifier.VerifyProofHandlePayload(context.Background(), addr, proof, stateInit, consume, ""); err != nil {
		t.Fatalf("valid proof failed: %v", err)
	}
	if err = verifier.VerifyProofHandlePayload(context.Background(), addr, proof, stateInit, consume, ""); !errors.Is(err, consumedErr) {
		t.Fatalf("expected rejection of consumed challenge, got %v", err)
	}
}

func TestTonConnectVerifier_CheckPayloadEncoding(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{name: "empty", wantErr: "invalid payload length"},
		{name: "odd length", payload: strings.Repeat("0", 127), wantErr: "invalid payload length"},
		{name: "too long", payload: strings.Repeat("0", 129), wantErr: "invalid payload length"},
		{name: "large malformed input", payload: strings.Repeat("z", 1<<20), wantErr: "invalid payload length"},
		{name: "invalid hex", payload: strings.Repeat("z", 128), wantErr: "invalid byte"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckPayload(tt.payload, "test secret")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestTonConnectVerifier_AddressValidation(t *testing.T) {
	tests := []struct {
		name string
		addr *address.Address
	}{
		{name: "none", addr: address.NewAddressNone()},
		{name: "external", addr: address.NewAddressExt(0, 256, make([]byte, 32))},
		{name: "variable", addr: address.NewAddressVar(0, 0, 256, make([]byte, 32))},
		{name: "short standard", addr: address.NewAddress(0, 0, make([]byte, 31))},
		{name: "long standard", addr: address.NewAddress(0, 0, make([]byte, 33))},
	}
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	verifier := NewTonConnectVerifier("example.com", time.Minute, nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifier.VerifyProof(context.Background(), tt.addr, proofTestSign(tt.addr, key), "challenge", nil)
			if err == nil || !strings.Contains(err.Error(), "standard 256-bit address") {
				t.Fatalf("expected invalid address, got %v", err)
			}
		})
	}
}
