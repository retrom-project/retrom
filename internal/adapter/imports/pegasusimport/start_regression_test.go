package pegasusimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"retrom/internal/adapter/files/serversource"
	"retrom/internal/adapter/runtime/dependencies"
	tagrepository "retrom/internal/repo/tagging"
	"retrom/internal/service/tagging"
	"retrom/internal/testkit/testsupport"

	"github.com/google/uuid"
)

const (
	startActor      = "019b0000-0000-7000-8000-000000000011"
	startCollection = "019b0000-0000-7000-8000-000000000012"
)

func startFixture(t *testing.T) (*Service, Summary) {
	t.Helper()
	db := newPegasusRetryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('start-profile','Start',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('019b0000-0000-7000-8000-000000000011','start-profile','start-admin','Start','ADMIN','ENABLED',1,1);
UPDATE pegasus_imports SET state='AWAITING_MAPPING',completed_at_ms=NULL,import_job_id=NULL,
failed_item_count=0,mapped_collection_count=0,created_by_user_id='019b0000-0000-7000-8000-000000000011',
source_snapshot_digest='cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc';
INSERT INTO pegasus_import_collections(id,import_id,metadata_relative_path,segment_ordinal,name,game_count,created_at_ms,updated_at_ms)
VALUES('019b0000-0000-7000-8000-000000000012','import','metadata.pegasus.txt',0,'Collection',1,1,1);
UPDATE pegasus_import_items SET execution_state='PENDING',completed_at_ms=NULL,error_code=NULL,
error_details_json=NULL,retryable=0,collection_id='019b0000-0000-7000-8000-000000000012';
`); err != nil {
		t.Fatal(err)
	}
	root := startMetadataSource(t, db)
	deps, err := dependencies.Load(filepath.Join("..", "..", "..", "..", "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := testsupport.SeedRuntimeProviders(t.Context(), db, deps.RuntimeCatalog); err != nil {
		t.Fatal(err)
	}
	if err := testsupport.SeedPlatformInstances(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	var target string
	if err := db.QueryRowContext(t.Context(), `SELECT id FROM platform_instances WHERE platform_id='gba' AND enabled=1 ORDER BY sort_order,id LIMIT 1`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	service := &Service{database: db, roots: map[string]Root{"games": root}, now: func() time.Time { return time.UnixMilli(10) }, tags: tagging.New(tagrepository.New(db), time.Now)}
	mapped, err := service.UpdateMappings(t.Context(), "import", 4, []Mapping{{CollectionID: startCollection, Action: "IMPORT", PlatformInstanceID: target, TagIDs: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	return service, mapped
}

func startMetadataSource(t *testing.T, db *sql.DB) Root {
	t.Helper()
	root := Root{ID: "games", Label: "Games", path: t.TempDir(), digest: strings.Repeat("a", 64)}
	if err := os.Mkdir(filepath.Join(root.path, "Roms"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root.path, "Roms", "metadata.pegasus.txt")
	metadata := []byte("collection: Collection\n\ngame: Game\nfile: game.gba\n")
	if err := os.WriteFile(path, metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(metadata)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO pegasus_import_metadata_files(import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,created_at_ms)
VALUES('import','metadata.pegasus.txt',?,?,?,'VALID',1)`, len(metadata), hex.EncodeToString(digest[:]), serversource.FactsDigest(info)); err != nil {
		t.Fatal(err)
	}
	return root
}

// Sequential: restore uuid's reader before parallel tests are scheduled.
func TestStartRejectsEntropyFailureWithoutCreatingExecution(t *testing.T) {
	service, before := startFixture(t)
	uuid.SetRand(unavailableCreationEntropy{})
	result, err := func() (Summary, error) {
		defer uuid.SetRand(nil)
		return service.StartImport(t.Context(), before.ID, before.Version)
	}()
	if !errors.Is(err, errCreationEntropy) || result.ID != "" {
		t.Errorf("start ignored entropy failure: %#v %v", result, err)
	}
	current, err := service.Get(t.Context(), before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "AWAITING_MAPPING" || current.Version != before.Version || current.ImportJobID != nil {
		t.Errorf("failed start persisted execution: %#v", current)
	}
}

func TestStartRejectsChangedRootConfigurationBeforeQueueing(t *testing.T) {
	t.Parallel()
	service, before := startFixture(t)
	root := service.roots["games"]
	root.digest = strings.Repeat("f", 64)
	service.roots["games"] = root
	result, err := service.StartImport(t.Context(), before.ID, before.Version)
	if !errors.Is(err, ErrSourceChanged) || result.ID != "" {
		t.Fatalf("changed root queued: %#v %v", result, err)
	}
}

func TestStartRechecksExpiryAtQueueTime(t *testing.T) {
	t.Parallel()
	service, before := startFixture(t)
	if _, err := service.database.ExecContext(t.Context(), `UPDATE pegasus_imports SET expires_at_ms=100 WHERE id='import'`); err != nil {
		t.Fatal(err)
	}
	calls := 0
	service.now = func() time.Time {
		calls++
		if calls == 1 {
			return time.UnixMilli(10)
		}
		return time.UnixMilli(100)
	}
	result, err := service.StartImport(t.Context(), before.ID, before.Version)
	if !errors.Is(err, ErrExpired) || result.ID != "" {
		t.Fatalf("expired plan queued: %#v %v", result, err)
	}
}

func TestStartHonorsSourceReaderBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, before := startFixture(t)
		budget := make(chan struct{}, 2)
		budget <- struct{}{}
		budget <- struct{}{}
		service.sourceReader = func(ctx context.Context) (func(), error) {
			select {
			case budget <- struct{}{}:
				return func() { <-budget }, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := service.StartImport(ctx, before.ID, before.Version); done <- err }()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("start bypassed source reader budget: %v", err)
		default:
		}
		cancel()
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting verification lost cancellation: %v", err)
		}
	})
}
