//go:build integration

package gamecontent

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/dbexec"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	"retrom/internal/payloadrelease"
	dependencypersistence "retrom/internal/persistence/dependencies"
	uploadpersistence "retrom/internal/persistence/uploads"
	dependencyservice "retrom/internal/service/dependencies"
	"retrom/internal/service/gamecontent"
	"retrom/internal/service/uploads"
	"retrom/internal/testsupport"
)

type retirementFixture struct {
	db                                  *sql.DB
	releases                            *payloadrelease.Service
	gameID, variantID, saveID, launchID string
}

func contentRetirementFixture(t *testing.T) retirementFixture {
	t.Helper()
	dir := t.TempDir()
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(dir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../.."))
	catalog, err := dependencies.Load(filepath.Join(root, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencyservice.New(catalog, dependencypersistence.New(database.SQL)).Bootstrap(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	uploadService := uploads.New(uploadpersistence.New(database.SQL), blobs, dir, time.Now)
	t.Cleanup(uploadService.Close)
	uploadID := completeUpload(t, t.Context(), database.SQL, uploadService, "retirement.gba", []byte("original retirement content"))
	importer := libraryimport.New(database.SQL, time.Now)
	created, err := importer.Create(t.Context(), libraryimport.CreateRequest{UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba"), MetadataProvider: "NONE"})
	if err != nil {
		t.Fatal(err)
	}
	published, err := importer.Approve(t.Context(), importItemID(t, t.Context(), database.SQL, created.ImportJobID), 1)
	if err != nil {
		t.Fatal(err)
	}
	saveID, launchID, _ := seedReplacementSave(t, t.Context(), database.SQL, blobs, published.GameID)
	releases, err := payloadrelease.New(database.SQL, blobs, time.Now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releases.Close)
	var variantID string
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT id FROM game_variants WHERE game_id=?`, published.GameID).Scan(&variantID); err != nil {
		t.Fatal(err)
	}
	return retirementFixture{database.SQL, releases, published.GameID, variantID, saveID, launchID}
}

type retirementCountFailure struct {
	driver.Result
	cause error
}

func (result retirementCountFailure) RowsAffected() (int64, error) { return 0, result.cause }

func TestContentRetirementRejectsUnconfirmedMutation(t *testing.T) {
	t.Parallel()
	for _, point := range []struct {
		name, prefix, fragment string
		zero                   bool
	}{
		{"launch count cause", "UPDATE launch_sessions SET", "finished_at_ms=", false},
		{"save zero", "DELETE FROM save_states", "WHERE", true},
		{"file zero", "DELETE FROM launch_content_files", "WHERE", true},
	} {
		t.Run(point.name, func(t *testing.T) {
			t.Parallel()
			fixture := contentRetirementFixture(t)
			cause := errors.New("content retirement count failure")
			if point.zero {
				cause = nil
			}
			var hits atomic.Int64
			fault := testsupport.OpenSQLFaultDatabase(t, fixture.db, testsupport.SQLFaultHooks{
				AfterExec: func(_ context.Context, query string, _ []driver.NamedValue, result driver.Result) (driver.Result, error) {
					query = strings.Join(strings.Fields(query), " ")
					if strings.HasPrefix(query, point.prefix) && strings.Contains(query, point.fragment) {
						hits.Add(1)
						return retirementCountFailure{Result: result, cause: cause}, nil
					}
					return result, nil
				},
			})
			tx, err := fault.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = gamecontent.RetireInScope(t.Context(), BindRetirement(tx), fixture.gameID, fixture.variantID, time.Now().UnixMilli())
			if err == nil {
				err = tx.Commit()
			} else {
				dbexec.Rollback(tx)
			}
			if err == nil || cause != nil && !errors.Is(err, cause) || hits.Load() != 1 {
				t.Fatalf("unconfirmed retirement committed: hits=%d err=%v", hits.Load(), err)
			}
			fixture.assertUnchanged(t)
		})
	}
}

func (fixture retirementFixture) assertUnchanged(t *testing.T) {
	t.Helper()
	var saves, files int
	var state string
	err := fixture.db.QueryRowContext(t.Context(), `SELECT
 (SELECT count(*) FROM save_states WHERE id=?),
 (SELECT count(*) FROM launch_content_files WHERE launch_session_id=?),
 (SELECT state FROM launch_sessions WHERE id=?)`, fixture.saveID, fixture.launchID, fixture.launchID).Scan(&saves, &files, &state)
	if err != nil || saves != 1 || files != 1 || state != "CREATED" {
		t.Fatalf("retirement partially committed: saves=%d files=%d state=%s err=%v", saves, files, state, err)
	}
}
