package wallet

import (
	"context"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/big"
)

// code hex from https://github.com/ton-blockchain/highload-wallet-contract-v3/commit/3d2843747b14bc2a8915606df736d47490cd3d49
const _HighloadV3CodeHex = "b5ee9c7241021001000228000114ff00f4a413f4bcf2c80b01020120020d02014803040078d020d74bc00101c060b0915be101d0d3030171b0915be0fa4030f828c705b39130e0d31f018210ae42e5a4ba9d8040d721d74cf82a01ed55fb04e030020120050a02027306070011adce76a2686b85ffc00201200809001aabb6ed44d0810122d721d70b3f0018aa3bed44d08307d721d70b1f0201200b0c001bb9a6eed44d0810162d721d70b15800e5b8bf2eda2edfb21ab09028409b0ed44d0810120d721f404f404d33fd315d1058e1bf82325a15210b99f326df82305aa0015a112b992306dde923033e2923033e25230800df40f6fa19ed021d721d70a00955f037fdb31e09130e259800df40f6fa19cd001d721d70a00937fdb31e0915be270801f6f2d48308d718d121f900ed44d0d3ffd31ff404f404d33fd315d1f82321a15220b98e12336df82324aa00a112b9926d32de58f82301de541675f910f2a106d0d31fd4d307d30cd309d33fd315d15168baf2a2515abaf2a6f8232aa15250bcf2a304f823bbf2a35304800df40f6fa199d024d721d70a00f2649130e20e01fe5309800df40f6fa18e13d05004d718d20001f264c858cf16cf8301cf168e1030c824cf40cf8384095005a1a514cf40e2f800c94039800df41704c8cbff13cb1ff40012f40012cb3f12cb15c9ed54f80f21d0d30001f265d3020171b0925f03e0fa4001d70b01c000f2a5fa4031fa0031f401fa0031fa00318060d721d300010f0020f265d2000193d431d19130e272b1fb00b585bf03"

type ConfigHighloadV3 struct {
	// MessageTTL must be > 5 and less than 1<<22
	MessageTTL uint32

	// This function wil be used to get query id and creation time for the new message.
	// ID can be iterator from your database, max id is 1<<23, when it is higher, start from 0 and repeat
	// MessageBuilder should be defined if you want to send transactions
	MessageBuilder func(ctx context.Context, subWalletId uint32) (id uint32, createdAt int64, err error)
}

type SpecHighloadV3 struct {
	wallet *Wallet

	config ConfigHighloadV3
}

func (s *SpecHighloadV3) BuildMessage(ctx context.Context, messages []*Message) (_ *cell.Cell, err error) {
	if s.config.MessageBuilder == nil {
		return nil, errors.New("query fetcher is not defined in spec config")
	}

	if s.config.MessageTTL >= 1<<22 {
		return nil, fmt.Errorf("too long ttl")
	}

	if s.config.MessageTTL <= 5 {
		return nil, fmt.Errorf("too short ttl")
	}

	queryID, createdAt, err := s.config.MessageBuilder(ctx, s.wallet.subwallet)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch queryID: %w", err)
	}

	if queryID >= 1<<23 {
		return nil, fmt.Errorf("too big query id")
	}

	if createdAt <= 0 {
		return nil, fmt.Errorf("created at should be positive")
	}

	var msg *Message

	if len(messages) > 254*254 {
		return nil, errors.New("for this type of wallet max 254*254 messages can be sent in the same time")
	} else if len(messages) == 1 && messages[0].InternalMessage.StateInit == nil { // messages with state init must be packed because of external msg validation in contract
		msg = messages[0]
	} else if len(messages) > 0 {
		msg, err = s.packActions(uint64(queryID), messages)
		if err != nil {
			return nil, fmt.Errorf("failed to pack messages to cell: %w", err)
		}
	} else {
		return nil, errors.New("should have at least one message")
	}

	msgCell, err := tlb.ToCell(msg.InternalMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to convert msg to cell: %w", err)
	}

	payload := cell.BeginCell().
		MustStoreUInt(uint64(s.wallet.subwallet), 32).
		MustStoreRef(msgCell).
		MustStoreUInt(uint64(msg.Mode), 8).
		MustStoreUInt(uint64(queryID), 23).
		MustStoreUInt(uint64(createdAt), 64).
		MustStoreUInt(uint64(s.config.MessageTTL), 22).
		EndCell()

	return cell.BeginCell().
		MustStoreSlice(payload.Sign(s.wallet.key), 512).
		MustStoreRef(payload).EndCell(), nil
}

func (s *SpecHighloadV3) packActions(queryId uint64, messages []*Message) (_ *Message, err error) {
	const messagesPerPack = 253

	if len(messages) > messagesPerPack {
		rest, err := s.packActions(queryId, messages[messagesPerPack:])
		if err != nil {
			return nil, err
		}
		messages = append(messages[:messagesPerPack], rest)
	}

	var amt = big.NewInt(0)
	var list = cell.BeginCell().EndCell()
	for _, message := range messages {
		amt = amt.Add(amt, message.InternalMessage.Amount.Nano())

		outMsg, err := tlb.ToCell(message.InternalMessage)
		if err != nil {
			return nil, err
		}

		/*
			out_list_empty$_ = OutList 0;
			out_list$_ {n:#} prev:^(OutList n) action:OutAction
			  = OutList (n + 1);
			action_send_msg#0ec3c86d mode:(## 8)
			  out_msg:^(MessageRelaxed Any) = OutAction;
		*/
		msg := cell.BeginCell().MustStoreUInt(0x0ec3c86d, 32).
			MustStoreUInt(uint64(message.Mode), 8).
			MustStoreRef(outMsg)

		list = cell.BeginCell().MustStoreRef(list).MustStoreBuilder(msg).EndCell()
	}

	return &Message{
		Mode: PayGasSeparately + IgnoreErrors,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      false,
			DstAddr:     s.wallet.addr,
			Amount:      tlb.FromNanoTON(amt),
			Body: cell.BeginCell().
				MustStoreUInt(0xae42e5a4, 32).
				MustStoreUInt(queryId, 64).
				MustStoreRef(list).
				EndCell(),
		},
	}, nil
}
