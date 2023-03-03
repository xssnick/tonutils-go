package ton

import (
	"context"
	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(GetTime{}, "liteServer.getTime = liteServer.CurrentTime")
	tl.Register(CurrentTime{}, "liteServer.currentTime now:int = liteServer.CurrentTime")
}

type GetTime struct{}

type CurrentTime struct {
	Now uint32 `tl:"int"`
}

func (c *APIClient) GetTime(ctx context.Context) (uint32, error) {
	var resp tl.Serializable
	err := c.client.QueryLiteserver(ctx, GetTime{}, &resp)
	if err != nil {
		return 0, err
	}

	switch t := resp.(type) {
	case CurrentTime:
		return t.Now, nil
	case LSError:
		return 0, t
	}
	return 0, errUnexpectedResponse(resp)
}
