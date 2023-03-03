package overlay

import (
	"github.com/xssnick/tonutils-go/tl"
)

func unwrapMessage(data tl.Serializable) (tl.Serializable, []byte) {
	if arr, ok := data.([]tl.Serializable); ok && len(arr) > 1 {
		if q, isQuery := arr[0].(Message); isQuery {
			return arr[1], q.Overlay
		}
	}
	return nil, nil
}

func unwrapQuery(data tl.Serializable) (tl.Serializable, []byte) {
	if arr, ok := data.([]tl.Serializable); ok && len(arr) > 1 {
		if q, isQuery := arr[0].(Query); isQuery {
			return arr[1], q.Overlay
		}
	}
	return nil, nil
}
