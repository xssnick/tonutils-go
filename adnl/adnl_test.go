package adnl

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tl"
)

func init() {
	tl.Register(TestMsg{}, "test.msg data:bytes = test.Message")
}

type TestMsg struct {
	Data []byte `tl:"bytes"`
}

func TestADNL_ClientServer(t *testing.T) {
	for _, multi := range []bool{false, true} {
		srvPub, srvKey, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		_, cliKey, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}

		gotSrvCustom := make(chan any, 1)
		gotCliCustom := make(chan any, 1)
		gotCliCustom2 := make(chan any, 1)
		gotSrvDiscon := make(chan any, 1)

		var mg NetManager
		if multi {
			dl, err := DefaultListener("127.0.0.1:9155")
			if err != nil {
				t.Fatal(err)
			}
			mg = NewMultiNetReader(dl)
		} else {
			mg = NewSingleNetReader(DefaultListener)
		}

		s := NewGatewayWithNetManager(srvKey, mg)
		err = s.StartServer("127.0.0.1:9155")
		if err != nil {
			t.Fatal(err)
		}

		s.SetConnectionHandler(func(client Peer) error {
			client.SetQueryHandler(func(msg *MessageQuery) error {
				switch m := msg.Data.(type) {
				case MessagePing:
					if m.Value == 9999 {
						client.Close()
						return fmt.Errorf("handle mock err")
					}

					err = client.Answer(context.Background(), msg.ID, MessagePong{
						Value: m.Value,
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				return nil
			})
			client.SetCustomMessageHandler(func(msg *MessageCustom) error {
				gotSrvCustom <- true
				return client.SendCustomMessage(context.Background(), TestMsg{Data: make([]byte, 1280)})
			})
			client.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) {
				gotSrvDiscon <- true
			})
			return nil
		})

		time.Sleep(1 * time.Second)

		clg := NewGateway(cliKey)

		err = clg.StartClient()
		if err != nil {
			t.Fatal(err)
		}

		cli, err := clg.RegisterClient("127.0.0.1:9155", srvPub)
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

			cliBad, err := clg.RegisterClient("127.0.0.1:9155", rndPub)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			// should fail
			err = cliBad.Query(ctx, &MessagePing{5555}, &res)
			if err == nil {
				t.Fatal(err)
			}
		})

		/*t.Run("bad query", func(t *testing.T) {
			_, rndOur, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}

			cliBadQuery, err := Connect(context.Background(), "127.0.0.1:9155", srvPub, rndOur)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			err = cliBadQuery.Query(ctx, &MessagePing{9999}, &res)
			if err == nil {
				t.Fatal(err)
			}

			timer := time.NewTimer(150 * time.Millisecond)
			defer timer.Stop()

			select {
			case <-gotSrvDiscon:
			case <-timer.C:
				t.Fatal("disconnect not triggered on server")
			}
		})*/

		t.Run("custom msg", func(t *testing.T) {
			err = cli.SendCustomMessage(context.Background(), TestMsg{Data: make([]byte, 4)})
			if err != nil {
				t.Fatal(err)
			}

			timer := time.NewTimer(150 * time.Millisecond)
			defer timer.Stop()

			select {
			case <-gotSrvCustom:
			case <-timer.C:
				t.Fatal("custom not received from client")
			}

			timer = time.NewTimer(150 * time.Millisecond)
			defer timer.Stop()

			select {
			case m := <-gotCliCustom:
				if len(m.(TestMsg).Data) != 1280 {
					t.Fatal("invalid custom from server")
				}
			case <-timer.C:
				t.Fatal("custom not received from server")
			}
		})

		t.Run("custom msg channel reinited", func(t *testing.T) {
			cli.SetCustomMessageHandler(func(msg *MessageCustom) error {
				gotCliCustom2 <- msg.Data
				return nil
			})

			err = cli.SendCustomMessage(context.Background(), TestMsg{Data: make([]byte, 4)})
			if err != nil {
				t.Fatal(err)
			}

			timer := time.NewTimer(150 * time.Millisecond)
			defer timer.Stop()

			select {
			case <-gotSrvCustom:
			case <-timer.C:
				t.Fatal("custom not received from client")
			}

			timer = time.NewTimer(150 * time.Millisecond)
			defer timer.Stop()

			select {
			case m := <-gotCliCustom2:
				if len(m.(TestMsg).Data) != 1280 {
					t.Fatal("invalid custom from server")
				}
			case <-timer.C:
				t.Fatal("custom not received from server")
			}
		})

		// now we close connection
		cli.Close()

		err = cli.Query(context.Background(), &MessagePing{1122}, &res)
		if err == nil {
			t.Fatal("conn should be closed")
		}

		s.Close()
	}
}

func TestADNL_ClientServerStartStop(t *testing.T) {
	_, aPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	bPub, bPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	a := NewGateway(aPriv)
	err = a.StartServer("127.0.0.1:9055")
	if err != nil {
		t.Fatal(err)
	}
	a.SetConnectionHandler(connHandler)

	b := NewGateway(bPriv)
	err = b.StartServer("127.0.0.1:9065")
	if err != nil {
		t.Fatal(err)
	}
	b.SetConnectionHandler(connHandler)

	p, err := a.RegisterClient("127.0.0.1:9065", bPub)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var res MessagePong
	err = p.Query(ctx, &MessagePing{7755}, &res)
	if err != nil {
		t.Fatal(err)
	}

	if res.Value != 7755 {
		t.Fatal("value not eq")
	}

	_ = b.Close()
	b = NewGateway(bPriv)
	err = b.StartServer("127.0.0.1:9065")
	if err != nil {
		t.Fatal(err)
	}
	b.SetConnectionHandler(connHandler)

	p.Close()
	p, err = a.RegisterClient("127.0.0.1:9065", bPub)
	if err != nil {
		t.Fatal(err)
	}

	err = p.Query(ctx, &MessagePing{1111}, &res)
	if err != nil {
		t.Fatal(err)
	}
}

func connHandler(client Peer) error {
	client.SetQueryHandler(func(msg *MessageQuery) error {
		switch m := msg.Data.(type) {
		case MessagePing:
			if m.Value == 9999 {
				client.Close()
				return fmt.Errorf("handle mock err")
			}

			err := client.Answer(context.Background(), msg.ID, MessagePong{
				Value: m.Value,
			})
			if err != nil {
				panic(err)
			}
		}
		return nil
	})
	client.SetCustomMessageHandler(func(msg *MessageCustom) error {
		return client.SendCustomMessage(context.Background(), TestMsg{Data: make([]byte, 1280)})
	})
	client.SetDisconnectHandler(func(addr string, key ed25519.PublicKey) {
	})
	return nil
}
