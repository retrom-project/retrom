//go:build integration

package launch

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	launchmodel "retrom/internal/model/launch"
	"retrom/internal/repo/dbexec"
	persistence "retrom/internal/repo/launch"
)

type launchQueryCounter struct {
	dbexec.Executor
	statements int
}

func (counter *launchQueryCounter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	counter.statements++
	return counter.Executor.QueryContext(ctx, query, args...)
}

func (counter *launchQueryCounter) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	counter.statements++
	return counter.Executor.QueryRowContext(ctx, query, args...)
}

func TestPreviewBundleReadsAuthorityAndFrozenMembersInOneStatement(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "query-bundle")
	counter := &launchQueryCounter{Executor: fixture.database}
	repository := persistence.NewSessionQueries(counter)
	ref := launchmodel.SessionRef{ID: preview.PreviewID, Preview: true}
	expectedSession := assertEmptyPreviewBundle(t, repository, counter, ref)
	mustRPGLaunchSQL(t, fixture.database, `INSERT INTO review_preview_files(preview_session_id,role,logical_name,blob_id,sort_order,created_at_ms)
 VALUES(?,'BIOS_BUNDLE','z.bin','rpg-project-a',0,0)`, preview.PreviewID)
	mustRPGLaunchSQL(t, fixture.database, `INSERT INTO review_preview_files(preview_session_id,role,logical_name,blob_id,sort_order,created_at_ms)
 VALUES(?,'BIOS_BUNDLE','a.bin','rpg-project-b',1,0)`, preview.PreviewID)
	assertPopulatedPreviewBundle(t, fixture, repository, counter, ref, expectedSession)
}

func assertEmptyPreviewBundle(t *testing.T, repository *persistence.SessionQueries, counter *launchQueryCounter, ref launchmodel.SessionRef) launchmodel.SessionRecord {
	t.Helper()
	empty, found, err := repository.Bundle(t.Context(), ref, "BIOS_BUNDLE")
	if err != nil || !found || empty.Files == nil || len(empty.Files) != 0 || empty.Session.State != "ACTIVE" || counter.statements != 1 {
		t.Fatalf("empty=%#v found=%t statements=%d error=%v", empty, found, counter.statements, err)
	}
	return empty.Session
}

func assertPopulatedPreviewBundle(t *testing.T, fixture reviewCheckpointFixture, repository *persistence.SessionQueries, counter *launchQueryCounter, ref launchmodel.SessionRef, expectedSession launchmodel.SessionRecord) {
	t.Helper()
	counter.statements = 0
	record, found, err := repository.Bundle(t.Context(), ref, "BIOS_BUNDLE")
	if err != nil || !found || len(record.Files) != 2 || record.Files[0].LogicalName != "z.bin" || record.Files[1].LogicalName != "a.bin" || counter.statements != 1 {
		t.Fatalf("record=%#v found=%t statements=%d error=%v", record, found, counter.statements, err)
	}
	if !reflect.DeepEqual(record.Session, expectedSession) || fixture.database.Stats().InUse != 0 {
		t.Fatalf("authority changed or rows retained: %#v", record.Session)
	}
	if missing, found, err := repository.Bundle(t.Context(), launchmodel.SessionRef{ID: "missing", Preview: true}, "BIOS_BUNDLE"); err != nil || found || len(missing.Files) != 0 {
		t.Fatalf("missing=%#v found=%t error=%v", missing, found, err)
	}
}

func TestPreviewProjectRepositoryKeepsExactAndUniqueFoldedPaths(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "query-project")
	repository := persistence.NewContentQueries(fixture.database)
	assertPreviewProjectExactAndFolded(t, repository, preview.PreviewID)
	mustRPGLaunchSQL(t, fixture.database, `INSERT INTO review_preview_files(preview_session_id,role,logical_name,blob_id,sort_order,created_at_ms)
 VALUES(?,'PROJECT_FILE','RPG_RT.LDB','rpg-project-b',99,0)`, preview.PreviewID)
	assertPreviewProjectAmbiguity(t, repository, preview.PreviewID)
	assertPreviewProjectCancellation(t, fixture, repository, preview.PreviewID)
}

func assertPreviewProjectExactAndFolded(t *testing.T, repository *persistence.ContentQueries, previewID string) {
	t.Helper()
	exact, found, err := repository.PreviewProject(t.Context(), previewID, "RPG_RT.ldb", false)
	if err != nil || !found || exact.Content.Format != "RPG_MAKER_PROJECT" || exact.Content.TargetID != "rpgmaker-2000" || exact.Content.DOSEntry != nil {
		t.Fatalf("exact=%#v found=%t error=%v", exact, found, err)
	}
	folded, found, err := repository.PreviewProject(t.Context(), previewID, "rpg_rt.LDB", true)
	if err != nil || !found || !reflect.DeepEqual(folded, exact) {
		t.Fatalf("folded=%#v found=%t error=%v", folded, found, err)
	}
	if _, found, err := repository.PreviewProject(t.Context(), previewID, "rpg_rt.LDB", false); err != nil || found {
		t.Fatalf("generic path folded: found=%t error=%v", found, err)
	}
}

func assertPreviewProjectAmbiguity(t *testing.T, repository *persistence.ContentQueries, previewID string) {
	t.Helper()
	if _, found, err := repository.PreviewProject(t.Context(), previewID, "rpg_rt.LdB", true); err != nil || found {
		t.Fatalf("ambiguous path accepted: found=%t error=%v", found, err)
	}
	if _, found, err := repository.PreviewProject(t.Context(), previewID, "RPG_RT.ldb", true); err != nil || !found {
		t.Fatalf("exact path lost: found=%t error=%v", found, err)
	}
}

func assertPreviewProjectCancellation(t *testing.T, fixture reviewCheckpointFixture, repository *persistence.ContentQueries, previewID string) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, found, err := repository.PreviewProject(ctx, previewID, "RPG_RT.ldb", false); !errors.Is(err, context.Canceled) || found || result.Content != (ContentView{}) {
		t.Fatalf("cancelled content=%#v found=%t error=%v", result, found, err)
	}
	if fixture.database.Stats().InUse != 0 {
		t.Fatal("project query retained a database connection")
	}
}
