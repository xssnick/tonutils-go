package wallet

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// Verified V5 Contract from:
// https://github.com/tonkeeper/tonkeeper-ton/commit/e8a7f3415e241daf4ac723f273fbc12776663c49#diff-c20d462b2e1ec616bbba2db39acc7a6c61edc3d5e768f5c2034a80169b1a56caR29
const _V5R1CodeHex = "b5ee9c7241010101002300084202e4cf3b2f4c6d6a61ea0f2b5447d266785b26af3637db2deee6bcd1aa826f34120dcd8e11"

// Constants
const (
	authSignedExternal  = 0x7369676e
	authSignedInternal  = 0x73696e74
	maxMessages         = 255
	maxActionListLength = 10000 // Change later to correct one
)

type KWalletId struct {
	WalletVersion   uint8
	NetworkGlobalId int32
	WorkChain       int8
	SubwalletNumber uint32
}

type ConfigWalletV5 struct {
	MessageTTL     uint32
	MessageBuilder func(ctx context.Context, subWalletID uint32) (id uint32, createdAt int64, err error)
}

type SpecWalletV5 struct {
	wallet *Wallet
	config ConfigWalletV5
	SpecSeqno
}

func bufferToBigInt(buffer []byte) *big.Int {
	return new(big.Int).SetBytes(buffer)
}

// Convert a byte buffer to a uint64
func bufferToUint64(buffer []byte) uint64 {
	if len(buffer) < 8 {
		// If buffer is smaller than 8 bytes, pad with zeros
		paddedBuffer := make([]byte, 8)
		copy(paddedBuffer[8-len(buffer):], buffer)
		return binary.BigEndian.Uint64(paddedBuffer)
	}
	return binary.BigEndian.Uint64(buffer[:8])
}

