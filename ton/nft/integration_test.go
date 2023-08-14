package nft

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"

	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

var api = func() ton.APIClientWrapped {
	client := liteclient.NewConnectionPool()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.AddConnectionsFromConfigUrl(ctx, "https://ton-blockchain.github.io/testnet-global.config.json")
	if err != nil {
		panic(err)
	}

	return ton.NewAPIClient(client).WithRetry()
}()

var _seed = os.Getenv("WALLET_SEED")

func Test_NftMintTransfer(t *testing.T) {
	seed := strings.Split(_seed, " ")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ctx = api.Client().StickyContext(ctx)

	w, err := wallet.FromSeed(api, seed, wallet.HighloadV2R2)
	if err != nil {
		t.Fatal("FromSeed err:", err.Error())
	}

	log.Println("test wallet address:", w.Address())

	block, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		t.Fatal("CurrentMasterchainInfo err:", err.Error())
	}

	balance, err := w.GetBalance(ctx, block)
	if err != nil {
		t.Fatal("GetBalance err:", err.Error())
	}

	if balance.Nano().Uint64() < 3000000 {
		t.Fatal("not enough balance", w.Address(), balance.String())
	}

	collectionAddr := address.MustParseAddr("EQBTObWUuWTb5ECnLI4x6a3szzstmMDOcc5Kdo-CpbUY9Y5K") // address = deployCollection(w) w.seed = (fiction ... rather)
	collection := NewCollectionClient(api, collectionAddr)
	collectionData, err := collection.GetCollectionData(ctx)
	if err != nil {
		panic(err)
	}

	nftAddr, err := collection.GetNFTAddressByIndex(ctx, collectionData.NextItemIndex)
	if err != nil {
		t.Fatal("GetNFTAddressByIndex err:", err.Error())
	}

	itemURI := fmt.Sprint(collectionData.NextItemIndex) + "/" + fmt.Sprint(collectionData.NextItemIndex) + ".json"
	mintData, err := collection.BuildMintPayload(collectionData.NextItemIndex, w.Address(), tlb.MustFromTON("0.01"), &ContentOffchain{
		URI: itemURI,
	})
	if err != nil {
		t.Fatal("BuildMintPayload err:", err.Error())
	}

	fmt.Println("Minting NFT...")
	mint := wallet.SimpleMessage(collectionAddr, tlb.MustFromTON("0.025"), mintData)

	err = w.Send(ctx, mint, true)
	if err != nil {
		t.Fatal("Send err:", err.Error())
	}

	fmt.Println("Minted NFT:", nftAddr.String(), 0)

	newAddr := address.MustParseAddr("EQB9ElEc88x6kOZytvUp0_18U-W1V-lBdvPcZ-BXdBWVrmeA") // address wallet with other seed = ("together ... lounge")
	nft := NewItemClient(api, nftAddr)
	transferData, err := nft.BuildTransferPayload(newAddr, tlb.MustFromTON("0.01"), nil)
	if err != nil {
		t.Fatal("BuildMintPayload err:", err.Error())
	}

	fmt.Println("Transferring NFT...")
	transfer := wallet.SimpleMessage(nftAddr, tlb.MustFromTON("0.065"), transferData)

	_, block, err = w.SendWaitTransaction(context.Background(), transfer)
	if err != nil {
		t.Fatal("Send err:", err.Error())
	}

	newData, err := nft.GetNFTDataAtBlock(ctx, block)
	if err != nil {
		t.Fatal("GetNFTData err:", err.Error())
	}

	fullContent, err := collection.GetNFTContentAtBlock(ctx, collectionData.NextItemIndex, newData.Content, block)
	if err != nil {
		t.Fatal("GetNFTData err:", err.Error())
	}

	if fullContent.(*ContentOffchain).URI != "https://tonutils.com/items/"+itemURI {
		t.Fatal("full content incorrect", fullContent.(*ContentOffchain).URI)
	}

	roy, err := collection.RoyaltyParamsAtBlock(ctx, block)
	if err != nil {
		t.Fatal("RoyaltyParams err:", err.Error())
	}

	if roy.Address.String() != "EQCyMa4xOmcr6H0OWbcYtwOTsf6YUSW3KuGj010WgWBVQhoJ" { // address which get paid for nft. Wallet.seed = (fiction ... rather)
		t.Fatal("royalty addr invalid")
	}

	if roy.Base != 0 || roy.Factor != 0 {
		t.Fatal("royalty invalid")
	}

	fmt.Println("Owner:", newData.OwnerAddress.String())
	fmt.Println("Full content:", fullContent.(*ContentOffchain).URI)
	fmt.Println("Royalty:", roy.Address.String(), roy.Base, "/", roy.Factor)

	if newData.OwnerAddress.String() != newAddr.String() {
		t.Fatal("nft owner not updated")
	}
}

