package main

import (
	"encoding/base64"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func main() {
	// dest address
	addr := address.MustParseAddr("EQBx6tZZWa2Tbv6BvgcvegoOQxkRrVaBVwBOoW85nbP37_Go")
	// binary payload
	body := cell.BeginCell().MustStoreUInt(0, 32).MustStoreStringSnake("hop hey la la lay!").EndCell()

	// prints TON url which can be used to send transaction from any wallet,
	// for example you can make QR code from it and scan using TonKeeper,
	// and this transaction will be executed by the wallet
	fmt.Printf("ton://transfer/%s?bin=%s&amount=%s", addr.String(),
		base64.URLEncoding.EncodeToString(body.ToBOC()), tlb.MustFromTON("0.55").Nano().String())
}
