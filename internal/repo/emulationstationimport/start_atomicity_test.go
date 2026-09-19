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
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

func TestStartResponseAndCommitFailuresRollbackEveryWrite(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"response", "commit"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			db := startDatabase(t)
			before := planRows(t, db)
			starter := NewStarter(db)
			source := verifiedStartSource{database: db}
			service := emulationstationimportservice.NewStarter(starter, source, func() time.Time { return time.UnixMilli(10) })

			if phase == "response" {
				if _, err := db.ExecContext(t.Context(),
					`ALTER TABLE emulationstation_imports RENAME COLUMN root_label_snapshot TO broken_root_label`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := db.ExecContext(t.Context(),
					`CREATE TRIGGER start_fault AFTER INSERT ON jobs BEGIN
SELECT RAISE(ABORT, 'injected commit failure'); END`); err != nil {
					t.Fatal(err)
				}
			}

			result, queued, err := service.Start(t.Context(), "import-0", 2, mappingActor)
			if result.ID != "" || queued || err == nil {
				t.Fatalf("%s result=%#v queued=%v error=%v", phase, result, queued, err)
			}
			if phase == "response" {
				if _, err := db.ExecContext(t.Context(),
					`ALTER TABLE emulationstation_imports RENAME COLUMN broken_root_label TO root_label_snapshot`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := db.ExecContext(t.Context(), `DROP TRIGGER start_fault`); err != nil {
					t.Fatal(err)
				}
			}
			after := planRows(t, db)
			if !reflect.DeepEqual(after, before) {
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
			service := emulationstationimportservice.NewStarter(NewStarter(db), source, func() time.Time { return time.UnixMilli(10) })
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
