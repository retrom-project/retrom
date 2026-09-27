//go:build integration

package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/testsupport"
)

func TestPreparedCreationBuildsRPGArtifactBeforeFirstWrite(t *testing.T) {
	t.Parallel()
	service, request, digest := preparedRPGFixture(t)
	artifactRoot := filepath.Dir(filepath.Dir(service.blobs.Path("018fbe68-0000-7000-8000-000000000001")))
	if preparedDigestExists(t, artifactRoot, digest) {
		t.Fatal("fixture already materialized RPG output")
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
			artifactReady = preparedDigestExists(t, artifactRoot, digest)
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

func preparedDigestExists(t *testing.T, root, digest string) bool {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "*", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(contents)) == digest {
			return true
		}
	}
	return false
}
