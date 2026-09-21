//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/blobstore"
	uploadpersistence "retrom/internal/persistence/uploads"
	"retrom/internal/service/uploads"
	"retrom/internal/testsupport"
)

func TestPreparedArcadeCatalogFailuresPrecedeCreationWrites(t *testing.T) {
	for _, operation := range []string{"classification", "relations", "default BIOS", "ROM entries", "disk entries"} {
		t.Run(operation, func(t *testing.T) {
			importer, request := preparedArcadeErrorFixture(t)
			cause := errors.New("arcade preparation catalog unavailable")
			reads, writes := 0, 0
			importer.database = testsupport.OpenSQLFaultDatabase(t, importer.database, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
					if matchesPreparationCatalogRead(operation, query) {
						reads++
						return cause
					}
					return nil
				},
				BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
					query = strings.TrimSpace(query)
					if strings.HasPrefix(query, "INSERT") || strings.HasPrefix(query, "UPDATE") || strings.HasPrefix(query, "DELETE") {
						writes++
					}
					return nil
				},
			})
			result, err := importer.Create(t.Context(), request)
			if !errors.Is(err, cause) || result != (Created{}) || reads == 0 || writes != 0 {
				t.Fatalf("operation=%s reads=%d writes=%d result=%+v err=%v", operation, reads, writes, result, err)
			}
			var imports int
			if err := importer.database.QueryRowContext(t.Context(), `SELECT count(*) FROM import_jobs`).Scan(&imports); err != nil {
				t.Fatal(err)
			}
			if imports != 0 {
				t.Fatalf("failed catalog committed %d imports", imports)
			}
		})
	}
}

func matchesPreparationCatalogRead(operation, query string) bool {
	switch operation {
	case "classification":
		return strings.Contains(query, "SELECT classification FROM dat_machines")
	case "relations":
		return strings.Contains(query, "COALESCE(cloneof")
	case "default BIOS":
		return strings.Contains(query, "FROM dat_bios_sets")
	case "ROM entries":
		return strings.Contains(query, "FROM dat_rom_entries")
	default:
		return strings.Contains(query, "FROM dat_disk_entries")
	}
}

func preparedArcadeErrorFixture(t *testing.T) (*Service, CreateRequest) {
	t.Helper()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	insertArcadeParentCatalog(t, database.SQL)
	uploader := uploads.New(uploadpersistence.New(database.SQL), blobs, dataDir, time.Now)
	upload := uploadCompleteFile(t, t.Context(), database.SQL, uploader, "a.zip", arcadeZIP(t, "a.bin", []byte("child")))
	importer := New(database.SQL, time.Now).WithBlobStore(blobs)
	return importer, CreateRequest{UploadID: upload.uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "arcade/fbneo"), MetadataProvider: "NONE"}
}
