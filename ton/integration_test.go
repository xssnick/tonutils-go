package ton

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

var api = func() *APIClient {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.AddConnection(ctx, "135.181.140.212:13206", "K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw=")
	if err != nil {
		panic(err)
	}

	return NewAPIClient(client)
}()

var testContractAddr = func() *address.Address {
	return address.MustParseAddr("kQBL2_3lMiyywU17g-or8N7v9hDmPCpttzBPE2isF2GTziky")
}()

func Test_RunMethod(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	b, err := api.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	c1 := cell.BeginCell().MustStoreUInt(0xAA, 8).EndCell().BeginParse()
	c2 := cell.BeginCell().MustStoreUInt(0xBB, 8).EndCell()

	res, err := api.RunGetMethod(ctx, b, testContractAddr, "clltst2", c1, c2)
	if err != nil {
		t.Fatal("run get method err:", err.Error())
		return
	}

	if !bytes.Equal(res[0].(*cell.Slice).MustToCell().Hash(), c1.MustToCell().Hash()) {
		t.Fatal("1st arg not eeq return 1st value")
	}

	cmp2 := cell.BeginCell().MustStoreUInt(0xAA, 8).MustStoreRef(c2).EndCell()
	if !bytes.Equal(res[1].(*cell.Cell).Hash(), cmp2.Hash()) {
		t.Fatal("1st arg not eeq return 1st value")
	}
}

func Test_ExternalMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	b, err := api.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	res, err := api.RunGetMethod(ctx, b, testContractAddr, "get_total")
	if err != nil {
		t.Fatal("run get method err:", err.Error())
		return
	}

	seqno := res[0].(int64)
	total := res[1].(int64)

	data := cell.BeginCell().
		MustStoreInt(seqno, 64).
		MustStoreUInt(1, 16). // add 1 to total
		EndCell()

	err = api.SendExternalMessage(ctx, &tlb.ExternalMessage{
		DstAddr: testContractAddr,
		Body:    data,
	})
	if err != nil {
		// FYI: it can fail if not enough balance on contract
		t.Fatal("SendExternalMessage err:", err.Error())
		return
	}

	// TODO: wait for update and check result

	log.Printf("Current seqno = %d and total = %d", seqno, total)
}

func Test_Account(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	b, err := api.GetMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("get block err:", err.Error())
		return
	}

	addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")
	res, err := api.GetAccount(ctx, b, addr)
	if err != nil {
		t.Fatal("get account err:", err.Error())
		return
	}

	fmt.Printf("Is active: %v\n", res.IsActive)
	if res.IsActive {
		fmt.Printf("Status: %s\n", res.State.Status)
		fmt.Printf("Balance: %s TON\n", res.State.Balance.TON())
		if res.Data != nil {
			fmt.Printf("Data: %s\n", res.Data.Dump())
		}
	} else {
		t.Fatal("TF account not active")
	}

	// take last tx info from account info
	lastHash := res.LastTxHash
	lastLt := res.LastTxLT

	fmt.Printf("\nTransactions:\n")
	for i := 0; i < 5; i++ {
		// last transaction has 0 prev lt
		if lastLt == 0 {
			break
		}

		// load transactions in batches with size 15
		list, err := api.ListTransactions(ctx, addr, 15, lastLt, lastHash)
		if err != nil {
			t.Fatal("send err:", err.Error())
			return
		}

		// oldest = first in list
		for _, t := range list {
			fmt.Println(t.String())
		}

		// set previous info from the oldest transaction in list
		lastHash = list[0].PrevTxHash
		lastLt = list[0].PrevTxLT
	}
}
