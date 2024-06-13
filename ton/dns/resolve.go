package dns

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/nft"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

var ErrNoSuchRecord = fmt.Errorf("no such dns record")

const _CategoryNextResolver = 0xba93
const _CategoryContractAddr = 0x9fd3
const _CategoryADNLSite = 0xad01
const _CategoryStorageSite = 0x7473

type TonApi interface {
	WaitForBlock(seqno uint32) ton.APIClientWrapped
	CurrentMasterchainInfo(ctx context.Context) (_ *ton.BlockIDExt, err error)
	RunGetMethod(ctx context.Context, blockInfo *ton.BlockIDExt, addr *address.Address, method string, params ...any) (*ton.ExecutionResult, error)
	GetBlockchainConfig(ctx context.Context, block *ton.BlockIDExt, onlyParams ...int32) (*ton.BlockchainConfig, error)
}

type Domain struct {
	Records *cell.Dictionary
	*nft.ItemEditableClient
}

type Client struct {
	root *address.Address
	api  TonApi
}

var randomizer = func() uint64 {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return binary.LittleEndian.Uint64(buf)
}

// Deprecated: use GetRootContractAddr
func RootContractAddr(api TonApi) (*address.Address, error) {
	return GetRootContractAddr(context.Background(), api)
}

func GetRootContractAddr(ctx context.Context, api TonApi) (*address.Address, error) {
	b, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get masterchain info: %w", err)
	}

	cfg, err := api.GetBlockchainConfig(ctx, b, 4)
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

func (c *Client) ResolveAtBlock(ctx context.Context, domain string, b *ton.BlockIDExt) (*Domain, error) {
	chain := strings.Split(domain, ".")
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 { // reverse array
		chain[i], chain[j] = chain[j], chain[i]
	}
	return c.resolve(ctx, c.root, strings.Join(chain, "\x00")+"\x00", b)
}

func (c *Client) resolve(ctx context.Context, contractAddr *address.Address, chain string, b *ton.BlockIDExt) (*Domain, error) {
	name := []byte(chain)
	nameCell := cell.BeginCell()

	if err := nameCell.StoreSlice(name, uint(len(name)*8)); err != nil {
		return nil, fmt.Errorf("failed to pack domain name: %w", err)
	}

	res, err := c.api.WaitForBlock(b.SeqNo).RunGetMethod(ctx, b, contractAddr, "dnsresolve", nameCell.EndCell().BeginParse(), 0)
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

	p, err := rec.BeginParse().LoadRef()
	if err != nil {
		return nil
	}

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

func (d *Domain) GetSiteRecord() (_ []byte, inStorage bool) {
	rec := d.GetRecord("site")
	if rec == nil {
		return nil, false
	}

	p, err := rec.BeginParse().LoadRef()
	if err != nil {
		return nil, false
	}

	category, err := p.LoadUInt(16)
	if err != nil {
		return nil, false
	}

	switch category {
	case _CategoryStorageSite:
		bagId, err := p.LoadSlice(256)
		if err != nil {
			return nil, true
		}
		return bagId, true
	case _CategoryADNLSite:
		addr, err := p.LoadSlice(256)
		if err != nil {
			return nil, false
		}
		return addr, false
	}
	return nil, false
}

func (d *Domain) BuildSetRecordPayload(name string, value *cell.Cell) *cell.Cell {
	const OPChangeDNSRecord = 0x4eb1f0f9

	h := sha256.New()
	h.Write([]byte(name))

	return cell.BeginCell().MustStoreUInt(OPChangeDNSRecord, 32).
		MustStoreUInt(randomizer(), 64).
		MustStoreSlice(h.Sum(nil), 256).MustStoreBuilder(value.ToBuilder()).EndCell()
}

func (d *Domain) BuildSetSiteRecordPayload(addr []byte, isStorage bool) *cell.Cell {
	var payload *cell.Cell
	if isStorage {
		payload = cell.BeginCell().MustStoreUInt(_CategoryStorageSite, 16).
			MustStoreSlice(addr, 256).
			EndCell()
	} else {
		payload = cell.BeginCell().MustStoreUInt(_CategoryADNLSite, 16).
			MustStoreSlice(addr, 256).
			MustStoreUInt(0, 8).
			EndCell()
	}
	// https://github.com/ton-blockchain/TEPs/blob/master/text/0081-dns-standard.md#dns-records
	return d.BuildSetRecordPayload("site", cell.BeginCell().MustStoreRef(payload).EndCell())
}

func (d *Domain) BuildSetWalletRecordPayload(addr *address.Address) *cell.Cell {
	record := cell.BeginCell().MustStoreUInt(_CategoryContractAddr, 16).MustStoreAddr(addr).EndCell()
	return d.BuildSetRecordPayload("wallet", record)
}
