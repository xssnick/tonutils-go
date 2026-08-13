package vm

import (
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/xssnick/tonutils-go/tvm/vmerr"
)

type Continuation interface {
	GetControlData() *ControlData
	Jump(state *State) (Continuation, error)
	Copy() Continuation
}

// nullContinuation represents a t_vmcont stack entry holding a null reference.
// It is distinct from the TVM null value and must never be executed.
type nullContinuation struct{}

var nullContinuationValue Continuation = &nullContinuation{}

func (*nullContinuation) GetControlData() *ControlData {
	return nil
}

func (*nullContinuation) Jump(*State) (Continuation, error) {
	return nil, vmerr.Error(vmerr.CodeTypeCheck, "null continuation")
}

func (*nullContinuation) Copy() Continuation {
	return nullContinuationValue
}

func (*nullContinuation) String() string {
	return "null"
}

// nilContinuation lets a continuation answer for itself whether its receiver is
// a typed nil, keeping IsNullContinuation off the reflection path for every
// continuation defined here (Register.AdjustWith calls it four times per jump).
type nilContinuation interface {
	isNilContinuation() bool
}

// IsNullContinuation reports whether cont holds a null continuation reference.
// Built-in VM continuations stay on a reflection-free path; the fallback keeps
// the exported host boundary correct for custom typed-nil implementations.
func IsNullContinuation(cont Continuation) bool {
	if cont == nil {
		return true
	}
	if n, ok := cont.(nilContinuation); ok {
		return n.isNilContinuation()
	}
	return isNilContinuationValue(cont)
}

func (*nullContinuation) isNilContinuation() bool       { return true }
func (c *OrdinaryContinuation) isNilContinuation() bool { return c == nil }
func (c *ArgExtContinuation) isNilContinuation() bool   { return c == nil }
func (c *QuitContinuation) isNilContinuation() bool     { return c == nil }
func (c *ExcQuitContinuation) isNilContinuation() bool  { return c == nil }
func (c *PushIntContinuation) isNilContinuation() bool  { return c == nil }
func (c *RepeatContinuation) isNilContinuation() bool   { return c == nil }
func (c *AgainContinuation) isNilContinuation() bool    { return c == nil }
func (c *WhileContinuation) isNilContinuation() bool    { return c == nil }
func (c *UntilContinuation) isNilContinuation() bool    { return c == nil }

type QuitContinuation struct {
	ExitCode int64
}

// shared instances for the exit codes produced on every call/return
var (
	quitCont0       = &QuitContinuation{ExitCode: 0}
	quitCont1       = &QuitContinuation{ExitCode: 1}
	quitContUnknown = &QuitContinuation{ExitCode: vmerr.CodeUnknown}
	handledErrors   = makeHandledErrors()
)

// quitContinuation returns a shared instance for hot exit codes;
// QuitContinuation is immutable, so sharing is safe.
func quitContinuation(exitCode int64) *QuitContinuation {
	switch exitCode {
	case 0:
		return quitCont0
	case 1:
		return quitCont1
	}
	return &QuitContinuation{ExitCode: exitCode}
}

func (c *QuitContinuation) GetControlData() *ControlData {
	return nil
}

func (c *QuitContinuation) Jump(state *State) (Continuation, error) {
	// Jumping into a quit continuation terminates the VM directly with the
	// stored code: the stack is preserved, no exception gas is charged and c2
	// is never involved, so the signal must not be routed through the
	// exception path.
	return nil, handledError(c.ExitCode)
}

func (c *QuitContinuation) Copy() Continuation {
	// real copy: ExitCode is exported, callers may mutate the copy
	cont := *c
	return &cont
}

type ExcQuitContinuation struct{}

type HandledException struct {
	vmerr.VMError
}

func makeHandledErrors() [16]error {
	var out [16]error
	for code := range out {
		out[code] = HandledException{VMError: vmerr.Error(int64(code))}
	}
	return out
}

func handledError(code int64) error {
	if code >= 0 && code < int64(len(handledErrors)) {
		return handledErrors[code]
	}
	return HandledException{VMError: vmerr.Error(code)}
}

