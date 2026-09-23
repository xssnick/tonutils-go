package tvm

import (
	"errors"
	"fmt"
	"math/bits"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// validateBuiltTransactionCell validates the final transaction structure.
// Message bodies and StateInit code/data/library references remain opaque.
func validateBuiltTransactionCell(root, inMsg *cell.Cell, outMsgs []OutMessage) error {
	var slice cell.Slice
	if err := root.BeginParseIntoWithoutTrace(&slice); err != nil {
		return err
	}
	loader := &slice

	magic, err := loader.LoadUInt(4)
	if err != nil {
		return err
	}
	if magic != 0b0111 {
		return fmt.Errorf("invalid transaction magic %b", magic)
	}
	if err = loader.SkipBits(256 + 64 + 256 + 64 + 32); err != nil {
		return err
	}
	outMsgCount, err := loader.LoadUInt(15)
	if err != nil {
		return err
	}
	if outMsgCount != uint64(len(outMsgs)) {
		return fmt.Errorf("output message count %d does not match built messages %d", outMsgCount, len(outMsgs))
	}
	if err = loader.SkipBits(2 + 2); err != nil {
		return err
	}

	ioCell, err := loader.LoadRefCell()
	if err != nil {
		return err
	}
	if err = validateBuiltTransactionIO(ioCell, inMsg, outMsgs, int(outMsgCount)); err != nil {
		return fmt.Errorf("invalid transaction IO: %w", err)
	}
	if err = validateBuiltCurrencyCollection(loader); err != nil {
		return fmt.Errorf("invalid transaction fees: %w", err)
	}

	stateUpdate, err := loader.LoadRefCell()
	if err != nil {
		return err
	}
	if err = validateBuiltTransactionHashUpdate(stateUpdate); err != nil {
		return fmt.Errorf("invalid state update: %w", err)
	}

	description, err := loader.LoadRefCell()
	if err != nil {
		return err
	}
	if err = validateBuiltTransactionDescription(description); err != nil {
		return fmt.Errorf("invalid transaction description: %w", err)
	}

	return transactionRequireEmptySlice(loader)
}

func validateBuiltTransactionIO(root, inMsg *cell.Cell, outMsgs []OutMessage, outMsgCount int) error {
	var slice cell.Slice
	if err := root.BeginParseIntoWithoutTrace(&slice); err != nil {
		return err
	}
	loader := &slice

	hasIn, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if hasIn != (inMsg != nil) {
		return errors.New("input message presence mismatch")
	}
	if hasIn {
		stored, err := loader.LoadRefCell()
		if err != nil {
			return err
		}
		if stored.HashKey() != inMsg.HashKey() {
			return errors.New("input message reference mismatch")
		}
		// The hash match ties stored to the prepared message, which
		// prepareParsedMessage already validated structurally.
	}

	hasOut, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if hasOut != (len(outMsgs) != 0) {
		return errors.New("output messages presence mismatch")
	}
	seen := 0
	count := 0
	if hasOut {
		root, err := loader.LoadRefCell()
		if err != nil {
			return err
		}
		count, err = root.AsDict(15).ForEachRefValue(func(stored *cell.Cell) error {
			if _, err := validateBuiltTransactionMessage(stored); err != nil {
				return fmt.Errorf("invalid output message %d: %w", seen, err)
			}
			if seen >= len(outMsgs) || outMsgs[seen].Cell == nil {
				return fmt.Errorf("output message %d is not present in built messages", seen)
			}
			expected := outMsgs[seen].Cell
			if stored != expected && stored.HashKey() != expected.HashKey() {
				return fmt.Errorf("output message %d reference mismatch", seen)
			}
			seen++
			return nil
		})
		if err != nil {
			return fmt.Errorf("invalid output messages dictionary: %w", err)
		}
	}
	if err = transactionRequireEmptySlice(loader); err != nil {
		return err
	}
	if count != outMsgCount || count != len(outMsgs) {
		return fmt.Errorf("output message dictionary count %d does not match transaction count %d and built messages %d", count, outMsgCount, len(outMsgs))
	}
	return nil
}

func validateBuiltTransactionHashUpdate(root *cell.Cell) error {
	var slice cell.Slice
	if err := root.BeginParseIntoWithoutTrace(&slice); err != nil {
		return err
	}
	loader := &slice
	magic, err := loader.LoadUInt(8)
	if err != nil {
		return err
	}
	if magic != 0x72 {
		return fmt.Errorf("invalid hash update magic %x", magic)
	}
	if err = loader.SkipBits(512); err != nil {
		return err
	}
	return transactionRequireEmptySlice(loader)
}

// transactionMessageLayout records the two Either choices the message cell
// actually made. Both are valid TL-B, so reproducing a parsed message requires
// knowing which one was written.
type transactionMessageLayout struct {
	stateInitInRef bool
	bodyInRef      bool
}

func validateBuiltTransactionMessage(root *cell.Cell) (transactionMessageLayout, error) {
	var layout transactionMessageLayout

	var slice cell.Slice
	if err := root.BeginParseIntoWithoutTrace(&slice); err != nil {
		return layout, err
	}
	loader := &slice

	external, err := loader.LoadBoolBit()
	if err != nil {
		return layout, err
	}
	if !external {
		if err = loader.SkipBits(3); err != nil {
			return layout, err
		}
		if err = validateBuiltMessageAddress(loader, builtAddressInt, builtAddressCanonical); err != nil {
			return layout, err
		}
		if err = validateBuiltMessageAddress(loader, builtAddressInt, builtAddressCanonical); err != nil {
			return layout, err
		}
		if err = validateBuiltCurrencyCollection(loader); err != nil {
			return layout, err
		}
		if err = validateBuiltVarUInt(loader, 16, false); err != nil {
			return layout, err
		}
		if err = validateBuiltVarUInt(loader, 16, false); err != nil {
			return layout, err
		}
		if err = loader.SkipBits(64 + 32); err != nil {
			return layout, err
		}
	} else {
		out, err := loader.LoadBoolBit()
		if err != nil {
			return layout, err
		}
		if out {
			if err = validateBuiltMessageAddress(loader, builtAddressInt, builtAddressCanonical); err != nil {
				return layout, err
			}
			if err = validateBuiltMessageAddress(loader, builtAddressExt, builtAddressCanonical); err != nil {
				return layout, err
			}
			if err = loader.SkipBits(64 + 32); err != nil {
				return layout, err
			}
		} else {
			if err = validateBuiltMessageAddress(loader, builtAddressExt, builtAddressCanonical); err != nil {
				return layout, err
			}
			if err = validateBuiltMessageAddress(loader, builtAddressInt, builtAddressCanonical); err != nil {
				return layout, err
			}
			if err = validateBuiltVarUInt(loader, 16, false); err != nil {
				return layout, err
			}
		}
	}

	hasInit, err := loader.LoadBoolBit()
	if err != nil {
		return layout, err
	}
	if hasInit {
		inRef, err := loader.LoadBoolBit()
		if err != nil {
			return layout, err
		}
		layout.stateInitInRef = inRef
		if inRef {
			stateInit, err := loader.LoadRefCell()
			if err != nil {
				return layout, err
			}
			if err = transactionValidateStateInitCell(stateInit); err != nil {
				return layout, err
			}
		} else if err = transactionValidateStateInit(loader); err != nil {
			return layout, err
		}
	}

	bodyInRef, err := loader.LoadBoolBit()
	if err != nil {
		return layout, err
	}
	layout.bodyInRef = bodyInRef
	if !bodyInRef {
		return layout, loader.SkipBitsAndRefs(loader.BitsLeft(), loader.RefsNum())
	}
	if _, err = loader.LoadRefCell(); err != nil {
		return layout, err
	}
	return layout, transactionRequireEmptySlice(loader)
}

type builtAddressKind uint8

type builtAddressValidation uint8

const (
	builtAddressExt builtAddressKind = 1 << iota
	builtAddressInt
)

const (
	// MessageRelaxed uses the generated structural check; the final normal
	// Message check additionally rejects non-canonical addr_var encodings.
	builtAddressStructural builtAddressValidation = iota
	builtAddressCanonical
)

func validateBuiltMessageAddress(loader *cell.Slice, allowed builtAddressKind, validation builtAddressValidation) error {
	typ, err := loader.LoadUInt(2)
	if err != nil {
		return err
	}
	switch typ {
	case 0:
		if allowed&builtAddressExt == 0 {
			return errors.New("external address used where internal address is required")
		}
		return nil
	case 1:
		if allowed&builtAddressExt == 0 {
			return errors.New("external address used where internal address is required")
		}
		ln, err := loader.LoadUInt(9)
		if err != nil {
			return err
		}
		return loader.SkipBits(uint(ln))
	case 2, 3:
		if allowed&builtAddressInt == 0 {
			return errors.New("internal address used where external address is required")
		}
		if err = validateBuiltAnycast(loader); err != nil {
			return err
		}
		if typ == 2 {
			return loader.SkipBits(8 + 256)
		}
		ln, err := loader.LoadUInt(9)
		if err != nil {
			return err
		}
		if validation == builtAddressStructural {
			return loader.SkipBits(32 + uint(ln))
		}
		workchain, err := loader.LoadInt(32)
		if err != nil {
			return err
		}
		if err = loader.SkipBits(uint(ln)); err != nil {
			return err
		}
		if workchain >= -128 && workchain <= 127 && ln == 256 {
			return errors.New("non-canonical variable internal address")
		}
		if workchain == 0 || workchain == -1 {
			return errors.New("reserved workchain in variable internal address")
		}
		return nil
	default:
		panic("unreachable")
	}
}

func validateBuiltAnycast(loader *cell.Slice) error {
	has, err := loader.LoadBoolBit()
	if err != nil || !has {
		return err
	}
	depth, err := loader.LoadUInt(5)
	if err != nil {
		return err
	}
	if depth == 0 || depth > 30 {
		return fmt.Errorf("invalid anycast depth %d", depth)
	}
	return loader.SkipBits(uint(depth))
}

func transactionValidateStateInit(loader *cell.Slice) error {
	for field := 0; field < 5; field++ {
		has, err := loader.LoadBoolBit()
		if err != nil {
			return err
		}
		if !has {
			continue
		}
		switch field {
		case 0:
			if err = loader.SkipBits(5); err != nil {
				return err
			}
		case 1:
			if err = loader.SkipBits(2); err != nil {
				return err
			}
		case 2, 3, 4:
			if _, err = loader.LoadRefCell(); err != nil {
				return err
			}
		}
	}
	return nil
}

func transactionValidateStateInitLibraries(libraries *cell.Dictionary, budget *int) error {
	if libraries == nil {
		return nil
	}
	return transactionValidateTypedDictionary(libraries.AsCell(), 256, budget, func(value *cell.Slice) error {
		// SimpleLib is exactly public:Bool root:^Cell. Its root stays opaque,
		// and the schema does not require its hash to match the dictionary key.
		if value.BitsLeft() != 1 || value.RefsNum() != 1 {
			return errors.New("invalid StateInit library entry")
		}
		return nil
	})
}

// OutListNode validates the message and its typed dictionaries in one
// budget. The historical schema also traverses StateInit libraries. Cell
// payloads and the previous action ref are opaque. A referenced Any body costs
// one cell, but its descendants remain opaque. Repeated typed refs are charged
// on every visit.
func transactionValidateActionMessageBudget(msgCell *cell.Cell, msg *tlb.Message, historicalLibraries bool) error {
	budget := 1024 - 2 // OutListNode and MessageRelaxed roots.
	if msg.MsgType == tlb.MsgTypeInternal {
		extra := msg.AsInternal().ExtraCurrencies
		if extra != nil {
			if err := transactionValidateTypedDictionary(extra.AsCell(), 32, &budget, func(value *cell.Slice) error {
				// Generated VarUInteger 32 validation only checks its shape;
				// canonicality and positivity are checked during SEND processing.
				size, err := value.LoadUInt(5)
				if err != nil {
					return err
				}
				if err := value.SkipBits(uint(size) * 8); err != nil {
					return err
				}
				return transactionRequireEmptySlice(value)
			}); err != nil {
				return err
			}
		}
	}
	state := transactionMessageStateInit(msg)
	layout, err := transactionOutboundMessageLayout(msgCell)
	if err != nil {
		return err
	}
	if layout.stateInitInRef {
		if budget == 0 {
			return errors.New("StateInit exceeds cell validation budget")
		}
		budget--
	}
	if historicalLibraries && state != nil {
		if err := transactionValidateStateInitLibraries(state.Lib, &budget); err != nil {
			return err
		}
	}
	if layout.bodyInRef {
		if budget == 0 {
			return errors.New("message body exceeds cell validation budget")
		}
		budget--
		var body cell.Slice
		if err := msg.Msg.Payload().BeginParseInto(&body); err != nil {
			return err
		}
		if body.IsSpecial() {
			return errors.New("referenced message body is special")
		}
	}
	return nil
}

// transactionValidateTypedDictionary performs one strong TL-B traversal,
// stopping before entering a node beyond the caller's budget. Loading all
// entries before checking the limit would expand shared subtrees unboundedly.
func transactionValidateTypedDictionary(root *cell.Cell, keyBits uint, budget *int, validateLeaf func(*cell.Slice) error) error {
	if root == nil {
		return nil
	}
	type pendingNode struct {
		root    *cell.Cell
		keyBits uint
	}
	var pending [257]pendingNode
	pending[0] = pendingNode{root: root, keyBits: keyBits}
	count := 1
	for count > 0 {
		if *budget <= 0 {
			return errors.New("typed dictionary exceeds cell validation budget")
		}
		*budget--
		count--
		node := pending[count]
		var value cell.Slice
		if err := node.root.BeginParseInto(&value); err != nil {
			return err
		}
		if value.IsSpecial() {
			return errors.New("special typed dictionary node")
		}

		// HmLabel accepts short, long and repeated-bit labels, including
		// non-minimal encodings. Only the length and available bits matter.
		long, err := value.LoadBoolBit()
		if err != nil {
			return err
		}
		var labelBits uint
		if !long {
			for {
				more, err := value.LoadBoolBit()
				if err != nil {
					return err
				}
				if !more {
					break
				}
				labelBits++
				if labelBits > node.keyBits {
					return errors.New("dictionary label exceeds key length")
				}
			}
			if err := value.SkipBits(labelBits); err != nil {
				return err
			}
		} else {
			same, err := value.LoadBoolBit()
			if err != nil {
				return err
			}
			if same {
				if err := value.SkipBits(1); err != nil {
					return err
				}
			}
			length, err := value.LoadUInt(uint(bits.Len(node.keyBits)))
			if err != nil {
				return err
			}
			labelBits = uint(length)
			if labelBits > node.keyBits {
				return errors.New("dictionary label exceeds key length")
			}
			if !same {
				if err := value.SkipBits(labelBits); err != nil {
					return err
				}
			}
		}
		if labelBits == node.keyBits {
			if err := validateLeaf(&value); err != nil {
				return err
			}
			continue
		}
		if value.BitsLeft() != 0 || value.RefsNum() != 2 {
			return errors.New("invalid typed dictionary fork")
		}
		left, err := value.LoadRefCell()
		if err != nil {
			return err
		}
		right, err := value.LoadRefCell()
		if err != nil {
			return err
		}
		childBits := node.keyBits - labelBits - 1
		pending[count] = pendingNode{root: right, keyBits: childBits}
		pending[count+1] = pendingNode{root: left, keyBits: childBits}
		count += 2
	}
	return nil
}

func transactionValidateStateInitCell(root *cell.Cell) error {
	var stateInit cell.Slice
	if err := root.BeginParseIntoWithoutTrace(&stateInit); err != nil {
		return err
	}
	if err := transactionValidateStateInit(&stateInit); err != nil {
		return err
	}
	return transactionRequireEmptySlice(&stateInit)
}

func validateBuiltTransactionDescription(root *cell.Cell) error {
	var slice cell.Slice
	if err := root.BeginParseIntoWithoutTrace(&slice); err != nil {
		return err
	}
	loader := &slice
	prefix, err := loader.LoadUInt(3)
	if err != nil {
		return err
	}

	switch prefix {
	case 0b000:
		storageOnly, err := loader.LoadBoolBit()
		if err != nil {
			return err
		}
		if storageOnly {
			return errors.New("storage-only description is not built by the emulator")
		}
		if err = loader.SkipBits(1); err != nil { // credit_first
			return err
		}
		if err = validateBuiltMaybeStoragePhase(loader); err != nil {
			return err
		}
		if err = validateBuiltMaybeCreditPhase(loader); err != nil {
			return err
		}
		if err = validateBuiltComputePhase(loader); err != nil {
			return err
		}
		if err = validateBuiltMaybeActionPhase(loader); err != nil {
			return err
		}
		if err = loader.SkipBits(1); err != nil { // aborted
			return err
		}
		if err = validateBuiltMaybeBouncePhase(loader); err != nil {
			return err
		}
		if err = loader.SkipBits(1); err != nil { // destroyed
			return err
		}
	case 0b001:
		if err = loader.SkipBits(1); err != nil { // is_tock
			return err
		}
		if err = validateBuiltStoragePhase(loader); err != nil {
			return err
		}
		if err = validateBuiltComputePhase(loader); err != nil {
			return err
		}
		if err = validateBuiltMaybeActionPhase(loader); err != nil {
			return err
		}
		if err = loader.SkipBits(2); err != nil { // aborted, destroyed
			return err
		}
	default:
		return fmt.Errorf("unsupported built transaction description %03b", prefix)
	}
	return transactionRequireEmptySlice(loader)
}

func validateBuiltStoragePhase(loader *cell.Slice) error {
	if err := validateBuiltVarUInt(loader, 16, false); err != nil {
		return err
	}
	if err := validateBuiltMaybeVarUInt(loader, 16, false); err != nil {
		return err
	}
	return validateBuiltStatusChange(loader)
}

func validateBuiltCreditPhase(loader *cell.Slice) error {
	if err := validateBuiltMaybeVarUInt(loader, 16, false); err != nil {
		return err
	}
	return validateBuiltCurrencyCollection(loader)
}

func validateBuiltComputePhase(loader *cell.Slice) error {
	vmPhase, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if !vmPhase {
		reason, err := loader.LoadUInt(2)
		if err != nil {
			return err
		}
		if reason == 0b11 {
			last, err := loader.LoadBoolBit()
			if err != nil {
				return err
			}
			if last {
				return errors.New("invalid compute skip reason")
			}
		}
		return nil
	}

	if err = loader.SkipBits(3); err != nil {
		return err
	}
	if err = validateBuiltVarUInt(loader, 16, false); err != nil {
		return err
	}
	details, err := loader.LoadRefCell()
	if err != nil {
		return err
	}
	var detailSlice cell.Slice
	if err = details.BeginParseIntoWithoutTrace(&detailSlice); err != nil {
		return err
	}
	detailLoader := &detailSlice
	if err = validateBuiltVarUInt(detailLoader, 7, false); err != nil {
		return err
	}
	if err = validateBuiltVarUInt(detailLoader, 7, false); err != nil {
		return err
	}
	hasGasCredit, err := detailLoader.LoadBoolBit()
	if err != nil {
		return err
	}
	if hasGasCredit {
		if err = validateBuiltVarUInt(detailLoader, 3, false); err != nil {
			return err
		}
	}
	if err = detailLoader.SkipBits(8 + 32); err != nil {
		return err
	}
	hasExitArg, err := detailLoader.LoadBoolBit()
	if err != nil {
		return err
	}
	if hasExitArg {
		if err = detailLoader.SkipBits(32); err != nil {
			return err
		}
	}
	if err = detailLoader.SkipBits(32 + 256 + 256); err != nil {
		return err
	}
	return transactionRequireEmptySlice(detailLoader)
}

func validateBuiltActionPhase(loader *cell.Slice) error {
	if err := loader.SkipBits(3); err != nil {
		return err
	}
	if err := validateBuiltStatusChange(loader); err != nil {
		return err
	}
	for i := 0; i < 2; i++ {
		if err := validateBuiltMaybeVarUInt(loader, 16, false); err != nil {
			return err
		}
	}
	if err := loader.SkipBits(32); err != nil {
		return err
	}
	hasResultArg, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if hasResultArg {
		if err = loader.SkipBits(32); err != nil {
			return err
		}
	}
	if err := loader.SkipBits(16*4 + 256); err != nil {
		return err
	}
	if err := validateBuiltStorageUsed(loader); err != nil {
		return err
	}
	return transactionRequireEmptySlice(loader)
}

func validateBuiltBouncePhase(loader *cell.Slice) error {
	first, err := loader.LoadBoolBit()
	if err != nil {
		return err
	}
	if first {
		if err = validateBuiltStorageUsed(loader); err != nil {
			return err
		}
		if err = validateBuiltVarUInt(loader, 16, false); err != nil {
			return err
		}
		return validateBuiltVarUInt(loader, 16, false)
	}

	noFunds, err := loader.LoadBoolBit()
	if err != nil || !noFunds {
		return err
	}
	if err = validateBuiltStorageUsed(loader); err != nil {
		return err
	}
	return validateBuiltVarUInt(loader, 16, false)
}

func validateBuiltStorageUsed(loader *cell.Slice) error {
	if err := validateBuiltVarUInt(loader, 7, false); err != nil {
		return err
	}
	return validateBuiltVarUInt(loader, 7, false)
}

func validateBuiltStatusChange(loader *cell.Slice) error {
	changed, err := loader.LoadBoolBit()
	if err != nil || !changed {
		return err
	}
	return loader.SkipBits(1)
}

func validateBuiltCurrencyCollection(loader *cell.Slice) error {
	if err := validateBuiltVarUInt(loader, 16, false); err != nil {
		return err
	}
	hasExtra, err := loader.LoadBoolBit()
	if err != nil || !hasExtra {
		return err
	}
	root, err := loader.LoadRefCell()
	if err != nil {
		return err
	}
	ok, err := root.AsDict(32).ValidateCheck(func(value *cell.Slice, _ *cell.Cell) (bool, error) {
		if err := validateBuiltVarUInt(value, 32, true); err != nil {
			return false, err
		}
		if err := transactionRequireEmptySlice(value); err != nil {
			return false, err
		}
		return true, nil
	}, false)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("invalid extra currency dictionary")
	}
	return nil
}

