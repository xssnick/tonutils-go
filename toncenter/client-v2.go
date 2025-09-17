package toncenter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/big"
	"net/url"
	"strconv"
	"strings"
)

type V2 struct {
	client *Client
}

func (c *Client) V2() *V2 {
	return &V2{client: c}
}

func (v *V2) apiBase() string {
	return strings.TrimRight(v.client.baseURL, "/") + "/api/v2"
}

type AddressInformationV2Result struct {
	Balance           NanoCoins      `json:"balance"`
	Code              *cell.Cell     `json:"code"`
	Data              *cell.Cell     `json:"data"`
	LastTransactionID *TransactionID `json:"last_transaction_id"`
	State             string         `json:"state"` // "active", "uninitialized", "frozen"
}

// GetAddressInformation /getAddressInformation
func (v *V2) GetAddressInformation(ctx context.Context, addr *address.Address) (*AddressInformationV2Result, error) {
	q := url.Values{"address": []string{addr.String()}}
	return V2GetCall[AddressInformationV2Result](ctx, v, "getAddressInformation", q)
}

type ExtraCurrencyV2 struct {
	Currency int64     `json:"id"`
	Balance  NanoCoins `json:"amount"`
}

type ExtendedAddressInformationV2 struct {
	Type              string            `json:"@type"`
	Balance           NanoCoins         `json:"balance"`
	ExtraCurrencies   []ExtraCurrencyV2 `json:"extra_currencies"`
	LastTransactionId TransactionID     `json:"last_transaction_id"`
	BlockId           BlockID           `json:"block_id"`
	SyncUtime         int64             `json:"sync_utime"`
	AccountState      map[string]any    `json:"account_state"`
	Revision          int               `json:"revision"`
}

// GetExtendedAddressInformation /getExtendedAddressInformation
func (v *V2) GetExtendedAddressInformation(ctx context.Context, addr *address.Address) (*ExtendedAddressInformationV2, error) {
	q := url.Values{"address": []string{addr.String()}}
	return V2GetCall[ExtendedAddressInformationV2](ctx, v, "getExtendedAddressInformation", q)
}

type WalletInformationV2Result struct {
	IsWallet          bool           `json:"wallet"`
	Balance           NanoCoins      `json:"balance"`
	AccountState      string         `json:"account_state"`
	WalletType        string         `json:"wallet_type"`
	Seqno             uint64         `json:"seqno"`
	LastTransactionID *TransactionID `json:"last_transaction_id"`
	WalletId          int64          `json:"wallet_id"`
}

// GetWalletInformation /getWalletInformation
func (v *V2) GetWalletInformation(ctx context.Context, addr *address.Address) (*WalletInformationV2Result, error) {
	q := url.Values{"address": []string{addr.String()}}
	return V2GetCall[WalletInformationV2Result](ctx, v, "getWalletInformation", q)
}

type MasterchainInfoV2Result struct {
	Last          BlockID `json:"last"`
	StateRootHash []byte  `json:"state_root_hash"`
	Init          BlockID `json:"init"`
}

func (v *V2) GetMasterchainInfo(ctx context.Context) (*MasterchainInfoV2Result, error) {
	return V2GetCall[MasterchainInfoV2Result](ctx, v, "getMasterchainInfo", nil)
}

type MasterchainBlockSignaturesV2Result struct {
	ID         BlockID `json:"id"`
	Signatures []struct {
		NodeID    []byte `json:"node_id"`
		Signature []byte `json:"signature"`
		Validator []byte `json:"validator"`
	} `json:"signatures"`
}

func (v *V2) GetMasterchainBlockSignatures(ctx context.Context, seqno uint64) (*MasterchainBlockSignaturesV2Result, error) {
	q := url.Values{"seqno": []string{strconv.FormatUint(seqno, 10)}}
	return V2GetCall[MasterchainBlockSignaturesV2Result](ctx, v, "getMasterchainBlockSignatures", q)
}

type shardsV2Result struct {
	Shards []BlockID `json:"shards"`
}

func (v *V2) GetShards(ctx context.Context, masterSeqno uint64) ([]BlockID, error) {
	q := url.Values{"seqno": []string{strconv.FormatUint(masterSeqno, 10)}}
	blocks, err := V2GetCall[shardsV2Result](ctx, v, "shards", q)
	if err != nil {
		return nil, err
	}

	return blocks.Shards, nil
}

