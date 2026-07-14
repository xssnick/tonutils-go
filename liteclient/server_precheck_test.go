package liteclient

import (
	"bytes"
	"testing"

	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
)

func TestRawADNLMessageQueryID(t *testing.T) {
	queryID := bytes.Repeat([]byte{0xA7}, 32)
	data, err := tl.Serialize(adnl.MessageQuery{
		ID:   queryID,
		Data: TCPPing{RandomID: 1},
	}, true)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := rawADNLMessageQueryID(data)
	if !ok {
		t.Fatal("query id was not detected")
	}
	if !bytes.Equal(got, queryID) {
		t.Fatalf("query id = %x, want %x", got, queryID)
	}
}

func TestRawADNLMessageQueryIDIgnoresNonQuery(t *testing.T) {
	data, err := tl.Serialize(TCPPing{RandomID: 1}, true)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := rawADNLMessageQueryID(data); ok {
		t.Fatal("non-query message should not be detected")
	}
}
