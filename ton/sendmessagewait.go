package ton

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
)

var ErrTxWasNotConfirmed = errors.New("transaction was not confirmed in a given deadline, but it may still be confirmed later")

func (c *APIClient) SendExternalMessageWaitTransaction(ctx context.Context, ext *tlb.ExternalMessage) (*tlb.Transaction, *BlockIDExt, []byte, error) {
	block, err := c.CurrentMasterchainInfo(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get block: %w", err)
	}

	acc, err := c.WaitForBlock(block.SeqNo).GetAccount(ctx, block, ext.DstAddr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get account state: %w", err)
	}

	inMsgHash := ext.Body.Hash()

	if err = c.SendExternalMessage(ctx, ext); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to send message: %w", err)
	}

	tx, block, err := c.waitConfirmation(ctx, block, acc, ext)
	if err != nil {
		return nil, nil, nil, err
	}

	return tx, block, inMsgHash, nil
}

func (c *APIClient) waitConfirmation(ctx context.Context, block *BlockIDExt, acc *tlb.Account, ext *tlb.ExternalMessage) (*tlb.Transaction, *BlockIDExt, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		// fallback timeout to not stuck forever with background context
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
	}
	till, _ := ctx.Deadline()

	ctx = c.Client().StickyContext(ctx)

	for time.Now().Before(till) {
		blockNew, err := c.WaitForBlock(block.SeqNo + 1).GetMasterchainInfo(ctx)
		if err != nil {
			continue
		}

		accNew, err := c.WaitForBlock(blockNew.SeqNo).GetAccount(ctx, blockNew, ext.DstAddr)
		if err != nil {
			continue
		}
		block = blockNew

		if accNew.LastTxLT == acc.LastTxLT {
			// if not in block, maybe LS lost our message, send it again
			if err = c.SendExternalMessage(ctx, ext); err != nil {
				continue
			}

			continue
		}

		lastLt, lastHash := accNew.LastTxLT, accNew.LastTxHash

		// it is possible that > 5 new not related transactions will happen, and we should not lose our scan offset,
		// to prevent this we will scan till we reach last seen offset.
		for time.Now().Before(till) {
			// we try to get last 5 transactions, and check if we have our new there.
			txList, err := c.WaitForBlock(block.SeqNo).ListTransactions(ctx, ext.DstAddr, 5, lastLt, lastHash)
			if err != nil {
				continue
			}

			sawLastTx := false
			for i, transaction := range txList {
				if i == 0 {
					// get previous of the oldest tx, in case if we need to scan deeper
					lastLt, lastHash = txList[0].PrevTxLT, txList[0].PrevTxHash
				}

				if !sawLastTx && transaction.PrevTxLT == acc.LastTxLT &&
					bytes.Equal(transaction.PrevTxHash, acc.LastTxHash) {
					sawLastTx = true
				}

				if transaction.IO.In != nil && transaction.IO.In.MsgType == tlb.MsgTypeExternalIn {
					extIn := transaction.IO.In.AsExternalIn()
					if ext.StateInit != nil {
						if extIn.StateInit == nil {
							continue
						}

						if !bytes.Equal(ext.StateInit.Data.Hash(), extIn.StateInit.Data.Hash()) {
							continue
						}

						if !bytes.Equal(ext.StateInit.Code.Hash(), extIn.StateInit.Code.Hash()) {
							continue
						}
					}

					if !bytes.Equal(extIn.Body.Hash(), ext.Body.Hash()) {
						continue
					}

					return transaction, block, nil
				}
			}

			if sawLastTx {
				break
			}
		}
		acc = accNew
	}

	return nil, nil, ErrTxWasNotConfirmed
}
