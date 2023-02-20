package ton

import (
	"context"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(GetTime{}, "liteServer.getTime = liteServer.CurrentTime")
	tl.Register(CurrentTime{}, "liteServer.currentTime now:int = liteServer.CurrentTime")
}

type GetTime struct{}

type CurrentTime struct {
	Now int32 `tl:"int"`
}

func (c *APIClient) GetTime(ctx context.Context) (uint32, error) {
	resp, err := c.client.DoRequest(ctx, GetTime{})
	if err != nil {
		return 0, err
	}

	switch resp.TypeID {
	case _CurrentTime:
		if len(resp.Data) < 4 {
			return 0, errors.New("not enough length")
		}
		time := new(CurrentTime)
		_, err = tl.Parse(time, resp.Data, false)
		if err != nil {
			return 0, fmt.Errorf("failed to parse response to CurrentTime, err: %w", err)
		}

		return uint32(time.Now), nil

	case _LSError:
		lsErr := new(LSError)
		_, err = tl.Parse(lsErr, resp.Data, false)
		if err != nil {
			return 0, fmt.Errorf("failed to parse error, err: %w", err)
		}
		return 0, lsErr
	}

	return 0, errors.New("unknown response type")
}
