package main

import (
	"context"
	"encoding/base64"
	"log"
	"strings"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func main() {
	client := liteclient.NewConnectionPool()

	// connect to mainnet lite server
	err := client.AddConnection(context.Background(), "135.181.140.212:13206", "K0t3+IWLOXHYMvMcrGZDPs+pn58a17LFbnXoQkKc2xw=")
	if err != nil {
		log.Fatalln("connection err: ", err.Error())
		return
	}

	api := ton.NewAPIClient(client)
	// bound all requests to single ton node
	ctx := client.StickyContext(context.Background())

	// seed words of account, you can generate them with any wallet or using wallet.NewSeed() method
	words := strings.Split("birth pattern then forest walnut then phrase walnut fan pumpkin pattern then cluster blossom verify then forest velvet pond fiction pattern collect then then", " ")

	w, err := wallet.FromSeed(api, words, wallet.V3)
	if err != nil {
		log.Fatalln("FromSeed err:", err.Error())
		return
	}

	log.Println("wallet address:", w.WalletAddress())

	block, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		log.Fatalln("CurrentMasterchainInfo err:", err.Error())
		return
	}

	// get seqNo of the wallet, this is needed in the cold environment
	var accountSeqNo uint32
	resp, err := api.WaitForBlock(block.SeqNo).RunGetMethod(ctx, block, w.WalletAddress(), "seqno")
	if err != nil {
		log.Fatalln("get seqno err: %w", err)
		return
	}

	iSeq, err := resp.Int(0)
	if err != nil {
		log.Fatalln("failed to parse seqno: %w", err)
		return
	}
	accountSeqNo = uint32(iSeq.Uint64())

	// determine if the wallet has been initialized, this is needed in the cold environment
	acc, err := api.WaitForBlock(block.SeqNo).GetAccount(ctx, block, w.WalletAddress())
	if err != nil {
		log.Fatalln("failed to get account state: %w", err)
	}

	initialized := acc.IsActive && acc.State.Status == tlb.AccountStatusActive

	balance, err := w.GetBalance(ctx, block)
	if err != nil {
		log.Fatalln("GetBalance err:", err.Error())
		return
	}

	if balance.Nano().Uint64() >= 3000000 {
		addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")

		log.Println("sending transaction...")

		//////////////////////////////////////////////////////
		// 1. build internal message in hot/online environment
		//////////////////////////////////////////////////////

		// default message ttl is 3 minutes, it is time during which you can send it to blockchain
		// if you need to set longer TTL, you could use this method
		// w.GetSpec().(*wallet.SpecV3).SetMessagesTTL(uint32((10 * time.Minute) / time.Second))

		// if destination wallet is not initialized you should set bounce = true
		msg, err := w.BuildTransfer(addr, tlb.MustFromTON("0.003"), false, "Hello from tonutils-go!")
		if err != nil {
			log.Fatalln("BuildTransfer err:", err.Error())
			return
		}

		/////////////////////////////////////////////////////////////////////////////////////
		// 2. serialize internal message to BoC before transiting to cold/offline environment
		//    note: you'll need to also pass accountSeqNo and the initilized bool to the cold
		//          environment because there won't be internet access.
		/////////////////////////////////////////////////////////////////////////////////////

		msgCell, err := tlb.ToCell(msg)
		if err != nil {
			log.Fatalln("ToCell err:", err.Error())
			return
		}

		msgCellB64 := base64.StdEncoding.EncodeToString(msgCell.ToBOC())
		log.Printf("internal message: %s, ready to be transited to cold system", msgCellB64)

		////////////////////////////////////////////////////////////////
		// 3. sign in the cold/offline environment after deserialization
		////////////////////////////////////////////////////////////////

		coldMsgCellBOC, err := base64.StdEncoding.DecodeString(msgCellB64)
		if err != nil {
			log.Fatalln("DecodeString err:", err.Error())
			return
		}

		coldMsgCell, err := cell.FromBOC(coldMsgCellBOC)
		if err != nil {
			log.Fatalln("FromBOC err:", err.Error())
			return
		}

		var coldMsg wallet.Message
		err = tlb.LoadFromCell(&coldMsg, coldMsgCell.BeginParse())
		if err != nil {
			log.Fatalln("LoadFromCell err:", err.Error())
			return
		}

		// pack message to send later or from other place
		ext, err := w.BuildExternalMessageOffline(ctx, accountSeqNo, initialized, &coldMsg)
		if err != nil {
			log.Fatalln("BuildExternalMessage err:", err.Error())
			return
		}

		////////////////////////////////////////////////////////////////////////////////////
		// 4. serialize and transit external message to hot/online environment for broadcast
		////////////////////////////////////////////////////////////////////////////////////

		extCell, err := tlb.ToCell(ext)
		if err != nil {
			log.Fatalln("ToCell err:", err.Error())
			return
		}

		extCellB64 := base64.StdEncoding.EncodeToString(extCell.ToBOC())

		log.Printf("signed external message: %s, ready to be transited to hot system", extCellB64)

		///////////////////////////////////////////////////////////
		// 5. deserialize external message and broadcast to network
		///////////////////////////////////////////////////////////

		hotMsgCellBOC, err := base64.StdEncoding.DecodeString(extCellB64)
		if err != nil {
			log.Fatalln("DecodeString err:", err.Error())
			return
		}

		hotMsgCell, err := cell.FromBOC(hotMsgCellBOC)
		if err != nil {
			log.Fatalln("FromBOC err:", err.Error())
			return
		}

		var hotMsg tlb.ExternalMessage
		err = tlb.LoadFromCell(&hotMsg, hotMsgCell.BeginParse())
		if err != nil {
			log.Fatalln("LoadFromCell err:", err.Error())
			return
		}

		// send message to blockchain
		err = api.SendExternalMessage(ctx, &hotMsg)
		if err != nil {
			log.Fatalln("Failed to send external message:", err.Error())
			return
		}

		log.Println("transaction sent, we are not waiting for confirmation")

		return
	}

	log.Println("not enough balance:", balance.String())
}
