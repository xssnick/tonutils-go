package vm

import (
	"fmt"
	"strings"
)

type TraceStep struct {
	Step         uint32
	Opcode       string
	Stack        *Stack
	GasRemaining int64
	Message      string
}

type TraceHook func(TraceStep)

func (s TraceStep) String() string {
	var b strings.Builder
	if s.Opcode != "" {
		fmt.Fprintf(&b, "#%d %s gas=%d", s.Step, s.Opcode, s.GasRemaining)
		if s.Stack != nil {
			b.WriteString("\n")
			b.WriteString(s.Stack.String())
		}
		return b.String()
	}
	if s.Message != "" {
		fmt.Fprintf(&b, "#%d %s", s.Step, s.Message)
		if s.Stack != nil {
			b.WriteString("\n")
			b.WriteString(s.Stack.String())
		}
		return b.String()
	}
	if s.Stack != nil {
		fmt.Fprintf(&b, "#%d gas=%d\n%s", s.Step, s.GasRemaining, s.Stack.String())
		return b.String()
	}
	fmt.Fprintf(&b, "#%d gas=%d", s.Step, s.GasRemaining)
	return b.String()
}

func (s *State) TraceEnabled() bool {
	return s != nil && s.TraceHook != nil
}

func (s *State) TraceOpcode(opcode string) {
	if !s.TraceEnabled() {
		return
	}

	var stack *Stack
	if s.Stack != nil {
		stack = s.Stack.Copy()
	}
	s.TraceHook(TraceStep{
		Step:         s.Steps,
		Opcode:       opcode,
		Stack:        stack,
		GasRemaining: s.Gas.Remaining,
	})
}

func (s *State) Tracef(format string, args ...any) {
	if !s.TraceEnabled() {
		return
	}

	s.TraceHook(TraceStep{
		Step:         s.Steps,
		GasRemaining: s.Gas.Remaining,
		Message:      fmt.Sprintf(format, args...),
	})
}

func (s *State) Trace(msg string) {
	if !s.TraceEnabled() {
		return
	}

	s.TraceHook(TraceStep{
		Step:         s.Steps,
		GasRemaining: s.Gas.Remaining,
		Message:      msg,
	})
}

func (s *State) TraceStack(prefix string, stack *Stack) {
	if !s.TraceEnabled() {
		return
	}

	var snapshot *Stack
	if stack != nil {
		snapshot = stack.Copy()
	}
	s.TraceHook(TraceStep{
		Step:         s.Steps,
		Message:      prefix,
		Stack:        snapshot,
		GasRemaining: s.Gas.Remaining,
	})
}
