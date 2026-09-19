package processlock

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"retrom/internal/model/diagnostics"
	"retrom/internal/testkit/testsupport"
)

type hostileCleanupError struct{}

func (*hostileCleanupError) Error() string          { panic("Error must not be called") }
func (*hostileCleanupError) Format(fmt.State, rune) { panic("Format must not be called") }

func TestLockCleanupReportsOnlyOuterErrorType(t *testing.T) {
	t.Parallel()
	var typedNil *hostileCleanupError
	for _, test := range []struct {
		name string
		err  error
		kind string
	}{
		{"nil", nil, ""},
		{"plain", errors.New("/private/secret"), "*errors.errorString"},
		{"path", &os.PathError{Op: "open", Path: "/private/secret", Err: os.ErrPermission}, "*fs.PathError"},
		{"wrapped", fmt.Errorf("private wrapper: %w", os.ErrPermission), "*fmt.wrapError"},
		{"hostile", &hostileCleanupError{}, "*processlock.hostileCleanupError"},
		{"typed-nil", typedNil, "*processlock.hostileCleanupError"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := &testsupport.DiagnosticRecorder{}
			reportClose(t.Context(), recorder, test.err)
			events := recorder.Events()
			if test.err == nil {
				if len(events) != 0 {
					t.Fatal("nil failure reported")
				}
				return
			}
			want := diagnostics.CleanupFailure("close", "", test.kind)
			if len(events) != 1 || events[0] != want {
				t.Fatalf("outer error type not preserved: %+v", events)
			}
		})
	}
}

func TestLeaseReportsCloseFailureWithoutReplacingDescriptorError(t *testing.T) {
	t.Parallel()
	recorder := &testsupport.DiagnosticRecorder{}
	lease, err := New(recorder).Acquire(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if len(recorder.Events()) != 0 {
		t.Fatal("successful release was reported as failure")
	}
	if err := lease.Close(); !errors.Is(err, errDescriptorInvalid) {
		t.Fatalf("descriptor error was replaced: %v", err)
	}
	events := recorder.Events()
	if len(events) != 1 || events[0] != diagnostics.CleanupFailure("close", "", "*fs.PathError") {
		t.Fatalf("failed descriptor cleanup diagnostics=%+v", events)
	}
}
