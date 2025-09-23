package main

import (
	"context"
	"crypto/ed25519"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/dht"
	"github.com/xssnick/tonutils-go/liteclient"
	log "log"
	"net"
)

func main() {
	_, srvPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Fatalln("failed to generate ed25519 key:", err.Error())
	}

	cfg, err := liteclient.GetConfigFromUrl(context.Background(), "https://ton-blockchain.github.io/global.config.json")
	if err != nil {
		log.Fatalln("failed to get lite client config:", err.Error())
	}

	nodes, err := dht.BootstrapNodesFromConfig(cfg)
	if err != nil {
		log.Fatalln("failed to get bootstrap nodes:", err.Error())
	}

	serverGw := adnl.NewGateway(srvPriv)
	if err = serverGw.StartServer("0.0.0.0:3129"); err != nil {
		log.Fatalln("failed to start server:", err.Error())
	}
	serverGw.SetAddressList([]*address.UDP{
		{
			Port: 3129,
			IP:   net.ParseIP("31.172.68.159"),
		},
	})
	defer serverGw.Close()

	srv, err := dht.New(serverGw, nodes, true)
	if err != nil {
		log.Fatalln("failed to create dht server:", err.Error())
	}

	log.Println("bootstrapping nodes")

	err = srv.Bootstrap(context.Background(), 100)
	if err != nil {
		log.Fatalln("failed to bootstrap dht server:", err.Error())
	}

	log.Println("DHT server started")

	<-make(chan bool)
}