type ShardProofLinkV2 struct {
	ID    BlockID    `json:"id"`
	Proof *cell.Cell `json:"proof"`
}

type McProofV2 struct {
	ToKeyBlock bool `json:"to_key_block"`

	From       BlockID    `json:"from"`
	To         BlockID    `json:"to"`
	Proof      *cell.Cell `json:"proof"`
	DestProof  *cell.Cell `json:"dest_proof"`
	StateProof *cell.Cell `json:"state_proof"`
}

type ShardBlockProofV2 struct {
	From     BlockID            `json:"from"`
	MasterID BlockID            `json:"mc_id"`
	Links    []ShardProofLinkV2 `json:"links"`
	McProof  []McProofV2        `json:"mc_proof"`
}

func (v *V2) GetShardBlockProof(ctx context.Context, workchain int32, shard int64, seqno uint64, fromSeqno *uint64) (*ShardBlockProofV2, error) {
	q := url.Values{
		"workchain": []string{strconv.FormatInt(int64(workchain), 10)},
		"shard":     []string{strconv.FormatInt(shard, 10)},
		"seqno":     []string{strconv.FormatUint(seqno, 10)},
	}
	if fromSeqno != nil {
		q.Set("from_seqno", strconv.FormatUint(*fromSeqno, 10))
	}

	return V2GetCall[ShardBlockProofV2](ctx, v, "getShardBlockProof", q)
}

func (v *V2) GetAddressBalance(ctx context.Context, addr *address.Address) (*NanoCoins, error) {
	q := url.Values{"address": []string{addr.String()}}
	return V2GetCall[NanoCoins](ctx, v, "getAddressBalance", q)
}

func (v *V2) GetAddressState(ctx context.Context, addr *address.Address) (string, error) {
	q := url.Values{"address": []string{addr.String()}}
	res, err := V2GetCall[string](ctx, v, v.apiBase()+"/getAddressState", q)
	if err != nil {
		return "", err
	}

	return *res, nil
}

type GetTransactionsV2Opts struct {
	Limit    *int
	Lt       *int64
	Hash     *string
	ToLT     *int64
	Archival *bool
}

type MessageV2 struct {
	Source      *AddrInfoV2 `json:"source"`
	Destination *AddrInfoV2 `json:"destination"`
	Value       NanoCoins   `json:"value"`
	FwdFee      NanoCoins   `json:"fwd_fee"`
	IhrFee      NanoCoins   `json:"ihr_fee"`
	CreatedLt   uint64      `json:"created_lt,string"`
	Body        *cell.Cell  `json:"body"`
	Message     []byte      `json:"message"`
}

type TransactionV2 struct {
	TransactionID TransactionID `json:"transaction_id"`
	UnixTime      int64         `json:"utime"`
	Data          *cell.Cell    `json:"data"`

	Fee        NanoCoins `json:"fee"`
	StorageFee NanoCoins `json:"storage_fee"`
	OtherFee   NanoCoins `json:"other_fee"`

	InMsg       *MessageV2  `json:"in_msg"`
	OutMessages []MessageV2 `json:"out_msgs"`

	BlockID   BlockID `json:"block_id"`
	Account   string  `json:"account"`
	Aborted   bool    `json:"aborted"`
	Destroyed bool    `json:"destroyed"`
}

func (v *V2) GetTransactions(ctx context.Context, addr *address.Address, opt *GetTransactionsV2Opts) ([]TransactionV2, error) {
	q := url.Values{"address": []string{addr.String()}}
	if opt != nil {
		if opt.Limit != nil {
			q.Set("limit", strconv.Itoa(*opt.Limit))
		}
		if opt.Lt != nil {
			q.Set("lt", strconv.FormatInt(*opt.Lt, 10))
		}
		if opt.Hash != nil {
			q.Set("hash", *opt.Hash)
		}
		if opt.ToLT != nil {
			q.Set("to_lt", strconv.FormatInt(*opt.ToLT, 10))
		}
		if opt.Archival != nil {
			q.Set("archival", strconv.FormatBool(*opt.Archival))
		}
	}

	res, err := V2GetCall[[]TransactionV2](ctx, v, "getTransactions", q)
	if err != nil {
		return nil, err
	}

	return *res, nil
}

