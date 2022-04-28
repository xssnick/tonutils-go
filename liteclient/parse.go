package liteclient

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

var TCPPing int32 = 1292381082
var TCPPong int32 = -597034237

var LSError int32 = -1146494648

var RunQueryResult int32 = -1550163605

var AccountStateResp int32 = 1887029073

var GetMasterchainInfo int32 = -1984567762
var MasterchainInfoResp int32 = -2055001983

var TimeResp int32 = -380436467
var VersionResp int32 = 1510248933

var ADNLQuery int32 = -1265895046
var ADNLQueryResponse int32 = 262964246

var LiteServerQuery int32 = 2039219935

func (c *Client) parseServerResp(data []byte) (typ int32, queryID string, payload []byte, err error) {
	if len(data) <= 4 {
		err = fmt.Errorf("too short adnl packet: %d", len(data))
		return
	}

	typ = int32(binary.LittleEndian.Uint32(data))
	data = data[4:]

	switch typ {
	case TCPPong:
		if len(data) < 8 {
			err = fmt.Errorf("too short pong packet: %d", len(data))
			return
		}
		queryID = hex.EncodeToString(data[:8])
		return
	case ADNLQueryResponse:
		if len(data) <= 32 {
			err = fmt.Errorf("too short adnl query response packet: %d", len(data))
			return
		}

		queryID = hex.EncodeToString(data[:32])

		data = data[32:]

		ln := int(data[0])
		if ln == 0xFE {
			if len(data) <= 4 {
				err = fmt.Errorf("too short adnl query response packet: %d", len(data))
				return
			}
			ln = int(binary.LittleEndian.Uint32(data[0:])) >> 8
			data = data[4:]
		} else {
			data = data[1:]
		}

		if len(data) < ln {
			err = fmt.Errorf("adnl payload size incorrect: %d", ln)
			return
		}

		typ = int32(binary.LittleEndian.Uint32(data))
		payload = data[4:ln]
	}

	return
}
