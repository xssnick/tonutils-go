package ton

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/adnl"
	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"hash/crc32"
	"math/big"
	"reflect"
	"sort"
)

func init() {
	tl.Register(ValidatorSetHashable{}, "test0.validatorSet#901660ed")
}

var ErrNoProof = fmt.Errorf("liteserver has no proof for this account in a given block, request newer block or disable proof checks")

func CheckShardMcStateExtraProof(master *BlockIDExt, shardProof []*cell.Cell) (*tlb.McStateExtra, error) {
	shardState, err := CheckBlockShardStateProof(shardProof, master.RootHash)
	if err != nil {
		return nil, fmt.Errorf("check block proof failed: %w", err)
	}

	if shardState.McStateExtra == nil {
		return nil, fmt.Errorf("not a masterchain block")
	}

	var stateExtra tlb.McStateExtra
	err = tlb.LoadFromCell(&stateExtra, shardState.McStateExtra.BeginParse())
	if err != nil {
		return nil, fmt.Errorf("failed to load masterchain state extra: %w", err)
	}
	return &stateExtra, nil
}

func CheckShardInMasterProof(master *BlockIDExt, shardProof []*cell.Cell, workchain int32, shardRootHash []byte) error {
	stateExtra, err := CheckShardMcStateExtraProof(master, shardProof)
	if err != nil {
		return fmt.Errorf("failed to check proof for mc state extra: %w", err)
	}

	shards, err := LoadShardsFromHashes(stateExtra.ShardHashes, true)
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

	block, err := CheckBlockProof(proof[0], blockRootHash)
	if err != nil {
		return nil, fmt.Errorf("incorrect block proof: %w", err)
	}

	upd, err := block.StateUpdate.PeekRef(1)
	if err != nil {
		return nil, fmt.Errorf("failed to load state update ref: %w", err)
	}

	shardStateProofData, err := cell.UnwrapProof(proof[1], upd.Hash(0))
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
		shardStateProofData, err := stateProof[1].BeginParse().LoadRef()
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

func CheckBackwardBlockProof(from, to *BlockIDExt, toKey bool, stateProof, destProof, proof *cell.Cell) error {
	if from.Workchain != address.MasterchainID || to.Workchain != address.MasterchainID {
		return fmt.Errorf("both blocks should be from masterchain")
	}

	if from.SeqNo <= to.SeqNo {
		return fmt.Errorf("to seqno should be < from seqno")
	}

	toBlock, err := CheckBlockProof(destProof, to.RootHash)
	if err != nil {
		return fmt.Errorf("failed to check traget block proof: %w", err)
	}

	if toBlock.BlockInfo.KeyBlock != toKey {
		return fmt.Errorf("target block type not matches requested")
	}

	stateExtra, err := CheckShardMcStateExtraProof(from, []*cell.Cell{proof, stateProof})
	if err != nil {
		return fmt.Errorf("failed to check proof for mc state extra: %w", err)
	}

	var info tlb.McStateExtraBlockInfo
	err = tlb.LoadFromCellAsProof(&info, stateExtra.Info.BeginParse())
	if err != nil {
		return fmt.Errorf("failed to load tx CurrencyCollection proof cell: %w", err)
	}

	toInfo := info.PrevBlocks.GetByIntKey(big.NewInt(int64(to.SeqNo)))
	if toInfo == nil {
		return fmt.Errorf("target block not found in state proof")
	}

	slc := toInfo.BeginParse()
	err = tlb.LoadFromCellAsProof(new(tlb.KeyMaxLt), slc)
	if err != nil {
		return fmt.Errorf("failed to load block KeyMaxLt proof cell: %w", err)
	}

	var blk tlb.KeyExtBlkRef
	err = tlb.LoadFromCellAsProof(&blk, slc)
	if err != nil {
		return fmt.Errorf("failed to load block KeyExtBlkRef proof cell: %w", err)
	}

	if blk.IsKey != toKey {
		return fmt.Errorf("target block type in proof not matches requested")
	}

	if !bytes.Equal(blk.BlkRef.RootHash, to.RootHash) {
		return fmt.Errorf("incorret target block hash in proof")
	}
	return nil
}

func CheckForwardBlockProof(from, to *BlockIDExt, toKey bool, configProof, destProof *cell.Cell, signatures *SignatureSet) error {
	if from.Workchain != address.MasterchainID || to.Workchain != address.MasterchainID {
		return fmt.Errorf("both blocks should be from masterchain")
	}

	if from.SeqNo >= to.SeqNo {
		return fmt.Errorf("to seqno should be > from seqno")
	}

	toBlock, err := CheckBlockProof(destProof, to.RootHash)
	if err != nil {
		return fmt.Errorf("failed to check traget block proof: %w", err)
	}

	if toBlock.BlockInfo.KeyBlock != toKey {
		return fmt.Errorf("target block type not matches requested")
	}

	if toBlock.BlockInfo.GenValidatorListHashShort != uint32(signatures.ValidatorSetHash) {
		return fmt.Errorf("incorrect validator set hash")
	}

	if toBlock.BlockInfo.GenCatchainSeqno != uint32(signatures.CatchainSeqno) {
		return fmt.Errorf("incorrect catchain seqno")
	}

	if toBlock.BlockInfo.SeqNo <= from.SeqNo {
		return fmt.Errorf("invalid target block seqno")
	}

	fromBlock, err := CheckBlockProof(configProof, from.RootHash)
	if err != nil {
		return fmt.Errorf("failed to check source block proof: %w", err)
	}

	if fromBlock.Extra == nil || fromBlock.Extra.Custom == nil {
		return fmt.Errorf("source block proof is lack of info")
	}

	catchainCfgCell := fromBlock.Extra.Custom.ConfigParams.Config.Params.GetByIntKey(big.NewInt(28))
	blockValidatorsCell := fromBlock.Extra.Custom.ConfigParams.Config.Params.GetByIntKey(big.NewInt(34))
	if catchainCfgCell == nil || blockValidatorsCell == nil {
		return fmt.Errorf("not all required configs are in proof")
	}
	if catchainCfgCell, err = catchainCfgCell.PeekRef(0); err != nil {
		return fmt.Errorf("no ref in catchain cell")
	}
	if blockValidatorsCell, err = blockValidatorsCell.PeekRef(0); err != nil {
		return fmt.Errorf("no ref in validators cell")
	}

	var catchainCfg tlb.CatchainConfig
	if err = tlb.LoadFromCell(&catchainCfg, catchainCfgCell.BeginParse()); err != nil {
		return fmt.Errorf("failed to parse catchain config: %w", err)
	}

	var blockValidators tlb.ValidatorSetAny
	if err = tlb.LoadFromCell(&blockValidators, blockValidatorsCell.BeginParse()); err != nil {
		return fmt.Errorf("failed to parse validators config: %w", err)
	}

	validators, err := getMainValidators(to, catchainCfg, blockValidators, toBlock.BlockInfo.GenCatchainSeqno)
	if err != nil {
		return fmt.Errorf("failed to verify and get main block validators: %w", err)
	}

	if err = checkBlockSignatures(to, signatures, validators); err != nil {
		return fmt.Errorf("failed to check validators signatures: %w", err)
	}

	return nil
}

func getMainValidators(block *BlockIDExt, catConfig tlb.CatchainConfig, validatorConfig tlb.ValidatorSetAny, ccSeqno uint32) ([]*tlb.ValidatorAddr, error) {
	if block.Workchain != address.MasterchainID {
		return nil, fmt.Errorf("only masterchain blocks currently supported")
	}

	var shuffle = false
	var validatorsNum int
	var validatorsListDict *cell.Dictionary

	switch t := catConfig.Config.(type) {
	case tlb.CatchainConfigV1:
	case tlb.CatchainConfigV2:
		shuffle = t.ShuffleMcValidators
	default:
		return nil, fmt.Errorf("unknown validator set type")
	}

	var definedWeight *uint64
	switch t := validatorConfig.Validators.(type) {
	case tlb.ValidatorSet:
		validatorsNum = int(t.Main)
		validatorsListDict = t.List
	case tlb.ValidatorSetExt:
		definedWeight = &t.TotalWeight
		validatorsNum = int(t.Main)
		validatorsListDict = t.List
	default:
		return nil, fmt.Errorf("unknown validator set type")
	}

	type validatorWithKey struct {
		addr *tlb.ValidatorAddr
		key  uint16
	}

	kvs, err := validatorsListDict.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to load validators list dict: %w", err)
	}

	var totalWeight uint64
	var validatorsKeys = make([]validatorWithKey, len(kvs))
	for i, kv := range kvs {
		var val tlb.ValidatorAddr
		if err := tlb.LoadFromCell(&val, kv.Value); err != nil {
			return nil, fmt.Errorf("failed to parse validator addr: %w", err)
		}

		key, err := kv.Key.LoadUInt(16)
		if err != nil {
			return nil, fmt.Errorf("failed to parse validator key: %w", err)
		}

		totalWeight += val.Weight
		validatorsKeys[i].addr = &val
		validatorsKeys[i].key = uint16(key)
	}

	if definedWeight != nil && totalWeight != *definedWeight {
		return nil, fmt.Errorf("incorrect sum of weights")
	}

	if len(validatorsKeys) == 0 {
		return nil, fmt.Errorf("zero validators")
	}

	sort.Slice(validatorsKeys, func(i, j int) bool {
		return validatorsKeys[i].key < validatorsKeys[j].key
	})

	if validatorsNum > len(validatorsKeys) {
		validatorsNum = len(validatorsKeys)
	}

	var validators = make([]*tlb.ValidatorAddr, validatorsNum)
	if shuffle {
		prng := NewValidatorSetPRNG(block.Shard, block.Workchain, ccSeqno, nil)

		idx := make([]uint32, validatorsNum)
		for i := 0; i < validatorsNum; i++ {
			j := prng.NextRanged(uint64(i) + 1)
			idx[i] = idx[j]
			idx[j] = uint32(i)
		}

		for i := 0; i < validatorsNum; i++ {
			validators[i] = validatorsKeys[idx[i]].addr
		}

		return validators, nil
	}

	for i := 0; i < validatorsNum; i++ {
		validators[i] = validatorsKeys[i].addr
	}

	return validators, nil
}