type ConsensusBlockV2Result struct {
	Seqno     uint64  `json:"consensus_block"`
	Timestamp float64 `json:"timestamp"`
}

func (v *V2) GetConsensusBlock(ctx context.Context) (*ConsensusBlockV2Result, error) {
	return V2GetCall[ConsensusBlockV2Result](ctx, v, "getConsensusBlock", nil)
}

type LookupBlockV2Options struct {
	Seqno *uint64
	LT    *uint64
	UTime *int64
	Exact *bool
}

func (v *V2) LookupBlock(ctx context.Context, workchain int32, shard int64, opts *LookupBlockV2Options) (*BlockID, error) {
	if opts == nil {
		return nil, errors.New("at least one option should be specified")
	}

	q := url.Values{
		"workchain": []string{strconv.FormatInt(int64(workchain), 10)},
		"shard":     []string{strconv.FormatInt(shard, 10)},
	}
	if opts.Seqno != nil {
		q.Set("seqno", strconv.FormatUint(*opts.Seqno, 10))
	}
	if opts.LT != nil {
		q.Set("lt", strconv.FormatUint(*opts.LT, 10))
	}
	if opts.UTime != nil {
		q.Set("unixtime", strconv.FormatInt(*opts.UTime, 10))
	}
	return V2GetCall[BlockID](ctx, v, "lookupBlock", q)
}

type BlockTxShort struct {
	Mode    int    `json:"mode"`
	Account string `json:"account"`
	Hash    []byte `json:"hash"`
	LT      uint64 `json:"lt,string"`
}

type BlockTransactionsV2Result struct {
	ID           BlockID        `json:"id"`
	Transactions []BlockTxShort `json:"transactions"`
	Incomplete   bool           `json:"incomplete"`
}

type GetBlockTransactionsV2Options struct {
	RootHash  []byte
	FileHash  []byte
	AfterLt   *uint64
	AfterHash []byte
	Count     *int
}

func (v *V2) GetBlockTransactions(ctx context.Context, workchain int32, shard int64, seqno uint64, opts *GetBlockTransactionsV2Options) (*BlockTransactionsV2Result, error) {
	q := url.Values{
		"workchain": []string{strconv.FormatInt(int64(workchain), 10)},
		"shard":     []string{strconv.FormatInt(shard, 10)},
		"seqno":     []string{strconv.FormatUint(seqno, 10)},
	}
	if opts != nil && opts.RootHash != nil {
		q.Set("root_hash", base64.URLEncoding.EncodeToString(opts.RootHash))
	}
	if opts != nil && opts.FileHash != nil {
		q.Set("file_hash", base64.URLEncoding.EncodeToString(opts.FileHash))
	}
	if opts != nil && opts.AfterLt != nil {
		q.Set("after_lt", strconv.FormatUint(*opts.AfterLt, 10))
	}
	if opts != nil && opts.AfterHash != nil {
		q.Set("after_hash", base64.URLEncoding.EncodeToString(opts.AfterHash))
	}
	if opts != nil && opts.Count != nil {
		q.Set("count", strconv.Itoa(*opts.Count))
	}
	return V2GetCall[BlockTransactionsV2Result](ctx, v, "getBlockTransactions", q)
}

type AddrInfoV2 struct {
	Addr *address.Address
}

func (a *AddrInfoV2) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return fmt.Errorf("empty value")
	}

	if b[0] == '"' && b[len(b)-1] == '"' {
		if len(b) == 2 {
			a.Addr = nil
			return nil
		}

		addr, err := address.ParseAddr(string(b[1 : len(b)-1]))
		if err != nil {
			return err
		}
		a.Addr = addr
		return nil
	}

	var tpd addrInfoTyped
	if err := json.Unmarshal(b, &tpd); err != nil {
		return fmt.Errorf("failed to decode addrInfoTyped: %w (%s)", err, string(b))
	}

	if tpd.Type != "accountAddress" {
		return fmt.Errorf("unexpected type: %s", tpd.Type)
	}

	if tpd.AccountAddress != "" {
		addr, err := address.ParseAddr(tpd.AccountAddress)
		if err != nil {
			return err
		}
		a.Addr = addr
	}

	return nil
}

func (a *AddrInfoV2) MarshalJSON() ([]byte, error) {
	tpd := addrInfoTyped{
		Type: "accountAddress",
	}
	if a.Addr != nil {
		tpd.AccountAddress = a.Addr.String()
	}
	return json.Marshal(tpd)
}

