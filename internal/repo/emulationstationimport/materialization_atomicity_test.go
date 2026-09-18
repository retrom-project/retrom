package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/dbexec"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestMaterializationSQLAndAffectedFailuresRollback(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"file", "asset", "warning", "phase"} {
		for _, affected := range []bool{false, true} {
			t.Run(
				operation+map[bool]string{false: "/SQL", true: "/count"}[affected],
				func(t *testing.T) {
					t.Parallel()
					db, unit, source, blob := materialDatabase(t)
					statement := "UPDATE emulationstation_import_item_files SET"
					target := source.Key.ItemID
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
					faultDB := testsupport.OpenSQLFaultDatabase(
						t, db,
						scanFaultHooks(statement, target, affected, &hits),
					)
					err := runMaterialOperation(
						t.Context(), materialService(faultDB),
						operation, unit, source, blob,
					)
					if !errors.Is(err, errLeaseStorage) || hits.Load() != 1 {
						t.Fatalf("cause=%v hits=%d", err, hits.Load())
					}
					if !reflect.DeepEqual(before, planRows(t, db)) {
						t.Fatal("failed material operation retained writes")
					}
				},
			)
		}
	}
}

func runMaterialOperation(
	ctx context.Context,
	service *emulationstationimportservice.Materialization,
	operation string,
	unit emulationstationimportmodel.Execution,
	source emulationstationimportmodel.MaterialSource,
	blob emulationstationimportmodel.VerifiedBlob,
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

func TestMaterializationCommitFailureRollsBackWritesAndResponse(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"pre-commit hook", "commit FK"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			db, unit, source, blob := materialDatabase(t)
			before := planRows(t, db)
			repo := NewMaterialization(db)
			installMaterialCommitHook(repo, stage)
			service := emulationstationimportservice.NewMaterialization(
				repo, func() time.Time { return time.UnixMilli(1100) },
			)
			result, err := service.Copy(t.Context(), unit, source, blob)
			if err == nil || result != "" {
				t.Fatalf("response=%s cause=%v", result, err)
			}
			if stage == "pre-commit hook" && !errors.Is(err, errLeaseStorage) {
				t.Fatalf("lost cause: %v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("failed commit retained material")
			}
		})
	}
}

func installMaterialCommitHook(repo *Materialization, stage string) {
	repo.WithPreCommitHook(func(tx dbexec.Executor) error {
		if stage == "pre-commit hook" {
			return errLeaseStorage
		}
		if _, err := tx.ExecContext(
			context.Background(), `PRAGMA defer_foreign_keys=ON`,
		); err != nil {
			return err
		}
		_, err := tx.ExecContext(
			context.Background(),
			`INSERT INTO job_input_snapshots(job_id,execution_no,`+
				`input_json,input_digest,created_at_ms)`+
				` VALUES('missing-material-parent',1,'{}','`+planDigest+`',12)`,
		)
		return err
	})
}
