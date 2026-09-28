package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	libraryservice "retrom/internal/service/libraryimport"
)

func TestImportAdmissionErrorSeparatesInputFromStorage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid input", fmt.Errorf("admit: %w", libraryservice.ErrInvalid), http.StatusConflict, "IMPORT_INPUT_INVALID"},
		{"lost fence", fmt.Errorf("admit: %w", libraryservice.ErrVersionConflict), http.StatusConflict, "IMPORT_INPUT_INVALID"},
		{"storage", errors.New("database unavailable"), http.StatusInternalServerError, "INTERNAL_ERROR"},
		{"scraper missing", libraryservice.ErrMetadataScraperNotConfigured, http.StatusInternalServerError, "INTERNAL_ERROR"},
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
