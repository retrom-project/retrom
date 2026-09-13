package emulationstationimport

import (
	"errors"
	"reflect"
	"testing"

	library "retrom/internal/service/libraryimport"
)

func TestReviewPreparerReplaysDuplicateAfterSourcePayloadCleanup(t *testing.T) {
	memory := newReviewPreparerMemory()
	memory.found = true
	memory.result.Items[0].State = "DISCARDED"
	memory.result.Items[0].ExistingGameID = "game"
	memory.result.Items[0].ExistingMatches = []library.ServerDuplicateMatch{{GameID: "game"}}
	unit, item := reviewPreparerInputs()
	item.Files = nil
	found, err := memory.service().Resume(t.Context(), unit, item)
	if err != nil || !found || !reflect.DeepEqual(
		memory.calls,
		[]string{"lookup", "resume", "finish"},
	) || memory.outcome.State != "SKIPPED_EXISTING" || memory.outcome.ExistingGameID != "game" || len(
		memory.outcome.ExistingMatches,
	) != 1 {
		t.Fatalf("found=%v error=%v calls=%v outcome=%#v", found, err, memory.calls, memory.outcome)
	}
}

func TestReviewPreparerKeepsDeterministicFailureDiagnostics(t *testing.T) {
	for _, kind := range []string{"metadata", "group", "multidisc", "limit", "handoff"} {
		t.Run(kind, func(t *testing.T) {
			memory := newReviewPreparerMemory()
			unit, item := reviewPreparerInputs()
			expected := "BLOCKED_CONTENT"
			code := "EMULATIONSTATION_CONTENT_FORMAT_UNSUPPORTED"
			switch kind {
			case "metadata":
				item.MetadataJSON = "{"
				code = "EMULATIONSTATION_METADATA_SYNTAX_INVALID"
			case "group":
				memory.createErr = library.ErrSourceGrouping
			case "multidisc":
				memory.createErr = library.ErrMultiDiscModeUnavailable
				code = "MULTI_DISC_MODE_UNAVAILABLE"
			case "limit":
				memory.createErr = library.ErrInvalid
				item.Files = make([]ExecutionFile, library.ServerSourceFileLimit+1)
				expected = "COMMIT_FAILED"
				code = "EMULATIONSTATION_LIBRARY_IMPORT_FAILED"
			case "handoff":
				memory.handoffErr = errors.New("database query failed")
				expected = "COMMIT_FAILED"
				code = "INTERNAL_ERROR"
			}
			if err := memory.service().Create(t.Context(), unit, item); err != nil {
				t.Fatal(err)
			}
			if memory.outcome.State != expected || memory.outcome.Code != code {
				t.Fatalf("outcome=%#v", memory.outcome)
			}
			assertReviewFailureDetails(t, kind, memory.outcome)
		})
	}
}

func assertReviewFailureDetails(t *testing.T, kind string, outcome ItemOutcome) {
	t.Helper()
	if kind == "metadata" {
		assertReviewMetadataFailure(t, outcome.Failure)
	}
	if kind == "limit" {
		assertReviewLimitFailure(t, outcome.Failure)
	}
	if kind == "handoff" {
		assertReviewHandoffFailure(t, outcome)
	}
}

func assertReviewMetadataFailure(t *testing.T, details *FailureDetails) {
	t.Helper()
	if details == nil || details.CauseCode != "METADATA_JSON_INVALID" || details.LibraryImportItemID == nil || *details.LibraryImportItemID != "ordinary-item" {
		t.Fatalf("metadata failure=%#v", details)
	}
}

func assertReviewLimitFailure(t *testing.T, details *FailureDetails) {
	t.Helper()
	if details == nil || details.CauseCode != "SOURCE_FILE_LIMIT_EXCEEDED" || details.ObservedFileCount == nil || *details.ObservedFileCount != 66 || details.AllowedFileCount == nil || *details.AllowedFileCount != 64 {
		t.Fatalf("file count failure=%#v", details)
	}
}

func assertReviewHandoffFailure(t *testing.T, outcome ItemOutcome) {
	t.Helper()
	details := outcome.Failure
	if !outcome.Retryable || details == nil || details.LibraryImportJobID == nil || *details.LibraryImportJobID != "ordinary-job" || details.TechnicalDetail != "safe diagnostic" {
		t.Fatalf("handoff failure=%#v", details)
	}
}
