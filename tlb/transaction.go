package tlb

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

func init() {
	Register(TransactionDescriptionOrdinary{})
	Register(TransactionDescriptionTickTock{})
	Register(TransactionDescriptionStorage{})
	Register(TransactionDescriptionMergeInstall{})
	Register(TransactionDescriptionMergePrepare{})
	Register(TransactionDescriptionSplitInstall{})
	Register(TransactionDescriptionSplitPrepare{})

	Register(ComputePhaseVM{})
	Register(ComputePhaseSkipped{})
	Register(BouncePhaseNegFunds{})
	Register(BouncePhaseOk{})
	Register(BouncePhaseNoFunds{})
}

type AccStatusChangeType string

const (
	AccStatusChangeUnchanged AccStatusChangeType = "UNCHANGED"
	AccStatusChangeFrozen    AccStatusChangeType = "FROZEN"
	AccStatusChangeDeleted   AccStatusChangeType = "DELETED"
)

type AccStatusChange struct {
	Type AccStatusChangeType
}

type StoragePhase struct {
	StorageFeesCollected Coins           `tlb:"."`
	StorageFeesDue       *Coins          `tlb:"maybe ."`
	StatusChange         AccStatusChange `tlb:"."`
}

type CreditPhase struct {
	DueFeesCollected *Coins             `tlb:"maybe ."`
	Credit           CurrencyCollection `tlb:"."`
}

type ComputeSkipReasonType string

const (
	ComputeSkipReasonNoState   ComputeSkipReasonType = "NO_STATE"
	ComputeSkipReasonBadState  ComputeSkipReasonType = "BAD_STATE"
	ComputeSkipReasonNoGas     ComputeSkipReasonType = "NO_GAS"
	ComputeSkipReasonSuspended ComputeSkipReasonType = "SUSPENDED"
)

type ComputeSkipReason struct {
	Type ComputeSkipReasonType
}

type ComputePhaseSkipped struct {
	_      Magic             `tlb:"$0"`
	Reason ComputeSkipReason `tlb:"."`
}

type ComputePhaseVM struct {
	_                Magic `tlb:"$1"`
	Success          bool  `tlb:"bool"`
	MsgStateUsed     bool  `tlb:"bool"`
	AccountActivated bool  `tlb:"bool"`
	GasFees          Coins `tlb:"."`
	Details          struct {
		GasUsed          *big.Int `tlb:"var uint 7"`
		GasLimit         *big.Int `tlb:"var uint 7"`
		GasCredit        *big.Int `tlb:"maybe var uint 3"`
		Mode             int8     `tlb:"## 8"`
		ExitCode         int32    `tlb:"## 32"`
		ExitArg          *int32   `tlb:"maybe ## 32"`
		VMSteps          uint32   `tlb:"## 32"`
		VMInitStateHash  []byte   `tlb:"bits 256"`
		VMFinalStateHash []byte   `tlb:"bits 256"`
	} `tlb:"^"`
}

type ComputePhase struct {
	Phase any `tlb:"[ComputePhaseVM,ComputePhaseSkipped]"`
}

type BouncePhase struct {
	Phase any `tlb:"[BouncePhaseOk,BouncePhaseNegFunds,BouncePhaseNoFunds]"`
}

type BouncePhaseNegFunds struct {
	_ Magic `tlb:"$00"`
}

type BouncePhaseNoFunds struct {
	_          Magic            `tlb:"$01"`
	MsgSize    StorageUsedShort `tlb:"."`
	ReqFwdFees Coins            `tlb:"."`
}

type BouncePhaseOk struct {
	_       Magic            `tlb:"$1"`
	MsgSize StorageUsedShort `tlb:"."`
	MsgFees Coins            `tlb:"."`
	FwdFees Coins            `tlb:"."`
}

type StorageUsedShort struct {
	Cells *big.Int `tlb:"var uint 7"`
	Bits  *big.Int `tlb:"var uint 7"`
}

type ActionPhase struct {
	Success         bool             `tlb:"bool"`
	Valid           bool             `tlb:"bool"`
	NoFunds         bool             `tlb:"bool"`
	StatusChange    AccStatusChange  `tlb:"."`
	TotalFwdFees    *Coins           `tlb:"maybe ."`
	TotalActionFees *Coins           `tlb:"maybe ."`
	ResultCode      int32            `tlb:"## 32"`
	ResultArg       *int32           `tlb:"maybe ## 32"`
	TotalActions    uint16           `tlb:"## 16"`
	SpecActions     uint16           `tlb:"## 16"`
	SkippedActions  uint16           `tlb:"## 16"`
	MessagesCreated uint16           `tlb:"## 16"`
	ActionListHash  []byte           `tlb:"bits 256"`
	TotalMsgSize    StorageUsedShort `tlb:"."`
}

