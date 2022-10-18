package ton

import (
	"context"
	"encoding/binary"
	"errors"
)

func (c *APIClient) GetTime(ctx context.Context) (uint32, error) {

	resp, err := c.client.Do(ctx, _GetTime, nil)
	if err != nil {
		return 0, err
	}

	switch resp.TypeID {
	case _CurrentTime:
		if len(resp.Data) < 4 {
			return 0, errors.New("not enough length")
		}
		time := binary.LittleEndian.Uint32(resp.Data)
		return time, nil

	case _LSError:
		var lsErr LSError
		resp.Data, err = lsErr.Load(resp.Data)
		if err != nil {
			return 0, err
		}
		return 0, lsErr
	}

	return 0, errors.New("unknown response type")
}
