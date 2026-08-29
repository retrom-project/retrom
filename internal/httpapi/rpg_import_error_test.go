package httpapi

import (
	"net/http"
	"testing"

	"retrom/internal/rpgmaker/detector"
	"retrom/internal/rpgmaker/fileset"
)

func TestImportCreationErrorPreservesRPGMakerTypedFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "ambiguous root", err: &fileset.ProjectError{Code: fileset.CodeRootAmbiguous}, wantStatus: http.StatusConflict, wantCode: "RPG_PROJECT_ROOT_AMBIGUOUS"},
		{name: "missing project", err: &fileset.ProjectError{Code: fileset.CodeProjectNotFound}, wantStatus: http.StatusBadRequest, wantCode: "RPG_PROJECT_NOT_FOUND"},
		{name: "selected core mismatch", err: &detector.Error{Code: detector.CodeSelectedCoreMismatch}, wantStatus: http.StatusUnprocessableEntity, wantCode: "RPG_SELECTED_CORE_MISMATCH"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, code, _ := importCreationError(test.err)
			if status != test.wantStatus || code != test.wantCode {
				t.Fatalf("importCreationError() = (%d, %q), want (%d, %q)", status, code, test.wantStatus, test.wantCode)
			}
		})
	}
}
