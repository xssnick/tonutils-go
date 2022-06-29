package tlb

import (
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type TransactionID struct {
	LT        uint64
	Hash      []byte
	AccountID []byte
}

type Transaction struct {
	_           Magic         `tlb:"$0111"`
	AccountAddr []byte        `tlb:"bits 256"`
	LT          uint64        `tlb:"## 64"`
	PrevTxHash  []byte        `tlb:"bits 256"`
	PrevTxLT    uint64        `tlb:"## 64"`
	Now         uint32        `tlb:"## 32"`
	OutMsgCount uint16        `tlb:"## 15"`
	OrigStatus  AccountStatus `tlb:"."`
	EndStatus   AccountStatus `tlb:"."`
	IO          struct {
		In  *Message   `tlb:"maybe ^"`
		Out []*Message `tlb:"maybe ^dict 15 -> array ^"`
	} `tlb:"^"`
	TotalFees   CurrencyCollection `tlb:"."`
	StateUpdate *cell.Cell         `tlb:"^"`
	Description *cell.Cell         `tlb:"^"`
}

func (t *Transaction) Dump() string {
	res := fmt.Sprintf("LT: %d\n\nInput:\nType %s\nFrom %s\nPayload:\n%s\n\nOutputs:\n", t.LT, t.IO.In.MsgType, t.IO.In.Msg.SenderAddr(), t.IO.In.Msg.Payload().Dump())
	for _, m := range t.IO.Out {
		res += m.AsInternal().Dump()
	}
	return res
}

func (t *Transaction) String() string {
	var destinations []string
	in, out := new(big.Int), new(big.Int)
	for _, m := range t.IO.Out {
		destinations = append(destinations, m.Msg.DestAddr().String())
		if m.MsgType == MsgTypeInternal {
			out.Add(out, m.AsInternal().Amount.NanoTON())
		}
	}

	if t.IO.In.MsgType == MsgTypeInternal {
		in = t.IO.In.AsInternal().Amount.NanoTON()
	}

	var build string

	if in.Cmp(big.NewInt(0)) != 0 {
		intTx := t.IO.In.AsInternal()
		build += fmt.Sprintf("LT: %d, In: %s TON, From %s, Comment: %s", t.LT, FromNanoTON(in).TON(), intTx.SrcAddr, intTx.Comment())
	}

	if out.Cmp(big.NewInt(0)) != 0 {
		if len(build) > 0 {
			build += ", "
		}
		build += fmt.Sprintf("Out: %s TON, To %s", FromNanoTON(out).TON(), destinations)
	}

	return build
}
