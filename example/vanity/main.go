package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"flag"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xssnick/tonutils-go/ton/wallet"
)

func main() {
	isSeed := flag.Bool("with-seed", false, "with seed its much slower")
	threads := flag.Uint64("threads", 8, "parallel threads")
	suffix := flag.String("suffix", "", "desired contract suffix, required")
	version := flag.String("wallet", "v3", "v3 or v4")
	flag.Parse()

	if *suffix == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	var ver wallet.Version
	switch *version {
	case "v3":
		ver = wallet.V3
	case "v4":
		ver = wallet.V4R2
	default:
		log.Println("unknown wallet version, use v3 or v4")
		os.Exit(1)
	}

	var counter uint64

	if !*isSeed {
		for x := uint64(0); x < *threads; x++ {
			go func() {
				for {
					atomic.AddUint64(&counter, 1)

					_, pk, _ := ed25519.GenerateKey(nil)
					w, err := wallet.FromPrivateKey(nil, pk, ver)
					if err != nil {
						continue
					}

					if strings.HasSuffix(w.WalletAddress().String(), *suffix) {
						log.Println("Address:", w.WalletAddress().String())
						log.Println("Private key:", hex.EncodeToString(pk.Seed()))
						os.Exit(0)
					}
				}
			}()
		}
	} else {
		for x := uint64(0); x < *threads; x++ {
			go func() {
				i := uint64(0)

				for {
					atomic.AddUint64(&counter, 1)
					i++

					seed := wallet.NewSeed()
					w, _ := wallet.FromSeed(nil, seed, wallet.V4R2)

					if strings.HasSuffix(w.WalletAddress().String(), *suffix) {
						log.Println("Address:", w.WalletAddress().String())
						log.Println("Seed phrase:", seed)
						os.Exit(0)
					}
				}
			}()
		}
	}

	log.Println("searching...")
	for {
		time.Sleep(1 * time.Second)
		log.Println("checked", atomic.LoadUint64(&counter), "per second")
		atomic.StoreUint64(&counter, 0)
	}
}
