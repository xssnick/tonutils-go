package wallet

import (
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

func TestLedgerCompatibleSeedLoad(t *testing.T) {
	seed := strings.Split("prison fuel story response target drill domain fitness heavy mixed meat lend father kiwi before elite exile fee swing make alcohol journey volcano tobacco", " ")

	testCases := []struct {
		name            string
		version         VersionConfig
		expectedAddress string
		expectError     bool
	}{
		{
			name:            "V3",
			version:         V3,
			expectedAddress: "UQAnnVwSCsdM-Tukh4qxzSySbtts9HP3tOgR1oQ_bR9wTy39",
			expectError:     false,
		},
		{
			name:            "V3R1",
			version:         V3R1,
			expectedAddress: "UQADgAfAIcLrtL9V9EpIVhRyLtwzVq324g-PFKa4JMFPOGfP",
			expectError:     false,
		},
		{
			name:            "V3R2",
			version:         V3R2,
			expectedAddress: "UQAnnVwSCsdM-Tukh4qxzSySbtts9HP3tOgR1oQ_bR9wTy39",
			expectError:     false,
		},
		{
			name:            "V4R1",
			version:         V4R1,
			expectedAddress: "UQDK_RYsjuYzz88Oh0y7sVlwTGPU7P6RXi0Z2eiH0jr4mtJc",
			expectError:     false,
		},
		{
			name:            "V4R2",
			version:         V4R2,
			expectedAddress: "UQDNrm1gX7-Vn3_dF-CsUcBqxKG-xqnGqEtHv2opLn9kso_F",
			expectError:     false,
		},
		{
			name: "V5R1Beta testnet",
			version: ConfigV5R1Beta{
				NetworkGlobalID: TestnetGlobalID,
				Workchain:       0,
			},
			expectedAddress: "UQBm1gbcTyB-JUNmGMduP-BBWxmMr0C2zjTqMud099roBDLg",
			expectError:     false,
		},
		{
			name: "V5R1Beta mainnet",
			version: ConfigV5R1Beta{
				NetworkGlobalID: MainnetGlobalID,
				Workchain:       0,
			},
			expectedAddress: "UQAd8wZRJXRH2v2csZv-qHvDQvcPsVzRdSH0oZ4WUskk7hXr",
			expectError:     false,
		},
		{
			name: "V5R1Final testnet",
			version: ConfigV5R1Final{
				NetworkGlobalID: TestnetGlobalID,
				Workchain:       0,
			},
			expectedAddress: "UQCa2r5G2qvZkV0UobPVrAwaF1ykeETpJkUVbhtSOJdTd0ZV",
			expectError:     false,
		},
		{
			name: "V5R1Final mainnet",
			version: ConfigV5R1Final{
				NetworkGlobalID: MainnetGlobalID,
				Workchain:       0,
			},
			expectedAddress: "UQA_IG06Eebapl2jBgH_UX64VG8wb0DDmEI9jWhKWBji220F",
			expectError:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := FromSeedWithOptions(nil, seed, tc.version, WithLedger())

			if tc.expectError {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if w.WalletAddress().String() != tc.expectedAddress {
				t.Fatal("wrong address", w.WalletAddress())
			}
		})
	}
}
