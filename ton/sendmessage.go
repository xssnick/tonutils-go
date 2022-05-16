package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

var ErrMessageNotAccepted = errors.New("message was not accepted by the contract")

func (c *APIClient) SendExternalMessage(ctx context.Context, addr *address.Address, msg *cell.Cell) error {
	return c.sendExternalMessage(ctx, addr, msg)
}

func (c *APIClient) sendExternalMessage(ctx context.Context, addr *address.Address, msg any) error {
	builder := cell.BeginCell().MustStoreUInt(0b10, 2).
		MustStoreUInt(0b00, 2). // src addr_none
		MustStoreAddr(addr). // dst addr
		MustStoreCoins(0) // import fee 0

	builder.MustStoreUInt(0, 1) // no state init

	switch d := msg.(type) {
	case []byte: // slice data
		builder.MustStoreUInt(0, 1).MustStoreSlice(d, len(d)*8)
	case *cell.Cell: // cell data
		builder.MustStoreUInt(1, 1).MustStoreRef(d)
	default:
		return errors.New("unknown arg type")
	}

	req := builder.EndCell().ToBOCWithFlags(false)

	resp, err := c.client.Do(ctx, _SendMessage, storableBytes(req))
	if err != nil {
		return err
	}

	switch resp.TypeID {
	case _SendMessageResult:
		// TODO: mode
		status := binary.LittleEndian.Uint32(resp.Data)

		if status != 1 {
			return fmt.Errorf("status: %d", status)
		}

		return nil
	case _LSError:
		code := binary.LittleEndian.Uint32(resp.Data)
		if code == 0 {
			return ErrMessageNotAccepted
		}

		return fmt.Errorf("lite server error, code %d: %s", code, string(resp.Data[5:]))
	}

	return errors.New("unknown response type")
}
