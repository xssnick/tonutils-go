package vm

import "testing"

// IsNullContinuation must keep reporting exactly the shapes it did before it
// became an optional-interface check: a nil interface, a typed nil pointer of
// any continuation type (implemented here or by a host), and the shared
// nullContinuation instance. canonicalOwnedContinuation is checked alongside
// it because it is now defined purely in terms of IsNullContinuation.
func TestIsNullContinuationShapes(t *testing.T) {
	var (
		nilInterface Continuation
		nilNull      *nullContinuation
		nilOrdinary  *OrdinaryContinuation
		nilArgExt    *ArgExtContinuation
		nilQuit      *QuitContinuation
		nilExcQuit   *ExcQuitContinuation
		nilPushInt   *PushIntContinuation
		nilRepeat    *RepeatContinuation
		nilAgain     *AgainContinuation
		nilWhile     *WhileContinuation
		nilUntil     *UntilContinuation
		nilCustom    *parityNilContinuation
	)

	for _, test := range []struct {
		name string
		cont Continuation
		null bool
	}{
		{name: "nil interface", cont: nilInterface, null: true},
		{name: "null sentinel", cont: nullContinuationValue, null: true},
		{name: "nil nullContinuation", cont: nilNull, null: true},
		{name: "nil ordinary", cont: nilOrdinary, null: true},
		{name: "nil arg ext", cont: nilArgExt, null: true},
		{name: "nil quit", cont: nilQuit, null: true},
		{name: "nil exc quit", cont: nilExcQuit, null: true},
		{name: "nil push int", cont: nilPushInt, null: true},
		{name: "nil repeat", cont: nilRepeat, null: true},
		{name: "nil again", cont: nilAgain, null: true},
		{name: "nil while", cont: nilWhile, null: true},
		{name: "nil until", cont: nilUntil, null: true},
		{name: "nil custom host continuation", cont: nilCustom, null: true},
		{name: "ordinary", cont: &OrdinaryContinuation{}, null: false},
		{name: "quit", cont: quitCont0, null: false},
		{name: "until", cont: &UntilContinuation{Body: quitCont0, After: quitCont1}, null: false},
		{name: "custom host continuation", cont: &parityNilContinuation{}, null: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsNullContinuation(test.cont); got != test.null {
				t.Fatalf("IsNullContinuation(%T) = %v, want %v", test.cont, got, test.null)
			}

			canonical := canonicalOwnedContinuation(test.cont)
			if test.null {
				if canonical != nullContinuationValue {
					t.Fatalf("canonicalOwnedContinuation(%T) = %v, want the null continuation", test.cont, canonical)
				}
				return
			}
			if canonical != test.cont {
				t.Fatalf("canonicalOwnedContinuation(%T) = %v, want the value itself", test.cont, canonical)
			}
		})
	}
}
