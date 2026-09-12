package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	application "retrom/internal/service/libraryimport"
)

func TestImportAdmissionErrorSeparatesInputFromStorage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid input", fmt.Errorf("admit: %w", application.ErrInvalid), http.StatusConflict, "IMPORT_INPUT_INVALID"},
		{"lost fence", fmt.Errorf("admit: %w", application.ErrVersionConflict), http.StatusConflict, "IMPORT_INPUT_INVALID"},
		{"storage", errors.New("database unavailable"), http.StatusInternalServerError, "INTERNAL_ERROR"},
		{"scraper missing", application.ErrMetadataScraperNotConfigured, http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			status, code, _ := importCreationError(test.err)
			if status != test.status || code != test.code {
				t.Fatalf("status=%d code=%s", status, code)
			}
		})
	}
}
