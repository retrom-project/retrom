package metadatascrape

import (
	"context"
	"strings"
	"testing"
	"time"

	"retrom/internal/testsupport/testpostgres"

	dbapi "retrom/internal/database"

	"retrom/internal/service/metadatascrape"
	"retrom/internal/testsupport"
)

var recoveryTime = time.Date(2028, 4, 5, 6, 7, 8, 0, time.UTC)

func recoveryNow() time.Time { return recoveryTime }

func recoveryDatabase(t *testing.T) dbapi.DB {
	t.Helper()
	database, err := testsupport.OpenDatabase(t.Context(), testpostgres.DSN(t), recoveryNow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = database.SQL.ExecContext(t.Context(), `INSERT INTO games(
 id,platform_instance_id,title,title_initial,description,developer,publisher,genre,
 metadata_source_kind,content_source_kind,source_manifest_json,
 source_manifest_digest,status,search_text,created_at_ms,updated_at_ms)
 VALUES('018fbe68-0000-7000-8000-000000000002',(SELECT id FROM platform_instances WHERE
catalog_template_key='gba/mgba'),
 'Metadata','M','','','','','ADMIN_EDIT','ADMIN_REPLACE','[]',?,'PUBLISHED','metadata',?,?)`,
		strings.Repeat("a", 64), recoveryTime.UnixMilli(), recoveryTime.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	err = NewScheduler(database.SQL).WithWrite(t.Context(), func(scope metadatascrape.ScheduleScope) error {
		return scope.Writes.Create(t.Context(), metadatascrape.SchedulePlan{
			Subject: metadatascrape.Subject{Kind: "GAME", ID: "018fbe68-0000-7000-8000-000000000002"},
			RunID:   "018fbe68-0000-7000-8000-000000000001", JobID: "job", Provider: "HASHEOUS",
			Dedupe: strings.Repeat("b", 64), PayloadJSON: `{}`, JobState: "QUEUED", RunState: "RUNNING", EventJSON: `{}`, Now: recoveryTime.UnixMilli(),
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return database.SQL
}

type recoveryProcess func(context.Context, metadatascrape.WorkerClaim, string) (int, string, error)

func (process recoveryProcess) Process(ctx context.Context, claim metadatascrape.WorkerClaim,
	payload string,
) (int, string, error) {
	return process(ctx, claim, payload)
}

func recoveryExec(t *testing.T, database dbapi.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}
