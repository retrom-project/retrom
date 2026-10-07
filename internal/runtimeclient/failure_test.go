package runtimeclient

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"

	"retrom/internal/model"
)

func TestRestoreIdentityFailuresPreserveConflictBoundary(t *testing.T) {
	t.Parallel()
	for _, code := range []string{
		"CORE_UNAVAILABLE", "CORE_CHANGED", "CONTENT_CHANGED", "FORMAT_UNREADABLE",
		"RUNTIME_SAVED_CONTEXT_INVALID", "RUNTIME_CORE_UNAVAILABLE",
	} {
		t.Run(code, func(t *testing.T) {
			response := `{"id":"request","error":"` + code + `"}` + "\n"
			var output any
			err := exchange(&bytes.Buffer{}, bufio.NewReader(strings.NewReader(response)), []byte(`{}`), &output)
			conflict := code == "CORE_UNAVAILABLE" || code == "CORE_CHANGED" ||
				code == "CONTENT_CHANGED" || code == "FORMAT_UNREADABLE"
			if errors.Is(err, model.ErrConflict) != conflict || errors.Is(err, model.ErrInvalid) == conflict {
				t.Fatalf("runtime failure lost domain category: %v", err)
			}
		})
	}
}

func TestParentFailureRetainsSpecificMissingNamesAcrossRuntimeIPC(t *testing.T) {
	t.Parallel()
	response := `{"id":"request","error":"RUNTIME_PARENT_MISSING","errorDetails":{"parents":"1941"}}` + "\n"
	var output any
	err := exchange(&bytes.Buffer{}, bufio.NewReader(strings.NewReader(response)), []byte(`{}`), &output)
	var parent *model.ArcadeParentError
	if !errors.As(err, &parent) || parent.Code != "RUNTIME_PARENT_MISSING" || !strings.Contains(parent.Message, "1941") {
		t.Fatalf("missing name lost across IPC: %v", err)
	}
}
