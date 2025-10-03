package toncenter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"math/big"
	"net/url"
	"strconv"
	"strings"
)

type V3 struct {
	client *Client
}

func (c *Client) V3() *V3 {
	return &V3{client: c}
}

func (v *V3) apiBase() string {
	return strings.TrimRight(v.client.baseURL, "/") + "/api/v3"
}

type stackElementV3 struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// /api/v3/adjacentTransactions

func (v *V3) EstimateFee(ctx context.Context, req EstimateFeeRequest) (*EstimateFeeV2Result, error) {
	return V3PostCall[EstimateFeeV2Result](ctx, v, "estimateFee", req)
}

type AddressInformationV3Result struct {
	Balance    NanoCoins  `json:"balance"`
	Code       *cell.Cell `json:"code"`
	Data       *cell.Cell `json:"data"`
	FrozenHash []byte     `json:"frozen_hash"`
	LastTxHash []byte     `json:"last_transaction_hash"`
	LastTxLT   uint64     `json:"last_transaction_lt,string"`
	Status     string     `json:"status"` // "active", "uninitialized", "frozen"
}

// GetAddressInformation /getAddressInformation
func (v *V3) GetAddressInformation(ctx context.Context, addr *address.Address) (*AddressInformationV3Result, error) {
	q := url.Values{"address": []string{addr.String()}}
	return V3GetCall[AddressInformationV3Result](ctx, v, "addressInformation", q)
}

type WalletInformationV3Result struct {
	Balance    NanoCoins `json:"balance"`
	LastTxHash []byte    `json:"last_transaction_hash"`
	LastTxLT   uint64    `json:"last_transaction_lt,string"`
	Seqno      uint64    `json:"seqno"`
	Status     string    `json:"status"`
	WalletId   int64     `json:"wallet_id"`
	WalletType string    `json:"wallet_type"`
}

// GetWalletInformation /getWalletInformation
func (v *V3) GetWalletInformation(ctx context.Context, addr *address.Address) (*WalletInformationV3Result, error) {
	q := url.Values{"address": []string{addr.String()}}
	return V3GetCall[WalletInformationV3Result](ctx, v, "walletInformation", q)
}

type RunGetMethodV3Result struct {
	GasUsed           uint64
	Stack             []any
	ExitCode          int
	BlockId           BlockID
	LastTransactionId *TransactionID
}

// RunGetMethod - Call contract method, stack elements could be *address.Address, *cell.Cell, *cell.Slice, and *big.Int
func (v *V3) RunGetMethod(ctx context.Context, addr *address.Address, method string, stack []any, masterSeqno *uint64) (*RunGetMethodV3Result, error) {
	type runGetMethodRequest struct {
		Address     *address.Address `json:"address"`
		Method      string           `json:"method,omitempty"`
		Stack       []stackElementV3 `json:"stack"`
		MasterSeqno *uint64          `json:"seqno,omitempty"`
	}

	type runGetMethodResult struct {
		GasUsed           uint64           `json:"gas_used"`
		Stack             []stackElementV3 `json:"stack"`
		ExitCode          int              `json:"exit_code"`
		BlockId           BlockID          `json:"block_id"`
		LastTransactionId *TransactionID   `json:"last_transaction_id"`
	}

	var stk = []stackElementV3{}
	for _, a := range stack {
		switch val := a.(type) {
		case *cell.Cell:
			stk = append(stk, stackElementV3{
				Type:  "cell",
				Value: json.RawMessage(strconv.Quote(base64.StdEncoding.EncodeToString(val.ToBOC()))),
			})
		case *cell.Slice:
			stk = append(stk, stackElementV3{
				Type:  "slice",
				Value: json.RawMessage(strconv.Quote(base64.StdEncoding.EncodeToString(val.MustToCell().ToBOC()))),
			})
		case *address.Address:
			stk = append(stk, stackElementV3{
				Type:  "slice",
				Value: json.RawMessage(strconv.Quote(base64.StdEncoding.EncodeToString(cell.BeginCell().MustStoreAddr(val).EndCell().ToBOC()))),
			})
		case *big.Int:
			if val == nil {
				return nil, fmt.Errorf("nil big.Int")
			}
			stk = append(stk, stackElementV3{
				Type:  "num",
				Value: json.RawMessage(strconv.Quote("0x" + val.Text(16))),
			})
		default:
			return nil, fmt.Errorf("unsupported stack element type")
		}
	}

	res, err := V3PostCall[runGetMethodResult](ctx, v, "runGetMethod", runGetMethodRequest{
		Address:     addr,
		Method:      method,
		Stack:       stk,
		MasterSeqno: masterSeqno,
	})
	if err != nil {
		return nil, err
	}

	stack, err = parseStackV3(res.Stack)
	if err != nil {
		return nil, fmt.Errorf("failed to parse stack: %w", err)
	}

	return &RunGetMethodV3Result{
		GasUsed:           res.GasUsed,
		Stack:             stack,
		ExitCode:          res.ExitCode,
		BlockId:           res.BlockId,
		LastTransactionId: res.LastTransactionId,
	}, nil
}

func parseStackV3(stack []stackElementV3) ([]any, error) {
	var stk []any
	for _, a := range stack {
		switch a.Type {
		case "cell", "slice":
			var val string
			if err := json.Unmarshal(a.Value, &val); err != nil {
				return nil, fmt.Errorf("failed to unmarshal stack element: %w", err)
			}

			b, err := base64.StdEncoding.DecodeString(val)
			if err != nil {
				return nil, err
			}

			c, err := cell.FromBOC(b)
			if err != nil {
				return nil, err
			}

			if a.Type == "cell" {
				stk = append(stk, c)
			} else {
				stk = append(stk, c.BeginParse())
			}
		case "num":
			var val string
			if err := json.Unmarshal(a.Value, &val); err != nil {
				return nil, fmt.Errorf("failed to unmarshal stack element: %w", err)
			}

			if !strings.HasPrefix(val, "0x") {
				return nil, fmt.Errorf("invalid number format")
			}
			val = val[2:]

			res, _ := new(big.Int).SetString(val, 16)
			if res == nil {
				return nil, fmt.Errorf("invalid number format")
			}

			stk = append(stk, res)
		case "tuple":
			var val []stackElementV3
			if err := json.Unmarshal(a.Value, &val); err != nil {
				return nil, fmt.Errorf("failed to unmarshal stack element: %w", err)
			}

			tup, err := parseStackV3(val)
			if err != nil {
				return nil, fmt.Errorf("failed to parse tuple: %w", err)
			}

			stk = append(stk, tup)
		default:
			return nil, fmt.Errorf("unsupported stack element type")
		}
	}
	return stk, nil
}

func (v *V3) SendMessage(ctx context.Context, data []byte) error {
	_, err := V3PostCall[any](ctx, v, "message", map[string][]byte{
		"boc": data,
	})
	return err
}

func V3PostCall[T any](ctx context.Context, v *V3, method string, req any) (*T, error) {
	return doPOST[T](ctx, v.client, v.apiBase()+"/"+method, req, true)
}

func V3GetCall[T any](ctx context.Context, v *V3, method string, query url.Values) (*T, error) {
	return doGET[T](ctx, v.client, v.apiBase()+"/"+method, query, true)
}
