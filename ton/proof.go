package ton

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

var ErrNoProof = fmt.Errorf("liteserver has no proof for this account in a given block, request newer block or disable proof checks")

func CheckShardInMasterProof(master *BlockIDExt, shardProof []*cell.Cell, workchain int32, shardRootHash []byte) error {
	shardState, err := CheckBlockShardStateProof(shardProof, master.RootHash)
	if err != nil {
		return fmt.Errorf("check block proof failed: %w", err)
	}

	if shardState.McStateExtra == nil {
		return fmt.Errorf("not a masterchain block")
	}

	var stateExtra tlb.McStateExtra
	err = tlb.LoadFromCell(&stateExtra, shardState.McStateExtra.BeginParse())
	if err != nil {
		return fmt.Errorf("failed to load masterchain state extra: %w", err)
	}

	shards, err := LoadShardsFromHashes(stateExtra.ShardHashes)
	if err != nil {
		return fmt.Errorf("failed to load shard hashes: %w", err)
	}

	for _, shard := range shards {
		if shard.Workchain == workchain && bytes.Equal(shard.RootHash, shardRootHash) {
			return nil
		}
	}
	return fmt.Errorf("required shard hash not found in proof")
}

func CheckBlockShardStateProof(proof []*cell.Cell, blockRootHash []byte) (*tlb.ShardStateUnsplit, error) {
	if len(proof) != 2 {
		return nil, fmt.Errorf("should have 2 roots")
	}

	block, err := CheckBlockProof(proof[1], blockRootHash)
	if err != nil {
		return nil, fmt.Errorf("incorrect block proof: %w", err)
	}

	upd, err := block.StateUpdate.PeekRef(1)
	if err != nil {
		return nil, fmt.Errorf("failed to load state update ref: %w", err)
	}

	shardStateProofData, err := cell.UnwrapProof(proof[0], upd.Hash(0))
	if err != nil {
		return nil, fmt.Errorf("incorrect shard state proof: %w", err)
	}

	var shardState tlb.ShardStateUnsplit
	if err = tlb.LoadFromCellAsProof(&shardState, shardStateProofData.BeginParse(), false); err != nil {
		return nil, fmt.Errorf("failed to parse ShardStateUnsplit: %w", err)
	}

	return &shardState, nil
}

func CheckBlockProof(proof *cell.Cell, blockRootHash []byte) (*tlb.Block, error) {
	blockProof, err := cell.UnwrapProof(proof, blockRootHash)
	if err != nil {
		return nil, fmt.Errorf("block proof check failed: %w", err)
	}

	var block tlb.Block
	if err := tlb.LoadFromCellAsProof(&block, blockProof.BeginParse(), false); err != nil {
		return nil, fmt.Errorf("failed to parse Block: %w", err)
	}

	return &block, nil
}

func CheckAccountStateProof(addr *address.Address, block *BlockIDExt, stateProof []*cell.Cell, shardProof []*cell.Cell, shardHash []byte, skipBlockCheck bool) (*tlb.ShardAccount, *tlb.DepthBalanceInfo, error) {
	if len(stateProof) != 2 {
		return nil, nil, fmt.Errorf("proof should have 2 roots")
	}

	var shardState *tlb.ShardStateUnsplit

	if !skipBlockCheck {
		blockHash := block.RootHash
		// we need shard proof only for not masterchain
		if len(shardHash) > 0 {
			if err := CheckShardInMasterProof(block, shardProof, addr.Workchain(), shardHash); err != nil {
				return nil, nil, fmt.Errorf("shard proof is incorrect: %w", err)
			}
			blockHash = shardHash
		}

		var err error
		shardState, err = CheckBlockShardStateProof(stateProof, blockHash)
		if err != nil {
			return nil, nil, fmt.Errorf("incorrect block proof: %w", err)
		}
	} else {
		shardStateProofData, err := stateProof[0].BeginParse().LoadRef()
		if err != nil {
			return nil, nil, fmt.Errorf("shard state proof should have ref: %w", err)
		}

		var state tlb.ShardStateUnsplit
		if err = tlb.LoadFromCellAsProof(&state, shardStateProofData, false); err != nil {
			return nil, nil, fmt.Errorf("failed to parse ShardStateUnsplit: %w", err)
		}
		shardState = &state
	}

	if shardState.Accounts.ShardAccounts == nil {
		return nil, nil, errors.New("no shard accounts in proof")
	}

	addrKey := cell.BeginCell().MustStoreSlice(addr.Data(), 256).EndCell()
	val := shardState.Accounts.ShardAccounts.Get(addrKey)
	if val == nil {
		return nil, nil, errors.New("no addr info in proof hashmap")
	}

	loadVal := val.BeginParse()

	var balanceInfo tlb.DepthBalanceInfo
	err := tlb.LoadFromCell(&balanceInfo, loadVal)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load DepthBalanceInfo: %w", err)
	}

	var accInfo tlb.ShardAccount
	err = tlb.LoadFromCell(&accInfo, loadVal)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load ShardAccount: %w", err)
	}

	return &accInfo, &balanceInfo, nil
}

func CheckTransactionProof(txHash []byte, txLT uint64, txAccount []byte, shardAccounts *tlb.ShardAccountBlocks) error {
	accProof := shardAccounts.Accounts.Get(cell.BeginCell().MustStoreSlice(txAccount, 256).EndCell())
	if accProof == nil {
		return fmt.Errorf("no tx account in proof")
	}

	accProofSlice := accProof.BeginParse()
	err := tlb.LoadFromCellAsProof(new(tlb.CurrencyCollection), accProofSlice)
	if err != nil {
		return fmt.Errorf("failed to load account CurrencyCollection proof cell: %w", err)
	}

	var accBlock tlb.AccountBlock
	err = tlb.LoadFromCellAsProof(&accBlock, accProofSlice)
	if err != nil {
		return fmt.Errorf("failed to load account from proof cell: %w", err)
	}

	accTx := accBlock.Transactions.Get(cell.BeginCell().MustStoreUInt(txLT, 64).EndCell())
	if accTx == nil {
		return fmt.Errorf("no tx in account block proof")
	}

	accTxSlice := accTx.BeginParse()
	err = tlb.LoadFromCellAsProof(new(tlb.CurrencyCollection), accTxSlice)
	if err != nil {
		return fmt.Errorf("failed to load tx CurrencyCollection proof cell: %w", err)
	}

	txAccProof, err := accTxSlice.LoadRef()
	if err != nil {
		return fmt.Errorf("failed to load ref of acc tx proof cell: %w", err)
	}

	if !bytes.Equal(txHash, txAccProof.MustToCell().Hash(0)) {
		return fmt.Errorf("incorrect tx hash in proof")
	}

	return nil
}
