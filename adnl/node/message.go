package node

import "github.com/xssnick/tonutils-go/tl"

func init() {
	tl.Register(ExternalMessage{}, "tonNode.externalMessage data:bytes = tonNode.ExternalMessage")
	tl.Register(NewExternalMessageBroadcast{}, "tonNode.externalMessageBroadcast message:tonNode.externalMessage = tonNode.Broadcast")
}

type ExternalMessage struct {
	Data []byte `tl:"bytes"`
}

type NewExternalMessageBroadcast struct {
	Message ExternalMessage `tl:"struct"`
}
