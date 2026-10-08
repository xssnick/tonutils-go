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

func (c *APIClient) SendExternalMessageToAllNodes(ctx context.Context, msg *tlb.ExternalMessage) error {
	var nodeCtxs []context.Context
	if c.client.StickyNodeID(ctx) != 0 {
		// balanced iteration excludes the node the context is pinned to, keep it as a target too
		nodeCtxs = append(nodeCtxs, ctx)
	}

	nodeCtx, err := c.client.StickyContextNextNodeBalanced(ctx)
	for err == nil {
		nodeCtxs = append(nodeCtxs, nodeCtx)
		nodeCtx, err = c.client.StickyContextNextNodeBalanced(nodeCtx)
	}

	if len(nodeCtxs) == 0 {
		return fmt.Errorf("failed to select node to send message to: %w", err)
	}

	results := make(chan error, len(nodeCtxs))
	for _, nodeCtx := range nodeCtxs {
		go func() {
			if sendErr := c.SendExternalMessage(nodeCtx, msg); sendErr != nil {
				results <- fmt.Errorf("node %d: %w", c.client.StickyNodeID(nodeCtx), sendErr)
				return
			}
			results <- nil
		}()
	}

	var errs []error
	for range nodeCtxs {
		sendErr := <-results
		if sendErr == nil {
			return nil
		}
		errs = append(errs, sendErr)
	}
	return fmt.Errorf("failed to send message to any node: %w", errors.Join(errs...))
}