func validateBuiltVarUInt(loader *cell.Slice, size uint, positive bool) error {
	lenBits := uint(bits.Len64(uint64(size - 1)))
	ln, err := loader.LoadUInt(lenBits)
	if err != nil {
		return err
	}
	if ln >= uint64(size) || positive && ln == 0 {
		return errors.New("invalid variable unsigned integer length")
	}
	if ln == 0 {
		return nil
	}
	first, err := loader.PreloadUInt(8)
	if err != nil {
		return err
	}
	if first == 0 {
		return errors.New("non-canonical variable unsigned integer")
	}
	return loader.SkipBits(uint(ln) * 8)
}

func validateBuiltMaybeVarUInt(loader *cell.Slice, size uint, positive bool) error {
	has, err := loader.LoadBoolBit()
	if err != nil || !has {
		return err
	}
	return validateBuiltVarUInt(loader, size, positive)
}

func validateBuiltMaybeStoragePhase(loader *cell.Slice) error {
	has, err := loader.LoadBoolBit()
	if err != nil || !has {
		return err
	}
	return validateBuiltStoragePhase(loader)
}

func validateBuiltMaybeCreditPhase(loader *cell.Slice) error {
	has, err := loader.LoadBoolBit()
	if err != nil || !has {
		return err
	}
	return validateBuiltCreditPhase(loader)
}

func validateBuiltMaybeActionPhase(loader *cell.Slice) error {
	has, err := loader.LoadBoolBit()
	if err != nil || !has {
		return err
	}
	ref, err := loader.LoadRefCell()
	if err != nil {
		return err
	}
	var slice cell.Slice
	if err = ref.BeginParseIntoWithoutTrace(&slice); err != nil {
		return err
	}
	return validateBuiltActionPhase(&slice)
}

func validateBuiltMaybeBouncePhase(loader *cell.Slice) error {
	has, err := loader.LoadBoolBit()
	if err != nil || !has {
		return err
	}
	return validateBuiltBouncePhase(loader)
}

func transactionRequireEmptySlice(loader *cell.Slice) error {
	if loader.BitsLeft() != 0 || loader.RefsNum() != 0 {
		return fmt.Errorf("trailing data: %d bits, %d refs", loader.BitsLeft(), loader.RefsNum())
	}
	return nil
}
