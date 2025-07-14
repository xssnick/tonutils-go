package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"log"
	"time"
)

var adnlAddrHex = flag.String("adnl", "", "adnl address")

func main() {
	flag.Parse()

	if len(*adnlAddrHex) != 64 {
		log.Fatalln("adnl address must be 32 bytes hex string, use -adnl flag")
		return
	}

	adnlAddr, err := hex.DecodeString(*adnlAddrHex)
	if err != nil {
		log.Fatalln("failed to decode adnl address:", err.Error())
	}

	_, prv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Fatalln("failed to generate ed25519 key:", err.Error())
	}

	gw := adnl.NewGateway(prv)
	if err = gw.StartClient(); err != nil {
		log.Fatalln("failed to start adnl client:", err.Error())
	}

	log.Println("Starting DHT client...")
	cli, err := dht.NewClientFromConfigUrl(context.Background(), gw, "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		log.Fatalln("failed to create dht client:", err.Error())
	}

	log.Println("Searching for addresses in DHT...")
	addrList, pubKey, err := cli.FindAddresses(context.Background(), adnlAddr)
	if err != nil {
		if errors.Is(err, dht.ErrDHTValueIsNotFound) {
			log.Println("ADNL is not found in DHT")
			return
		}

		log.Fatalln("failed to find addresses:", err.Error())
	}

	log.Println("Resolved public key:", hex.EncodeToString(pubKey))
	log.Println("Found addresses", len(addrList.Addresses))

	for _, address := range addrList.Addresses {
		addr := address.IP.String() + ":" + fmt.Sprint(address.Port)
		log.Println("Found address:", addr, "checking ping...")

		peer, err := gw.RegisterClient(addr, pubKey)
		if err != nil {
			log.Println(addr, "Failed to register peer:", err.Error())
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		p, err := peer.Ping(ctx)
		if err != nil {
			log.Println(addr, "Ping failed:", err.Error())
			continue
		}
		cancel()

		log.Println("Available, ping to", addr, "is", p.String())
	}
	log.Println("Done")
}
