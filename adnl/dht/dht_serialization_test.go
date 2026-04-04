package dht

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/keys"
	"github.com/xssnick/tonutils-go/tl"
)

func mustTestValue(t *testing.T) Value {
	t.Helper()

	seed, err := hex.DecodeString("2222222222222222222222222222222222222222222222222222222222222222")
	if err != nil {
		t.Fatal(err)
	}

	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	id := keys.PublicKeyED25519{Key: pub}

	idKey, err := tl.Hash(id)
	if err != nil {
		t.Fatal(err)
	}

	val := Value{
		KeyDescription: KeyDescription{
			Key: Key{
				ID:    idKey,
				Name:  []byte("interop-test"),
				Index: 0,
			},
			ID:         id,
			UpdateRule: UpdateRuleSignature{},
		},
		Data: []byte("hello-from-cpp"),
		TTL:  int32(time.Now().Add(5 * time.Minute).Unix()),
	}

	descSig, err := tl.Serialize(KeyDescription{
		Key:        val.KeyDescription.Key,
		ID:         val.KeyDescription.ID,
		UpdateRule: val.KeyDescription.UpdateRule,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	val.KeyDescription.Signature = ed25519.Sign(priv, descSig)

	valueSig, err := tl.Serialize(Value{
		KeyDescription: val.KeyDescription,
		Data:           val.Data,
		TTL:            val.TTL,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	val.Signature = ed25519.Sign(priv, valueSig)

	return val
}

func TestValueFoundResultSerializesNestedValueBoxed(t *testing.T) {
	val := mustTestValue(t)

	got, err := tl.Serialize(ValueFoundResult{Value: val}, true)
	if err != nil {
		t.Fatal(err)
	}

	wantValue, err := tl.Serialize(val, true)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) < 4 {
		t.Fatal("serialized result is too short")
	}

	if !bytes.Equal(got[4:], wantValue) {
		t.Fatalf("nested dht.value must be boxed inside dht.valueFound")
	}
}

func TestValueFoundResultMatchesCPPReferenceBytes(t *testing.T) {
	seed, err := hex.DecodeString("2222222222222222222222222222222222222222222222222222222222222222")
	if err != nil {
		t.Fatal(err)
	}

	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	id := keys.PublicKeyED25519{Key: pub}

	idKey, err := tl.Hash(id)
	if err != nil {
		t.Fatal(err)
	}

	val := Value{
		KeyDescription: KeyDescription{
			Key: Key{
				ID:    idKey,
				Name:  []byte("interop-test"),
				Index: 7,
			},
			ID:         id,
			UpdateRule: UpdateRuleSignature{},
		},
		Data: []byte("hello-from-cpp"),
		TTL:  1234567890,
	}

	descSig, err := tl.Serialize(KeyDescription{
		Key:        val.KeyDescription.Key,
		ID:         val.KeyDescription.ID,
		UpdateRule: val.KeyDescription.UpdateRule,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	val.KeyDescription.Signature = ed25519.Sign(priv, descSig)

	valueSig, err := tl.Serialize(Value{
		KeyDescription: val.KeyDescription,
		Data:           val.Data,
		TTL:            val.TTL,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	val.Signature = ed25519.Sign(priv, valueSig)

	got, err := tl.Serialize(ValueFoundResult{Value: val}, true)
	if err != nil {
		t.Fatal(err)
	}

	want, err := hex.DecodeString("74f70ce4cb27ad90241c2d6d00b9d9d5a0e321dc955c1ef0be909991becba314f06cb896662fed140c696e7465726f702d7465737400000007000000c6b41348a09aa5f47a6759802ff955f8dc2d2a14a5c99d23be97f864127ff9383455a4f0f7319fcc406b3e72debecf49c8e664e737b8614b093d438ba0640b6ae264859f73346da919cef8b408834c598bee23fc076afcd2b707094359b260c9b16631b5836af7c8050000000e68656c6c6f2d66726f6d2d63707000d202964940f75760f93c39d1c77c7905e43d28272361aaabadf446fe7ef1989d7033eaefc579b4b665600e805ed8ce87f09207b6d34556e885e18c7ab347bfad8a690f910b000000")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("go bytes differ from C++ reference\nwant: %x\ngot:  %x", want, got)
	}
}

func TestStoreSerializesNestedValueBare(t *testing.T) {
	val := mustTestValue(t)

	got, err := tl.Serialize(Store{Value: &val}, true)
	if err != nil {
		t.Fatal(err)
	}

	wantValue, err := tl.Serialize(val, false)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) < 4 {
		t.Fatal("serialized store is too short")
	}

	if !bytes.Equal(got[4:], wantValue) {
		t.Fatalf("nested dht.value must be bare inside dht.store")
	}
}
