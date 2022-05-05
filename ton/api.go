package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/sigurn/crc16"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/liteclient/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

var _GetMasterchainInfo int32 = -1984567762
var _RunContractGetMethod int32 = 1556504018

var LSError int32 = -1146494648

var _RunQueryResult int32 = -1550163605

type LiteClient interface {
	Do(ctx context.Context, typeID int32, payload []byte) (*liteclient.LiteResponse, error)
}

type APIClient struct {
	client LiteClient
}

func NewAPIClient(client LiteClient) *APIClient {
	return &APIClient{
		client: client,
	}
}

func (c *APIClient) GetBlockInfo(ctx context.Context) (*tlb.BlockInfo, error) {
	resp, err := c.client.Do(ctx, _GetMasterchainInfo, nil)
	if err != nil {
		return nil, err
	}

	block := new(tlb.BlockInfo)
	_, err = block.Load(resp.Data)
	if err != nil {
		return nil, err
	}

	return block, nil
}

func (c *APIClient) RunGetMethod(ctx context.Context, blockInfo *tlb.BlockInfo, addr *address.Address, method string, params ...interface{}) ([]interface{}, error) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, 0b00000100)

	data = append(data, blockInfo.Serialize()...)

	chain := make([]byte, 4)
	binary.LittleEndian.PutUint32(chain, uint32(addr.Workchain()))

	data = append(data, chain...)
	data = append(data, addr.Data()...)

	// https://github.com/ton-blockchain/ton/blob/24dc184a2ea67f9c47042b4104bbb4d82289fac1/crypto/smc-envelope/SmartContract.h#L75
	mName := make([]byte, 8)
	crc := uint64(crc16.Checksum([]byte(method), crc16.MakeTable(crc16.CRC16_XMODEM))) | 0x10000
	binary.LittleEndian.PutUint64(mName, crc)
	data = append(data, mName...)

	builder := cell.BeginCell().MustStoreUInt(0, 16).MustStoreUInt(uint64(len(params)), 8)
	if len(params) > 0 {
		// we need to send in reverse order
		reversed := make([]any, len(params))
		for i := 0; i < len(params); i++ {
			reversed[(len(params)-1)-i] = params[i]
		}
		params = reversed

		var refNext = cell.BeginCell().EndCell()

		for i := len(params) - 1; i >= 0; i-- {
			switch v := params[i].(type) {
			case uint64, uint32, uint16, uint8, uint, int:
				var val uint64
				switch x := params[i].(type) {
				case int:
					val = uint64(x)
				case uint:
					val = uint64(x)
				case uint32:
					val = uint64(x)
				case uint16:
					val = uint64(x)
				case uint8:
					val = uint64(x)
				}

				if i == len(params)-1 {
					refNext = cell.BeginCell().MustStoreUInt(1, 8).MustStoreUInt(val, 64).MustStoreRef(refNext).EndCell()
					break
				}
				builder.MustStoreUInt(1, 8).MustStoreUInt(val, 64).MustStoreRef(refNext)
			case *big.Int:
				if i == len(params)-1 {
					refNext = cell.BeginCell().MustStoreUInt(2, 8).MustStoreBigInt(v, 256).MustStoreRef(refNext).EndCell()
					break
				}
				builder.MustStoreUInt(2, 8).MustStoreBigInt(v, 256).MustStoreRef(refNext)
			case *cell.Cell:
				if i == len(params)-1 {
					refNext = cell.BeginCell().MustStoreUInt(3, 8).MustStoreRef(refNext).MustStoreRef(v).EndCell()
					break
				}
				builder.MustStoreUInt(3, 8).MustStoreRef(refNext).MustStoreRef(v)
			/*case []byte:
			sCell := cell.BeginCell()
			if err := sCell.StoreSlice(v, 8*len(v)); err != nil {
				return nil, err
			}

			if i == len(params)-1 {
				refNext = cell.BeginCell().MustStoreUInt(4, 8).MustStoreRef(refNext).MustStoreRef(sCell.EndCell()).EndCell()
				break
			}
			builder.MustStoreUInt(4, 8).MustStoreRef(refNext).MustStoreRef(sCell.EndCell())*/
			default:
				// TODO: auto convert if possible
				return nil, errors.New("currently only int, uints and *big.Int allowed as params currently")
			}
		}
	}

	req := builder.EndCell().ToBOCWithFlags(false)

	// param
	data = append(data, byte(len(req)))
	data = append(data, req...)

	if round := (len(req) + 1) % 4; round != 0 {
		data = append(data, make([]byte, 4-round)...)
	}

	resp, err := c.client.Do(ctx, _RunContractGetMethod, data)
	if err != nil {
		return nil, err
	}

	switch resp.TypeID {
	case _RunQueryResult:
		// TODO: mode
		_ = binary.LittleEndian.Uint32(resp.Data)

		resp.Data = resp.Data[4:]

		b := new(tlb.BlockInfo)
		resp.Data, err = b.Load(resp.Data)
		if err != nil {
			return nil, err
		}

		shard := new(tlb.BlockInfo)
		resp.Data, err = shard.Load(resp.Data)
		if err != nil {
			return nil, err
		}

		// TODO: check proofs mode

		exitCode := binary.LittleEndian.Uint32(resp.Data)
		resp.Data = resp.Data[4:]

		if exitCode != 0 {
			return nil, fmt.Errorf("contract exit code: %d", exitCode)
		}

		var state []byte
		state, resp.Data = loadBytes(resp.Data)

		cl, err := cell.FromBOC(state)
		if err != nil {
			return nil, err
		}

		loader := cl.BeginParse()

		// TODO: better parsing
		// skip first 2 bytes, dont know exact meaning yet, but not so critical
		_, err = loader.LoadUInt(16)
		if err != nil {
			return nil, err
		}

		// return params num
		num, err := loader.LoadUInt(8)
		if err != nil {
			return nil, err
		}

		var result []any

		for i := 0; i < int(num); i++ {
			// value type
			typ, err := loader.LoadUInt(8)
			if err != nil {
				return nil, err
			}

			next, err := loader.LoadRef()
			if err != nil {
				return nil, err
			}

			switch typ {
			case 1: // uint64
				val, err := loader.LoadUInt(64)
				if err != nil {
					return nil, err
				}

				result = append(result, val)
			case 2: // uint256
				val, err := loader.LoadBigInt(256)
				if err != nil {
					return nil, err
				}

				result = append(result, val)
			case 3: // cell
				ref, err := loader.LoadRef()
				if err != nil {
					return nil, err
				}

				c, err := ref.ToCell()
				if err != nil {
					return nil, err
				}

				result = append(result, c)
			case 4: // slice
				ref, err := loader.LoadRef()
				if err != nil {
					return nil, err
				}

				sz, payload, err := ref.RestBits()
				if err != nil {
					return nil, err
				}

				// represent slice as cell, to parse it easier
				result = append(result, cell.BeginCell().MustStoreSlice(payload, sz).EndCell())
			default:
				return nil, errors.New("unknown data type of result in response: " + fmt.Sprint(typ))
			}

			loader = next
		}

		// it comes in reverse order from lite server, reverse it back
		reversed := make([]any, len(result))
		for i := 0; i < len(result); i++ {
			reversed[(len(result)-1)-i] = result[i]
		}

		return reversed, nil
	case LSError:
		return nil, fmt.Errorf("lite server error, code %d: %s", binary.LittleEndian.Uint32(resp.Data), string(resp.Data[5:]))
	}

	return nil, errors.New("unknown response type")
}

func loadBytes(data []byte) (loaded []byte, buffer []byte) {
	offset := 1
	ln := int(data[0])
	if ln == 0xFE {
		ln = int(binary.LittleEndian.Uint32(data)) >> 8
		offset = 4
	}

	return data[offset : offset+ln], data[offset+ln:]
}
