package address

import (
	"github.com/xssnick/tonutils-go/tl"
	"net"
)

func init() {
	tl.Register(UDP{}, "adnl.address.udp ip:int port:int = adnl.Address")
	tl.Register(List{}, "adnl.addressList addrs:(vector adnl.Address) version:int reinit_date:int priority:int expire_at:int = adnl.AddressList")
}

type UDP struct {
	IP   net.IP `tl:"int"`
	Port int32  `tl:"int"`
}

type List struct {
	Addresses  []*UDP `tl:"vector struct boxed"`
	Version    int32  `tl:"int"`
	ReinitDate int32  `tl:"int"`
	Priority   int32  `tl:"int"`
	ExpireAt   int32  `tl:"int"`
}
