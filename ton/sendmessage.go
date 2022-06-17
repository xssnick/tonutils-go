package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/utils"
)

var ErrMessageNotAccepted = errors.New("message was not accepted by the contract")

func (c *APIClient) SendExternalMessage(ctx context.Context, addr *address.Address, msg *cell.Cell) error {
	return c.SendExternalInitMessage(ctx, addr, msg, nil)
}

func (c *APIClient) SendExternalInitMessage(ctx context.Context, addr *address.Address, msg *cell.Cell, state *tlb.StateInit) error {
	builder := cell.BeginCell().MustStoreUInt(0b10, 2).
		MustStoreUInt(0b00, 2). // src addr_none
		MustStoreAddr(addr).    // dst addr
		MustStoreCoins(0)       // import fee 0

	builder.MustStoreBoolBit(state != nil) // has state init
	if state != nil {
		stateCell, err := state.ToCell()
		if err != nil {
			return err
		}

		if builder.BitsLeft()-2 < stateCell.BitsSize() || builder.RefsLeft()-2 < msg.RefsNum() {
			builder.MustStoreBoolBit(true) // state as ref
			builder.MustStoreRef(stateCell)
		} else {
			builder.MustStoreBoolBit(false) // state as slice
			builder.MustStoreBuilder(stateCell.ToBuilder())
		}
	}

	if builder.BitsLeft() < msg.BitsSize() || builder.RefsLeft() < msg.RefsNum() {
		builder.MustStoreBoolBit(true) // body as ref
		builder.MustStoreRef(msg)
	} else {
		builder.MustStoreBoolBit(false) // state as slice
		builder.MustStoreBuilder(msg.ToBuilder())
	}

	req := builder.EndCell().ToBOCWithFlags(false)

	resp, err := c.client.Do(ctx, _SendMessage, utils.TLBytes(req))
	if err != nil {
		return err
	}

	switch resp.TypeID {
	case _SendMessageResult:
		status := binary.LittleEndian.Uint32(resp.Data)

		if status != 1 {
			return fmt.Errorf("status: %d", status)
		}

		return nil
	case _LSError:
		return LSError{
			Code: binary.LittleEndian.Uint32(resp.Data),
			Text: string(resp.Data[4:]),
		}
	}

	return errors.New("unknown response type")
}
