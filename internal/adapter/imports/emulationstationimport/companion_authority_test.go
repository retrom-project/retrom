package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	persistence "retrom/internal/repo/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestESCompanionsRetainExecutionStopCauses(t *testing.T) {
	for _, kind := range []string{"worker", "deadline", "context"} {
		t.Run(kind, func(t *testing.T) {
			fixture, unit, item := companionFixture(t)
			selection, err := fixture.service.companions().Find(fixture.context, unit, item.ID)
			if err != nil || len(selection.Files) != 2 {
				t.Fatalf("candidates=%#v error=%v", selection.Files, err)
			}
			ctx := fixture.context
			cause := ErrVersionConflict
			switch kind {
			case "worker":
				if _, err := fixture.database.ExecContext(
					ctx,
					`UPDATE jobs SET worker_id='replacement',version=version+1 WHERE id=?`,
					unit.JobID,
				); err != nil {
					t.Fatal(err)
				}
			case "deadline":
				*fixture.now = time.UnixMilli(unit.DeadlineAtMS)
				cause = ErrExpired
			case "context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				cause = context.Canceled
			}
			files, err := fixture.service.companions().Files(ctx, unit, item)
			if !errors.Is(err, cause) || len(files) != 0 {
				t.Fatalf("lost companion stop cause: files=%#v error=%v", files, err)
			}
		})
	}
}

func TestESCompanionCatalogWriteRejectsOwnerReplacedAfterCopy(t *testing.T) {
	fixture, unit, item := companionFixture(t)
	mutator := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{})
	calls := 0
	sources := companionCopyHook{importExecutorAdapter: importExecutorAdapter{service: fixture.service}, after: func() {
		calls++
		if _, err := mutator.ExecContext(
			fixture.context,
			`UPDATE jobs SET worker_id='replacement',version=version+1 WHERE id=?`,
			unit.JobID,
		); err != nil {
			t.Fatal(err)
		}
	}}
	service := application.NewCompanions(persistence.NewCompanions(fixture.database), sources, fixture.service.now)
	files, err := service.Files(fixture.context, unit, item)
	var count int
	if readErr := fixture.database.QueryRowContext(fixture.context, `SELECT count(*) FROM blobs`).Scan(
		&count,
	); readErr != nil {
		t.Fatal(readErr)
	}
	if !errors.Is(err, ErrVersionConflict) || len(files) != 0 || count != 0 {
		t.Fatalf("stale companion registration: calls=%d files=%#v catalog=%d error=%v", calls, files, count, err)
	}
}

func TestESCompanionsPreserveExecutionReadFailure(t *testing.T) {
	fixture, unit, item := companionFixture(t)
	cause := errors.New("companion execution read failed")
	hits := 0
	fixture.service.database = testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.HasPrefix(query, "SELECT job.id,plan.id,job.kind,") && len(args) == 1 && args[0].Value == unit.JobID {
				hits++
				return cause
			}
			return nil
		},
	})
	files, err := fixture.service.companions().Files(fixture.context, unit, item)
	if hits == 0 || !errors.Is(err, cause) || len(files) != 0 {
		t.Fatalf("lost storage cause: hits=%d files=%#v error=%v", hits, files, err)
	}
}

type companionCopyHook struct {
	importExecutorAdapter
	before, after func()
}

func (source companionCopyHook) CopyFile(
	ctx context.Context,
	unit emulationstationimportmodel.Execution,
	file emulationstationimportmodel.ExecutionFile,
) (emulationstationimportmodel.VerifiedBlob, error) {
	if source.before != nil {
		source.before()
	}
	blob, err := source.importExecutorAdapter.CopyFile(ctx, unit, file)
	if err == nil && source.after != nil {
		source.after()
	}
	return blob, err
}

func TestESCompanionCopyPropagatesExecutionObservationCause(t *testing.T) {
	fixture, unit, item := companionFixture(t)
	cause := errors.New("companion observation read failed")
	enabled := false
	hits := 0
	fixture.service.database = testsupport.OpenSQLFaultDatabase(
		t,
		fixture.database,
		testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if enabled && strings.HasPrefix(
				query,
				"SELECT job.id,plan.id,job.kind,",
			) && len(
				args,
			) == 1 && args[0].Value == unit.JobID {
				hits++
				return cause
			}
			return nil
		}},
	)
	sources := companionCopyHook{
		importExecutorAdapter: importExecutorAdapter{service: fixture.service},
		before:                func() { enabled = true },
	}
	service := application.NewCompanions(persistence.NewCompanions(fixture.service.database), sources, fixture.service.now)
	files, err := service.Files(fixture.context, unit, item)
	if !errors.Is(err, cause) || !errors.Is(err, application.ErrExecutionObservation) || hits != 1 || files != nil {
		t.Fatalf("files=%#v error=%v hits=%d", files, err, hits)
	}
}
