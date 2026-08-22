package platforminstance_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/platforminstance"
	"retrom/internal/store"
)

const testUserID = "01980000-0000-7000-8000-000000009901"

func newService(t *testing.T) (*platforminstance.Service, *sql.DB) {
	t.Helper()
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time {
		return time.UnixMilli(1_786_000_000_000)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.SQL.Exec(`
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('test-profile','Test',0);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'test-profile','directory-admin','Directory Admin','ADMIN','ENABLED',0,0);
`, testUserID); err != nil {
		t.Fatal(err)
	}
	dependencySet, err := dependencies.Load(
		filepath.Join("..", "..", "data"), []string{"4.2.3", "4.3.0-pre"}, "4.2.3",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(t.Context(), database.SQL, time.UnixMilli(1_786_000_000_000)); err != nil {
		t.Fatal(err)
	}
	service := platforminstance.New(database.SQL, func() time.Time {
		return time.UnixMilli(1_786_000_000_000)
	})
	if err := service.ValidateCatalog(t.Context()); err != nil {
		t.Fatal(err)
	}
	return service, database.SQL
}

func actor() platforminstance.AuditActor {
	return platforminstance.AuditActor{Kind: "USER", UserID: testUserID, Label: nil, RequestID: "test-request"}
}

func TestApplyCreatesCatalogAtomicallyAndReplays(t *testing.T) {
	t.Parallel()
	service, database := newService(t)
	before, err := service.Recommendations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if before.Summary.TotalCount != 27 || before.Summary.MissingCount != 27 {
		t.Fatalf("before summary = %#v", before.Summary)
	}
	response, err := service.Apply(t.Context(), actor(), testUserID, "11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	var result platforminstance.ApplyResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Summary.CreatedCount != 27 || result.Summary.CoveredCount != 0 ||
		result.Summary.RemainingMissingCount != 0 || len(result.Created) != 27 {
		t.Fatalf("apply result = %#v", result.Summary)
	}
	replay, err := service.Apply(t.Context(), actor(), testUserID, "11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || string(replay.Body) != string(response.Body) {
		t.Fatal("idempotent replay did not return the stored response")
	}
	var directories, catalogKeys, audits int
	if err := database.QueryRow(`
SELECT (SELECT count(*) FROM platform_instances WHERE deleted_at_ms IS NULL),
       (SELECT count(*) FROM platform_instances WHERE catalog_template_key IS NOT NULL),
       (SELECT count(*) FROM audit_events WHERE action='PLATFORM_INSTANCE_RECOMMENDED_CREATED')
`).Scan(&directories, &catalogKeys, &audits); err != nil {
		t.Fatal(err)
	}
	if directories != 27 || catalogKeys != 27 || audits != 27 {
		t.Fatalf("created counts = directories:%d keys:%d audits:%d", directories, catalogKeys, audits)
	}
}

func TestCoveragePreservesEquivalentCustomizedDisabledAndDeletedChoices(t *testing.T) {
	t.Parallel()
	service, database := newService(t)
	manual, err := service.Create(t.Context(), actor(), platforminstance.CreateInput{
		PlatformID: "gba", DefaultCoreID: "mgba", Name: "我的 GBA", SortOrder: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(t.Context(), actor(), testUserID, "22222222-2222-4222-8222-222222222222"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
UPDATE platform_instances SET name='自定义 NES',version=version+1
WHERE catalog_template_key='nes/fceumm';
UPDATE platform_instances SET enabled=0,version=version+1
WHERE catalog_template_key='snes/snes9x';
UPDATE platform_instances SET deleted_at_ms=updated_at_ms,enabled=0,version=version+1
WHERE catalog_template_key='arcade/fbneo';
`); err != nil {
		t.Fatal(err)
	}
	recommendations, err := service.Recommendations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	states := make(map[string]platforminstance.Recommendation, len(recommendations.Items))
	for _, item := range recommendations.Items {
		states[item.TemplateKey] = item
	}
	if states["gba/mgba"].State != platforminstance.StateCoveredByEquivalent ||
		states["gba/mgba"].PlatformInstanceID == nil || *states["gba/mgba"].PlatformInstanceID != manual.ID {
		t.Fatalf("equivalent state = %#v", states["gba/mgba"])
	}
	if states["nes/fceumm"].State != platforminstance.StateCustomized {
		t.Fatalf("custom state = %#v", states["nes/fceumm"])
	}
	if states["snes/snes9x"].State != platforminstance.StateSuppressed ||
		states["arcade/fbneo"].State != platforminstance.StateSuppressed {
		t.Fatalf("suppressed states = snes:%#v arcade:%#v", states["snes/snes9x"], states["arcade/fbneo"])
	}
	second, err := service.Apply(t.Context(), actor(), testUserID, "33333333-3333-4333-8333-333333333333")
	if err != nil {
		t.Fatal(err)
	}
	var result platforminstance.ApplyResult
	if err := json.Unmarshal(second.Body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Summary.CreatedCount != 0 || result.Summary.SuppressedCount != 2 {
		t.Fatalf("second apply = %#v", result.Summary)
	}
	var manualKey sql.NullString
	if err := database.QueryRow(`SELECT catalog_template_key FROM platform_instances WHERE id=?`, manual.ID).Scan(&manualKey); err != nil {
		t.Fatal(err)
	}
	if manualKey.Valid {
		t.Fatalf("manual catalog key = %q", manualKey.String)
	}
}

func TestApplyRollsBackWhenAuditFails(t *testing.T) {
	t.Parallel()
	service, database := newService(t)
	badActor := actor()
	badActor.Kind = "INVALID"
	if _, err := service.Apply(
		t.Context(), badActor, testUserID, "44444444-4444-4444-8444-444444444444",
	); err == nil {
		t.Fatal("Apply succeeded with an invalid audit actor")
	}
	var directories, records int
	if err := database.QueryRow(`
SELECT (SELECT count(*) FROM platform_instances),(SELECT count(*) FROM idempotency_records)
`).Scan(&directories, &records); err != nil {
		t.Fatal(err)
	}
	if directories != 0 || records != 0 {
		t.Fatalf("partial result remained: directories=%d idempotency=%d", directories, records)
	}
}

func TestConcurrentApplyCreatesOneDirectoryPerTemplate(t *testing.T) {
	service, database := newService(t)
	keys := []string{"55555555-5555-4555-8555-555555555555", "66666666-6666-4666-8666-666666666666"}
	errorsByRequest := make([]error, len(keys))
	var wait sync.WaitGroup
	for index, key := range keys {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, errorsByRequest[index] = service.Apply(context.Background(), actor(), testUserID, key)
		}()
	}
	wait.Wait()
	for _, err := range errorsByRequest {
		if err != nil {
			t.Fatal(err)
		}
	}
	var directories, distinctKeys int
	if err := database.QueryRow(`
SELECT count(*),count(DISTINCT catalog_template_key) FROM platform_instances
`).Scan(&directories, &distinctKeys); err != nil {
		t.Fatal(err)
	}
	if directories != 27 || distinctKeys != 27 {
		t.Fatalf("concurrent counts = %d/%d", directories, distinctKeys)
	}
}

func TestValidateCatalogFailsClosedOnDisabledRelationship(t *testing.T) {
	t.Parallel()
	service, database := newService(t)
	if _, err := database.Exec(`UPDATE platform_cores SET enabled=0 WHERE platform_id='nes' AND core_id='fceumm'`); err != nil {
		t.Fatal(err)
	}
	if err := service.ValidateCatalog(t.Context()); !errors.Is(err, platforminstance.ErrCatalogInvalid) {
		t.Fatalf("ValidateCatalog error = %v", err)
	}
}
