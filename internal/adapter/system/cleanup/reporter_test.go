package cleanup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"retrom/internal/model/diagnostics"
)

func testReporter(buffer *bytes.Buffer) *Reporter {
	return NewReporter(slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, value slog.Attr) slog.Attr {
			if value.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return value
		},
	})))
}

func TestReporterPreservesOldErrorLogFields(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/old-log-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != "70f13dfbf40a4e920c749601a2c155444004e1d5628bec200e2169d1c499c449" {
		t.Fatal("old Go cleanup observations changed")
	}
	var capture struct{ Cases []oldCleanupCase }
	if err := json.Unmarshal(data, &capture); err != nil {
		t.Fatal(err)
	}
	// First nine records characterize Error reporting. Remove/RemoveAll and
	// rollback records remain evidence for their separate resource migrations.
	if len(capture.Cases) != 22 {
		t.Fatalf("capture cases=%d", len(capture.Cases))
	}
	for _, record := range capture.Cases[:9] {
		t.Run(record.Name, func(t *testing.T) { assertOldCleanupErrorReplay(t, record) })
	}
}

func TestReporterSanitizesLiteralsAndReportsAfterCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var output bytes.Buffer
	testReporter(&output).Report(ctx, diagnostics.DiagnosticEvent{
		Operation: "/private/secret", Code: "secret", Message: "error /private/secret", RequestID: "secret",
	})
	var got map[string]string
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "secret") || got["operation"] != "cleanup" ||
		got["errorType"] != "error" || got["code"] != diagnostics.CleanupFailureCode ||
		got["msg"] != "resource cleanup failed" || got["level"] != "WARN" {
		t.Fatalf("unsafe or dropped diagnostic: %s", output.String())
	}
	if bytes.Count(output.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("expected one record after cancellation: %s", output.String())
	}
}

func TestReporterRetainsGeneratedRequestID(t *testing.T) {
	t.Parallel()
	const request = "01980000-0000-7000-8000-00000000f601"
	var output bytes.Buffer
	testReporter(&output).Report(t.Context(), diagnostics.CleanupFailure("close", request, "*fs.PathError"))
	var got map[string]string
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["requestID"] != request {
		t.Fatalf("request identity changed: %v", got)
	}
}

func TestReporterRequiresAnExplicitLogger(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("nil logger silently accepted")
		}
	}()
	NewReporter(nil)
}

type hostileError struct{}

func (*hostileError) Error() string          { panic("Error must not be called") }
func (*hostileError) Format(fmt.State, rune) { panic("Format must not be called") }

func oldCleanupFixture(name string) error {
	const secret = "/private/RETROM_PRIVATE_SENTINEL_4a296d"
	switch name {
	case "nil":
		return nil
	case "plain-error", "nonfatal-return-primary":
		return errors.New(secret)
	case "wrapped-error":
		return fmt.Errorf("wrapped private failure: %w", errors.New(secret))
	case "path-error":
		return &os.PathError{Op: "open", Path: secret, Err: os.ErrPermission}
	case "errno":
		return syscall.EPERM
	case "joined-errors":
		return errors.Join(errors.New(secret), os.ErrPermission)
	case "hostile-error":
		return &hostileError{}
	case "typed-nil-error":
		var missing *hostileError
		return missing
	default:
		panic("unknown old cleanup fixture")
	}
}

type oldCleanupCase struct {
	Name string
	Logs []map[string]string
}

func assertOldCleanupErrorReplay(t *testing.T, record oldCleanupCase) {
	t.Helper()
	var output bytes.Buffer
	reporter := testReporter(&output)
	failure := oldCleanupFixture(record.Name)
	primary := errors.New("primary")
	actual := func() error {
		defer func() {
			if failure != nil {
				reporter.Report(t.Context(), diagnostics.CleanupFailure("close", "", fmt.Sprintf("%T", failure)))
			}
		}()
		return primary
	}()
	if reflect.TypeOf(actual) != reflect.TypeOf(primary) || !errors.Is(actual, primary) {
		t.Fatal("cleanup reporting replaced primary error")
	}
	if failure == nil {
		if output.Len() != 0 || len(record.Logs) != 0 {
			t.Fatal("nil cleanup failure emitted a record")
		}
		return
	}
	var got map[string]string
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["code"] != diagnostics.CleanupFailureCode || got["requestID"] != "" {
		t.Fatalf("diagnostic metadata=%v", got)
	}
	delete(got, "code")
	delete(got, "requestID")
	if len(record.Logs) != 1 {
		t.Fatalf("old error record count=%d", len(record.Logs))
	}
	old := record.Logs[0]
	// The captured custom error was declared by a package-main harness.
	// Only that fixture's package qualifier differs in this package test;
	// standard/production error types and every other field remain exact.
	if old["errorType"] == "*main.hostileError" {
		old["errorType"] = "*cleanup.hostileError"
	}
	if !reflect.DeepEqual(got, old) {
		t.Fatalf("old fields changed: got=%v want=%v", got, old)
	}
}
