package diagnostics

import (
	"strings"
	"testing"
)

func TestCleanupFailureContainsOnlySanitizedScalarFacts(t *testing.T) {
	t.Parallel()
	const request = "01980000-0000-7000-8000-00000000f601"
	for _, test := range []struct {
		name, operation, request, kind       string
		wantOperation, wantRequest, wantKind string
	}{
		{"normal", "close ScummVM executable", request, "*fs.PathError", "close ScummVM executable", request, "*fs.PathError"},
		{"wrapped", "rollback", "", "*fmt.wrapError", "rollback", "", "*fmt.wrapError"},
		{"value", "close", "", "syscall.Errno", "close", "", "syscall.Errno"},
		{"empty", "", "", "", "cleanup", "", "error"},
		{"path", "/private/secret", request, "*fs.PathError", "cleanup", request, "*fs.PathError"},
		{"control", "close\nsecret", request, "*fs.PathError", "cleanup", request, "*fs.PathError"},
		{"unicode", "关闭", request, "*fs.PathError", "cleanup", request, "*fs.PathError"},
		{"blank", "  ", request, "*fs.PathError", "cleanup", request, "*fs.PathError"},
		{"long-operation", strings.Repeat("a", 129), request, "*fs.PathError", "cleanup", request, "*fs.PathError"},
		{"operation-bound", strings.Repeat("a", 128), request, "*fs.PathError", strings.Repeat("a", 128), request, "*fs.PathError"},
		{"anonymous", "close", request, "struct { error; Secret string }", "close", request, "error"},
		{"package-path", "close", request, "*private/secret.Error", "close", request, "error"},
		{"method-text", "close", request, "Error: /private/secret", "close", request, "error"},
		{"extra-pointer", "close", request, "**fs.PathError", "close", request, "error"},
		{"empty-package", "close", request, "*.PathError", "close", request, "error"},
		{"empty-type", "close", request, "*fs.", "close", request, "error"},
		{"digit-type", "close", request, "*fs.0Error", "close", request, "error"},
		{"long-type", "close", request, "*fs." + strings.Repeat("E", 129), "close", request, "error"},
		{"request-path", "close", "/private/secret", "*fs.PathError", "close", "", "*fs.PathError"},
		{"request-newline", "close", request[:35] + "\n", "*fs.PathError", "close", "", "*fs.PathError"},
		{"request-separator", "close", strings.ReplaceAll(request, "-", "f"), "*fs.PathError", "close", "", "*fs.PathError"},
		{"request-hex", "close", "g" + request[1:], "*fs.PathError", "close", "", "*fs.PathError"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := CleanupFailure(test.operation, test.request, test.kind)
			want := DiagnosticEvent{
				Operation: test.wantOperation, Code: CleanupFailureCode,
				Message: test.wantKind, RequestID: test.wantRequest,
			}
			if got != want {
				t.Fatalf("event=%+v want=%+v", got, want)
			}
		})
	}
}
