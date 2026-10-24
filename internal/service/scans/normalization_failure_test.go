package scans

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"retrom/internal/model"
)

func TestNormalizationFailureReportsStageWithoutLeakingPrivateReason(t *testing.T) {
	t.Parallel()
	cause := errors.New("RUNTIME_TEST_DECODER_FAILED /private/source")
	err := normalizationFailure(cause)
	var content *model.RuntimeContentError
	if !errors.As(err, &content) || content.Code != "CONTENT_NORMALIZATION_FAILED" ||
		strings.Contains(content.Message, "/private") || !errors.Is(err, cause) {
		t.Fatalf("normalization stage/diagnostic cause lost: %+v %v", content, err)
	}
	if importFailureCode(err) != "CONTENT_NORMALIZATION_FAILED" {
		t.Fatal("scan progress hid the conversion-stage failure")
	}
}

func TestOrdinaryImportFailuresRetainTheirExistingProjection(t *testing.T) {
	t.Parallel()
	for _, err := range []error{model.ErrInvalid, &model.RuntimeContentError{Code: "RUNTIME_ENTRY_MISSING", Message: "missing"}} {
		if importFailureCode(err) != "IMPORT_FAILED" {
			t.Fatal("normalization changed an ordinary import error")
		}
	}
}

func TestNormalizationFailurePreservesCancellationAndAdmissionSemantics(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, model.ErrUnavailable, model.ErrStorageFull} {
		err := normalizationFailure(fmt.Errorf("runtime request: %w", cause))
		var content *model.RuntimeContentError
		if !errors.Is(err, cause) || errors.As(err, &content) {
			t.Fatalf("operational failure became invalid content: %v", err)
		}
	}
}
