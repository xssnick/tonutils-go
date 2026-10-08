package tlb

import "testing"

func TestAccountBlockToCellMainnetGolden(t *testing.T) {
	block := loadMainnetBlock(t)

	var accounts ShardAccountBlocksAugDict
	if err := LoadFromCell(&accounts, block.Extra.ShardAccountBlocks.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	items, err := accounts.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("mainnet block has no account blocks")
	}

	want, err := items[0].Value.Copy().ToCell()
	if err != nil {
		t.Fatal(err)
	}
	var accountBlock AccountBlock
	if err = LoadFromCell(&accountBlock, items[0].Value.Copy()); err != nil {
		t.Fatal(err)
	}

	rebuiltTransactions, err := NewAccountTransactionsAugDict()
	if err != nil {
		t.Fatal(err)
	}
	transactions, err := accountBlock.Transactions.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, transaction := range transactions {
		value, err := transaction.Value.Copy().ToCell()
		if err != nil {
			t.Fatal(err)
		}
		if err = rebuiltTransactions.Set(transaction.Key, value); err != nil {
			t.Fatal(err)
		}
	}
	accountBlock.Transactions = rebuiltTransactions

	got, err := accountBlock.ToCell()
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "mainnet account block", got, want)

	throughLoader, err := ToCell(&accountBlock)
	if err != nil {
		t.Fatal(err)
	}
	mustCellHashEqual(t, "mainnet account block through generic serializer", throughLoader, want)
}

func TestAccountBlockToCellRejectsInvalidRequiredFields(t *testing.T) {
	block := loadMainnetBlock(t)
	var accounts ShardAccountBlocksAugDict
	if err := LoadFromCell(&accounts, block.Extra.ShardAccountBlocks.MustBeginParse()); err != nil {
		t.Fatal(err)
	}
	items, err := accounts.RangeExtra(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("mainnet block has no account blocks")
	}
	var accountBlock AccountBlock
	if err = LoadFromCell(&accountBlock, items[0].Value.Copy()); err != nil {
		t.Fatal(err)
	}

	invalidAddress := accountBlock
	invalidAddress.Addr = invalidAddress.Addr[:31]
	if _, err = invalidAddress.ToCell(); err == nil {
		t.Fatal("short account address was accepted")
	}

	missingStateUpdate := accountBlock
	missingStateUpdate.StateUpdate = nil
	if _, err = missingStateUpdate.ToCell(); err == nil {
		t.Fatal("nil state update was accepted")
	}

	emptyTransactions, err := NewAccountTransactionsAugDict()
	if err != nil {
		t.Fatal(err)
	}
	missingTransactions := accountBlock
	missingTransactions.Transactions = emptyTransactions
	if _, err = missingTransactions.ToCell(); err == nil {
		t.Fatal("empty inline transaction dictionary was accepted")
	}
}
