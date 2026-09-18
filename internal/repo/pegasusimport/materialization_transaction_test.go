package pegasusimport

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
	"retrom/internal/repo/dbexec"
	pegasusimportservice "retrom/internal/service/pegasusimport"
)

func materialDatabase(
	t *testing.T,
) (*sql.DB, pegasusimportmodel.MaterialKey, pegasusimportmodel.VerifiedBlob) {
	t.Helper()
	db := itemWorkDatabase(t)
	if _, err := db.ExecContext(
		t.Context(),
		`INSERT INTO pegasus_import_item_files(item_id,ordinal,declared_kind,relative_path,`+
			`size_bytes,source_facts_digest,state,created_at_ms,updated_at_ms)`+
			` VALUES('item-0',0,'FILE','game.gba',4,`+
			`'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','DISCOVERED',1,1);`+
			`INSERT INTO pegasus_import_item_assets(item_id,kind,resolution_method,relative_path,`+
			`size_bytes,source_facts_digest,state,media_type,width_px,height_px,created_at_ms,updated_at_ms)`+
			` VALUES('item-0','COVER','EXPLICIT_GAME','cover.png',4,`+
			`'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','DISCOVERED','image/png',1,1,1,1);`,
	); err != nil {
		t.Fatal(err)
	}
	return db, pegasusimportmodel.MaterialKey{
			ItemID:  "item-0",
			Ordinal: 0,
		}, pegasusimportmodel.VerifiedBlob{
			SHA256: strings.Repeat("c", 64),
			MD5:    strings.Repeat("c", 32),
			SHA1:   strings.Repeat("c", 40),
			CRC32:  "cccccccc",
			Size:   4,
		}
}

func materialRows(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	result := workflowRows(t, db)
	for _, table := range []string{
		"blobs", "pegasus_import_item_files", "pegasus_import_item_assets",
	} {
		result[table] = workflowTable(t, db, table)
	}
	return result
}

func TestMaterializationWritesFenceFrozenSourceAndLiveOwner(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"file", "asset", "warning"} {
		t.Run(operation, func(t *testing.T) {
			fields := []string{
				"job version", "parent version", "execution",
				"attempt", "worker", "lease", "deadline",
				"parent state", "item version",
				"path", "size", "facts", "source state",
			}
			if operation != "file" {
				fields = append(fields, "dimensions")
			}
			for _, field := range fields {
				t.Run(field, func(t *testing.T) {
					assertMaterialFence(t, operation, field)
				})
			}
		})
	}
}

func assertMaterialFence(t *testing.T, operation, field string) {
	t.Helper()
	db, key, blob := materialDatabase(t)
	if operation != "file" {
		key.Kind = "COVER"
	}
	beforeRows := materialRows(t, db)
	repo := NewMaterialization(db)
	before, err := repo.LoadMaterialSource(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	switch field {
	case "item version":
		before.Before.Item.Version++
	case "path":
		before.Source.Path = "changed"
	case "size":
		before.Source.Size++
	case "facts":
		before.Source.Facts = strings.Repeat("b", 64)
	case "source state":
		before.State = "COPIED"
	case "dimensions":
		width := int64(2)
		before.Source.Width = &width
	default:
		invalidateRecovery(&before.Before.Execution, field)
	}
	if operation == "warning" {
		err = repo.CommitMaterialWarning(
			t.Context(),
			pegasusimportmodel.MaterialWarning{
				Before: before, State: "READ_FAILED",
				Code:     "PEGASUS_IMAGE_INVALID",
				Warnings: []map[string]any{{"code": "PEGASUS_IMAGE_INVALID", "field": "cover"}},
				NowMS:    10,
			},
		)
	} else {
		_, err = repo.CommitMaterialBinding(
			t.Context(),
			pegasusimportmodel.MaterialBinding{Before: before, Blob: blob, NowMS: 10},
		)
	}
	if !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
		t.Fatalf("%s %s error=%v", operation, field, err)
	}
	if !reflect.DeepEqual(beforeRows, materialRows(t, db)) {
		t.Fatalf("%s %s left material/catalog/warnings", operation, field)
	}
}

func TestMaterializationRollsBackCompletedWritesAndResponse(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"file", "asset", "warning", "phase"} {
		t.Run(operation, func(t *testing.T) {
			assertMaterialRollback(t, operation)
		})
	}
}

