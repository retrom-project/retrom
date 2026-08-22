// Package testassert provides small shared assertion primitives for readable
// unit and integration tests without repeating control-flow boilerplate.
package testassert

// T is the subset of testing.T used by the assertion helpers.
type T interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Error(args ...any)
	Errorf(format string, args ...any)
}

// Any evaluates predicates from left to right and preserves logical-OR short circuiting.
func Any(predicates ...func() bool) bool {
	for _, predicate := range predicates {
		if predicate() {
			return true
		}
	}
	return false
}

// All evaluates predicates from left to right and preserves logical-AND short circuiting.
func All(predicates ...func() bool) bool {
	for _, predicate := range predicates {
		if !predicate() {
			return false
		}
	}
	return true
}

// False stops the test when failed is true.
func False(t T, failed bool, args ...any) {
	t.Helper()
	if failed {
		t.Fatal(args...)
	}
}

// Falsef stops the test with a formatted message when failed is true.
func Falsef(t T, failed bool, format string, args ...any) {
	t.Helper()
	if failed {
		t.Fatalf(format, args...)
	}
}

// CheckFalse records a non-fatal test failure when failed is true.
func CheckFalse(t T, failed bool, args ...any) {
	t.Helper()
	if failed {
		t.Error(args...)
	}
}

// CheckFalsef records a formatted non-fatal test failure when failed is true.
func CheckFalsef(t T, failed bool, format string, args ...any) {
	t.Helper()
	if failed {
		t.Errorf(format, args...)
	}
}

// True stops the test unless satisfied is true.
func True(t T, satisfied bool, args ...any) {
	False(t, !satisfied, args...)
}

// Truef stops the test with a formatted message unless satisfied is true.
func Truef(t T, satisfied bool, format string, args ...any) {
	Falsef(t, !satisfied, format, args...)
}

// CheckTrue records a non-fatal test failure unless satisfied is true.
func CheckTrue(t T, satisfied bool, args ...any) {
	CheckFalse(t, !satisfied, args...)
}

// CheckTruef records a formatted non-fatal test failure unless satisfied is true.
func CheckTruef(t T, satisfied bool, format string, args ...any) {
	CheckFalsef(t, !satisfied, format, args...)
}
