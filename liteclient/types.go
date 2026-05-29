package liteclient

import (
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(LiteServerQueryPrefix{}, "liteServer.queryPrefix = Object")
	tl.Register(LiteServerQuery{}, "liteServer.query data:bytes = Object")
	tl.Register(ServerBusy{}, "liteServer.serverBusy code:int message:string = liteServer.ServerBusy")
	tl.Register(TCPPing{}, "tcp.ping random_id:long = tcp.Pong")
	tl.Register(TCPPong{}, "tcp.pong random_id:long = tcp.Pong")
	tl.Register(TCPAuthenticate{}, "tcp.authentificate nonce:bytes = tcp.Message")
	tl.Register(TCPAuthenticationNonce{}, "tcp.authentificationNonce nonce:bytes = tcp.Message")
	tl.Register(TCPAuthenticationComplete{}, "tcp.authentificationComplete key:PublicKey signature:bytes = tcp.Message")
}

var (
	ErrNoActiveConnections = errors.New("no active connections")
	ErrADNLReqTimeout      = errors.New("adnl request timeout")
	ErrNoNodesLeft         = errors.New("no more active nodes left")
)

type LiteServerQueryPrefix struct{}

type LiteServerQuery struct {
	Data any `tl:"bytes struct boxed"`
}

type ServerBusy struct {
	Code int32  `tl:"int"`
	Text string `tl:"string"`
}

func (e ServerBusy) Error() string {
	return fmt.Sprintf("lite server busy, code %d: %s", e.Code, e.Text)
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
