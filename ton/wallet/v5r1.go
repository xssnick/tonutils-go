package wallet

import (
	"context"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"time"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// https://github.com/tonkeeper/tonkeeper-ton/commit/e8a7f3415e241daf4ac723f273fbc12776663c49#diff-c20d462b2e1ec616bbba2db39acc7a6c61edc3d5e768f5c2034a80169b1a56caR29
const _V5R1CodeHex = "b5ee9c7241010101002300084202e4cf3b2f4c6d6a61ea0f2b5447d266785b26af3637db2deee6bcd1aa826f34120dcd8e11"

type ConfigV5R1 struct {
	NetworkGlobalID int32
	Workchain       int8
}

type SpecV5R1 struct {
	SpecRegular
	SpecSeqno

	config ConfigV5R1
}

const MainnetGlobalID = -239
const TestnetGlobalID = -3

func (s *SpecV5R1) BuildMessage(ctx context.Context, _ bool, _ *ton.BlockIDExt, messages []*Message) (_ *cell.Cell, err error) {
	// TODO: remove block, now it is here for backwards compatibility

	if len(messages) > 255 {
		return nil, errors.New("for this type of wallet max 4 messages can be sent in the same time")
	}

	seq, err := s.seqnoFetcher(ctx, s.wallet.subwallet)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch seqno: %w", err)
	}

	actions, err := packV5Actions(messages)
	if err != nil {
		return nil, fmt.Errorf("failed to build actions: %w", err)
	}

	payload := cell.BeginCell().
		MustStoreUInt(0x7369676e, 32). // external sign op code
		MustStoreInt(int64(s.config.NetworkGlobalID), 32).
		MustStoreInt(int64(s.config.Workchain), 8).
		MustStoreUInt(0, 8). // version of v5
		MustStoreUInt(uint64(s.wallet.subwallet), 32).
		MustStoreUInt(uint64(timeNow().Add(time.Duration(s.messagesTTL)*time.Second).UTC().Unix()), 32).
		MustStoreUInt(uint64(seq), 32).
		MustStoreBuilder(actions)

	sign := payload.EndCell().Sign(s.wallet.key)
	msg := cell.BeginCell().MustStoreBuilder(payload).MustStoreSlice(sign, 512).EndCell()

	return msg, nil
}

func packV5Actions(messages []*Message) (*cell.Builder, error) {
	if len(messages) > 255 {
		return nil, fmt.Errorf("max 255 messages allowed for v5")
	}

	var list = cell.BeginCell().EndCell()
	for _, message := range messages {
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

	return cell.BeginCell().MustStoreUInt(0, 1).MustStoreRef(list), nil
}
