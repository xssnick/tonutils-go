package ton

import (
	"context"

	"github.com/xssnick/tonutils-go/liteclient"
)

const _GetMasterchainInfo int32 = -1984567762
const _RunContractGetMethod int32 = 1556504018
const _GetAccountState int32 = 1804144165

const _RunQueryResult int32 = -1550163605
const _AccountState int32 = 1887029073

const _LSError int32 = -1146494648

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
