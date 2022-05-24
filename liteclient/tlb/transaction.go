package tlb

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

type TxHash []byte

type Transaction struct {
	LT         uint64
	PrevTxLT   uint64
	PrevTxHash TxHash
	In         *Message
	Out        []*Message
}

func (t *Transaction) LoadFromCell(loader *cell.LoadCell) error {
	magic, err := loader.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load magic bits: %w", err)
	}

	if magic != 0b0111 {
		return errors.New("not a transaction")
	}

	_, err = loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load addr data: %w", err)
	}

	lt, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load lt: %w", err)
	}

	prevTxHash, err := loader.LoadSlice(256)
	if err != nil {
		return fmt.Errorf("failed to load prev tx hash: %w", err)
	}

	prevTxLT, err := loader.LoadUInt(64)
	if err != nil {
		return fmt.Errorf("failed to load prev tx lt: %w", err)
	}

	now, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load now: %w", err)
	}
	_ = now

	outMsgCnt, err := loader.LoadUInt(15)
	if err != nil {
		return fmt.Errorf("failed to load out msgs num: %w", err)
	}
	_ = outMsgCnt

	// TODO: load additional info, acc statuses

	existsInfo, err := loader.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load exists info ref: %w", err)
	}

	hasInMsg, err := existsInfo.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load has in msg bit: %w", err)
	}

	var inMsg *Message
	if hasInMsg {
		msg, err := existsInfo.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load in msg ref: %w", err)
		}

		inMsg = new(Message)
		err = inMsg.LoadFromCell(msg)
		if err != nil {
			return fmt.Errorf("failed to parse in msg: %w", err)
		}
	}

	hasOutMsgs, err := existsInfo.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load has out msg bit: %w", err)
	}

	var outMsgs []*Message
	if hasOutMsgs {
		hashRoot, err := existsInfo.LoadRef()
		if err != nil {
			return fmt.Errorf("failed to load out msgs container: %w", err)
		}

		outs, err := hashRoot.LoadDict(15)
		if err != nil {
			return fmt.Errorf("failed to parse out msgs hashmap: %w", err)
		}

		for _, kv := range outs.All() {
			ref, err := kv.Value.BeginParse().LoadRef()
			if err != nil {
				return fmt.Errorf("failed to parse out msg ref: %w", err)
			}

			msg := new(Message)
			err = msg.LoadFromCell(ref)
			if err != nil {
				return fmt.Errorf("failed to parse out msg: %w", err)
			}

			outMsgs = append(outMsgs, msg)
		}
	}

	*t = Transaction{
		LT:         lt,
		PrevTxLT:   prevTxLT,
		PrevTxHash: prevTxHash,
		In:         inMsg,
		Out:        outMsgs,
	}

	return nil
}

func (t *Transaction) Dump() string {
	res := fmt.Sprintf("LT: %d\n\nInput:\nType %s\nFrom %s\nPayload:\n%s\n\nOutputs:\n", t.LT, t.In.MsgType, t.In.SenderAddr(), t.In.Payload().Dump())
	for _, m := range t.Out {
		res += m.AsInternal().Dump()
	}
	return res
}

func (t *Transaction) String() string {
	var destinations []string
	in, out := new(big.Int), new(big.Int)
	for _, m := range t.Out {
		destinations = append(destinations, m.DestAddr().String())
		if m.MsgType == MsgTypeInternal {
			out.Add(out, m.AsInternal().Amount.NanoTON())
		}
	}

	if t.In.MsgType == MsgTypeInternal {
		in = t.In.AsInternal().Amount.NanoTON()
	}

	var build string

	if in.Cmp(big.NewInt(0)) != 0 {
		build += fmt.Sprintf("In: %s TON, From %s", Grams{in}.TON(), t.In.AsInternal().SrcAddr)
	}

	if out.Cmp(big.NewInt(0)) != 0 {
		if len(build) > 0 {
			build += ", "
		}
		build += fmt.Sprintf("Out: %s TON, To %s", Grams{out}.TON(), destinations)
	}

	return build
}
