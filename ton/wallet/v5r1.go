package wallet

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/xssnick/tonutils-go/tlb"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Moved from WalletContractV5.ts
const (
	auth_extension       = 0x6578746e
	auth_signed_external = 0x7369676e
	auth_signed_internal = 0x73696e74
)

// Compiled from: https://github.com/tonkeeper/w5
// b5ee9c72410214010002b6000114ff00f4a413f4bcf2c80b0102012005020102f203011220d728239b4b3b74300401e6208308d722018308d723208020d721d34fd31fd31fed44d0d22020d34fd70bff08f9014098f910f2a35122baf2a15035baf2a2f823bbf264f800a4c8ca2001cf16c9ed54f80f20c7108e23d74c20d020c700dc8e15d72820761e436c20d71d06c712f265d74cd020c700e630ed558e82db3ce2110201480f060201200807001bbe5f0f6a2684080b8eb90fa021840201200c090201200b0a0019b45d1da89a10043ae43ae169f00015b592fda89a1ae14416c1700202760e0d0014a880ed44d0d70a20c2ff0018ab9ced44d08071d721d70bff02d8d020c702dc01d0d60301c713dc01d72c232bc3a3748ec701fa4030fa4401a4b2ed44d0810171d721f4058307f40edd21c7108e2430d74c20d020c700dc8e15d72820761e436c20d71d06c712f265d74cd020c700e630ed558e8330db3ce28e8b3120d72c239b4b73a431dde2111001f020d7498102b1b9dc208308d722018308d723208020d721d34fd31fd31fed44d0d22020d34fd70bff08f9014098f910dd5122baf2a15035baf2a2f823bbf264a4c8ca2001cf16c9ed54f80f20c7108e23d74c20d020c700dc8e15d72820761e436c20d71d06c712f265d74cd020c700e630ed558e82db3ce21102ca93d200018edcd72c20e206dcfc2091709901d72c22f577a52412e25210b18e3d30d72c21065dcad48e2fd200ed44d0d2205204983020c100f2aba3a48e1121c2fff2ab810150d721d70b00f2aaa4a3e2c8ca2058cf16c9ed5492f229e2e30dd74cd0e8d74c1312004220d020c700dc8e15d72820761e436c20d71d06c712f265d74cd020c700e630ed55008c01fa4001fa4421a4b2ed44d0810171d71821d70a2001f405069d3002c8ca0740148307f453f2a78e1133048307f45bf2a8206e02c10012b0f26ce2c85003cf1612f400c9ed549b4062a2

// This one is in use by Tonkeeper at the moment
// https://github.com/tonkeeper/tonkeeper-ton/commit/e8a7f3415e241daf4ac723f273fbc12776663c49#diff-c20d462b2e1ec616bbba2db39acc7a6c61edc3d5e768f5c2034a80169b1a56caR29
const _V5R1CodeHex = "b5ee9c7241010101002300084202e4cf3b2f4c6d6a61ea0f2b5447d266785b26af3637db2deee6bcd1aa826f34120dcd8e11"

type ConfigWalletV5 struct {
	// MessageTTL must be > 5 and less than 1<<22
	MessageTTL uint32

	// This function wil be used to get query id and creation time for the new message.
	// ID can be iterator from your database, max id is 1<<23, when it is higher, start from 0 and repeat
	// MessageBuilder should be defined if you want to send transactions
	MessageBuilder func(ctx context.Context, subWalletId uint32) (id uint32, createdAt int64, err error)
}

type SpecWalletV5 struct {
	wallet *Wallet
	config ConfigWalletV5
	SpecSeqno
}

func (s *SpecWalletV5) BuildMessage(ctx context.Context, messages []*Message) (_ *cell.Cell, err error) {
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
		return nil, fmt.Errorf("failed to convert msg to cell: %w", err)
	}

	if queryID >= 1<<23 {
		return nil, fmt.Errorf("too big query id")
	}

	if createdAt <= 0 {
		return nil, fmt.Errorf("created at should be positive")
	}

	var msg *Message

	for i, msg := range messages {
		fmt.Printf("Message %d: %v\n", i, msg)
	}

	if len(messages) > 255 {
		return nil, errors.New("for this type of wallet max 255 messages can be sent at the same time")
	} else if len(messages) > 0 { // Check if messages has at least one element
		if len(messages) > 1 {
			msg, err = s.packActions(uint64(queryID), messages)
			if err != nil {
				return nil, fmt.Errorf("failed to pack messages to cell: %w", err)
			}
		} else {
			msg = messages[0]
		}
	} else {
		return nil, errors.New("should have at least one message")
	}

	msgCell, err := tlb.ToCell(msg.InternalMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to convert msg to cell: %w", err)
	}

	seq, err := s.seqnoFetcher(ctx, s.wallet.subwallet)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch seqno: %w", err)
	}

	payload := cell.BeginCell().
		//MustStoreUInt(uint64(0), 8). // version
		MustStoreUInt(uint64(s.wallet.subwallet), 32).
		MustStoreUInt(uint64(timeNow().Add(time.Duration(s.config.MessageTTL)*time.Second).UTC().Unix()), 32).
		MustStoreUInt(uint64(seq), 32).
		MustStoreRef(msgCell).
		EndCell()

	// Sign the payload
	signature := payload.Sign(s.wallet.key)
	if err != nil {
		return nil, err
	}

	// Construct the final signed payload
	signedPayload := cell.BeginCell().
		MustStoreUInt(auth_signed_external, 32).
		MustStoreRef(payload).          // Store the payload
		MustStoreSlice(signature, 512). // Store the signature
		EndCell()

	return signedPayload, nil

}

func (s *SpecWalletV5) packActions(queryId uint64, messages []*Message) (*Message, error) {
	if len(messages) > 253 {
		rest, err := s.packActions(queryId, messages[253:])
		if err != nil {
			return nil, err
		}
		messages = append(messages[:253], rest)
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
		Mode: 1 + 2,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      false,
			DstAddr:     s.wallet.addr,
			Amount:      tlb.FromNanoTON(amt),
			Body: cell.BeginCell().
				MustStoreUInt(0xae42e5a4, 32). // auth_signed_internal maybe?
				MustStoreUInt(queryId, 64).
				MustStoreRef(list).
				EndCell(),
		},
	}, nil
}

// TODO: implement plugins