func assertMaterialRollback(t *testing.T, operation string) {
	t.Helper()
	db, key, blob := materialDatabase(t)
	if operation == "asset" || operation == "warning" {
		key.Kind = "COVER"
	}
	beforeRows := materialRows(t, db)
	cause := errors.New("failure after actual writes")
	repo := NewMaterialization(db)
	repo.WithPreCommitHook(func(_ dbexec.Executor) error { return cause })
	err := runMaterialRollbackOperation(t, repo, operation, key, blob)
	if !errors.Is(err, cause) {
		t.Fatalf("%s not written then rejected: %v", operation, err)
	}
	if !reflect.DeepEqual(beforeRows, materialRows(t, db)) {
		t.Fatalf("%s partially committed", operation)
	}
}

func runMaterialRollbackOperation(
	t *testing.T, repo *Materialization,
	operation string, key pegasusimportmodel.MaterialKey,
	blob pegasusimportmodel.VerifiedBlob,
) error {
	t.Helper()
	switch operation {
	case "phase":
		phase, err := repo.LoadExecutionPhase(t.Context(), "work")
		if err != nil {
			t.Fatal(err)
		}
		return repo.CommitPhaseChange(
			t.Context(),
			pegasusimportmodel.PhaseChange{Before: phase, Phase: "VALIDATING", NowMS: 10},
		)
	case "warning":
		before, err := repo.LoadMaterialSource(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		return repo.CommitMaterialWarning(
			t.Context(),
			pegasusimportmodel.MaterialWarning{
				Before: before, State: "READ_FAILED",
				Code:     "PEGASUS_IMAGE_INVALID",
				Warnings: []map[string]any{{"code": "PEGASUS_IMAGE_INVALID", "field": "cover"}},
				NowMS:    10,
			},
		)
	default:
		before, err := repo.LoadMaterialSource(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		_, err = repo.CommitMaterialBinding(
			t.Context(),
			pegasusimportmodel.MaterialBinding{Before: before, Blob: blob, NowMS: 10},
		)
		return err
	}
}

func TestMaterializationReplayAcceptsHeartbeatButRejectsDifferentCASFacts(t *testing.T) {
	t.Parallel()
	db, key, blob := materialDatabase(t)
	repo := NewMaterialization(db)
	snapshot, err := repo.LoadMaterialSource(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	source := snapshot.Source
	service := pegasusimportservice.NewMaterialization(
		NewMaterialization(db), func() time.Time { return time.UnixMilli(10) },
	)
	identity := pegasusimportmodel.ExecutionIdentity{
		JobID: "work", ImportID: "import-0",
		WorkerID: "old-worker", ExecutionNo: 1, Attempt: 1,
	}
	id, err := service.Copy(t.Context(), identity, source, blob)
	if err != nil || id == "" {
		t.Fatalf("copy=%s %v", id, err)
	}
	if _, err := db.ExecContext(
		t.Context(),
		`UPDATE jobs SET version=version+1,leased_until_ms=99 WHERE id='work';`+
			`UPDATE pegasus_imports SET version=version+1 WHERE id='import-0'`,
	); err != nil {
		t.Fatal(err)
	}
	before := materialRows(t, db)
	replay, err := service.Copy(t.Context(), identity, source, blob)
	if err != nil || replay != id {
		t.Fatalf("replay=%s %v", replay, err)
	}
	blob.MD5 = strings.Repeat("b", 32)
	if result, err := service.Copy(
		t.Context(), identity, source, blob,
	); result != "" || !errors.Is(err, pegasusimportmodel.ErrVersionConflict) {
		t.Fatalf("different replay=%s %v", result, err)
	}
	if !reflect.DeepEqual(before, materialRows(t, db)) {
		t.Fatal("replay rewrote material")
	}
}

func TestMaterializationWarningPreservesMalformedJSONCauseAndRows(t *testing.T) {
	t.Parallel()
	db, key, _ := materialDatabase(t)
	key.Kind = "COVER"
	if _, err := db.ExecContext(
		t.Context(),
		`UPDATE pegasus_import_items SET warnings_json='{}' WHERE id='item-0'`,
	); err != nil {
		t.Fatal(err)
	}
	before := materialRows(t, db)
	_, err := NewMaterialization(db).LoadMaterialSource(t.Context(), key)
	var cause *json.UnmarshalTypeError
	if !errors.As(err, &cause) {
		t.Fatalf("warning cause=%v", err)
	}
	if !reflect.DeepEqual(before, materialRows(t, db)) {
		t.Fatal("invalid warnings changed source")
	}
}
