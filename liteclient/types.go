package liteclient

import "github.com/xssnick/tonutils-go/tl"

func init() {
	tl.Register(LiteServerQuery{}, "liteServer.query data:bytes = Object")
	tl.Register(TCPPing{}, "tcp.ping random_id:long = tcp.Pong")
	tl.Register(TCPPong{}, "tcp.pong random_id:long = tcp.Pong")
	tl.Register(TCPAuthenticate{}, "tcp.authentificate nonce:bytes = tcp.Message")
	tl.Register(TCPAuthenticationNonce{}, "tcp.authentificationNonce nonce:bytes = tcp.Message")
	tl.Register(TCPAuthenticationComplete{}, "tcp.authentificationComplete key:PublicKey signature:bytes = tcp.Message")
}

type LiteServerQuery struct {
	Data any `tl:"bytes struct boxed"`
}

type TCPPing struct {
	RandomID int64 `tl:"long"`
}

type TCPPong struct {
	RandomID int64 `tl:"long"`
}

type TCPAuthenticate struct {
	Nonce []byte `tl:"bytes"`
}

type TCPAuthenticationNonce struct {
	Nonce []byte `tl:"bytes"`
}

type TCPAuthenticationComplete struct {
	PublicKey any    `tl:"struct boxed [pub.ed25519]"`
	Signature []byte `tl:"bytes"`
}