func (s *SpecWalletV5) BuildMessage(ctx context.Context, messages []*Message) (*cell.Cell, error) {
	// Define network
	walletId := KWalletId{
		WalletVersion:   0,
		NetworkGlobalId: -3,
		WorkChain:       0,
		SubwalletNumber: 0,
	}

	if s.config.MessageBuilder == nil {
		return nil, errors.New("query fetcher is not defined in spec config")
	}
	if s.config.MessageTTL >= 1<<22 {
		return nil, fmt.Errorf("too long ttl")
	}
	if s.config.MessageTTL <= 5 {
		return nil, fmt.Errorf("too short ttl")
	}

	queryID, createdAt, err := s.config.MessageBuilder(ctx, walletId.SubwalletNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch query id: %w", err)
	}
	if queryID >= 1<<23 {
		return nil, fmt.Errorf("too big query id")
	}
	if createdAt <= 0 {
		return nil, fmt.Errorf("created at should be positive")
	}

	seq, err := s.seqnoFetcher(ctx, walletId.SubwalletNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch seqno: %w", err)
	}

	if len(messages) > maxMessages {
		return nil, errors.New("for this type of wallet max 255 messages can be sent at the same time")
	} else if len(messages) == 0 {
		return nil, errors.New("should have at least one message")
	}

	var msg *Message
	msg, err = s.packActions(uint64(seq), messages)
	if err != nil {
		return nil, fmt.Errorf("failed to pack messages: %w", err)
	}

	msgCell, err := tlb.ToCell(msg.InternalMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to convert message to cell: %w", err)
	}

	walletIdCell := cell.BeginCell().
		MustStoreInt(int64(walletId.NetworkGlobalId), 32).
		MustStoreInt(int64(walletId.WorkChain), 8).
		MustStoreUInt(uint64(walletId.WalletVersion), 8).
		MustStoreUInt(uint64(walletId.SubwalletNumber), 32). // 0??? get_wallet_id 0xffffff11000000000000?
		EndCell()

	// Convert the cell to a byte buffer
	buffer := walletIdCell.ToBOC()

	// Log the signature
	fmt.Printf("NetworkGlobalId: %d (size: %d bits)\n", walletId.NetworkGlobalId, 32)
	fmt.Printf("WorkChain: %d (size: %d bits)\n", walletId.WorkChain, 8)
	fmt.Printf("WalletVersion: %d (size: %d bits)\n", walletId.WalletVersion, 8)
	fmt.Printf("SubwalletNumber: %d (size: %d bits)\n", walletId.SubwalletNumber, 32)
	fmt.Printf("TTL: %d (size: %d bits)\n", uint64(time.Now().Add(time.Duration(s.config.MessageTTL)*time.Second).UTC().Unix()), 32)
	fmt.Printf("Sequence Number: %d (size: %d bits)\n", uint64(seq), 32)
	fmt.Printf("Query ID: %d (size: %d bits)\n", uint64(queryID), 64)
	//fmt.Printf("Signature: %x\n", signature)
	//fmt.Printf("Payload size: %d bits\n", len(payload.ToBOC())*8)
	//fmt.Printf("Signature size: %d bits\n", len(signature)*8)

	signedPayloadTMP := cell.BeginCell().
		MustStoreUInt(authSignedExternal, 32).     // Ensure opcode alignment
		MustStoreUInt(bufferToUint64(buffer), 80). // Store the byte buffer directly as a bit slice
		MustStoreUInt(uint64(time.Now().Add(time.Duration(s.config.MessageTTL)*time.Second).UTC().Unix()), 32).
		MustStoreUInt(uint64(seq), 32). // Ensure sequence number is stored as 32 bits
		MustStoreRef(msgCell).
		EndCell()

	// Construct the final signed payload
	signedPayload := cell.BeginCell().
		MustStoreUInt(authSignedExternal, 32).     // Ensure opcode alignment
		MustStoreUInt(bufferToUint64(buffer), 80). // Store the byte buffer directly as a bit slice
		MustStoreUInt(uint64(time.Now().Add(time.Duration(s.config.MessageTTL)*time.Second).UTC().Unix()), 32).
		MustStoreUInt(uint64(seq), 32). // Ensure sequence number is stored as 32 bits
		MustStoreRef(msgCell).
		MustStoreSlice(signedPayloadTMP.Sign(s.wallet.key), 512). // need to sign the whole cell and add before the end
		EndCell()

	return signedPayload, nil
}

// packActions method to pack multiple actions into a single message
func (s *SpecWalletV5) packActions(queryId uint64, messages []*Message) (*Message, error) {
	fmt.Printf("I got called !!!")
	amt := big.NewInt(0)
	listBuilder := cell.BeginCell().MustStoreUInt(0, 1)

	for _, message := range messages {
		amt = amt.Add(amt, message.InternalMessage.Amount.Nano())

		outMsg, err := tlb.ToCell(message.InternalMessage)
		if err != nil {
			return nil, err
		}

		msg := cell.BeginCell().
			MustStoreUInt(0x0ec3c86d, 32). // action_send_msg opcode
			MustStoreUInt(uint64(message.Mode), 8).
			MustStoreRef(cell.BeginCell().MustStoreRef(outMsg).EndCell()).
			EndCell()

		listBuilder.MustStoreRef(
			cell.BeginCell().MustStoreRef(msg).EndCell(),
		)
	}

	list := listBuilder.EndCell()

	// Validate the action list length
	if len(list.ToBOC()) > maxActionListLength {
		return nil, errors.New("action list too long")
	}

	internalMessageCell := cell.BeginCell().
		MustStoreUInt(authSignedInternal, 32). // internal_signed opcode
		MustStoreUInt(queryId, 64).
		MustStoreRef(list).
		EndCell()

	message := &Message{
		Mode: 1 + 2,
		InternalMessage: &tlb.InternalMessage{
			IHRDisabled: true,
			Bounce:      false,
			DstAddr:     s.wallet.addr,
			Amount:      tlb.FromNanoTON(amt),
			Body:        internalMessageCell,
		},
	}

	return message, nil
}
