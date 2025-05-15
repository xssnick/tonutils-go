package wallet

import (
	"context"
	"encoding/json"
	"github.com/xssnick/tonutils-go/address"
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

	timeNow = func() time.Time {
		return time.Unix(1747303900, 0)
	}

	vi := NewTonConnectVerifier("ton-connect.github.io", 30*time.Minute, api)

	addr := address.MustParseRawAddr(data.Address)

	err = vi.VerifyProof(context.Background(), addr, data.Proof, "1747303885:", data.StateInit)
	if err != nil {
		t.Fatalf("failed to verify proof: %v", err)
	}

}
