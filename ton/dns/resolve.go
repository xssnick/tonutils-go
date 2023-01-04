package dns

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/nft"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

var ErrNoSuchRecord = fmt.Errorf("no such dns record")

const _CategoryNextResolver = 0xba93
const _CategoryContractAddr = 0x9fd3
const _CategoryADNLSite = 0xad01

type TonApi interface {
	CurrentMasterchainInfo(ctx context.Context) (_ *tlb.BlockInfo, err error)
	RunGetMethod(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...any) (*ton.ExecutionResult, error)
	GetBlockchainConfig(ctx context.Context, block *tlb.BlockInfo, onlyParams ...int32) (*ton.BlockchainConfig, error)
}

type Domain struct {
	Records *cell.Dictionary
	*nft.ItemEditableClient
}

type Client struct {
	root *address.Address
	api  TonApi
}

func RootContractAddr(api TonApi) (*address.Address, error) {
	b, err := api.CurrentMasterchainInfo(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	cfg, err := api.GetBlockchainConfig(context.Background(), b, 4)
	if err != nil {
		return nil, fmt.Errorf("failed to get root address from network config: %w", err)
	}

	data := cfg.Get(4)
	if data == nil {
		return nil, fmt.Errorf("failed to get root address from network config")
	}

	hash, err := data.BeginParse().LoadSlice(256)
	if err != nil {
		return nil, fmt.Errorf("failed to get root address from network config 4, failed to load hash: %w", err)
	}

	// TODO: get from config
	return address.NewAddress(0, 255, hash), nil
}

func NewDNSClient(api TonApi, root *address.Address) *Client {
	return &Client{
		root: root,
		api:  api,
	}
}

func (c *Client) Resolve(ctx context.Context, domain string) (*Domain, error) {
	b, err := c.api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}
	return c.ResolveAtBlock(ctx, domain, b)
}

func (c *Client) ResolveAtBlock(ctx context.Context, domain string, b *tlb.BlockInfo) (*Domain, error) {
	chain := strings.Split(domain, ".")
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 { // reverse array
		chain[i], chain[j] = chain[j], chain[i]
	}
	return c.resolve(ctx, c.root, strings.Join(chain, "\x00")+"\x00", b)
}

func (c *Client) resolve(ctx context.Context, contractAddr *address.Address, chain string, b *tlb.BlockInfo) (*Domain, error) {
	name := []byte(chain)
	nameCell := cell.BeginCell()

	if err := nameCell.StoreSlice(name, uint(len(name)*8)); err != nil {
		return nil, fmt.Errorf("failed to pack domain name: %w", err)
	}

	res, err := c.api.RunGetMethod(ctx, b, contractAddr, "dnsresolve", nameCell.EndCell().BeginParse(), 0)
	if err != nil {
		if cErr, ok := err.(ton.ContractExecError); ok && cErr.Code == ton.ErrCodeContractNotInitialized {
			return nil, ErrNoSuchRecord
		}
		return nil, fmt.Errorf("failed to run dnsresolve method: %w", err)
	}

	bits, err := res.Int(0)
	if err != nil {
		return nil, fmt.Errorf("bits get err: %w", err)
	}

	if bits.Uint64()%8 != 0 {
		return nil, fmt.Errorf("resolved bits is not mod 8")
	}
	bytesResolved := int(bits.Uint64() / 8)

	data, err := res.Cell(1)
	if err != nil {
		if yes, _ := res.IsNil(1); yes {
			// domain is not taken from auction, consider it as a valid domain but with no records
			return &Domain{
				Records:            cell.NewDict(256),
				ItemEditableClient: nft.NewItemEditableClient(c.api, contractAddr),
			}, nil
		}
		return nil, fmt.Errorf("data get err: %w", err)
	}

	s := data.BeginParse()

	var category uint64
	if len(chain) > bytesResolved { // if partially resolved
		category, err = s.LoadUInt(16)
		if err != nil {
			return nil, fmt.Errorf("failed to load category: %w", err)
		}

		if category != _CategoryNextResolver {
			return nil, fmt.Errorf("failed to load next dns, unexpected category: %x", category)
		}

		nextRoot, err := s.LoadAddr()
		if err != nil {
			return nil, fmt.Errorf("failed to load next root: %w", err)
		}

		return c.resolve(ctx, nextRoot, chain[bytesResolved:], b)
	}

	records, err := s.ToDict(256)
	if err != nil {
		return nil, fmt.Errorf("failed to load recirds dict: %w", err)
	}

	return &Domain{
		Records:            records,
		ItemEditableClient: nft.NewItemEditableClient(c.api, contractAddr),
	}, nil
}

func (d *Domain) GetRecord(name string) *cell.Cell {
	h := sha256.New()
	h.Write([]byte(name))

	return d.Records.Get(cell.BeginCell().MustStoreSlice(h.Sum(nil), 256).EndCell())
}

func (d *Domain) GetWalletRecord() *address.Address {
	rec := d.GetRecord("wallet")
	if rec == nil {
		return nil
	}
	p := rec.BeginParse()

	category, err := p.LoadUInt(16)
	if err != nil {
		return nil
	}

	if category != _CategoryContractAddr {
		return nil
	}

	addr, err := p.LoadAddr()
	if err != nil {
		return nil
	}

	return addr
}

func (d *Domain) GetSiteRecord() []byte {
	rec := d.GetRecord("site")
	if rec == nil {
		return nil
	}
	p := rec.BeginParse()

	p, err := p.LoadRef()
	if err != nil {
		return nil
	}

	category, err := p.LoadUInt(16)
	if err != nil {
		return nil
	}

	if category != _CategoryADNLSite {
		return nil
	}

	addr, err := p.LoadSlice(256)
	if err != nil {
		return nil
	}

	return addr
}

func (d *Domain) BuildSetRecordPayload(name string, value *cell.Cell) *cell.Cell {
	h := sha256.New()
	h.Write([]byte(name))

	return cell.BeginCell().MustStoreSlice(h.Sum(nil), 256).MustStoreRef(value).EndCell()
}

func (d *Domain) BuildSetSiteRecordPayload(adnlAddress []byte) *cell.Cell {
	record := cell.BeginCell().MustStoreUInt(_CategoryADNLSite, 16).MustStoreSlice(adnlAddress, 256).EndCell()
	return d.BuildSetRecordPayload("site", record)
}

func (d *Domain) BuildSetWalletRecordPayload(addr *address.Address) *cell.Cell {
	record := cell.BeginCell().MustStoreUInt(_CategoryContractAddr, 16).MustStoreAddr(addr).EndCell()
	return d.BuildSetRecordPayload("wallet", record)
}
