# tonutils-go
[![Based on TON][ton-svg]][ton]
![Coverage](https://img.shields.io/badge/Coverage-71.6%25-brightgreen)

Golang library for interacting with TON blockchain.

This library is native golang implementation of ADNL and lite protocol. It works as connection pool and can be connected to multiple lite servers in the same time, balancing is done on lib side.

It is concurrent safe and can be used from multiple goroutines under high workloads.

If you love this library and want to support its development you can donate any amount of coins to this ton address ☺️
`EQBx6tZZWa2Tbv6BvgcvegoOQxkRrVaBVwBOoW85nbP37_Go`

### How to use
- [Connection](#Connection)
- [Wallet](#Wallet)
  - [Transfer](#Wallet)
  - [Balance](#Wallet)
- [Accounts](#Account-info-and-transactions)
  - [List transactions](#Account-info-and-transactions)
- [Contracts](#Contracts)
  - [Use get methods](#Using-GET-methods)
  - [Send external message](#Send-external-message)
- [Cells](#Cells)
  - [Create](#Cells)
  - [Parse](#Cells)
  - [TLB Loader](#TLB-Loader)
- [Custom reconnect policy](#Custom-reconnect-policy)
- [Features to implement](#Features-to-implement)


You can find usage examples in **[example](https://github.com/xssnick/tonutils-go/tree/master/example)** directory

You can also join our **[Telegram group](https://t.me/tonutils)** and ask any questions :)

### Connection
You can get list of public lite servers from official TON configs:
* Mainnet - `https://ton-blockchain.github.io/global.config.json`
* Testnet - `https://ton-blockchain.github.io/testnet-global.config.json`

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
```
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

if balance.NanoTON().Uint64() >= 3000000 {
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
block, err := api.GetMasterchainInfo(context.Background())
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
val, err := res[0].(*cell.Cell).BeginParse().LoadUInt(64)
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
fmt.Printf("Balance: %s TON\n", account.State.Balance.TON())
if account.Data != nil { // Can be nil if account is not active
    // Data: [0000003829a9a31772c9ed6b62a6e2eba14a93b90462e7a367777beb8a38fb15b9f33844d22ce2ff]
    fmt.Printf("Data: %s\n", account.Data.Dump())
}

// load last 15 transactions
list, err := api.ListTransactions(context.Background(), addr, 15, account.LastTxLT, account.LastTxHash)
if err != nil {
    // In some cases you can get error:
    // lite server error, code 4294966896: cannot compute block with specified transaction: lt not in db
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

### TLB Loader
You can also load cells to structures, similar to JSON, using tags. 
You can find more details in comment-description of `tlb.LoadFromCell` method

Example:
```golang
type ShardState struct {
    _               Magic      `tlb:"#9023afe2"`
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
if err = tlb.LoadFromCell(&state, cl.BeginParse()); err != nil {
    panic(err)
}
```
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
* Event subscriptions
* Payment channels
* DNS
* Merkle proofs


<!-- Badges -->
[ton-svg]: https://img.shields.io/badge/Based%20on-TON-blue
[ton]: https://ton.org
[tg-chat]: https://t.me/tonutils