type addrInfoTyped struct {
	Type           string `json:"@type"`
	AccountAddress string `json:"account_address"`
}

type BlockTransactionsExtV2Result struct {
	ID           BlockID         `json:"id"`
	Transactions []TransactionV2 `json:"transactions"`
	Incomplete   bool            `json:"incomplete"`
}

func (v *V2) GetBlockTransactionsExt(ctx context.Context, workchain int32, shard int64, seqno uint64, opts *GetBlockTransactionsV2Options) (*BlockTransactionsExtV2Result, error) {
	q := url.Values{
		"workchain": []string{strconv.FormatInt(int64(workchain), 10)},
		"shard":     []string{strconv.FormatInt(shard, 10)},
		"seqno":     []string{strconv.FormatUint(seqno, 10)},
	}
	if opts != nil && opts.RootHash != nil {
		q.Set("root_hash", base64.URLEncoding.EncodeToString(opts.RootHash))
	}
	if opts != nil && opts.FileHash != nil {
		q.Set("file_hash", base64.URLEncoding.EncodeToString(opts.FileHash))
	}
	if opts != nil && opts.AfterLt != nil {
		q.Set("after_lt", strconv.FormatUint(*opts.AfterLt, 10))
	}
	if opts != nil && opts.AfterHash != nil {
		q.Set("after_hash", base64.URLEncoding.EncodeToString(opts.AfterHash))
	}
	if opts != nil && opts.Count != nil {
		q.Set("count", strconv.Itoa(*opts.Count))
	}
	return V2GetCall[BlockTransactionsExtV2Result](ctx, v, "getBlockTransactionsExt", q)
}

type GetBlockHeaderOptions struct {
	RootHash []byte
	FileHash []byte
}

type BlockHeaderV2Result struct {
	ID                     BlockID   `json:"id"`
	GlobalId               int32     `json:"global_id"`
	Version                uint32    `json:"version"`
	Flags                  uint32    `json:"flags"`
	AfterMerge             bool      `json:"after_merge"`
	AfterSplit             bool      `json:"after_split"`
	BeforeSplit            bool      `json:"before_split"`
	WantMerge              bool      `json:"want_merge"`
	WantSplit              bool      `json:"want_split"`
	ValidatorListHashShort int64     `json:"validator_list_hash_short"`
	CatchainSeqno          uint64    `json:"catchain_seqno"`
	MinRefMcSeqno          uint64    `json:"min_ref_mc_seqno"`
	IsKeyBlock             bool      `json:"is_key_block"`
	PrevKeyBlockSeqno      uint64    `json:"prev_key_block_seqno"`
	StartLt                uint64    `json:"start_lt,string"`
	EndLt                  uint64    `json:"end_lt,string"`
	GenUtime               uint32    `json:"gen_utime"`
	VertSeqno              uint64    `json:"vert_seqno"`
	PrevBlocks             []BlockID `json:"prev_blocks"`
}

func (v *V2) GetBlockHeader(ctx context.Context, workchain int32, shard int64, seqno uint64, opts *GetBlockHeaderOptions) (*BlockHeaderV2Result, error) {
	q := url.Values{
		"workchain": []string{strconv.FormatInt(int64(workchain), 10)},
		"shard":     []string{strconv.FormatInt(shard, 10)},
		"seqno":     []string{strconv.FormatUint(seqno, 10)},
	}
	if opts != nil {
		if opts.RootHash != nil {
			q.Set("root_hash", base64.URLEncoding.EncodeToString(opts.RootHash))
		}
		if opts.FileHash != nil {
			q.Set("file_hash", base64.URLEncoding.EncodeToString(opts.FileHash))
		}
	}
	return V2GetCall[BlockHeaderV2Result](ctx, v, "getBlockHeader", q)
}

type configParamV2Result struct {
	Value struct {
		Param *cell.Cell `json:"bytes"`
	} `json:"config"`
}

func (v *V2) GetConfigParam(ctx context.Context, configID int64, seqno *int64) (*cell.Cell, error) {
	q := url.Values{"config_id": []string{strconv.FormatInt(configID, 10)}}
	if seqno != nil {
		q.Set("seqno", strconv.FormatInt(*seqno, 10))
	}
	res, err := V2GetCall[configParamV2Result](ctx, v, "getConfigParam", q)
	if err != nil {
		return nil, err
	}
	return res.Value.Param, nil
}

