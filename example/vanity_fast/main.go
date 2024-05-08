package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"hash"
	"log"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xssnick/tonutils-go/address"
)

func main() {
	threads := flag.Uint64("threads", uint64(runtime.NumCPU()), "parallel threads")
	suffix := flag.String("suffix", "", "desired contract suffix, required")
	caseSensitive := flag.Bool("case", false, "is case sensitive")

	flag.Parse()

	if *suffix == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	var counter uint64

	for x := uint64(0); x < *threads; x++ {
		go generateWallets(*caseSensitive, *suffix, &counter)
	}

	log.Println("searching...")
	start := time.Now()
	for {
		time.Sleep(1 * time.Second)
		log.Println("checked", atomic.LoadUint64(&counter)/uint64(time.Since(start).Seconds()), "per second")
	}
}

func generateWallets(caseSensitive bool, suffix string, counter *uint64) {
	var equalityFunc func(a string, b string) bool
	if caseSensitive {
		equalityFunc = func(a, b string) bool {
			return a == b
		}
	} else {
		equalityFunc = func(a, b string) bool {
			return strings.EqualFold(a, b)
		}
	}

	// We use same bytes array for every iteration to avoid allocations
	addrFrom := make([]byte, 36) // bytes for address conversion function
	addrTo := make([]byte, 48)   // bytes for address conversion function result
	hashDst := make([]byte, 32)  // bytes for sha256 state init hash

	subwalletIDBytes := []byte{0, 0, 0, 0}

	v3DataCell := []byte{
		0, 80, 0, 0, 0, 0, // ?
		41, 169, 163, 23, // subwallet id
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // public key
	}

	v3StateInit := []byte{
		2, 1, 52, 0, 0, 0, 0, 132,
		218, 250, 68, 159, 152, 166, 152, 119,
		137, 186, 35, 35, 88, 7, 43, 192,
		247, 109, 196, 82, 64, 2, 165, 208,
		145, 139, 154, 117, 210, 213, 153,
		0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, // bytes for data cell hash
	}

	hash := sha256.New()
	strCmpOffset := 48 - len(suffix)

	for {
		_, pk, _ := ed25519.GenerateKey(nil)
		key := pk.Public().(ed25519.PublicKey)

		// We set key bytes to data cell only once, later we only change subwallet id
		copy(v3DataCell[10:], key)

		for i := uint32(0); i < 1_000_000_000; i++ {
			atomic.AddUint64(counter, 1)

			binary.BigEndian.PutUint32(subwalletIDBytes, i)
			getHashV3HashFromKey(hash, subwalletIDBytes, v3DataCell, v3StateInit, hashDst)

			addr := address.NewAddress(0, 0, hashDst).Bounce(false)
			addr.StringToBytes(addrTo, addrFrom)

			if equalityFunc(suffix, string(addrTo[strCmpOffset:])) {
				log.Println(
					"========== FOUND ==========\n",
					"Address:", addr.String(), "\n", "Private key:", hex.EncodeToString(pk.Seed()), i,
					"\n========== FOUND ==========",
				)
			}
		}
	}
}

func getHashV3HashFromKey(hash hash.Hash, numCell []byte, dataCell []byte, finalHashBytes []byte, dst []byte) {
	copy(dataCell[6:10], numCell)

	hash.Reset()
	hash.Write(dataCell)
	hash.Sum(finalHashBytes[39:39])

	hash.Reset()
	hash.Write(finalHashBytes)
	hash.Sum(dst[:0])
}
