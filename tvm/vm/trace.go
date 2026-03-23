package vm

// TraceHook can be set by callers that need lightweight VM tracing.
// By default tracing is disabled.
var TraceHook func(format string, args ...any)

func Tracef(format string, args ...any) {
	if TraceHook == nil {
		return
	}
	TraceHook(format, args...)
}

func tracef(format string, args ...any) {
	Tracef(format, args...)
}

func trace(msg string) {
	tracef("%s", msg)
}

func traceStack(prefix string, s *Stack) {
	if TraceHook == nil {
		return
	}
	if prefix == "" {
		tracef("%s", s.String())
		return
	}
	tracef("%s\n%s", prefix, s.String())
}
