package liteclient

type GlobalConfig struct {
	Type        string             `json:"@type"`
	DHT         DHTConfig          `json:"dht"`
	Liteservers []LiteserverConfig `json:"liteservers"`
	Validator   ValidatorConfig    `json:"validator"`
}

type DHTConfig struct {
	Type        string   `json:"@type"`
	K           int      `json:"k"`
	A           int      `json:"a"`
	StaticNodes DHTNodes `json:"static_nodes"`
}

type DHTNodes struct {
	Type  string    `json:"@type"`
	Nodes []DHTNode `json:"nodes"`
}

type DHTNode struct {
	Type      string         `json:"@type"`
	ID        ServerID       `json:"id"`
	AddrList  DHTAddressList `json:"addr_list"`
	Version   int            `json:"version"`
	Signature string         `json:"signature"`
}

type LiteserverConfig struct {
	IP   int64    `json:"ip"`
	Port int      `json:"port"`
	ID   ServerID `json:"id"`
}

type DHTAddressList struct {
	Type       string       `json:"@type"`
	Addrs      []DHTAddress `json:"addrs"`
	Version    int          `json:"version"`
	ReinitDate int          `json:"reinit_date"`
	Priority   int          `json:"priority"`
	ExpireAt   int          `json:"expire_at"`
}

type DHTAddress struct {
	Type string `json:"@type"`
	IP   int    `json:"ip"`
	Port int    `json:"port"`
}

type ServerID struct {
	Type string `json:"@type"`
	Key  string `json:"key"`
}

type ValidatorConfig struct {
	Type      string        `json:"@type"`
	ZeroState ConfigBlock   `json:"zero_state"`
	InitBlock ConfigBlock   `json:"init_block"`
	Hardforks []ConfigBlock `json:"hardforks"`
}

type ConfigBlock struct {
	Workchain int32  `json:"workchain"`
	Shard     int64  `json:"shard"`
	SeqNo     uint32 `json:"seqno"`
	RootHash  []byte `json:"root_hash"`
	FileHash  []byte `json:"file_hash"`
}
