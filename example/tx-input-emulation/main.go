package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/tvm"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

/*
This example listens for new transactions of one account, emulates the inbound message
on top of the previously known code/data, and compares the emulated new data with the
real on-chain account data.

Important limitations:
  - This uses TVM-level EmulateExternalMessage / EmulateInternalMessage, not the full
    transaction emulator. It is good for simple compute-path verification, but it does
    not model storage/action/bounce phases exactly like the block executor.
  - Exact comparison is only possible while the processed transaction is still the latest
    transaction of the account. If newer transactions have already landed, the public API
    does not expose the exact intermediate post-state, so the example logs a skip.
  - Transactions without inbound messages (ticktock/storage/etc.) are skipped and the
    local state is resynced from chain.
*/

type trackedState struct {
	code    *cell.Cell
	data    *cell.Cell
	balance *big.Int
}

func main() {
	addr := address.MustParseAddr("EQDKHZ7e70CzqdvZCC83Z4WVR8POC_ZB0J1Y4zo88G-zCXmC")

	client := liteclient.NewConnectionPool()
	if err := client.AddConnectionsFromConfigUrl(context.Background(), "https://ton-blockchain.github.io/global.config.json"); err != nil {
		log.Fatalln("connection err: ", err.Error())
	}

	api := ton.NewAPIClient(client, ton.ProofCheckPolicyFast).WithRetryTimeout(3, 5*time.Second).WithLSInfoInErrors()
	ctx, stop := signal.NotifyContext(client.StickyContext(context.Background()), os.Interrupt, syscall.SIGTERM)
	defer stop()

	acc, err := loadLatestAccount(ctx, api, addr)
	if err != nil {
		log.Fatalln("get account err: ", err.Error())
	}
	if !acc.IsActive || acc.Code == nil {
		log.Fatalln("account is not active or has no code")
	}

	state := trackedState{
		code:    acc.Code,
		data:    acc.Data,
		balance: acc.State.Balance.Nano(),
	}

	log.Printf("listening %s from LT %d", addr.String(), acc.LastTxLT-100)

	txCh := make(chan *tlb.Transaction)
	go api.SubscribeOnTransactions(ctx, addr, acc.LastTxLT-100, txCh)

	for tx := range txCh {
		log.Printf("tx lt=%d now=%d", tx.LT, tx.Now)

		if tx.IO.In == nil || tx.IO.In.Msg == nil {
			log.Printf("skip tx %d: no inbound message, resync state from chain", tx.LT)
			if err = resyncTrackedState(ctx, api, addr, &state); err != nil {
				log.Printf("resync failed after skip: %v", err)
			}
			continue
		}

		res, err := emulateInboundTx(addr, tx, state)
		if err != nil {
			log.Printf("emulation failed for tx %d: %v", tx.LT, err)
			if err = resyncTrackedState(ctx, api, addr, &state); err != nil {
				log.Printf("resync failed after emulation error: %v", err)
			}
			continue
		}

		state.code = res.Code
		state.data = res.Data

		latest, err := loadLatestAccount(ctx, api, addr)
		if err != nil {
			log.Printf("cannot fetch latest account after tx %d: %v", tx.LT, err)
			continue
		}

		if latest.LastTxLT != tx.LT {
			log.Printf(
				"cannot verify tx %d exactly: latest on-chain tx is already %d, current public API does not expose intermediate post-state",
				tx.LT, latest.LastTxLT,
			)
			continue
		}

		if bytes.Equal(res.Data.Hash(), latest.Data.Hash()) {
			log.Printf("OK tx=%d exit=%d accepted=%t data_hash=%x", tx.LT, res.ExitCode, res.Accepted, latest.Data.Hash())
		} else {
			log.Printf("MISMATCH tx=%d exit=%d accepted=%t emulated_data=%x onchain_data=%x", tx.LT, res.ExitCode, res.Accepted, res.Data.Hash(), latest.Data.Hash())
			log.Printf("emulated data dump:\n%s", res.Data.Dump())
			log.Printf("on-chain data dump:\n%s", latest.Data.Dump())
		}

		state.code = latest.Code
		state.data = latest.Data
		state.balance = latest.State.Balance.Nano()
	}
}

func emulateInboundTx(addr *address.Address, tx *tlb.Transaction, state trackedState) (*tvm.MessageExecutionResult, error) {
	machine := tvm.NewTVM()
	cfg := tvm.MessageEmulationConfig{
		Address: addr,
		Now:     tx.Now,
		Balance: new(big.Int).Set(state.balance),
	}

	tm := time.Now()
	defer func() {
		log.Printf("emulation took %s", time.Since(tm))
	}()

	switch msg := tx.IO.In.Msg.(type) {
	case *tlb.ExternalMessage:
		return machine.EmulateExternalMessage(state.code, state.data, msg, cfg)
	case *tlb.InternalMessage:
		amount := msg.Amount.Nano()
		if amount.Sign() < 0 || amount.BitLen() > 64 {
			return nil, fmt.Errorf("internal amount does not fit uint64: %s", amount.String())
		}
		return machine.EmulateInternalMessage(state.code, state.data, msg.Body, amount.Uint64(), cfg)
	default:
		return nil, fmt.Errorf("unsupported inbound message type: %T", tx.IO.In.Msg)
	}
}

func loadLatestAccount(ctx context.Context, api ton.APIClientWrapped, addr *address.Address) (*tlb.Account, error) {
	block, err := api.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, err
	}
	return api.WaitForBlock(block.SeqNo).GetAccount(ctx, block, addr)
}

func resyncTrackedState(ctx context.Context, api ton.APIClientWrapped, addr *address.Address, state *trackedState) error {
	acc, err := loadLatestAccount(ctx, api, addr)
	if err != nil {
		return err
	}
	state.code = acc.Code
	state.data = acc.Data
	state.balance = acc.State.Balance.Nano()
	return nil
}
