package wallet

import (
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
		t.Fatal("should be invalid")
	}

	_, err = FromSeedWithPassword(nil, seed, "", V3)
	if err == nil {
		t.Fatal("should be invalid")
	}

	seedNoPass := NewSeed()

	_, err = FromSeed(nil, seedNoPass, V3)
	if err != nil {
		t.Fatal(err)
	}

	_, err = FromSeedWithPassword(nil, seedNoPass, "123", V3)
	if err == nil {
		t.Fatal("should be invalid")
	}
}
