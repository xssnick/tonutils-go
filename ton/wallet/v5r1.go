package wallet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// This is from WalletContractV5.js from Tonkeeper Desktop @ton/ton
// b5ee9c7241010101002300084202e4cf3b2f4c6d6a61ea0f2b5447d266785b26af3637db2deee6bcd1aa826f34120dcd8e11
// b5ee9c7241010101002300084202cd0f77c476ca15a9e8fdf6699d25b68e00e7261fd2b7be4346eb847b14f02c97d922995c

// Compiled from: https://github.com/tonkeeper/w5
// b5ee9c72410214010002b6000114ff00f4a413f4bcf2c80b0102012005020102f203011220d728239b4b3b74300401e6208308d722018308d723208020d721d34fd31fd31fed44d0d22020d34fd70bff08f9014098f910f2a35122baf2a15035baf2a2f823bbf264f800a4c8ca2001cf16c9ed54f80f20c7108e23d74c20d020c700dc8e15d72820761e436c20d71d06c712f265d74cd020c700e630ed558e82db3ce2110201480f060201200807001bbe5f0f6a2684080b8eb90fa021840201200c090201200b0a0019b45d1da89a10043ae43ae169f00015b592fda89a1ae14416c1700202760e0d0014a880ed44d0d70a20c2ff0018ab9ced44d08071d721d70bff02d8d020c702dc01d0d60301c713dc01d72c232bc3a3748ec701fa4030fa4401a4b2ed44d0810171d721f4058307f40edd21c7108e2430d74c20d020c700dc8e15d72820761e436c20d71d06c712f265d74cd020c700e630ed558e8330db3ce28e8b3120d72c239b4b73a431dde2111001f020d7498102b1b9dc208308d722018308d723208020d721d34fd31fd31fed44d0d22020d34fd70bff08f9014098f910dd5122baf2a15035baf2a2f823bbf264a4c8ca2001cf16c9ed54f80f20c7108e23d74c20d020c700dc8e15d72820761e436c20d71d06c712f265d74cd020c700e630ed558e82db3ce21102ca93d200018edcd72c20e206dcfc2091709901d72c22f577a52412e25210b18e3d30d72c21065dcad48e2fd200ed44d0d2205204983020c100f2aba3a48e1121c2fff2ab810150d721d70b00f2aaa4a3e2c8ca2058cf16c9ed5492f229e2e30dd74cd0e8d74c1312004220d020c700dc8e15d72820761e436c20d71d06c712f265d74cd020c700e630ed55008c01fa4001fa4421a4b2ed44d0810171d71821d70a2001f405069d3002c8ca0740148307f453f2a78e1133048307f45bf2a8206e02c10012b0f26ce2c85003cf1612f400c9ed549b4062a2

// This one is in use by Tonkeeper at the moment
// https://github.com/tonkeeper/tonkeeper-ton/commit/e8a7f3415e241daf4ac723f273fbc12776663c49#diff-c20d462b2e1ec616bbba2db39acc7a6c61edc3d5e768f5c2034a80169b1a56caR29
// Decoded with console.log(Buffer.from('HASH', 'base64').toString('hex'));
const _V5R1TestCodeHex = "b5ee9c7241010101002300084202e4cf3b2f4c6d6a61ea0f2b5447d266785b26af3637db2deee6bcd1aa826f34120dcd8e11"
const _V5R1MainCodeHex = "b5ee9c7241010101002300084202e4cf3b2f4c6d6a61ea0f2b5447d266785b26af3637db2deee6bcd1aa826f34120dcd8e11"

type SpecV5R1Test struct {
	SpecRegular
	SpecSeqno
}

type SpecV5R1Main struct {
	SpecRegular
	SpecSeqno
}

// Comment: Separated them as Testnet has different Network ID. Could have been a boolean, but then breaking any existing app with this lib
// Decided on functions instead to define Testnet.

func (s *SpecV5R1Test) BuildMessage(ctx context.Context, _ bool, _ *ton.BlockIDExt, messages []*Message) (_ *cell.Cell, err error) {
	// TODO: remove block, now it is here for backwards compatibility

	if len(messages) > 255 {
		return nil, errors.New("for this type of wallet max 255 messages can be sent in the same time")
	}

	seq, err := s.seqnoFetcher(ctx, s.wallet.subwallet)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch seqno: %w", err)
	}

	payload := cell.BeginCell().MustStoreUInt(uint64(s.wallet.subwallet), 32).
		MustStoreUInt(uint64(timeNow().Add(time.Duration(s.messagesTTL)*time.Second).UTC().Unix()), 32).
		MustStoreUInt(uint64(seq), 32).
		MustStoreInt(0, 8) // op

	for i, message := range messages {
		intMsg, err := tlb.ToCell(message.InternalMessage)
		if err != nil {
			return nil, fmt.Errorf("failed to convert internal message %d to cell: %w", i, err)
		}

		payload.MustStoreUInt(uint64(message.Mode), 8).MustStoreRef(intMsg)
	}

	sign := payload.EndCell().Sign(s.wallet.key)
	msg := cell.BeginCell().MustStoreSlice(sign, 512).MustStoreBuilder(payload).EndCell()

	return msg, nil
}

func (s *SpecV5R1Main) BuildMessage(ctx context.Context, _ bool, _ *ton.BlockIDExt, messages []*Message) (_ *cell.Cell, err error) {
	// TODO: remove block, now it is here for backwards compatibility

	if len(messages) > 255 {
		return nil, errors.New("for this type of wallet max 255 messages can be sent in the same time")
	}

	seq, err := s.seqnoFetcher(ctx, s.wallet.subwallet)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch seqno: %w", err)
	}

	payload := cell.BeginCell().MustStoreUInt(uint64(s.wallet.subwallet), 32).
		MustStoreUInt(uint64(timeNow().Add(time.Duration(s.messagesTTL)*time.Second).UTC().Unix()), 32).
		MustStoreUInt(uint64(seq), 32).
		MustStoreInt(0, 8) // op

	for i, message := range messages {
		intMsg, err := tlb.ToCell(message.InternalMessage)
		if err != nil {
			return nil, fmt.Errorf("failed to convert internal message %d to cell: %w", i, err)
		}

		payload.MustStoreUInt(uint64(message.Mode), 8).MustStoreRef(intMsg)
	}

	sign := payload.EndCell().Sign(s.wallet.key)
	msg := cell.BeginCell().MustStoreSlice(sign, 512).MustStoreBuilder(payload).EndCell()

	return msg, nil
}

// TODO: implement plugins
