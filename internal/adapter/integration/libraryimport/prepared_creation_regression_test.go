//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"os"
	"strings"
	"testing"

	"retrom/internal/testkit/testsupport"
)

func TestPreparedCreationBuildsRPGArtifactBeforeFirstWrite(t *testing.T) {
	t.Parallel()
	service, request, digest := preparedRPGFixture(t)
	artifactPath := service.blobs.Path(digest)
	if _, err := os.Stat(artifactPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture already materialized RPG output: %v", err)
	}
	cause := errors.New("stop at prepared creation write")
	artifactReady := false
	writes := 0
	service.database = testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			verb := strings.ToUpper(strings.TrimSpace(query))
			if !strings.HasPrefix(verb, "INSERT ") && !strings.HasPrefix(verb, "UPDATE ") && !strings.HasPrefix(verb, "DELETE ") {
				return nil
			}
			writes++
			_, err := os.Stat(artifactPath)
			artifactReady = err == nil
			return cause
		},
	})
	result, err := service.Create(t.Context(), request)
	if !errors.Is(err, cause) || result != (Created{}) || writes != 1 {
		t.Fatalf("result=%+v error=%v writes=%d", result, err, writes)
	}
	if !artifactReady {
		t.Fatal("RPG MKXPZ builder had not run when creation started writing")
	}
}