type TransactionDescriptionOrdinary struct {
	_            Magic         `tlb:"$0000"`
	CreditFirst  bool          `tlb:"bool"`
	StoragePhase *StoragePhase `tlb:"maybe ."`
	CreditPhase  *CreditPhase  `tlb:"maybe ."`
	ComputePhase ComputePhase  `tlb:"."`
	ActionPhase  *ActionPhase  `tlb:"maybe ^"`
	Aborted      bool          `tlb:"bool"`
	BouncePhase  *BouncePhase  `tlb:"maybe ."`
	Destroyed    bool          `tlb:"bool"`
}

type TransactionDescriptionStorage struct {
	_            Magic        `tlb:"$0001"`
	StoragePhase StoragePhase `tlb:"."`
}

type TransactionDescriptionTickTock struct {
	_            Magic        `tlb:"$001"`
	IsTock       bool         `tlb:"bool"`
	StoragePhase StoragePhase `tlb:"."`
	ComputePhase ComputePhase `tlb:"."`
	ActionPhase  *ActionPhase `tlb:"maybe ^"`
	Aborted      bool         `tlb:"bool"`
	Destroyed    bool         `tlb:"bool"`
}

type SplitMergeInfo struct {
	CurShardPfxLen uint8  `tlb:"## 6"`
	AccSplitDepth  uint8  `tlb:"## 6"`
	ThisAddr       []byte `tlb:"bits 256"`
	SiblingAddr    []byte `tlb:"bits 256"`
}

type TransactionDescriptionSplitPrepare struct {
	_            Magic          `tlb:"$0100"`
	SplitInfo    SplitMergeInfo `tlb:"."`
	StoragePhase *StoragePhase  `tlb:"maybe ."`
	ComputePhase ComputePhase   `tlb:"."`
	ActionPhase  *ActionPhase   `tlb:"maybe ^"`
	Aborted      bool           `tlb:"bool"`
	Destroyed    bool           `tlb:"bool"`
}

type TransactionDescriptionSplitInstall struct {
	_                  Magic          `tlb:"$0101"`
	SplitInfo          SplitMergeInfo `tlb:"."`
	PrepareTransaction *Transaction   `tlb:"^"`
	Installed          bool           `tlb:"bool"`
}

type TransactionDescriptionMergePrepare struct {
	_            Magic          `tlb:"$0110"`
	SplitInfo    SplitMergeInfo `tlb:"."`
	StoragePhase StoragePhase   `tlb:"."`
	Aborted      bool           `tlb:"bool"`
}

type TransactionDescriptionMergeInstall struct {
	_                  Magic          `tlb:"$0111"`
	SplitInfo          SplitMergeInfo `tlb:"."`
	PrepareTransaction *Transaction   `tlb:"^"`
	StoragePhase       *StoragePhase  `tlb:"maybe ."`
	CreditPhase        *CreditPhase   `tlb:"maybe ."`
	ComputePhase       ComputePhase   `tlb:"."`
	ActionPhase        *ActionPhase   `tlb:"maybe ^"`
	Aborted            bool           `tlb:"bool"`
	Destroyed          bool           `tlb:"bool"`
}

type TransactionDescription struct {
	Description any `tlb:"[TransactionDescriptionOrdinary,TransactionDescriptionStorage,TransactionDescriptionTickTock,TransactionDescriptionSplitPrepare,TransactionDescriptionSplitInstall,TransactionDescriptionMergePrepare,TransactionDescriptionMergeInstall]"`
}

type HashUpdate struct {
	_       Magic  `tlb:"#72"`
	OldHash []byte `tlb:"bits 256"`
	NewHash []byte `tlb:"bits 256"`
}

type Transaction struct {
	_           Magic         `tlb:"$0111"`
	AccountAddr []byte        `tlb:"bits 256"`
	LT          uint64        `tlb:"## 64"`
	PrevTxHash  []byte        `tlb:"bits 256"`
	PrevTxLT    uint64        `tlb:"## 64"`
	Now         uint32        `tlb:"## 32"`
	OutMsgCount uint16        `tlb:"## 15"`
	OrigStatus  AccountStatus `tlb:"."`
	EndStatus   AccountStatus `tlb:"."`
	IO          struct {
		In  *Message      `tlb:"maybe ^"`
		Out *MessagesList `tlb:"maybe ^"`
	} `tlb:"^"`
	TotalFees   CurrencyCollection     `tlb:"."`
	StateUpdate HashUpdate             `tlb:"^"` // of Account
	Description TransactionDescription `tlb:"^"`

	// not in scheme, but will be filled based on request data for flexibility
	Hash []byte `tlb:"-"`
}

