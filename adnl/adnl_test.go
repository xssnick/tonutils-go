package adnl

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/adnl/address"
	"github.com/xssnick/tonutils-go/adnl/keys"
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

		s.mx.RLock()
		ln := len(s.processors)
		s.mx.RUnlock()
		if ln == 0 {
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

func TestGateway_RegisterClientPreservesAddressListState(t *testing.T) {
	_, srvKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	srv := NewGateway(srvKey)
	addr, err := address.NewAddress(net.ParseIP("127.0.0.1"), 9155)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetAddressList([]address.Address{
		addr,
	})

	expected := srv.GetAddressList()

	peerPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	peerID, err := tl.Hash(keys.PublicKeyED25519{Key: peerPub})
	if err != nil {
		t.Fatal(err)
	}

	cli, err := srv.registerClient(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12000}, peerPub, string(peerID))
	if err != nil {
		t.Fatal(err)
	}

	got := cli.client.(*ADNL).GetAddressList()
	if got.Version != expected.Version {
		t.Fatalf("unexpected address list version: got %d want %d", got.Version, expected.Version)
	}
	if got.ReinitDate != expected.ReinitDate {
		t.Fatalf("unexpected address list reinit date: got %d want %d", got.ReinitDate, expected.ReinitDate)
	}
}

func TestGateway_SetAddressListClonesInput(t *testing.T) {
	_, srvKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	srv := NewGateway(srvKey)
	addresses := []address.Address{
		&address.UDP{
			IP:   net.IPv4(127, 0, 0, 1).To4(),
			Port: 9155,
		},
	}

	srv.SetAddressList(addresses)

	addresses[0].(*address.UDP).IP = net.IPv4(8, 8, 8, 8).To4()
	addresses[0].(*address.UDP).Port = 9999

	got := srv.GetAddressList()
	if len(got.Addresses) != 1 {
		t.Fatalf("unexpected address count: %d", len(got.Addresses))
	}

	if ip := address.IPValue(got.Addresses[0]); !ip.Equal(net.IPv4(127, 0, 0, 1).To4()) {
		t.Fatalf("unexpected stored ip: %v", ip)
	}
	if port := address.PortValue(got.Addresses[0]); port != 9155 {
		t.Fatalf("unexpected stored port: %d", port)
	}
}

func TestGateway_StartServerWithUnspecifiedAddressInitializesAddressList(t *testing.T) {
	_, srvKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	gw := NewGateway(srvKey)
	if err = gw.StartServer("0.0.0.0:0"); err != nil {
		t.Fatal(err)
	}
	defer gw.Close()

	list := gw.GetAddressList()
	if list.Version == 0 || list.ReinitDate == 0 {
		t.Fatalf("unexpected empty address list state: %+v", list)
	}
	if len(list.Addresses) != 0 {
		t.Fatalf("unexpected advertised addresses: %d", len(list.Addresses))
	}

	peerPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = gw.RegisterClient("127.0.0.1:30111", peerPub); err != nil {
		t.Fatal(err)
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
