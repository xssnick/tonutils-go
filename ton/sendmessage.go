package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
)

func init() {
	tl.Register(SendMessage{}, "liteServer.sendMessage body:bytes = liteServer.SendMsgStatus")
}

type SendMessage struct {
	Body []byte `tl:"bytes"`
}

var ErrMessageNotAccepted = errors.New("message was not accepted by the contract")

func (c *APIClient) SendExternalMessage(ctx context.Context, msg *tlb.ExternalMessage) error {
	req, err := msg.ToCell()
	if err != nil {
		return fmt.Errorf("failed to serialize external message, err: %w", err)
	}

	resp, err := c.client.DoRequest(ctx, SendMessage{Body: req.ToBOCWithFlags(false)})
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
		var lsErr LSError
		resp.Data, err = lsErr.Load(resp.Data)
		if err != nil {
			return err
		}
		return lsErr
	}

	return errors.New("unknown response type")
}