func (t *Transaction) Dump() string {
	var in string
	if t.IO.In != nil {
		var pl = "EMPTY"
		if p := t.IO.In.Msg.Payload(); p != nil {
			pl = p.Dump()
		}
		in = fmt.Sprintf("\nInput:\nType %s\nFrom %s\nPayload:\n%s\n", t.IO.In.MsgType, t.IO.In.Msg.SenderAddr(), pl)
	}
	res := fmt.Sprintf("LT: %d\n%s\nOutputs:\n", t.LT, in)
	if t.IO.Out != nil {
		list, err := t.IO.Out.ToSlice()
		if err != nil {
			return res + "\nOUT MESSAGES NOT PARSED DUE TO ERR: " + err.Error()
		}

		for _, m := range list {
			switch m.MsgType {
			case MsgTypeInternal:
				res += m.AsInternal().Dump()
			case MsgTypeExternalOut:
				res += "[EXT OUT] " + m.AsExternalOut().Body.Dump()
			default:
				res += "[UNKNOWN]"
			}
		}
	}
	return res
}

func (t *Transaction) String() string {
	var destinations []string
	in, out := new(big.Int), new(big.Int)

	if t.IO.Out != nil {
		listOut, err := t.IO.Out.ToSlice()
		if err != nil {
			return "\nOUT MESSAGES NOT PARSED DUE TO ERR: " + err.Error()
		}

		for _, m := range listOut {
			destinations = append(destinations, m.Msg.DestAddr().String())
			if m.MsgType == MsgTypeInternal {
				out.Add(out, m.AsInternal().Amount.Nano())
			}
		}
	}

	var build string

	switch t.Description.Description.(type) {
	default:
		return "[" + strings.ReplaceAll(reflect.TypeOf(t.Description.Description).Name(), "TransactionDescription", "") + "]"
	case TransactionDescriptionOrdinary:
	}
	if t.IO.In != nil {
		build += fmt.Sprintf("LT: %d", t.LT)

		if t.IO.In.MsgType == MsgTypeInternal {
			in = t.IO.In.AsInternal().Amount.Nano()

			intTx := t.IO.In.AsInternal()
			build += fmt.Sprintf(", In: %s TON, From %s", FromNanoTON(in).String(), intTx.SrcAddr)
			comment := intTx.Comment()
			if comment != "" {
				build += ", Comment: " + comment
			}
		} else if t.IO.In.MsgType == MsgTypeExternalIn {
			exTx := t.IO.In.AsExternalIn()
			build += ", ExternalIn, hash: " + hex.EncodeToString(exTx.Body.Hash())
		}
	}

	if out.Cmp(big.NewInt(0)) != 0 {
		if len(build) > 0 {
			build += ", "
		}
		build += fmt.Sprintf("Out: %s TON, To %s", FromNanoTON(out).String(), destinations)
	}

	return build
}

func (a *AccStatusChange) LoadFromCell(loader *cell.Slice) error {
	isChanged, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}

	if isChanged {
		isDeleted, err := loader.LoadBoolBit()
		if err != nil {
			return err
		}

		if isDeleted {
			a.Type = AccStatusChangeDeleted
			return nil
		}

		a.Type = AccStatusChangeFrozen
		return nil
	}

	a.Type = AccStatusChangeUnchanged
	return nil
}

func (a AccStatusChange) ToCell() (*cell.Cell, error) {
	switch a.Type {
	case AccStatusChangeUnchanged:
		return cell.BeginCell().MustStoreUInt(0b0, 1).EndCell(), nil
	case AccStatusChangeFrozen:
		return cell.BeginCell().MustStoreUInt(0b01, 2).EndCell(), nil
	case AccStatusChangeDeleted:
		return cell.BeginCell().MustStoreUInt(0b11, 2).EndCell(), nil
	}
	return nil, fmt.Errorf("unknown state change type %s", a.Type)
}

func (c *ComputeSkipReason) LoadFromCell(loader *cell.Slice) error {
	pfx, err := loader.LoadUInt(2)
	if err != nil {
		return err
	}

	switch pfx {
	case 0b00:
		c.Type = ComputeSkipReasonNoState
		return nil
	case 0b01:
		c.Type = ComputeSkipReasonBadState
		return nil
	case 0b10:
		c.Type = ComputeSkipReasonNoGas
		return nil
	case 0b11:
		isNotSuspended, err := loader.LoadBoolBit()
		if err != nil {
			return err
		}

		if !isNotSuspended {
			c.Type = ComputeSkipReasonSuspended
			return nil
		}
	}
	return fmt.Errorf("unknown compute skip reason")
}

func (c ComputeSkipReason) ToCell() (*cell.Cell, error) {
	switch c.Type {
	case ComputeSkipReasonNoState:
		return cell.BeginCell().MustStoreUInt(0b00, 2).EndCell(), nil
	case ComputeSkipReasonBadState:
		return cell.BeginCell().MustStoreUInt(0b01, 2).EndCell(), nil
	case ComputeSkipReasonNoGas:
		return cell.BeginCell().MustStoreUInt(0b10, 2).EndCell(), nil
	case ComputeSkipReasonSuspended:
		return cell.BeginCell().MustStoreUInt(0b110, 3).EndCell(), nil
	}
	return nil, fmt.Errorf("unknown compute skip reason %s", c.Type)
}