func (v *V2) GetConfigAll(ctx context.Context, seqno *int64) (*cell.Cell, error) {
	var q = url.Values{}
	if seqno != nil {
		q.Set("seqno", strconv.FormatInt(*seqno, 10))
	}
	res, err := V2GetCall[configParamV2Result](ctx, v, "getConfigAll", q)
	if err != nil {
		return nil, err
	}
	return res.Value.Param, nil
}

type OutMsgQueueSizeShardV2 struct {
	ID   BlockID `json:"id"`
	Size uint64  `json:"size"`
}

type OutMsgQueueSizesV2Result struct {
	Shards    []OutMsgQueueSizeShardV2 `json:"shards"`
	SizeLimit uint64                   `json:"ext_msg_queue_size_limit"`
}

func (v *V2) GetOutMsgQueueSizes(ctx context.Context) (*OutMsgQueueSizesV2Result, error) {
	return V2GetCall[OutMsgQueueSizesV2Result](ctx, v, "getOutMsgQueueSizes", nil)
}

type TokenDataV2Result struct {
	TotalSupply   *big.Int         `json:"total_supply"`
	Mintable      bool             `json:"mintable"`
	AdminAddress  *address.Address `json:"admin_address"`
	JettonContent struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	} `json:"jetton_content"`
	JettonWalletCode *cell.Cell `json:"jetton_wallet_code"`
	ContractType     string     `json:"contract_type"`
}

func (v *V2) GetTokenData(ctx context.Context, addr *address.Address) (*TokenDataV2Result, error) {
	q := url.Values{"address": []string{addr.String()}}
	return V2GetCall[TokenDataV2Result](ctx, v, "getTokenData", q)
}

// Deprecated: TonCenter advises to not use it, because it is not always finding transaction
func (v *V2) TryLocateTx(ctx context.Context, source, destination *address.Address, createdLT uint64) (*TransactionV2, error) {
	q := url.Values{
		"source":      []string{source.String()},
		"destination": []string{destination.String()},
		"created_lt":  []string{strconv.FormatUint(createdLT, 10)},
	}
	return V2GetCall[TransactionV2](ctx, v, "tryLocateTx", q)
}

// Deprecated: TonCenter advises to not use it, because it is not always finding transaction
func (v *V2) TryLocateResultTx(ctx context.Context, source, destination *address.Address, createdLT uint64) (*TransactionV2, error) {
	q := url.Values{
		"source":      []string{source.String()},
		"destination": []string{destination.String()},
		"created_lt":  []string{strconv.FormatUint(createdLT, 10)},
	}
	return V2GetCall[TransactionV2](ctx, v, "tryLocateResultTx", q)
}

// Deprecated: TonCenter advises to not use it, because it is not always finding transaction
func (v *V2) TryLocateSourceTx(ctx context.Context, source, destination *address.Address, createdLT uint64) (*TransactionV2, error) {
	q := url.Values{
		"source":      []string{source.String()},
		"destination": []string{destination.String()},
		"created_lt":  []string{strconv.FormatUint(createdLT, 10)},
	}
	return V2GetCall[TransactionV2](ctx, v, "tryLocateSourceTx", q)
}

func (v *V2) SendBoc(ctx context.Context, data []byte) error {
	_, err := V2PostCall[any](ctx, v, "sendBoc", map[string][]byte{
		"boc": data,
	})
	return err
}

type EstimateFeeRequest struct {
	Address      *address.Address `json:"address"`
	Body         *cell.Cell       `json:"body"`
	InitCode     *cell.Cell       `json:"init_code,omitempty"`
	InitData     *cell.Cell       `json:"init_data,omitempty"`
	IgnoreChkSig bool             `json:"ignore_chksig"`
}

type Fee struct {
	InFwdFee   uint64 `json:"in_fwd_fee"`
	StorageFee uint64 `json:"storage_fee"`
	GasFee     uint64 `json:"gas_fee"`
	FwdFee     uint64 `json:"fwd_fee"`
}

type EstimateFeeV2Result struct {
	SourceFees      Fee   `json:"source_fees"`
	DestinationFees []Fee `json:"destination_fees"`
}

