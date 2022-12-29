package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"github.com/xssnick/tonutils-go/adnl/dht"
	rldphttp "github.com/xssnick/tonutils-go/adnl/rldp/http"
	"io"
	"log"
	"net/http"
	"os"
)

func handler(writer http.ResponseWriter, request *http.Request) {
	_, _ = writer.Write([]byte("Hello, " + request.URL.Query().Get("name") +
		"\nThis TON site works natively using tonutils-go!"))
}

func main() {
	dhtClient, err := dht.NewClientFromConfigUrl(context.Background(), "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	mx := http.NewServeMux()
	mx.HandleFunc("/hello", handler)

	s := rldphttp.NewServer(loadKey(), dhtClient, mx)

	addr, err := rldphttp.SerializeADNLAddress(s.Address())
	if err != nil {
		panic(err)
	}

	log.Println("Starting server on", addr+".adnl")
	if err = s.ListenAndServe(getPublicIP() + ":9056"); err != nil {
		panic(err)
	}
}

func getPublicIP() string {
	req, err := http.Get("http://ip-api.com/json/")
	if err != nil {
		return err.Error()
	}
	defer req.Body.Close()

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return err.Error()
	}

	var ip struct {
		Query string
	}
	_ = json.Unmarshal(body, &ip)

	return ip.Query
}

func loadKey() ed25519.PrivateKey {
	file := "./key.txt"
	data, err := os.ReadFile(file)
	if err != nil {
		_, srvKey, err := ed25519.GenerateKey(nil)
		if err != nil {
			panic(err)
		}

		err = os.WriteFile(file, []byte(hex.EncodeToString(srvKey.Seed())), 555)
		if err != nil {
			panic(err)
		}

		return srvKey
	}

	dec, err := hex.DecodeString(string(data))
	return ed25519.NewKeyFromSeed(dec)
}
