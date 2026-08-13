package tlb

import (
	"fmt"

	"github.com/xssnick/tonutils-go/tvm/cell"
)

// TransactionKind is the description variant of a transaction, read from the
// description tag without parsing the phase bodies behind it.
type TransactionKind uint8

const (
	TransactionKindOrdinary TransactionKind = iota
	TransactionKindStorage
	TransactionKindTickTock
	TransactionKindSplitPrepare
	TransactionKindSplitInstall
	TransactionKindMergePrepare
	TransactionKindMergeInstall
)

func (k TransactionKind) String() string {
	switch k {
	case TransactionKindOrdinary:
		return "ordinary"
	case TransactionKindStorage:
		return "storage"
	case TransactionKindTickTock:
		return "tick_tock"
	case TransactionKindSplitPrepare:
		return "split_prepare"
	case TransactionKindSplitInstall:
		return "split_install"
	case TransactionKindMergePrepare:
		return "merge_prepare"
	case TransactionKindMergeInstall:
		return "merge_install"
	default:
		return fmt.Sprintf("kind(%d)", uint8(k))
	}
}

// TransactionLean is a verifier's view of Transaction. A deterministic replay
// re-derives the whole transaction and compares it bit-for-bit, so the parsed
// copy only has to supply the fields the replay is driven by: header scalars,
// the state update hashes, the total fees and the description kind. The
// inbound message stays a reference for the caller to prepare once, the
// output dictionary is only walked for presence, and the description body is
// not parsed at all — every field this type skips is still authenticated by
// the replay's hash comparison.
type TransactionLean struct {
	AccountAddr [32]byte
	LT          uint64
	PrevTxHash  [32]byte
	PrevTxLT    uint64
	Now         uint32
	OutMsgCount uint16
	OrigStatus  AccountStatus
	EndStatus   AccountStatus
	// InMsg is the inbound message cell; nil when the transaction has none.
	InMsg *cell.Cell
	// HasOutMsgs records the presence bit of the output dictionary.
	HasOutMsgs bool
	TotalFees  CurrencyCollection
	// OldHash and NewHash are the account state update boundary hashes.
	OldHash [32]byte
	NewHash [32]byte
	Kind    TransactionKind
	// IsTock is meaningful only when Kind is TransactionKindTickTock.
	IsTock bool
}

