package wallet

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func TestNewSeedWithPassword(t *testing.T) {
	seed := NewSeedWithPassword("123")
	_, err := FromSeedWithPassword(nil, seed, "123", V3)
	if err != nil {
		t.Fatal(err)
	}

	_, err = FromSeedWithPassword(nil, seed, "1234", V3)
	if err == nil {
		t.Fatal("should be invalid 1", seed)
	}

	_, err = FromSeedWithPassword(nil, seed, "", V3)
	if err == nil {
		t.Fatal("should be invalid 2", seed)
	}

	_, err = FromSeedWithPassword(nil, []string{"birth", "core"}, "", V3)
	if err == nil {
		t.Fatal("should be invalid 3")
	}

	seed = NewSeed()
	seed[7] = "wat"
	_, err = FromSeedWithPassword(nil, seed, "", V3)
	if err == nil {
		t.Fatal("should be invalid 4", seed)
	}

	seedNoPass := NewSeed()

	_, err = FromSeed(nil, seedNoPass, V3)
	if err != nil {
		t.Fatal(err)
	}

	_, err = FromSeedWithPassword(nil, seedNoPass, "123", V3)
	if err == nil {
		t.Fatal("should be invalid 5", seedNoPass)
	}
}

func TestBIP39Create(t *testing.T) {
	seed := NewSeed()
	wallet, err := FromSeed(nil, seed, ConfigV5R1Final{
		NetworkGlobalID: TestnetGlobalID,
		Workchain:       0,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	addr := wallet.WalletAddress()

	// only test
	fmt.Println("ton wallet seed:", seed)
	fmt.Println("ton wallet mainnet Address:", addr.Copy().Testnet(false))
	fmt.Println("ton wallet testnet Address:", addr.Copy().Testnet(true))
	fmt.Println("ton wallet privateKey:", hex.EncodeToString(wallet.PrivateKey()))
	fmt.Println("ton wallet publicKey:", hex.EncodeToString(wallet.pubKey))
}

func TestBIP39Load(t *testing.T) {
	seed := strings.Split("awesome scale mansion decade will rail beyond pink into enrich flock before cream oval pottery priority acid onion burst salad police pyramid stick hawk", " ")
	w, err := FromSeed(nil, seed, ConfigV5R1Final{
		NetworkGlobalID: MainnetGlobalID,
		Workchain:       0,
	}, true)
	if err != nil {
		t.Fatal(err)
	}

	if w.WalletAddress().String() != "UQCdMgVv3MHurW103oa4tdsuP1a-wZmNE0ZweBlK_Iy7tK1o" {
		t.Fatal("wrong address", w.WalletAddress())
	}
}
