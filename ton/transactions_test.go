package ton

import (
	"context"
	"testing"

	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
)

func TestSubscribeOnTransactionsWithFilter(t *testing.T) {
	t.Skip()
	client := liteclient.NewConnectionPool()

	configUrl := "https://ton.org/global.config.json"
	err := client.AddConnectionsFromConfigUrl(context.Background(), configUrl)
	if err != nil {
		panic(err)
	}
	api := NewAPIClient(client)
	apiClient := api.WithRetry()

	c := make(chan *tlb.Transaction)
	go func() {
		for _ = range c {
			// println(tx.String())
		}
	}()

	err = apiClient.SubscribeOnAllTransactions(context.Background(), nil, c)
	if err != nil {
		panic(err)
	}
}
