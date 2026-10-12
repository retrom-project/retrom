package runtimeclient

import (
	"encoding/json"
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
			err := decodeFailure(t, response)
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
	err := decodeFailure(t, response)
	var parent *model.RuntimeContentError
	if !errors.As(err, &parent) || parent.Code != "RUNTIME_PARENT_MISSING" || !strings.Contains(parent.Message, "1941") {
		t.Fatalf("missing name lost across IPC: %v", err)
	}
}

func TestContentFailureKeepsRuntimeReasonAcrossIPC(t *testing.T) {
	for _, code := range []string{
		"RUNTIME_ARCADE_MACHINE_UNKNOWN", "RUNTIME_FLYCAST_MEDIA_UNSUPPORTED", "RUNTIME_FLYCAST_ROM_MISSING",
		"RUNTIME_FLYCAST_DISC_MISSING", "RUNTIME_FLYCAST_DISC_INVALID", "RUNTIME_FLYCAST_DISC_AMBIGUOUS",
	} {
		t.Run(code, func(t *testing.T) {
			response := `{"id":"request","error":"` + code + `"}` + "\n"
			err := decodeFailure(t, response)
			var content *model.RuntimeContentError
			if !errors.As(err, &content) || content.Code != code || content.Message == "" || !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("content reason lost across IPC: %v", err)
			}
		})
	}
}

func decodeFailure(t *testing.T, raw string) error {
	t.Helper()
	var response runtimeResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatal(err)
	}
	return runtimeFailure(response.Error, response.ErrorDetails)
}
