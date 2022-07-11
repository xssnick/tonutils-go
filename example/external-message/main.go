package main

import (
	"context"
	"log"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

/*
This example is for such contract.
It is recommended to deploy your own before run this script
because this address can have not enough TON due to many executions of this example.
Or you can at least add some coins to contract address

() recv_external(slice in_msg) impure {
  int seqno = in_msg~load_uint(64);
  int n = in_msg.preload_uint(16);

  var data = get_data().begin_parse();
  int stored_seq = data~load_uint(64);

  throw_if(409, seqno != stored_seq);

  accept_message();

  int total = data.preload_uint(64);
  set_data(begin_cell().store_uint(stored_seq + 1, 64).store_uint(total + n, 64).end_cell());
}

(int, int) get_total() method_id {
  var data = get_data().begin_parse();
  int stored_seq = data~load_uint(64);

  return (stored_seq, data.preload_uint(64));
}
*/

func main() {
	client := liteclient.NewConnectionPool()

	// connect to testnet lite server
	err := client.AddConnection(context.Background(), "65.21.74.140:46427", "JhXt7H1dZTgxQTIyGiYV4f9VUARuDxFl/1kVBjLSMB8=")
	if err != nil {
		log.Fatalln("connection err: ", err.Error())
		return
	}

	// initialize ton api lite connection wrapper
	api := ton.NewAPIClient(client)

	// we need fresh block info to run get methods
	block, err := api.GetMasterchainInfo(context.Background())
	if err != nil {
		log.Fatalln("get block err:", err.Error())
		return
	}

	// call method to get seqno of contract
	res, err := api.RunGetMethod(context.Background(), block, address.MustParseAddr("kQBkh8dcas3_OB0uyFEDdVBBSpAWNEgdQ66OYF76N4cDXAFQ"), "get_total")
	if err != nil {
		log.Fatalln("run get method err:", err.Error())
		return
	}

	seqno := res[0].(uint64)
	total := res[1].(uint64)

	log.Printf("Current seqno = %d and total = %d", seqno, total)

	data := cell.BeginCell().
		MustStoreUInt(seqno, 64).
		MustStoreUInt(1, 16). // add 1 to total
		EndCell()

	err = api.SendExternalMessage(context.Background(), &tlb.ExternalMessage{
		DstAddr: address.MustParseAddr("kQBkh8dcas3_OB0uyFEDdVBBSpAWNEgdQ66OYF76N4cDXAFQ"),
		Body:    data,
	})
	if err != nil {
		// FYI: it can fail if not enough balance on contract
		log.Printf("send err: %s", err.Error())
		return
	}

	log.Println("External message successfully processed and should be added to blockchain soon!")
	log.Println("Rerun this script in a couple seconds and you should see total and seqno changed.")
}
