package tlb

import "github.com/xssnick/tonutils-go/tvm/cell"

func init() {
	Register(ValidatorSet{})
	Register(ValidatorSetExt{})

	Register(CatchainConfigV1{})
	Register(CatchainConfigV2{})

	Register(ConsensusConfigV1{})
	Register(ConsensusConfigV2{})
	Register(ConsensusConfigV3{})
	Register(ConsensusConfigV4{})
}

type ValidatorSetAny struct {
	Validators any `tlb:"[ValidatorSet,ValidatorSetExt]"`
}

type ValidatorSet struct {
	_          Magic            `tlb:"#11"`
	UTimeSince uint32           `tlb:"## 32"`
	UTimeUntil uint32           `tlb:"## 32"`
	Total      uint16           `tlb:"## 16"`
	Main       uint16           `tlb:"## 16"`
	List       *cell.Dictionary `tlb:"dict 16"`
}

type ValidatorSetExt struct {
	_           Magic            `tlb:"#12"`
	UTimeSince  uint32           `tlb:"## 32"`
	UTimeUntil  uint32           `tlb:"## 32"`
	Total       uint16           `tlb:"## 16"`
	Main        uint16           `tlb:"## 16"`
	TotalWeight uint64           `tlb:"## 64"`
	List        *cell.Dictionary `tlb:"dict 16"`
}

type Validator struct {
	_         Magic            `tlb:"#53"`
	PublicKey SigPubKeyED25519 `tlb:"."`
	Weight    uint64           `tlb:"## 64"`
}

type ValidatorAddr struct {
	_         Magic            `tlb:"#73"`
	PublicKey SigPubKeyED25519 `tlb:"."`
	Weight    uint64           `tlb:"## 64"`
	ADNLAddr  []byte           `tlb:"bits 256"`
}

type SigPubKeyED25519 struct {
	_   Magic  `tlb:"#8e81278a"`
	Key []byte `tlb:"bits 256"`
}

type CatchainConfig struct {
	Config any `tlb:"[CatchainConfigV1,CatchainConfigV2]"`
}

type CatchainConfigV1 struct {
	_                       Magic  `tlb:"#c1"`
	McCatchainLifetime      uint32 `tlb:"## 32"`
	ShardCatchainLifetime   uint32 `tlb:"## 32"`
	ShardValidatorsLifetime uint32 `tlb:"## 32"`
	ShardValidatorsNum      uint32 `tlb:"## 32"`
}

type CatchainConfigV2 struct {
	_                       Magic  `tlb:"#c2"`
	Flags                   uint8  `tlb:"## 7"`
	ShuffleMcValidators     bool   `tlb:"bool"`
	McCatchainLifetime      uint32 `tlb:"## 32"`
	ShardCatchainLifetime   uint32 `tlb:"## 32"`
	ShardValidatorsLifetime uint32 `tlb:"## 32"`
	ShardValidatorsNum      uint32 `tlb:"## 32"`
}

type ConsensusConfig struct {
	Config any `tlb:"[ConsensusConfigV1,ConsensusConfigV2,ConsensusConfigV3,ConsensusConfigV4]"`
}

type ConsensusConfigV1 struct {
	_                    Magic  `tlb:"#d6"`
	RoundCandidates      uint32 `tlb:"## 32"`
	NextCandidateDelayMs uint32 `tlb:"## 32"`
	ConsensusTimeoutMs   uint32 `tlb:"## 32"`
	FastAttempts         uint32 `tlb:"## 32"`
	AttemptDuration      uint32 `tlb:"## 32"`
	CatchainMaxDeps      uint32 `tlb:"## 32"`
	MaxBlockBytes        uint32 `tlb:"## 32"`
	MaxCollatedBytes     uint32 `tlb:"## 32"`
}

type ConsensusConfigV2 struct {
	_                    Magic  `tlb:"#d7"`
	Flags                uint8  `tlb:"## 7"`
	NewCatchainIds       bool   `tlb:"bool"`
	RoundCandidates      uint8  `tlb:"## 8"`
	NextCandidateDelayMs uint32 `tlb:"## 32"`
	ConsensusTimeoutMs   uint32 `tlb:"## 32"`
	FastAttempts         uint32 `tlb:"## 32"`
	AttemptDuration      uint32 `tlb:"## 32"`
	CatchainMaxDeps      uint32 `tlb:"## 32"`
	MaxBlockBytes        uint32 `tlb:"## 32"`
	MaxCollatedBytes     uint32 `tlb:"## 32"`
}

type ConsensusConfigV3 struct {
	_                    Magic  `tlb:"#d8"`
	Flags                uint8  `tlb:"## 7"`
	NewCatchainIds       bool   `tlb:"bool"`
	RoundCandidates      uint8  `tlb:"## 8"`
	NextCandidateDelayMs uint32 `tlb:"## 32"`
	ConsensusTimeoutMs   uint32 `tlb:"## 32"`
	FastAttempts         uint32 `tlb:"## 32"`
	AttemptDuration      uint32 `tlb:"## 32"`
	CatchainMaxDeps      uint32 `tlb:"## 32"`
	MaxBlockBytes        uint32 `tlb:"## 32"`
	MaxCollatedBytes     uint32 `tlb:"## 32"`
	ProtoVersion         uint16 `tlb:"## 16"`
}

type ConsensusConfigV4 struct {
	_                     Magic  `tlb:"#d9"`
	Flags                 uint8  `tlb:"## 7"`
	NewCatchainIds        bool   `tlb:"bool"`
	RoundCandidates       uint8  `tlb:"## 8"`
	NextCandidateDelayMs  uint32 `tlb:"## 32"`
	ConsensusTimeoutMs    uint32 `tlb:"## 32"`
	FastAttempts          uint32 `tlb:"## 32"`
	AttemptDuration       uint32 `tlb:"## 32"`
	CatchainMaxDeps       uint32 `tlb:"## 32"`
	MaxBlockBytes         uint32 `tlb:"## 32"`
	MaxCollatedBytes      uint32 `tlb:"## 32"`
	ProtoVersion          uint16 `tlb:"## 16"`
	CatchainMaxBlocksCoff uint32 `tlb:"## 32"`
}
