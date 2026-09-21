package libraryimport

import (
	"errors"
	"testing"
)

func TestReviewDeduplicateRejectsInvalidScopeAndContinuation(t *testing.T) {
	const first = "01990000-0000-7000-8000-000000000001"
	const last = "01990000-0000-7000-8000-000000000002"
	cases := []struct {
		name    string
		request ReviewDeduplicateRequest
	}{
		{"invalid cursor", ReviewDeduplicateRequest{AfterItemID: "invalid", ThroughItemID: last}},
		{"missing bound", ReviewDeduplicateRequest{AfterItemID: first}},
		{"reversed range", ReviewDeduplicateRequest{AfterItemID: last, ThroughItemID: first}},
		{"equal range", ReviewDeduplicateRequest{AfterItemID: last, ThroughItemID: last}},
		{"conflicting batches", ReviewDeduplicateRequest{Scope: ReviewBulkScope{ImportJobID: first, PegasusImportID: last}}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if _, err := normalizeReviewDeduplicateRequest(item.request); !errors.Is(err, ErrReviewBulkInvalidScope) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	normalized, err := normalizeReviewDeduplicateRequest(ReviewDeduplicateRequest{
		Scope: ReviewBulkScope{Q: " Test   Game "}, AfterItemID: first, ThroughItemID: last,
	})
	if err != nil || normalized.Scope.Q != "test game" {
		t.Fatalf("normalized = %#v, %v", normalized, err)
	}
}