func (v *V2) EstimateFee(ctx context.Context, req EstimateFeeRequest) (*EstimateFeeV2Result, error) {
	return V2PostCall[EstimateFeeV2Result](ctx, v, "estimateFee", req)
}

type RunGetMethodV2Result struct {
	GasUsed           uint64
	Stack             []any
	ExitCode          int
	BlockId           BlockID
	LastTransactionId *TransactionID
}

// Deprecated: Use RunGetMethod from V3 api
//
// RunGetMethod - Call contract method, stack elements could be *address.Address, *cell.Cell, *cell.Slice, and *big.Int
func (v *V2) RunGetMethod(ctx context.Context, addr *address.Address, method string, stack []any, masterSeqno *uint64) (*RunGetMethodV2Result, error) {
	type runGetMethodRequest struct {
		Address *address.Address `json:"address"`
		Method  string           `json:"method,omitempty"`

		// Array of stack elements: *cell.Cell, *cell.Slice, and *big.Int supported
		Stack [][]string `json:"stack"`

		MasterSeqno *uint64 `json:"seqno,omitempty"`
	}

	type runGetMethodResult struct {
		GasUsed           uint64         `json:"gas_used"`
		Stack             [][]any        `json:"stack"`
		ExitCode          int            `json:"exit_code"`
		BlockId           BlockID        `json:"block_id"`
		LastTransactionId *TransactionID `json:"last_transaction_id"`
	}

	var stk = [][]string{}
	for _, a := range stack {
		switch val := a.(type) {
		case *cell.Cell:
			stk = append(stk, []string{"tvm.Cell", base64.StdEncoding.EncodeToString(val.ToBOC())})
		case *cell.Slice:
			stk = append(stk, []string{"tvm.Slice", base64.StdEncoding.EncodeToString(val.MustToCell().ToBOC())})
		case *address.Address:
			stk = append(stk, []string{"tvm.Slice", base64.StdEncoding.EncodeToString(cell.BeginCell().MustStoreAddr(val).EndCell().ToBOC())})
		case *big.Int:
			if val == nil {
				return nil, fmt.Errorf("nil big.Int")
			}
			stk = append(stk, []string{"num", "0x" + val.Text(16)})
		default:
			return nil, fmt.Errorf("unsupported stack element type")
		}
	}

	res, err := V2PostCall[runGetMethodResult](ctx, v, "runGetMethod", runGetMethodRequest{
		Address:     addr,
		Method:      method,
		Stack:       stk,
		MasterSeqno: masterSeqno,
	})
	if err != nil {
		return nil, err
	}

	stack, err = parseStackV2(res.Stack)
	if err != nil {
		return nil, fmt.Errorf("failed to parse stack: %w", err)
	}

	return &RunGetMethodV2Result{
		GasUsed:           res.GasUsed,
		Stack:             stack,
		ExitCode:          res.ExitCode,
		BlockId:           res.BlockId,
		LastTransactionId: res.LastTransactionId,
	}, nil
}

func parseStackV2(stack [][]any) ([]any, error) {
	var stk []any
	for _, a := range stack {
		if len(a) != 2 {
			return nil, fmt.Errorf("incorrect stack element")
		}

		name, ok := a[0].(string)
		if !ok {
			return nil, fmt.Errorf("incorrect stack element name type")
		}

		val, ok := a[1].(string)
		if !ok {
			return nil, fmt.Errorf("result stack type '%s' is not supported for v2 api due to incompatible format, use LS or v3", name)
		}

		switch name {
		case "num":
			if !strings.HasPrefix(val, "0x") {
				return nil, fmt.Errorf("invalid number format")
			}
			val = val[2:]

			res, _ := new(big.Int).SetString(val, 16)
			if res == nil {
				return nil, fmt.Errorf("invalid number format")
			}

			stk = append(stk, res)
		default:
			return nil, fmt.Errorf("unsupported stack element type")
		}
	}
	return stk, nil
}

func V2PostCall[T any](ctx context.Context, v *V2, method string, req any) (*T, error) {
	return doPOST[T](ctx, v.client, v.apiBase()+"/"+method, req, false)
}

func V2GetCall[T any](ctx context.Context, v *V2, method string, query url.Values) (*T, error) {
	return doGET[T](ctx, v.client, v.apiBase()+"/"+method, query, false)
}