func checkBlockSignatures(block *BlockIDExt, sigs *SignatureSet, validators []*tlb.ValidatorAddr) error {
	if len(sigs.Signatures) == 0 || len(validators) == 0 {
		return fmt.Errorf("zero signatures or validators")
	}

	setHash, err := calcValidatorSetHash(uint32(sigs.CatchainSeqno), validators)
	if err != nil {
		return fmt.Errorf("failed to calc validator set hash: %w", err)
	}

	if setHash != uint32(sigs.ValidatorSetHash) {
		return fmt.Errorf("incorrect validator set hash")
	}

	var totalWeight, signedWeight uint64
	validatorsMap := map[string]*tlb.ValidatorAddr{}
	for _, v := range validators {
		kid, err := tl.Hash(adnl.PublicKeyED25519{Key: v.PublicKey.Key})
		if err != nil {
			return fmt.Errorf("failed to calc validator key id: %w", err)
		}

		totalWeight += v.Weight
		validatorsMap[string(kid)] = v
	}

	blockIDBytes, err := tl.Serialize(BlockID{RootHash: block.RootHash, FileHash: block.FileHash}, true)
	if err != nil {
		return fmt.Errorf("failed to serialize block id: %w", err)
	}

	sort.Slice(sigs.Signatures, func(i, j int) bool {
		return string(sigs.Signatures[i].NodeIDShort) < string(sigs.Signatures[j].NodeIDShort)
	})

	for i, sig := range sigs.Signatures {
		if i > 0 && string(sigs.Signatures[i-1].NodeIDShort) == string(sig.NodeIDShort) {
			return fmt.Errorf("duplicated node signature")
		}

		v, ok := validatorsMap[string(sig.NodeIDShort)]
		if !ok {
			return fmt.Errorf("signature of unknown validator %s", hex.EncodeToString(sig.NodeIDShort))
		}

		if !ed25519.Verify(v.PublicKey.Key, blockIDBytes, sig.Signature) {
			return fmt.Errorf("incorrect signature of validator %s", hex.EncodeToString(sig.NodeIDShort))
		}
		signedWeight += v.Weight

		if signedWeight > totalWeight {
			break
		}
	}

	if 3*signedWeight <= 2*totalWeight {
		return fmt.Errorf("insufficient signed weight (%d/%d)", 3*signedWeight, 2*totalWeight)
	}

	return nil
}

