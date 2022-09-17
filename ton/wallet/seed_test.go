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
