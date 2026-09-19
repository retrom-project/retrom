package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

type startFaultRepository struct {
	*Starter
	phase string
}

func (repository startFaultRepository) WithStart(ctx context.Context, work func(emulationstationimportmodel.StartScope) error) error {
	return repository.Starter.WithStart(ctx, func(scope emulationstationimportmodel.StartScope) error {
		records, ok := scope.Write.(startRecords)
		if !ok {
			return errors.New("unexpected start repository records")
		}
		scope.Write = startFaultWriter{StartWriter: scope.Write, executor: records.executor, phase: repository.phase}
		return work(scope)
	})
}

type startFaultWriter struct {
	emulationstationimportmodel.StartWriter

	executor dbexec.Executor
	phase    string
}

func (writer startFaultWriter) Queue(ctx context.Context, plan emulationstationimportmodel.StartPlan) error {
	if err := writer.StartWriter.Queue(ctx, plan); err != nil {
		return err
	}
	statement := `ALTER TABLE emulationstation_imports RENAME COLUMN root_label_snapshot TO broken_root_label`
	if writer.phase == "commit" {
		statement = `PRAGMA defer_foreign_keys=ON;
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES('absent-parent-job',1,'{}','` + planDigest + `',10)`
	}
	if _, err := writer.executor.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("inject start failure: %w", err)
	}
	return nil
}

func TestStartResponseAndCommitFailuresRollbackEveryWrite(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"response", "commit"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			db := startDatabase(t)
			before := planRows(t, db)
			service := application.NewStarter(startFaultRepository{Starter: NewStarter(db), phase: phase}, verifiedStartSource{database: db}, func() time.Time { return time.UnixMilli(10) })
			result, queued, err := service.Start(t.Context(), "import-0", 2, mappingActor)
			want := "read queued EmulationStation plan"
			if phase == "commit" {
				want = "FOREIGN KEY constraint failed"
			}
			if result.ID != "" || queued || err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s result=%#v queued=%v error=%v", phase, result, queued, err)
			}
			if !reflect.DeepEqual(planRows(t, db), before) {
				t.Fatal("failed response or commit left jobs, input, events, audit, item or release writes")
			}
		})
	}
}

type changingStartSource struct {
	verifiedStartSource
	change func() error
}

func (source changingStartSource) VerifyGamelists(ctx context.Context, root, path string, values []emulationstationimportmodel.GamelistEvidence) error {
	if err := source.verifiedStartSource.VerifyGamelists(ctx, root, path, values); err != nil {
		return err
	}
	return source.change()
}

func TestStartRechecksDatabaseDriftAfterSourceVerification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, query string
		want        error
	}{
		{"tag", `UPDATE tags SET status='DELETED',deleted_at_ms=10`, emulationstationimportmodel.ErrMapping},
		{"instance", `UPDATE platform_instances SET version=version+1`, emulationstationimportmodel.ErrMappingTargetChanged},
		{"root", `UPDATE emulationstation_imports SET root_config_digest='` + strings.Repeat("b", 64) + `'`, emulationstationimportmodel.ErrSourceChanged},
		{"source", `UPDATE emulationstation_imports SET source_snapshot_digest='` + strings.Repeat("b", 64) + `'`, emulationstationimportmodel.ErrSourceChanged},
		{"year", `UPDATE emulationstation_imports SET release_year_max=release_year_max+1`, emulationstationimportmodel.ErrSourceChanged},
		{"gamelist", `UPDATE emulationstation_import_gamelists SET size_bytes=size_bytes+1`, emulationstationimportmodel.ErrSourceChanged},
		{"expiry", `UPDATE emulationstation_imports SET expires_at_ms=10`, emulationstationimportmodel.ErrExpired},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			db := startDatabase(t)
			var before map[string]string
			source := changingStartSource{verifiedStartSource: verifiedStartSource{database: db}, change: func() error {
				if _, err := db.ExecContext(t.Context(), test.query); err != nil {
					return fmt.Errorf("change frozen start input: %w", err)
				}
				before = planRows(t, db)
				return nil
			}}
			service := application.NewStarter(NewStarter(db), source, func() time.Time { return time.UnixMilli(10) })
			result, queued, err := service.Start(t.Context(), "import-0", 2, mappingActor)
			if result.ID != "" || queued || !errors.Is(err, test.want) {
				t.Fatalf("%s drift result=%#v queued=%v error=%v", test.name, result, queued, err)
			}
			if !reflect.DeepEqual(planRows(t, db), before) {
				t.Fatal("drifted start wrote plan, tags, job, input, event or release")
			}
		})
	}
}

func TestStartEvidenceQueryUsesBoundedSentinel(t *testing.T) {
	t.Parallel()
	db := startDatabase(t)
	if _, err := db.ExecContext(t.Context(), `WITH RECURSIVE sequence(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM sequence WHERE n<1000)
INSERT INTO emulationstation_import_gamelists(import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,created_at_ms)
SELECT 'import-0',printf('extra-%04d/gamelist.xml',n),1,?,?, 'VALID',1 FROM sequence`, planDigest, planDigest); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewStarter(db).Inspect(t.Context(), "import-0")
	if err != nil || len(snapshot.Gamelists) != emulationstationimportmodel.MaxSnapshotGamelists+1 {
		t.Fatalf("bounded gamelists=%d error=%v", len(snapshot.Gamelists), err)
	}
}
