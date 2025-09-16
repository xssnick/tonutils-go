package toncenter

import (
	"encoding/json"
	"github.com/xssnick/tonutils-go/tlb"
	"math/big"
)

const TonDecimals = 9

type TransactionID struct {
	LT   uint64 `json:"lt,string"`
	Hash []byte `json:"hash"`
}

type BlockID struct {
	Workchain int32  `json:"workchain"`
	Shard     int64  `json:"shard,string"`
	Seqno     uint64 `json:"seqno"`
	RootHash  []byte `json:"root_hash"`
	FileHash  []byte `json:"file_hash"`
}

type NanoCoins struct {
	val *big.Int
}

func (n *NanoCoins) Coins(decimals int) (tlb.Coins, error) {
	return tlb.FromNano(n.val, decimals)
}

func (n *NanoCoins) MustCoins(decimals int) tlb.Coins {
	c, err := n.Coins(decimals)
	if err != nil {
		panic(err)
	}
	return c
}

func (n *NanoCoins) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	n.val = new(big.Int)
	return n.val.UnmarshalText([]byte(s))
}

func (n *NanoCoins) MarshalJSON() ([]byte, error) {
	return json.Marshal(n.val.String())
}
