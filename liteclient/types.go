package liteclient

import (
	"bytes"
	"encoding/binary"
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

func (p LiteServerQueryPrefix) Serialize(buf *bytes.Buffer) error {
	return nil
}

func (p *LiteServerQueryPrefix) Parse(data []byte) ([]byte, error) {
	return data, nil
}

type LiteServerQuery struct {
	Data any `tl:"bytes struct boxed"`
}

func (q *LiteServerQuery) Parse(data []byte) ([]byte, error) {
	var err error
	q.Data, data, err = parseBoxedPayload(data, false)
	return data, err
}

func (q *LiteServerQuery) ParseNoCopy(data []byte) ([]byte, error) {
	var err error
	q.Data, data, err = parseBoxedPayload(data, true)
	return data, err
}

func (q LiteServerQuery) Serialize(buf *bytes.Buffer) error {
	return serializeBoxedPayload(buf, q.Data)
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

func (p TCPPing) Serialize(buf *bytes.Buffer) error {
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], uint64(p.RandomID))
	buf.Write(tmp[:])
	return nil
}

func (p *TCPPing) Parse(data []byte) ([]byte, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("tcp ping is too short")
	}
	p.RandomID = int64(binary.LittleEndian.Uint64(data))
	return data[8:], nil
}

type TCPPong struct {
	RandomID int64 `tl:"long"`
}

func (p TCPPong) Serialize(buf *bytes.Buffer) error {
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], uint64(p.RandomID))
	buf.Write(tmp[:])
	return nil
}

func (p *TCPPong) Parse(data []byte) ([]byte, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("tcp pong is too short")
	}
	p.RandomID = int64(binary.LittleEndian.Uint64(data))
	return data[8:], nil
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

func serializeBoxedPayload(buf *bytes.Buffer, v tl.Serializable) error {
	from := buf.Len()
	var zero [4]byte
	buf.Write(zero[:])

	if _, err := tl.Serialize(v, true, buf); err != nil {
		return err
	}

	tl.RemapBufferAsSlice(buf, from)
	return nil
}

func parseBoxedPayload(data []byte, noCopy bool) (tl.Serializable, []byte, error) {
	source, rest, err := tl.FromBytesNoCopy(data)
	if err != nil {
		return nil, nil, err
	}

	list := make([]tl.Serializable, 0, 2)
	for len(source) > 0 {
		var obj any
		if noCopy {
			source, err = tl.ParseNoCopy(&obj, source, true)
		} else {
			source, err = tl.Parse(&obj, source, true)
		}
		if err != nil {
			return nil, nil, err
		}
		list = append(list, obj)
	}

	switch len(list) {
	case 1:
		return list[0], rest, nil
	case 0:
		return nil, nil, fmt.Errorf("empty bytes slice cannot be parsed as boxed payload")
	default:
		return list, rest, nil
	}
}
