package testutil

// testControl is a type alias for adding extra logic into the test setup. This
// should never be used outside of this package, and is only meant to control
// some error paths to ensure we have full test coverage in place.
type testControl int

const (
	unknown testControl = iota

	// forcedFailureFirst could be used in the if check for function calls that
	// are difficult to let it fail.
	// This should be used for the first branching to fail.
	forcedFailureFirst

	// forcedFailureFirst could be used in the if check for function calls that
	// are difficult to let it fail.
	// This should be used for the second branching to fail.
	forcedFailureSecond

	// forcedFailureFirst could be used in the if check for function calls that
	// are difficult to let it fail.
	// This should be used for the third branching to fail.
	forcedFailureThird

	sentinel
)