type ValidatorItemHashable struct {
	Key    []byte `tl:"int256"`
	Weight uint64 `tl:"long"`
	Addr   []byte `tl:"int256"`
}

type ValidatorSetHashable struct {
	CCSeqno    uint32                  `tl:"int"`
	Validators []ValidatorItemHashable `tl:"vector struct"`
}

var castTable = crc32.MakeTable(crc32.Castagnoli)

func calcValidatorSetHash(ccSeqno uint32, validators []*tlb.ValidatorAddr) (uint32, error) {
	var vls = make([]ValidatorItemHashable, len(validators))
	for i, validator := range validators {
		vls[i].Key = validator.PublicKey.Key
		vls[i].Weight = validator.Weight
		vls[i].Addr = validator.ADNLAddr
	}

	h := ValidatorSetHashable{
		CCSeqno:    ccSeqno,
		Validators: vls,
	}

	b, err := tl.Serialize(h, true)
	if err != nil {
		return 0, fmt.Errorf("failed to serialize: %w", err)
	}
	return crc32.Checksum(b, castTable), nil
}

func (c *APIClient) VerifyProofChain(ctx context.Context, from, to *BlockIDExt) error {
	isForward := to.SeqNo > from.SeqNo

	for from.SeqNo != to.SeqNo {
		part, err := c.GetBlockProof(ctx, from, to)
		if err != nil {
			if lsErr, ok := err.(LSError); ok && (lsErr.Code == 651 || lsErr.Code == -400) { // block not applied error
				// try next node
				if ctx, err = c.client.StickyContextNextNode(ctx); err != nil {
					return fmt.Errorf("failed to pick next node: %w", err)
				}
				continue
			}
			return fmt.Errorf("failed to get master block proof from %d to %d: %w", from.SeqNo, to.SeqNo, err)
		}

		if !part.From.Equals(from) {
			return fmt.Errorf("unexpected from block: %d, want %d", part.From.SeqNo, from.SeqNo)
		}

		checkBackProof := func(bwd *BlockLinkBackward) error {
			destProof, err := cell.FromBOC(bwd.DestProof)
			if err != nil {
				return fmt.Errorf("dest proof boc parse err: %w", err)
			}

			stateProof, err := cell.FromBOC(bwd.StateProof)
			if err != nil {
				return fmt.Errorf("state proof boc parse err: %w", err)
			}

			proof, err := cell.FromBOC(bwd.Proof)
			if err != nil {
				return fmt.Errorf("proof boc parse err: %w", err)
			}

			err = CheckBackwardBlockProof(bwd.From, bwd.To, bwd.ToKeyBlock, stateProof, destProof, proof)
			if err != nil {
				return fmt.Errorf("invalid backward block from %d to %d proof: %w", bwd.From.SeqNo, bwd.To.SeqNo, err)
			}
			return nil
		}

		if isForward {
			for _, step := range part.Steps {
				fwd, ok := step.(BlockLinkForward)
				if !ok {
					// proof back to key block
					bwd, ok := step.(BlockLinkBackward)
					if !ok {
						return fmt.Errorf("wrong proof step type %v", reflect.TypeOf(step).String())
					}

					if err = checkBackProof(&bwd); err != nil {
						return err
					}

					from = bwd.To
					continue
				}

				destProof, err := cell.FromBOC(fwd.DestProof)
				if err != nil {
					return fmt.Errorf("dest proof boc parse err: %w", err)
				}

				configProof, err := cell.FromBOC(fwd.ConfigProof)
				if err != nil {
					return fmt.Errorf("config proof boc parse err: %w", err)
				}

				err = CheckForwardBlockProof(from, fwd.To, fwd.ToKeyBlock, configProof, destProof, fwd.SignatureSet)
				if err != nil {
					return fmt.Errorf("invalid forward block from %d to %d proof: %w", fwd.From.SeqNo, fwd.To.SeqNo, err)
				}
				from = fwd.To
			}
		} else {
			for _, step := range part.Steps {
				bwd, ok := step.(BlockLinkBackward)
				if !ok {
					return fmt.Errorf("wrong proof direction in response bw %v", reflect.TypeOf(step).String())
				}

				if err = checkBackProof(&bwd); err != nil {
					return err
				}
			}
		}
		from = part.To
	}

	if !from.Equals(to) {
		return fmt.Errorf("target block not equals expected")
	}
	return nil
}
