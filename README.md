# tonutils-go

<img align="right" width="425px" src="https://github.com/xssnick/props/blob/master/logoimg.png?raw=true">

[![Based on TON][ton-svg]][ton]
[![Telegram Channel][tgc-svg]][tg-channel]
![Coverage](https://img.shields.io/badge/Coverage-74.0%25-brightgreen)

Golang library for interacting with TON blockchain.

This library is native golang implementation of ADNL and lite protocol. It works as connection pool and can be connected to multiple lite servers in the same time, balancing is done on lib side.

It is concurrent safe and can be used from multiple goroutines under high workloads.

All main TON protocols are implemented: ADNL, DHT, RLDP, Overlays, etc.

------

If you love this library and want to support its development you can donate any amount of coins to this ton address ☺️
`EQBx6tZZWa2Tbv6BvgcvegoOQxkRrVaBVwBOoW85nbP37_Go`

You can find many usage examples in **[example](https://github.com/xssnick/tonutils-go/tree/master/example)** directory

### How to use
- [Connection](#Connection)
- [Wallet](#Wallet)
  - [Example](https://github.com/xssnick/tonutils-go/blob/master/example/wallet/main.go)
  - [Create](#Wallet)
  - [Transfer](#Wallet)
  - [Balance](#Wallet)
  - [Transfer to many](https://github.com/xssnick/tonutils-go/blob/master/example/highload-wallet/main.go)
  - [Send message to contract](https://github.com/xssnick/tonutils-go/blob/master/example/send-to-contract/main.go)
  - [Build transaction and send from other place](https://github.com/xssnick/tonutils-go/blob/master/example/wallet-cold-alike/main.go#L61)
- [Accounts](#Account-info-and-transactions)
  - [List transactions](#Account-info-and-transactions)
  - [Get balance](https://github.com/xssnick/tonutils-go/blob/master/example/account-state/main.go)
  - [Subscribe on transactions](https://github.com/xssnick/tonutils-go/blob/master/example/accept-payments/main.go)
- [NFT](#NFT)
  - [Details](#NFT)
  - [Mint](https://github.com/xssnick/tonutils-go/blob/master/example/nft-mint/main.go#L42)
  - [Transfer](https://github.com/xssnick/tonutils-go/blob/master/ton/nft/integration_test.go#L89)
- [Jettons](#Jettons)
  - [Details](#Jettons)
  - [Transfer](https://github.com/xssnick/tonutils-go/blob/master/example/jetton-transfer/main.go)
- [DNS](#DNS)
  - [Resolve](#DNS)
  - [Get records](#Records)
  - [Set records](#Records)
- [Contracts](#Contracts)
  - [Use get methods](#Using-GET-methods)
  - [Send external message](#Send-external-message)
  - [Deploy](#Deploy)
- [Cells](#Cells)
  - [Create](#Cells)
  - [Parse](#Cells)
  - [Dictionary](#Dictionary)
  - [TLB Loader/Serializer](#TLB-Loader)
  - [BoC](#BoC)
  - [Proof creation](#Proofs)
  - [ProofTrace](#ProofTrace)
- [Network](https://github.com/xssnick/tonutils-go/tree/master/adnl)
  - [ADNL UDP](https://github.com/xssnick/tonutils-go/blob/master/adnl/adnl_test.go)
- [Custom reconnect policy](#Custom-reconnect-policy)
- [Features to implement](#Features-to-implement)

You could also join our **[Telegram channel](https://t.me/tonutilsnews)** and **[group](https://t.me/tonutils)**, feel free ask any questions :)

### Connection
You can use liteservers from TON configs:
* Mainnet public servers - `https://ton-blockchain.github.io/global.config.json`
* Testnet public servers - `https://ton-blockchain.github.io/testnet-global.config.json`

from liteservers section, you need to convert int to ip and take port and key.

Or you can run your own full node, see TON docs.

You can connect like that:
```golang
client := liteclient.NewConnectionPool()

configUrl := "https://ton-blockchain.github.io/testnet-global.config.json"
err := client.AddConnectionsFromConfigUrl(context.Background(), configUrl)
if err != nil {
    panic(err)
}
api := ton.NewAPIClient(client)
api = api.WithRetryTimeout(0, 5*time.Second) // automatic retries with failover and a per-attempt timeout
```

Since `client` implements connection pool, it can be the chance that some block is not yet applied on one of the nodes, so it can lead to errors.
If you want to bound all requests of operation to single node, use 
```go
ctx := client.StickyContext(context.Background())
```
And pass this context to methods.

### Wallet
You can use existing wallet or generate new one using `wallet.NewSeed()`, wallet will be initialized by the first message sent from it. This library will deploy and initialize wallet contract if it is not initialized yet. 

You can also send any message to any contract using `w.Send` method, it accepts `tlb.InternalMessage` structure, you can dive into `w.Transfer` implementation and see how it works.

Example of basic usage:
```golang
words := strings.Split("birth pattern ...", " ")

w, err := wallet.FromSeed(api, words, wallet.V3)
if err != nil {
    panic(err)
}

balance, err := w.GetBalance(context.Background(), block)
if err != nil {
    panic(err)
}

if balance.Nano().Uint64() >= 3000000 {
    addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")
    err = w.Transfer(context.Background(), addr, tlb.MustFromTON("0.003"), "Hey bro, happy birthday!")
    if err != nil {
        panic(err)
    }
}
```
You can find full working example at `example/wallet/main.go`
### Contracts 
Here is the description of features which allow us to trigger contract's methods

#### Using GET methods
Let's imagine that we have contract with method
```func
cell mult(int a, int b) method_id {
  return begin_cell().store_uint(a * b, 64).end_cell();
}
```

We can trigger it and get result this way:
```golang
// api = initialized ton.APIClient, see Connection in readme

// we need fresh block info to run get methods
block, err := api.CurrentMasterchainInfo(context.Background())
if err != nil {
    panic(err)
}

// contract address
addr := address.MustParseAddr("kQB3P0cDOtkFDdxB77YX-F2DGkrIszmZkmyauMnsP1gg0inM")

// run get method `mult` of contract with int arguments 7 and 8
res, err := api.RunGetMethod(context.Background(), block, addr, "mult", 7, 8)
if err != nil {
    // if contract exit code != 0 it will be treated as an error too
    panic(err)
}

// we are sure that return value is 1 cell, we can directly cast it and parse
val, err := res.MustCell(0).BeginParse().LoadUInt(64)
if err != nil {
    panic(err)
}

// prints 56
println(val)
```

#### Send external message
Using messages, you can interact with contracts to modify state. For example, it can be used to interact with wallet and send transactions to others.

You can send message to contract like that:
```golang
// message body
data := cell.BeginCell().
    MustStoreUInt(777, 64).
    EndCell()

// contract address
addr := address.MustParseAddr("kQBkh8dcas3_OB0uyFEDdVBBSpAWNEgdQ66OYF76N4cDXAFQ")

// send external message, processing fees will be taken from contract
err = api.SendExternalMessage(context.Background(), addr, data)
if err != nil {
    panic(err)
}
```
You can find full working example at `example/external-message/main.go`

#### Deploy
Contracts can be deployed using wallet's method `DeployContract`, 
you should pass 3 cells there: contract code, contract initial data, message body.

You can find example [here](https://github.com/xssnick/tonutils-go/blob/master/example/deploy-nft-collection/main.go)

### Account info and transactions
You can get full account information including balance, stored data and even code using GetAccount method. 
You can also get account's list of transactions with all details.

Example:
```golang
// TON Foundation account
addr := address.MustParseAddr("EQCD39VS5jcptHL8vMjEXrzGaRcCVYto7HUn4bpAOg8xqB2N")

account, err := api.GetAccount(context.Background(), b, addr)
if err != nil {
    log.Fatalln("get account err:", err.Error())
    return
}

// Balance: ACTIVE
fmt.Printf("Status: %s\n", account.State.Status)
// Balance: 66559946.09 TON
fmt.Printf("Balance: %s TON\n", account.State.Balance.String())
if account.Data != nil { // Can be nil if account is not active
    // Data: [0000003829a9a31772c9ed6b62a6e2eba14a93b90462e7a367777beb8a38fb15b9f33844d22ce2ff]
    fmt.Printf("Data: %s\n", account.Data.Dump())
}

// load last 15 transactions
list, err := api.ListTransactions(context.Background(), addr, 15, account.LastTxLT, account.LastTxHash)
if err != nil {
    // In some cases you can get error:
    // lite server error, code XXX: cannot compute block with specified transaction: lt not in db
    // it means that current lite server does not store older data, you can query one with full history
    log.Printf("send err: %s", err.Error())
    return
}

// oldest = first in list
for _, t := range list {
    // Out: 620.9939549 TON, To [EQCtiv7PrMJImWiF2L5oJCgPnzp-VML2CAt5cbn1VsKAxLiE]
    // In: 494.521721 TON, From EQB5lISMH8vLxXpqWph7ZutCS4tU4QdZtrUUpmtgDCsO73JR 
    // ....
    fmt.Println(t.String())
}
```
You can find extended working example at `example/account-state/main.go`

### NFT
You can mint, transfer, and get NFT information using `nft.ItemClient` and `nft.CollectionClient`, like that:
```golang
api := ton.NewAPIClient(client)

nftAddr := address.MustParseAddr("EQDuPc-3EoqH72Gd6M45vmFsktQ8AzqaN14mweJhCjxg0d_b")
item := nft.NewItemClient(api, nftAddr)

nftData, err := item.GetNFTData(context.Background())
if err != nil {
    panic(err)
}

// get info about our nft's collection
collection := nft.NewCollectionClient(api, nftData.CollectionAddress)
collectionData, err := collection.GetCollectionData(context.Background())
if err != nil {
    panic(err)
}

fmt.Println("Collection addr      :", nftData.CollectionAddress.String())
fmt.Println("    content          :", collectionData.Content.(*nft.ContentOffchain).URI)
fmt.Println("    owner            :", collectionData.OwnerAddress.String())
fmt.Println("    minted items num :", collectionData.NextItemIndex)
fmt.Println()
fmt.Println("NFT addr         :", nftAddr.String())
fmt.Println("    initialized  :", nftData.Initialized)
fmt.Println("    owner        :", nftData.OwnerAddress.String())
fmt.Println("    index        :", nftData.Index)

if nftData.Initialized {
    // get full nft's content url using collection method that will merge base url with nft's data
    nftContent, err := collection.GetNFTContent(context.Background(), nftData.Index, nftData.Content)
    if err != nil {
        panic(err)
    }
    fmt.Println("    part content :", nftData.Content.(*nft.ContentOffchain).URI)
    fmt.Println("    full content :", nftContent.(*nft.ContentOffchain).URI)
} else {
    fmt.Println("    empty content")
}
```
You can find full examples at `example/nft-info/main.go` and `example/nft-mint/main.go`

### Jettons
You can get information about jetton, get jetton wallet and its balance, transfer jettons, and burn them using `jetton.Client` like that:
```golang
// jetton contract address
contract := address.MustParseAddr("EQAbMQzuuGiCne0R7QEj9nrXsjM7gNjeVmrlBZouyC-SCLlO")
master := jetton.NewJettonMasterClient(api, contract)

// get information about jetton
data, err := master.GetJettonData(context.Background())
if err != nil {
    log.Fatal(err)
}

log.Println("total supply:", data.TotalSupply.Uint64())

// get jetton wallet for account
ownerAddr := address.MustParseAddr("EQC9bWZd29foipyPOGWlVNVCQzpGAjvi1rGWF7EbNcSVClpA")
jettonWallet, err := master.GetJettonWallet(context.Background(), ownerAddr)
if err != nil {
    log.Fatal(err)
}

jettonBalance, err := jettonWallet.GetBalance(context.Background())
if err != nil {
    log.Fatal(err)
}
log.Println("balance:", jettonBalance.String())
```
You can find full example, which also contains transfer, at `example/jettons/main.go`

### DNS
You can get information about domains, get connected wallet and any other records, transfer domain, edit and do any other nft compatible operations using `dns.Client` like that:
```golang
resolver := dns.NewDNSClient(api, dns.RootContractAddr(api))

domain, err := resolver.Resolve(context.Background(), "alice.ton")
if err != nil {
    panic(err)
}

log.Println("domain points to wallet address:", domain.GetWalletRecord())
```
You can find full example at `example/dns/main.go`

##### Records

You could get any record of the domain using `domain.GetRecord("record name")` method, it will return cell that will contain data structure depends on record type. 
Alternatively you can also use `domain.GetWalletRecord()` and `domain.GetSiteRecord()`, they will return already parsed data, like address for wallet, or slice of bytes with type for site.

To set domain records you can use `domain.BuildSetRecordPayload("record name", dataCell)`, or predefined `BuildSetSiteRecordPayload(adnlAddress)`, `BuildSetWalletRecordPayload(walletAddress)`.
It will generate body cell that you need to send to domain contract from the owner's wallet.

Example:
```golang
// get root dns address from network config
root, err := dns.RootContractAddr(api)
if err != nil {
    panic(err)
}

resolver := dns.NewDNSClient(api, root)
domainInfo, err := resolver.Resolve(ctx, "utils.ton")
if err != nil {
    panic(err)
}

// prepare transaction payload cell which will change site address record
body := domainInfo.BuildSetSiteRecordPayload(adnlAddr)

// w = wallet.FromSeed("domain owner seed phrase")
err = w.Send(context.Background(), &wallet.Message{
  Mode: 1, // pay fees separately (from balance, not from amount)
  InternalMessage: &tlb.InternalMessage{
    Bounce:  true, // return amount in case of processing error
    DstAddr: domainInfo.GetNFTAddress(), // destination is domain contract
    Amount:  tlb.MustFromTON("0.03"),
    Body:    body,
  },
}, true)
if err != nil {
    panic(err)
}

// done! now record of your site is changed!
```

### Cells
Work with cells is very similar to FunC cells:
```golang
builder := cell.BeginCell().MustStoreUInt(0b10, 2).
    MustStoreUInt(0b00, 2). // src addr_none
    MustStoreAddr(addr).    // dst addr
    MustStoreCoins(0)       // import fee 0

builder.MustStoreUInt(0b11, 2). // has state init as cell
    MustStoreRef(cell.BeginCell().
        MustStoreUInt(0b00, 2).                     // no split depth, no special
        MustStoreUInt(1, 1).MustStoreRef(code).     // with code
        MustStoreUInt(1, 1).MustStoreRef(initData). // with data
        MustStoreUInt(0, 1).                        // no libs
    EndCell()).
    MustStoreUInt(0, 1). // slice data
    MustStoreUInt(0, 1)  // 1 bit as body, cause its required

result := builder.EndCell()

// {bits_size}[{hex_data}]
//  279[8800b18cc741b244e114685e1a9e9dc835bff5c157a32a38df49e87b71d0f0d29ba418] -> {
//    5[30] -> {
//      0[],
//      8[01]
//    }
//  }

fmt.Println(result.Dump())
```

Load from cell:
```golang
slice := someCell.BeginParse()
wc := slice.MustLoadUInt(8)
data := slice.MustLoadSlice(256)
```
There are 2 types of methods `Must` and regular. The difference is that in case of error, `Must` will panic, 
but regular will just return error, so use `Must` only when you are sure that your data fits max cell size and other conditions

To debug cells you can use `Dump()` and `DumpBits()` methods of cell, they will return string with beautifully formatted cells and their refs tree

##### Dictionary

Dictionary values are stored as ordinary cells, so in most cases you work with them through `LoadValue` and then parse the returned slice.

```golang
dict := cell.NewDict(32)

key := cell.BeginCell().MustStoreUInt(7, 32).EndCell()
value := cell.BeginCell().
    MustStoreUInt(0xAB, 8).
    MustStoreRef(
        cell.BeginCell().
            MustStoreUInt(0xCDEF, 16).
            EndCell(),
    ).
    EndCell()

if err := dict.Set(key, value); err != nil {
    panic(err)
}

loaded, err := dict.LoadValue(key)
if err != nil {
    panic(err)
}

firstByte := loaded.MustLoadUInt(8)
child, err := loaded.LoadRef()
if err != nil {
    panic(err)
}

fmt.Println(firstByte)                     // 171
fmt.Println(child.MustLoadUInt(16))       // 52719
```

If you need to iterate over all values, use `LoadAll`:

```golang
items, err := dict.LoadAll()
if err != nil {
    panic(err)
}

for _, item := range items {
    keyCell, err := item.Key.ToCell()
    if err != nil {
        panic(err)
    }
    valueCell, err := item.Value.ToCell()
    if err != nil {
        panic(err)
    }

    fmt.Println("key:", keyCell.DumpBits())
    fmt.Println("value:", valueCell.Dump())
}
```
##### BoC

Sometimes it is needed to import or export cell, for example to transfer over the network, for this, Bag of Cells serialization format is exists.

You can simply export cell using `ToBOC()` method of cell, and import it using `cell.FromBOC(bytes)`.

[Example of use can be found in tests](https://github.com/xssnick/tonutils-go/blob/master/tvm/cell/cell_test.go#L76) or in [transfer-url-for-qr](https://github.com/xssnick/tonutils-go/blob/master/example/transfer-url-for-qr/main.go) example

##### Proofs

There are 2 ways to create proofs:

1. low-level `ProofSkeleton`, when you know exact refs you want to keep
2. recommended `ProofTrace`, when proof should follow real parsing flow

Low-level skeleton example:
```golang
sk := cell.CreateProofSkeleton()
sk.ProofRef(0).ProofRef(1)

// Tips:
//   you could also do SetRecursive() on needed ref to add all its child cells to proof
//   you can merge 2 proof skeletons using Merge

merkleProof, err := someCell.CreateProof(sk)
if err != nil {
    t.Fatal(err)
}

fmt.Println(merkleProof.Dump())
```

To check proof you could use `cell.CheckProof(merkleProof, hash)` method, or `cell.UnwrapProof(merkleProof, hash)` if you want to continue to read proof body.

##### ProofTrace

`ProofTrace` is the easiest way to build proof for data you actually loaded from a dictionary, slice or nested structure.

```golang
dict := cell.NewDict(32)

key := cell.BeginCell().MustStoreUInt(7, 32).EndCell()
value := cell.BeginCell().
    MustStoreUInt(0xAB, 8).
    MustStoreRef(
        cell.BeginCell().
            MustStoreUInt(0xCDEF, 16).
            EndCell(),
    ).
    EndCell()

if err := dict.Set(key, value); err != nil {
    panic(err)
}

root := dict.AsCell()

trace := cell.NewProofTrace()
observed := root.AsDict(32).SetObserver(trace)

loaded, err := observed.LoadValue(key)
if err != nil {
    panic(err)
}

// Keep the whole loaded value subtree in ordinary form instead of pruned branch.
trace.MarkRecursive(loaded)

proof, err := root.CreateProof(trace.Skeleton())
if err != nil {
    panic(err)
}

proofBody, err := cell.UnwrapProof(proof, root.Hash())
if err != nil {
    panic(err)
}

loadedFromProof, err := proofBody.AsDict(32).LoadValue(key)
if err != nil {
    panic(err)
}

fmt.Println(loadedFromProof.MustLoadUInt(8)) // 171
```

If your value contains nested dictionary, observer will continue automatically through `LoadDict`:

```golang
trace := cell.NewProofTrace()
outerObserved := outerRoot.AsDict(16).SetObserver(trace)

outerValue, err := outerObserved.LoadValue(outerKey)
if err != nil {
    panic(err)
}

innerDict, err := outerValue.LoadDict(16)
if err != nil {
    panic(err)
}

innerValue, err := innerDict.LoadValue(innerKey)
if err != nil {
    panic(err)
}

trace.MarkRecursive(innerValue)

proof, err := outerRoot.CreateProof(trace.Skeleton())
if err != nil {
    panic(err)
}
```

If you need to continue tracing through manually loaded ref root, use `LoadRefCellWithNode` and `SetObserverNode`:

```golang
trace := cell.NewProofTrace()
observed := root.AsDict(16).SetObserver(trace)

value, err := observed.LoadValue(key)
if err != nil {
    panic(err)
}

refRoot, node, err := value.LoadRefCellWithNode()
if err != nil {
    panic(err)
}

subdict := refRoot.AsDict(16).SetObserverNode(trace, node)
subval, err := subdict.LoadValue(subkey)
if err != nil {
    panic(err)
}

trace.MarkRecursive(subval)
proof, err := root.CreateProof(trace.Skeleton())
if err != nil {
    panic(err)
}
```

Use `MarkRecursive` when the loaded value itself should stay fully readable inside proof.  
If you only need proof of path existence and intermediate branches may be pruned, just omit it.

### TLB Loader
You can load cells directly into Go structs using TLB tags and serialize them back using the same struct definition.

For most application-level code it is recommended to use TLB structs instead of manual raw cell assembly.
Raw `cell.BeginCell()` / `Load*` flows are still useful for low-level work, debugging, uncommon layouts and dynamic structures,
but if payload layout is known in advance, `tlb.LoadFromCell` and `tlb.ToCell` are usually simpler, shorter and less error-prone.

Supported tags:

- `tlb:"-"` - skip field completely
- `tlb:"#deadbeef"` - exact hex magic / constructor prefix for `_ tlb.Magic`
- `tlb:"$1101"` - exact binary magic / constructor prefix for `_ tlb.Magic`
- `tlb:"## N"` - fixed-width integer in `N` bits. For small widths regular Go integer types can be used, for large widths use `*big.Int`
- `tlb:"bits N"` - exactly `N` bits into/from `[]byte`
- `tlb:"bool"` - one-bit boolean
- `tlb:"addr"` - TON address, usually `*address.Address`
- `tlb:"."` - nested struct stored in the current cell
- `tlb:"^"` - nested value stored in a referenced cell. If the field type is `*cell.Cell`, raw referenced cell is returned without nested parsing
- `tlb:"maybe <tag>"` - one presence bit followed by nested value only when present. Common examples: `maybe ^`, `maybe .`
- `tlb:"either X Y"` - one selector bit: `0` means parse/store as `X`, `1` means parse/store as `Y`. During serialization it tries `X` first, then `Y`
- `tlb:"either leave {bits},{refs} X Y"` - same as `either`, but chosen branch must still leave the requested number of free bits and refs in the current builder after serialization
- `tlb:"dict N"` - `HashmapE` dictionary with `N`-bit keys
- `tlb:"dict inline N"` - inline `Hashmap` dictionary with `N`-bit keys
- `tlb:"?FieldName <tag>"` - conditional field. It is loaded/stored only when previously declared boolean field `FieldName` is `true`
- `tlb:"[TypeA,TypeB,...]"` - interface/union field resolved by registered magic. Can be combined with other tags, for example `^ [ShardStateUnsplit,ShardStateSplit]`

Tags can be combined. Typical examples:

- `tlb:"maybe ^"` - optional reference
- `tlb:"maybe ."` - optional inline nested struct
- `tlb:"?HasValue maybe ^"` - conditional optional reference
- `tlb:"^ [TypeA,TypeB]"` - referenced union selected by magic
- `tlb:"dict inline 256"` - inline dictionary with 256-bit keys

Custom parsing/serialization hooks are also supported:

- if a value implements `tlb.Unmarshaler`, `LoadFromCell` calls its custom loader
- if a value implements `tlb.Marshaller`, `ToCell` calls its custom serializer

For exact behavior and edge cases, see comments on `tlb.LoadFromCell` and `tlb.ToCell`.

Example of parsing:
```golang
type ShardState struct {
    _               tlb.Magic  `tlb:"#9023afe2"`
    GlobalID        int32      `tlb:"## 32"`
    ShardIdent      ShardIdent `tlb:"."`
    Seqno           uint32     `tlb:"## 32"`
    OutMsgQueueInfo *cell.Cell `tlb:"^"`
    Accounts        struct {
        ShardAccounts *cell.Dictionary `tlb:"dict 256"`
    } `tlb:"^"`
}

type ShardIdent struct {
    PrefixBits  int8   `tlb:"## 6"`
    WorkchainID int32  `tlb:"## 32"`
    ShardPrefix uint64 `tlb:"## 64"`
}

var state ShardState
if err := tlb.LoadFromCell(&state, cl.BeginParse()); err != nil {
    panic(err)
}

fmt.Println("global id:", state.GlobalID)
fmt.Println("seqno:", state.Seqno)
fmt.Println("accounts dict empty:", state.Accounts.ShardAccounts.IsEmpty())
```

Example of parsing your own payload:
```golang
type ExamplePayload struct {
    _       tlb.Magic  `tlb:"#a1b2c3d4"`
    QueryID uint64     `tlb:"## 64"`
    Flags   uint8      `tlb:"## 8"`
    Body    *cell.Cell `tlb:"^"`
}

payloadCell := cell.BeginCell().
    MustStoreUInt(0xa1b2c3d4, 32).
    MustStoreUInt(123, 64).
    MustStoreUInt(7, 8).
    MustStoreRef(
        cell.BeginCell().
            MustStoreUInt(0xCAFE, 16).
            EndCell(),
    ).
    EndCell()

var payload ExamplePayload
if err := tlb.LoadFromCell(&payload, payloadCell.BeginParse()); err != nil {
    panic(err)
}

fmt.Println(payload.QueryID)                  // 123
fmt.Println(payload.Flags)                    // 7
fmt.Println(payload.Body.BeginParse().MustLoadUInt(16)) // 0xCAFE
```

#### TLB Serialize
You can serialize the same structs back into cells using `tlb.ToCell`.

```golang
type ExamplePayload struct {
    _       tlb.Magic  `tlb:"#a1b2c3d4"`
    QueryID uint64     `tlb:"## 64"`
    Flags   uint8      `tlb:"## 8"`
    Body    *cell.Cell `tlb:"^"`
}

payload := ExamplePayload{
    QueryID: 123,
    Flags:   7,
    Body: cell.BeginCell().
        MustStoreUInt(0xCAFE, 16).
        EndCell(),
}

payloadCell, err := tlb.ToCell(payload)
if err != nil {
    panic(err)
}

fmt.Println(payloadCell.Dump())
```

Roundtrip example:
```golang
payloadCell, err := tlb.ToCell(payload)
if err != nil {
    panic(err)
}

var decoded ExamplePayload
if err = tlb.LoadFromCell(&decoded, payloadCell.BeginParse()); err != nil {
    panic(err)
}

fmt.Println(decoded.QueryID) // 123
```

See also [build NFT mint message](https://github.com/xssnick/tonutils-go/blob/master/ton/nft/collection.go#L189) for a real-world serializer example.

### Custom reconnect policy
By default, standard reconnect method will be used - `c.DefaultReconnect(3*time.Second, 3)` which will do 3 tries and wait 3 seconds after each.

But you can use your own reconnection logic, this library support callbacks, in this case OnDisconnect callback can be used, you can set it like this:
```golang
client.SetOnDisconnect(func(addr, serverKey string) {
	// ... do something
})
```

### Features to implement
* ✅ Support cell and slice as arguments to run get method
* ✅ Reconnect on failure
* ✅ Get account state method
* ✅ Send external message
* ✅ Get transactions
* ✅ Deploy contracts
* ✅ Wallet operations
* ✅ Cell dictionaries support
* ✅ MustLoad methods
* ✅ Parse global config json
* ✅ Jettons
* ✅ DNS
* ✅ ADNL UDP Client/Server
* ✅ ADNL TCP Client/Server
* ✅ RLDP Client/Server
* ✅ TON Sites Client/Server
* ✅ DHT Client
* ✅ Merkle proofs validation and creation
* ✅ Overlays
* ✅ TL Parser/Serializer
* ✅ TL-B Parser/Serializer
* ✅ Payment channels
* ✅ Liteserver proofs automatic validation
* DHT Server
* TVM (Contract execution emulation)

<!-- Badges -->
[ton-svg]: https://img.shields.io/badge/Based%20on-TON-blue
[tgc-svg]: https://img.shields.io/badge/Telegram%20-Subscribe-24A1DE
[tgg-svg]: https://img.shields.io/badge/Telegram%20-Group-blue
[ton]: https://ton.org
[tg-chat]: https://t.me/tonutils
[tg-channel]: https://t.me/tonutilsnews