func deployCollection(w *wallet.Wallet) {
	roy := cell.BeginCell().MustStoreUInt(0, 16).MustStoreUInt(0, 16).MustStoreAddr(w.Address()).EndCell()

	main := ContentOffchain{URI: "https://tonutils.com/main.json"}
	mainCell, _ := main.ContentCell()

	// https://github.com/ton-blockchain/TIPs/issues/64
	// Standard says that prefix should be 0x01, but looks like it was misunderstanding in other implementations and 0x01 was dropped
	// so, we make compatibility
	commonContentCell := cell.BeginCell().MustStoreStringSnake("https://tonutils.com/items/").EndCell()

	cc := cell.BeginCell().MustStoreRef(mainCell).MustStoreRef(commonContentCell).EndCell()
	dd := cell.BeginCell().MustStoreAddr(w.Address()).MustStoreUInt(0, 64).MustStoreRef(cc).MustStoreRef(getItemCode()).MustStoreRef(roy).EndCell()

	fff, err := w.DeployContract(context.Background(), tlb.MustFromTON("0.05"), cell.BeginCell().EndCell(), getContractCode(),
		dd, true)
	if err != nil {
		panic(err.Error())
	}

	println("deployed:", fff.String())
}

func getContractCode() *cell.Cell {
	// data from toncli's .boc compiled file
	var contractMsg = "B5EE9C724102170100023D0001458801F1A5CE7BF12B9C12787A9BB2044D8B4C7AA4F862AFD922B81719ECA94AAEBF361A010201340203010EFF00F80088FB0404001000000000000000000114FF00F4A413F4BCF2C80B0502016206070202CD0809020120111203EBD10638048ADF000E8698180B8D848ADF07D201800E98FE99FF6A2687D20699FEA6A6A184108349E9CA829405D47141BAF8280E8410854658056B84008646582A802E78B127D010A65B509E58FE59F80E78B64C0207D80701B28B9E382F970C892E000F18112E001718119026001F1812F82C207F97840A0B0C0201200D0E00603502D33F5313BBF2E1925313BA01FA00D43028103459F0068E1201A44343C85005CF1613CB3FCCCCCCC9ED54925F05E200A6357003D4308E378040F4966FA5208E2906A4208100FABE93F2C18FDE81019321A05325BBF2F402FA00D43022544B30F00623BA9302A402DE04926C21E2B3E6303250444313C85005CF1613CB3FCCCCCCC9ED54002801FA40304144C85005CF1613CB3FCCCCCCC9ED540201200F10003D45AF0047021F005778018C8CB0558CF165004FA0213CB6B12CCCCC971FB008002D007232CFFE0A33C5B25C083232C044FD003D0032C03260001B3E401D3232C084B281F2FFF2742002012013140025BC82DF6A2687D20699FEA6A6A182DE86A182C40043B8B5D31ED44D0FA40D33FD4D4D43010245F04D0D431D430D071C8CB0701CF16CCC980201201516002FB5DAFDA89A1F481A67FA9A9A860D883A1A61FA61FF480610002DB4F47DA89A1F481A67FA9A9A86028BE09E008E003E00B03AAA233D"
	msgBytes, _ := hex.DecodeString(contractMsg)

	msgCell, err := cell.FromBOC(msgBytes)
	if err != nil {
		panic(err)
	}

	state := &tlb.StateInit{}
	err = tlb.LoadFromCell(state, msgCell.BeginParse().MustLoadRef())
	if err != nil {
		panic(err)
	}

	// we need to load 1 ref of code because its build for external deploy, but we need internal
	return state.Code.BeginParse().MustLoadRef().MustToCell()
}

func getItemCode() *cell.Cell {
	// data from toncli's .boc compiled file
	var contractMsg = "B5EE9C724102110100020F0001458800146DF9E025F24542CFEBD1999AAF53625AEE1994ED01B0C8FFB7BB89445206C01A010201340203010EFF00F80088FB0404001000000000000000000114FF00F4A413F4BCF2C80B0502016206070202CE08090009A11F9FE0050201200A0B0201200F1002D70C8871C02497C0F83434C0C05C6C2497C0F83E903E900C7E800C5C75C87E800C7E800C3C00812CE3850C1B088D148CB1C17CB865407E90350C0408FC00F801B4C7F4CFE08417F30F45148C2EA3A1CC840DD78C9004F80C0D0D0D4D60840BF2C9A884AEB8C097C12103FCBC200C0D00113E910C1C2EBCB8536001F65135C705F2E191FA4021F001FA40D20031FA00820AFAF0801BA121945315A0A1DE22D70B01C300209206A19136E220C2FFF2E192218E3E821005138D91C85009CF16500BCF16712449145446A0708010C8CB055007CF165005FA0215CB6A12CB1FCB3F226EB39458CF17019132E201C901FB00104794102A375BE20E00727082108B77173505C8CBFF5004CF1610248040708010C8CB055007CF165005FA0215CB6A12CB1FCB3F226EB39458CF17019132E201C901FB000082028E3526F0018210D53276DB103744006D71708010C8CB055007CF165005FA0215CB6A12CB1FCB3F226EB39458CF17019132E201C901FB0093303234E25502F003003B3B513434CFFE900835D27080269FC07E90350C04090408F80C1C165B5B60001D00F232CFD633C58073C5B3327B5520999FA27B"
	msgBytes, _ := hex.DecodeString(contractMsg)

	msgCell, err := cell.FromBOC(msgBytes)
	if err != nil {
		panic(err)
	}

	state := &tlb.StateInit{}
	err = tlb.LoadFromCell(state, msgCell.BeginParse().MustLoadRef())
	if err != nil {
		panic(err)
	}

	// we need to load 1 ref of code because its build for external deploy, but we need internal
	return state.Code.BeginParse().MustLoadRef().MustToCell()
}
