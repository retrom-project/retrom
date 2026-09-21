package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/testsupport"
)

func TestMaterializationSQLAndAffectedFailuresRollback(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"file", "asset", "warning", "phase"} {
		for _, affected := range []bool{false, true} {
			t.Run(operation+map[bool]string{false: "/SQL", true: "/count"}[affected], func(t *testing.T) {
				t.Parallel()
				db, unit, source, blob := materialDatabase(t)
				statement, target := "UPDATE emulationstation_import_item_files SET", source.Key.ItemID
				switch operation {
				case "asset":
					statement = "UPDATE emulationstation_import_item_assets SET"
					source = materialAsset(source)
				case "warning":
					statement = "UPDATE emulationstation_import_items SET warnings_json"
					source = materialAsset(source)
				case "phase":
					statement = "UPDATE emulationstation_imports SET"
					target = unit.ImportID
				}
				before := planRows(t, db)
				var hits atomic.Int64
				faultDB := testsupport.OpenSQLFaultDatabase(t, db, scanFaultHooks(statement, target, affected, &hits))
				err := runMaterialOperation(t.Context(), materialService(faultDB), operation, unit, source, blob)
				if !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
					t.Fatalf("cause=%v hits=%d", err, hits.Load())
				}
				if !reflect.DeepEqual(before, planRows(t, db)) {
					t.Fatal("failed material operation retained writes")
				}
			})
		}
	}
}

func runMaterialOperation(
	ctx context.Context,
	service *application.Materialization,
	operation string,
	unit application.Execution,
	source application.MaterialSource,
	blob application.VerifiedBlob,
) error {
	switch operation {
	case "warning":
		return service.Warning(ctx, unit, source, "EMULATIONSTATION_IMAGE_INVALID")
	case "phase":
		return service.SetPhase(ctx, unit, "PREPARING_REVIEWS")
	default:
		_, err := service.Copy(ctx, unit, source, blob)
		return err
	}
}

type materialLateFailure struct {
	*Materialization
	commit bool
}

func (repository materialLateFailure) WithMaterialization(
	ctx context.Context,
	run func(application.MaterialScope) error,
) error {
	return repository.Materialization.WithMaterialization(ctx, func(scope application.MaterialScope) error {
		if err := run(scope); err != nil {
			return err
		}
		if !repository.commit {
			return errLeaseStorage
		}
		records, ok := scope.Write.(materialRecords)
		if !ok {
			return errors.New("unexpected material writer")
		}
		if _, err := records.executor.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
			return err
		}
		_, err := records.executor.ExecContext(
			ctx,
			`INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms) VALUES('missing-material-parent',1,'{}','`+planDigest+`',12)`,
		)
		return err
	})
}

func TestMaterializationCommitFailureRollsBackWritesAndResponse(t *testing.T) {
	t.Parallel()
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{false: "callback", true: "commit"}[commit], func(t *testing.T) {
			t.Parallel()
			db, unit, source, blob := materialDatabase(t)
			before := planRows(t, db)
			service := application.NewMaterialization(
				materialLateFailure{Materialization: NewMaterialization(db), commit: commit},
				func() time.Time { return time.UnixMilli(1100) },
			)
			result, err := service.Copy(t.Context(), unit, source, blob)
			if err == nil || result != "" || !commit && !errors.Is(err, errLeaseStorage) {
				t.Fatalf("response=%s cause=%v", result, err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("failed commit retained material")
			}
		})
	}
}
