package ton

import (
	"context"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
)

func init() {
	tl.Register(SendMessage{}, "liteServer.sendMessage body:bytes = liteServer.SendMsgStatus")
	tl.Register(SendMessageStatus{}, "liteServer.sendMsgStatus status:int = liteServer.SendMsgStatus")
}

type SendMessage struct {
	Body []byte `tl:"bytes"`
}

type SendMessageStatus struct {
	Status int32 `tl:"int"`
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
		msgStatus := new(SendMessageStatus)
		_, err = tl.Parse(msgStatus, resp.Data, false)
		if err != nil {
			return fmt.Errorf("falied to parse response to sendMessageStatus, err: %w", err)
		}

		if msgStatus.Status != 1 {
			return fmt.Errorf("status: %d", msgStatus.Status)
		}

		return nil
	case _LSError:
		lsErr := new(LSError)
		_, err = tl.Parse(lsErr, resp.Data, false)
		if err != nil {
			return fmt.Errorf("failed to parse error, err: %w", err)
		}
		return lsErr
	}

	return errors.New("unknown response type")
}