func (e HandledException) Error() string {
	return e.VMError.Error()
}

func (e HandledException) Unwrap() error {
	return e.VMError
}

// IsHandledException is an allocation-free errors.As for HandledException.
func IsHandledException(err error) bool {
	for err != nil {
		if _, ok := err.(HandledException); ok {
			return true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, sub := range x.Unwrap() {
				if IsHandledException(sub) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
	return false
}

func (c *ExcQuitContinuation) GetControlData() *ControlData {
	return nil
}

func (c *ExcQuitContinuation) Jump(state *State) (Continuation, error) {
	v, err := state.Stack.PopIntRange(0, 0xffff)
	if err != nil {
		code, ok := vmerr.ErrorCode(err)
		if !ok {
			code = vmerr.CodeFatal
		}
		return nil, handledError(code)
	}

	return nil, handledError(v.Int64())
}

func (c *ExcQuitContinuation) Copy() Continuation {
	// stateless, sharing is safe
	return c
}

type OrdinaryContinuation struct {
	Data ControlData
	Code *cell.Slice
}

func forceContinuationCodepage(state *State, cp int) error {
	if cp == state.CP {
		return nil
	}
	if cp != 0 {
		return vmerr.Error(vmerr.CodeInvalidOpcode, "unsupported codepage")
	}
	state.CP = cp
	return nil
}

func applyContinuationCodepage(state *State, cp int) error {
	if cp == CP {
		return nil
	}
	return forceContinuationCodepage(state, cp)
}

func (c *OrdinaryContinuation) GetControlData() *ControlData {
	return &c.Data
}

func (c *OrdinaryContinuation) Jump(state *State) (Continuation, error) {
	state.Reg.AdjustWith(&c.Data.Save)
	state.adoptCurrentCode(c.Code)
	if err := forceContinuationCodepage(state, c.Data.CP); err != nil {
		return nil, err
	}
	return nil, nil
}

// adoptCurrentCode installs code as the executing code window without
// touching code itself, so re-entering the same continuation always starts
// from the same position. CurrentCode is advanced during execution, so the
// State keeps its own slice: it is overwritten in place while exclusively
// owned and reallocated only after the pointer escaped into a continuation.
func (s *State) adoptCurrentCode(code *cell.Slice) {
	trace := cell.CombineTraces(code.Trace(), s.Cells.Trace())

	if s.currentCodeOwned {
		*s.CurrentCode = *code
	} else {
		s.CurrentCode = code.Copy()
		s.currentCodeOwned = true
	}
	s.CurrentCode.SetTrace(trace)
}

func (c *OrdinaryContinuation) Copy() Continuation {
	cont := &OrdinaryContinuation{
		Data: c.Data.Copy(),
		Code: c.Code,
	}
	return cont
}

type ArgExtContinuation struct {
	Data ControlData
	Ext  Continuation
}

func (c *ArgExtContinuation) GetControlData() *ControlData {
	return &c.Data
}

func (c *ArgExtContinuation) Jump(state *State) (Continuation, error) {
	state.Reg.AdjustWith(&c.Data.Save)
	if err := applyContinuationCodepage(state, c.Data.CP); err != nil {
		return nil, err
	}
	return c.Ext, nil
}

func (c *ArgExtContinuation) Copy() Continuation {
	cont := &ArgExtContinuation{
		Data: c.Data.Copy(),
		Ext:  c.Ext,
	}
	return cont
}

type PushIntContinuation struct {
	Int  int64
	Next Continuation
}

func (c *PushIntContinuation) GetControlData() *ControlData {
	return nil
}

func (c *PushIntContinuation) Jump(state *State) (Continuation, error) {
	if err := state.Stack.PushSmallInt(c.Int); err != nil {
		return nil, err
	}
	return c.Next, nil
}

func (c *PushIntContinuation) Copy() Continuation {
	return &PushIntContinuation{
		Int:  c.Int,
		Next: c.Next,
	}
}

type RepeatContinuation struct {
	Count int64
	Body  Continuation
	After Continuation
}

func (c *RepeatContinuation) GetControlData() *ControlData {
	return nil
}

func (c *RepeatContinuation) Jump(state *State) (Continuation, error) {
	if c.Count <= 0 {
		state.Trace("finish REPEAT")
		return c.After, nil
	}

	if state.TraceEnabled() {
		state.Tracef("iteration REPEAT %d", c.Count)
	}

	if cd := c.Body.GetControlData(); cd != nil && !IsNullContinuation(cd.Save.C[0]) {
		return c.Body, nil
	}

	state.Reg.C[0] = &RepeatContinuation{
		Count: c.Count - 1,
		Body:  c.Body,
		After: c.After,
	}

	return c.Body, nil
}

func (c *RepeatContinuation) Copy() Continuation {
	return &RepeatContinuation{
		Count: c.Count,
		Body:  c.Body,
		After: c.After,
	}
}

type AgainContinuation struct {
	Body Continuation
}

func (c *AgainContinuation) GetControlData() *ControlData {
	return nil
}

func (c *AgainContinuation) Jump(state *State) (Continuation, error) {
	if cd := c.Body.GetControlData(); cd == nil || IsNullContinuation(cd.Save.C[0]) {
		state.Reg.C[0] = c.Copy()
	}
	return c.Body, nil
}

func (c *AgainContinuation) Copy() Continuation {
	return &AgainContinuation{
		Body: c.Body,
	}
}

type WhileContinuation struct {
	CheckCond bool
	Body      Continuation
	Cond      Continuation
	After     Continuation

	// flip is the opposite-phase twin of this loop continuation, allocated
	// lazily and reused across iterations instead of allocating a fresh
	// value-identical WhileContinuation on every cond<->body transition.
	flip *WhileContinuation
}

func (c *WhileContinuation) GetControlData() *ControlData {
	return nil
}

func (c *WhileContinuation) Jump(state *State) (Continuation, error) {
	if c.CheckCond {
		more, err := state.Stack.PopBool()
		if err != nil {
			return nil, err
		}

		if !more {
			state.Trace("finish WHILE")
			return c.After, nil
		}

		if cd := c.Body.GetControlData(); cd == nil || IsNullContinuation(cd.Save.C[0]) {
			state.Reg.C[0] = c.flipped()
		}

		return c.Body, nil
	}

	if cd := c.Cond.GetControlData(); cd == nil || IsNullContinuation(cd.Save.C[0]) {
		state.Reg.C[0] = c.flipped()
	}

	return c.Cond, nil
}

// flipped returns the opposite-phase continuation of this loop, like
// UntilContinuation reuses itself. Body/Cond/After never change after
// construction, so the cached twin is value-identical to a freshly
// allocated one on every iteration.
func (c *WhileContinuation) flipped() *WhileContinuation {
	if c.flip == nil {
		c.flip = &WhileContinuation{
			CheckCond: !c.CheckCond,
			Body:      c.Body,
			Cond:      c.Cond,
			After:     c.After,
			flip:      c,
		}
	}
	return c.flip
}

func (c *WhileContinuation) Copy() Continuation {
	return &WhileContinuation{
		CheckCond: c.CheckCond,
		Body:      c.Body,
		Cond:      c.Cond,
		After:     c.After,
	}
}

type UntilContinuation struct {
	Body  Continuation
	After Continuation
}

func (c *UntilContinuation) GetControlData() *ControlData {
	return nil
}

func (c *UntilContinuation) Jump(state *State) (Continuation, error) {
	end, err := state.Stack.PopBool()
	if err != nil {
		return nil, err
	}

	if end {
		state.Trace("finish UNTIL")
		return c.After, nil
	}

	if cd := c.Body.GetControlData(); cd == nil || IsNullContinuation(cd.Save.C[0]) {
		state.Reg.C[0] = c
	}

	return c.Body, nil
}

func (c *UntilContinuation) Copy() Continuation {
	return &UntilContinuation{
		Body:  c.Body,
		After: c.After,
	}
}
