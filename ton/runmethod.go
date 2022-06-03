package ton

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/sigurn/crc16"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/utils"
)

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
				case uint64:
					val = x
				}

				if i == 0 {
					builder.MustStoreUInt(1, 8).MustStoreUInt(val, 64).MustStoreRef(refNext)
					break
				}
				refNext = cell.BeginCell().MustStoreUInt(1, 8).MustStoreUInt(val, 64).MustStoreRef(refNext).EndCell()
			case *big.Int:
				if i == 0 {
					builder.MustStoreUInt(2, 8).MustStoreBigInt(v, 256).MustStoreRef(refNext)
					break
				}
				refNext = cell.BeginCell().MustStoreUInt(2, 8).MustStoreBigInt(v, 256).MustStoreRef(refNext).EndCell()
			case *cell.Cell:
				if i == 0 {
					builder.MustStoreUInt(3, 8).MustStoreRef(refNext).MustStoreRef(v)
					break
				}
				refNext = cell.BeginCell().MustStoreUInt(3, 8).MustStoreRef(refNext).MustStoreRef(v).EndCell()
			case []byte:
				ln := 8 * len(v)

				sCell := cell.BeginCell()
				if err := sCell.StoreSlice(v, ln); err != nil {
					return nil, err
				}

				if i == 0 {
					builder.MustStoreUInt(4, 8).
						MustStoreUInt(0, 10).
						MustStoreUInt(uint64(ln), 10).
						MustStoreUInt(0, 6).
						MustStoreRef(refNext).MustStoreRef(sCell.EndCell())
					break
				}
				refNext = cell.BeginCell().MustStoreUInt(4, 8).
					MustStoreUInt(0, 10).
					MustStoreUInt(uint64(ln), 10).
					MustStoreUInt(0, 6).
					MustStoreRef(refNext).MustStoreRef(sCell.EndCell()).EndCell()
			default:
				// TODO: auto convert if possible
				return nil, errors.New("only integers, uints, *big.Int, *cell.Cell, and []byte allowed as contract args")
			}
		}
	}

	req := builder.EndCell().ToBOCWithFlags(false)

	// param
	data = append(data, utils.TLBytes(req)...)

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
			case 2: // uint256 (0x0200)
				intTyp, err := loader.LoadUInt(8)
				if err != nil {
					return nil, err
				}

				if intTyp == 0xFF {
					// TODO: its nan actually
					result = append(result, new(big.Int))
					break
				}

				below0 := intTyp > 0

				val, err := loader.LoadBigInt(256)
				if err != nil {
					return nil, err
				}

				if below0 {
					val = val.Mul(val, new(big.Int).SetInt64(-1))
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
				begin, err := loader.LoadUInt(10)
				if err != nil {
					return nil, err
				}
				end, err := loader.LoadUInt(10)
				if err != nil {
					return nil, err
				}

				// TODO: load ref ids

				ref, err := loader.LoadRef()
				if err != nil {
					return nil, err
				}

				// seek to begin
				_, err = ref.LoadSlice(int(begin))
				if err != nil {
					return nil, err
				}

				sz := int(end - begin)
				payload, err := ref.LoadSlice(sz)
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
	case _LSError:
		return nil, fmt.Errorf("lite server error, code %d: %s", binary.LittleEndian.Uint32(resp.Data), string(resp.Data[5:]))
	}

	return nil, errors.New("unknown response type")
}
