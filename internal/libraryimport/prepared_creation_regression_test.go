//go:build integration

package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbapi "retrom/internal/database"

	"retrom/internal/testsupport"
)

func TestPreparedCreationBuildsRPGArtifactBeforeFirstWrite(t *testing.T) {
	t.Parallel()
	service, request, digest := preparedRPGFixture(t)
	var source string
	if err := dbapi.QueryRowContext(t.Context(), service.database, `SELECT final_file_record FROM upload_files WHERE upload_session_id=? LIMIT 1`, request.UploadID).Scan(&source); err != nil {
		t.Fatal(err)
	}
	artifactRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(service.blobs.Path(source)))))
	if preparedDigestExists(t, artifactRoot, digest) {
		t.Fatal("fixture already materialized RPG output")
	}
	cause := errors.New("stop at prepared creation write")
	artifactReady := false
	writes := 0
	service.database = testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			verb := strings.ToUpper(strings.TrimSpace(query))
			if !strings.HasPrefix(verb, "INSERT ") && !strings.HasPrefix(verb, "UPDATE ") &&
				!strings.HasPrefix(verb, "DELETE ") {
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
	found := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if fmt.Sprintf("%x", hash.Sum(nil)) == digest {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}
