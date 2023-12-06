package ton

import (
	"encoding/hex"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"testing"
)

func TestLoadShardsFromHashes(t *testing.T) {
	data, err := hex.DecodeString("b5ee9c724102090100010b000103d040012201c002032201c00405284801012610bab489d8faa8c9dfaa65e8281895cfc66591881d1d5351574975ce386f2b00032201c00607284801013ca47d35fc14db1a5f2e33f74cf3e833974de0fea9f49759ea84d2522124d12b000201eb50134ea4181081ebe000013951e6cc660000013951e6cc6608c7a91c9653b122d1e49487ecc663e5bb59d8974d6ddd03a4c5cdd7c325498e92b103dc863224263143d3b59124e2a4bce36ddd4ce7f4c43ae0476430da34061280003e18b880000000000000001080fd6b2b80b3cecae02d12000000c90828480101c2256a5539b179d8831bcbfb692dc691ba4c604a72a60ff375306e2b29764a4900010013407735940203b9aca0202872d22f")
	if err != nil {
		t.Fatal(err)
	}

	cl, err := cell.FromBOC(data)
	if err != nil {
		t.Fatal(err)
	}

	di, err := cl.BeginParse().ToDict(32)
	if err != nil {
		t.Fatal(err)
	}

	gotShards, err := LoadShardsFromHashes(di, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotShards) != 1 {
		t.Fatal("not 1 shard")
	}

	gotShards, err = LoadShardsFromHashes(di, false)
	if err == nil {
		t.Fatal("should be err")
	}
}
