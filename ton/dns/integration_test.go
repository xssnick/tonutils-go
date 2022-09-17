package dns

import (
	"context"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/ton"
	"testing"
	"time"
)

var api = func() *ton.APIClient {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	return ton.NewAPIClient(client)
}()

func TestDNSClient_Resolve(t *testing.T) {
	cli := NewDNSClient(api, RootContractAddr(api))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	d, err := cli.Resolve(ctx, "alice.ton")
	if err != nil {
		t.Fatal(err)
	}

	wal := d.GetWalletRecord()

	iData, err := d.GetNFTData(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if iData.OwnerAddress.String() != "EQA0i8-CdGnF_DhUHHf92R1ONH6sIA9vLZ_WLcCIhfBBXwtG" {
		t.Fatal("owner diff")
	}

	if wal.String() != "EQA0i8-CdGnF_DhUHHf92R1ONH6sIA9vLZ_WLcCIhfBBXwtG" {
		t.Fatal("wallet record diff")
	}
}

func TestDNSClient_ResolveSub(t *testing.T) {
	cli := NewDNSClient(api, RootContractAddr(api))
	_, err := cli.Resolve(context.Background(), "aa.alice.ton")
	if err != nil {
		if err != ErrNoSuchRecord {
			t.Fatal(err)
		}
	}

	_, err = cli.Resolve(context.Background(), "buchbahpih.ton")
	if err != nil {
		if err != ErrNoSuchRecord {
			t.Fatal(err)
		}
	}
}
