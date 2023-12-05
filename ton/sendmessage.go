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
var ErrNoTransactionsWereFound = errors.New("no transactions were found")

func (c *APIClient) SendExternalMessage(ctx context.Context, msg *tlb.ExternalMessage) error {
	req, err := tlb.ToCell(msg)
	if err != nil {
		return fmt.Errorf("failed to serialize external message, err: %w", err)
	}

	var resp tl.Serializable
	err = c.client.QueryLiteserver(ctx, SendMessage{Body: req.ToBOCWithFlags(false)}, &resp)
	if err != nil {
		return err
	}

	switch t := resp.(type) {
	case SendMessageStatus:
		if t.Status != 1 {
			return fmt.Errorf("status: %d", t.Status)
		}

		return nil
	case LSError:
		return t
	}
	return errUnexpectedResponse(resp)
}