// LoadFromCell parses the transaction header exactly like
// (*Transaction).LoadFromCell but materializes no message bodies, no output
// list and no description phases. It is deliberately no stricter than the
// full parser: anything it skips must be authenticated by the caller some
// other way, such as a replay hash comparison.
func (t *TransactionLean) LoadFromCell(loader *cell.Slice) error {
	magic, err := loader.LoadUInt(4)
	if err != nil {
		return fmt.Errorf("failed to load transaction magic: %w", err)
	}
	if magic != 0b0111 {
		return fmt.Errorf("invalid transaction magic %b", magic)
	}

	if err = loader.LoadSliceInto(t.AccountAddr[:], 256); err != nil {
		return fmt.Errorf("failed to load account address: %w", err)
	}
	if t.LT, err = loader.LoadUInt(64); err != nil {
		return fmt.Errorf("failed to load lt: %w", err)
	}
	if err = loader.LoadSliceInto(t.PrevTxHash[:], 256); err != nil {
		return fmt.Errorf("failed to load previous tx hash: %w", err)
	}
	if t.PrevTxLT, err = loader.LoadUInt(64); err != nil {
		return fmt.Errorf("failed to load previous tx lt: %w", err)
	}
	now, err := loader.LoadUInt(32)
	if err != nil {
		return fmt.Errorf("failed to load now: %w", err)
	}
	t.Now = uint32(now)
	outMsgCount, err := loader.LoadUInt(15)
	if err != nil {
		return fmt.Errorf("failed to load output message count: %w", err)
	}
	t.OutMsgCount = uint16(outMsgCount)

	if err = t.OrigStatus.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load original status: %w", err)
	}
	if err = t.EndStatus.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load end status: %w", err)
	}

	ioRef, err := loader.LoadRefCell()
	if err != nil {
		return fmt.Errorf("failed to load transaction io ref: %w", err)
	}
	var io cell.Slice
	if err = ioRef.BeginParseInto(&io); err != nil {
		return fmt.Errorf("failed to parse transaction io: %w", err)
	}
	hasIn, err := io.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load inbound message flag: %w", err)
	}
	t.InMsg = nil
	if hasIn {
		if t.InMsg, err = io.LoadRefCell(); err != nil {
			return fmt.Errorf("failed to load inbound message ref: %w", err)
		}
	}
	hasOut, err := io.LoadBoolBit()
	if err != nil {
		return fmt.Errorf("failed to load output messages flag: %w", err)
	}
	t.HasOutMsgs = hasOut
	if hasOut {
		if _, err = io.LoadRefCell(); err != nil {
			return fmt.Errorf("failed to load output messages ref: %w", err)
		}
	}

	if err = t.TotalFees.LoadFromCell(loader); err != nil {
		return fmt.Errorf("failed to load total fees: %w", err)
	}

	stateUpdateRef, err := loader.LoadRefCell()
	if err != nil {
		return fmt.Errorf("failed to load state update ref: %w", err)
	}
	var stateUpdate cell.Slice
	if err = stateUpdateRef.BeginParseInto(&stateUpdate); err != nil {
		return fmt.Errorf("failed to parse state update: %w", err)
	}
	updateMagic, err := stateUpdate.LoadUInt(8)
	if err != nil {
		return fmt.Errorf("failed to load hash update magic: %w", err)
	}
	if updateMagic != 0x72 {
		return fmt.Errorf("invalid hash update magic %x", updateMagic)
	}
	if err = stateUpdate.LoadSliceInto(t.OldHash[:], 256); err != nil {
		return fmt.Errorf("failed to load old hash: %w", err)
	}
	if err = stateUpdate.LoadSliceInto(t.NewHash[:], 256); err != nil {
		return fmt.Errorf("failed to load new hash: %w", err)
	}

	descRef, err := loader.LoadRefCell()
	if err != nil {
		return fmt.Errorf("failed to load description ref: %w", err)
	}
	var desc cell.Slice
	if err = descRef.BeginParseInto(&desc); err != nil {
		return fmt.Errorf("failed to parse description: %w", err)
	}
	pfx, err := desc.LoadUInt(3)
	if err != nil {
		return fmt.Errorf("failed to load transaction description magic: %w", err)
	}
	t.IsTock = false
	switch pfx {
	case 0b000:
		isStorage, err := desc.LoadBoolBit()
		if err != nil {
			return fmt.Errorf("failed to load transaction description storage flag: %w", err)
		}
		if isStorage {
			t.Kind = TransactionKindStorage
		} else {
			t.Kind = TransactionKindOrdinary
		}
	case 0b001:
		t.Kind = TransactionKindTickTock
		if t.IsTock, err = desc.LoadBoolBit(); err != nil {
			return fmt.Errorf("failed to load is_tock flag: %w", err)
		}
	case 0b010:
		isInstall, err := desc.LoadBoolBit()
		if err != nil {
			return fmt.Errorf("failed to load split transaction install flag: %w", err)
		}
		if isInstall {
			t.Kind = TransactionKindSplitInstall
		} else {
			t.Kind = TransactionKindSplitPrepare
		}
	case 0b011:
		isInstall, err := desc.LoadBoolBit()
		if err != nil {
			return fmt.Errorf("failed to load merge transaction install flag: %w", err)
		}
		if isInstall {
			t.Kind = TransactionKindMergeInstall
		} else {
			t.Kind = TransactionKindMergePrepare
		}
	default:
		return fmt.Errorf("unknown transaction description magic prefix %b", pfx)
	}

	return nil
}
