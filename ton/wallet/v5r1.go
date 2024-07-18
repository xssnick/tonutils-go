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

// Contract source:
// https://github.com/ton-blockchain/wallet-contract-v5/blob/main/build/wallet_v5.compiled.json
const _V5R1FinalCodeHex = "b5ee9c7241021401000281000114ff00f4a413f4bcf2c80b01020120020d020148030402dcd020d749c120915b8f6320d70b1f2082106578746ebd21821073696e74bdb0925f03e082106578746eba8eb48020d72101d074d721fa4030fa44f828fa443058bd915be0ed44d0810141d721f4058307f40e6fa1319130e18040d721707fdb3ce03120d749810280b99130e070e2100f020120050c020120060902016e07080019adce76a2684020eb90eb85ffc00019af1df6a2684010eb90eb858fc00201480a0b0017b325fb51341c75c875c2c7e00011b262fb513435c280200019be5f0f6a2684080a0eb90fa02c0102f20e011e20d70b1f82107369676ebaf2e08a7f0f01e68ef0eda2edfb218308d722028308d723208020d721d31fd31fd31fed44d0d200d31f20d31fd3ffd70a000af90140ccf9109a28945f0adb31e1f2c087df02b35007b0f2d0845125baf2e0855036baf2e086f823bbf2d0882292f800de01a47fc8ca00cb1f01cf16c9ed542092f80fde70db3cd81003f6eda2edfb02f404216e926c218e4c0221d73930709421c700b38e2d01d72820761e436c20d749c008f2e09320d74ac002f2e09320d71d06c712c2005230b0f2d089d74cd7393001a4e86c128407bbf2e093d74ac000f2e093ed55e2d20001c000915be0ebd72c08142091709601d72c081c12e25210b1e30f20d74a111213009601fa4001fa44f828fa443058baf2e091ed44d0810141d718f405049d7fc8ca0040048307f453f2e08b8e14038307f45bf2e08c22d70a00216e01b3b0f2d090e2c85003cf1612f400c9ed54007230d72c08248e2d21f2e092d200ed44d0d2005113baf2d08f54503091319c01810140d721d70a00f2e08ee2c8ca0058cf16c9ed5493f2c08de20010935bdb31e1d74cd0b4d6c35e"

type ConfigV5R1Final struct {
	NetworkGlobalID int32
	Workchain       int8
}

type SpecV5R1Final struct {
	SpecRegular
	SpecSeqno

	config ConfigV5R1Final
}

func (c ConfigV5R1Final) String() string {
	return "V5R1Final"
}

// Function to generate the context ID based on the given workchain
func genContextID(workchain uint8, version uint8, subwallet uint16) uint32 {
	var ctx uint32

	// Convert workchain to uint32 after ensuring it's correctly handled as an 8-bit value
	ctx |= 1 << 31                 // Write 1 bit as 1 at the leftmost bit
	ctx |= uint32(workchain) << 23 // Write 8 bits of workchain, shifted to position
	ctx |= uint32(version) << 15   // Write 8 bits of 0 (wallet version)
	ctx |= uint32(subwallet)       // Write 15 bits of 0 (subwallet number)

	return ctx
}

type V5R1ID struct {
	NetworkGlobalID int32
	WorkChain       int8
	SubwalletNumber uint16
	WalletVersion   uint8
}

func (w V5R1ID) Serialized() uint32 {
	return genContextID(uint8(w.WorkChain), w.WalletVersion, w.SubwalletNumber) ^ uint32(w.NetworkGlobalID)
}

func (s *SpecV5R1Final) BuildMessage(ctx context.Context, _ bool, _ *ton.BlockIDExt, messages []*Message) (_ *cell.Cell, err error) {
	if len(messages) > 255 {
		return nil, errors.New("for this type of wallet max 255 messages can be sent at the same time")
	}

	seq, err := s.seqnoFetcher(ctx, s.wallet.subwallet)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch seqno: %w", err)
	}

	actions, err := packV5Actions(messages)
	if err != nil {
		return nil, fmt.Errorf("failed to build actions: %w", err)
	}

	walletId := V5R1ID{
		NetworkGlobalID: s.config.NetworkGlobalID,
		WorkChain:       s.config.Workchain,
		SubwalletNumber: uint16(s.wallet.subwallet),
		WalletVersion:   0,
	}

	payload := cell.BeginCell().
		MustStoreUInt(0x7369676e, 32).                                                                    // external sign op code
		MustStoreUInt(uint64(walletId.Serialized()), 32).                                                 // serialized WalletId
		MustStoreUInt(uint64(time.Now().Add(time.Duration(s.messagesTTL)*time.Second).UTC().Unix()), 32). // validUntil
		MustStoreUInt(uint64(seq), 32).                                                                   // seq (block)
		MustStoreBuilder(actions)                                                                         // Action list

	sign := payload.EndCell().Sign(s.wallet.key)
	msg := cell.BeginCell().MustStoreBuilder(payload).MustStoreSlice(sign, 512).EndCell()

	return msg, nil
}

// Validate messages
func validateMessageFields(messages []*Message) error {
	if len(messages) > 255 {
		return fmt.Errorf("max 255 messages allowed for v5")
	}
	for _, message := range messages {
		if message.InternalMessage == nil {
			return fmt.Errorf("internal message cannot be nil")
		}
	}
	return nil
}

// Pack Actions
func packV5Actions(messages []*Message) (*cell.Builder, error) {
	if err := validateMessageFields(messages); err != nil {
		return nil, err
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
		msg := cell.BeginCell().MustStoreUInt(0x0ec3c86d, 32). // action_send_msg prefix
									MustStoreUInt(uint64(message.Mode), 8). // mode
									MustStoreRef(outMsg)                    // message reference

		list = cell.BeginCell().MustStoreRef(list).MustStoreBuilder(msg).EndCell()
	}

	// Ensure the action list ends with 0, 1 as per the new specification
	return cell.BeginCell().MustStoreUInt(1, 1).MustStoreRef(list).MustStoreUInt(0, 1), nil
}
