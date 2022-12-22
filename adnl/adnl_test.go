package adnl

import (
	"context"
	"crypto/ed25519"
	"github.com/xssnick/tonutils-go/tl"
	"testing"
	"time"
)

func init() {
	tl.Register(TestMsg{}, "test.msg data:bytes = test.Message")
}

type TestMsg struct {
	Data []byte `tl:"bytes"`
}

func TestADNL_ClientServer(t *testing.T) {
	srvPub, srvKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	gotSrvCustom := make(chan any, 1)
	gotCliCustom := make(chan any, 1)
	gotSrvDiscon := make(chan any, 1)

	s := NewServer(srvKey)
	s.SetQueryHandler(func(client *ADNL, msg *MessageQuery) error {
		switch m := msg.Data.(type) {
		case MessagePing:
			err = client.Answer(context.Background(), msg.ID, MessagePong{
				Value: m.Value,
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		return nil
	})
	s.SetCustomMessageHandler(func(client *ADNL, msg *MessageCustom) error {
		gotSrvCustom <- true
		return client.SendCustomMessage(context.Background(), TestMsg{Data: make([]byte, 1280)})
	})
	s.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) {
		gotSrvDiscon <- true
	})

	go func() {
		if err = s.ListenAndServe("127.0.0.1:9055"); err != nil {
			t.Fatal(err)
		}
	}()

	time.Sleep(1 * time.Second)

	cli, err := Connect(context.Background(), "127.0.0.1:9055", srvPub, nil)
	if err != nil {
		t.Fatal(err)
	}
	cli.SetCustomMessageHandler(func(msg *MessageCustom) error {
		gotCliCustom <- msg.Data
		return nil
	})

	var res MessagePong
	err = cli.Query(context.Background(), &MessagePing{7755}, &res)
	if err != nil {
		t.Fatal(err)
	}

	if res.Value != 7755 {
		t.Fatal("value not eq")
	}

	if !cli.channel.ready {
		t.Fatal("client channel was not installed")
	}
	if len(s.processors) == 0 {
		t.Fatal("no processors for server")
	}

	// now should be in channel
	err = cli.Query(context.Background(), &MessagePing{8899}, &res)
	if err != nil {
		t.Fatal(err)
	}

	if res.Value != 8899 {
		t.Fatal("value chan not eq")
	}

	t.Run("bad client", func(t *testing.T) {
		rndPub, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}

		cliBad, err := Connect(context.Background(), "127.0.0.1:9055", rndPub, nil)
		if err != nil {
			t.Fatal(err)
		}

		err = cliBad.SendCustomMessage(context.Background(), &MessagePing{5555})
		if err != nil {
			t.Fatal(err)
		}

		select {
		case <-gotSrvDiscon:
		case <-time.After(150 * time.Millisecond):
			t.Fatal("disconnect not triggered on server")
		}
	})

	t.Run("custom msg", func(t *testing.T) {
		err = cli.SendCustomMessage(context.Background(), TestMsg{Data: make([]byte, 4)})
		if err != nil {
			t.Fatal(err)
		}

		select {
		case <-gotSrvCustom:
		case <-time.After(150 * time.Millisecond):
			t.Fatal("custom not received from client")
		}

		select {
		case m := <-gotCliCustom:
			if len(m.(TestMsg).Data) != 1280 {
				t.Fatal("invalid custom from server")
			}
		case <-time.After(150 * time.Millisecond):
			t.Fatal("custom not received from server")
		}
	})

	// now we close connection
	cli.Close()

	err = cli.Query(context.Background(), &MessagePing{1122}, &res)
	if err == nil {
		t.Fatal("conn should be closed")
	}
}
