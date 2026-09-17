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
	pegasusimportservice "retrom/internal/service/pegasusimport"
)

func materialDatabase(t *testing.T) (*sql.DB, pegasusimportmodel.MaterialKey, pegasusimportmodel.VerifiedBlob) {
	t.Helper()
	db := itemWorkDatabase(t)
	if _, err := db.ExecContext(
		t.Context(),
		`INSERT INTO pegasus_import_item_files(item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,state,created_at_ms,updated_at_ms)
VALUES('item-0',0,'FILE','game.gba',4,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','DISCOVERED',1,1);
INSERT INTO pegasus_import_item_assets(item_id,kind,resolution_method,relative_path,size_bytes,source_facts_digest,state,media_type,width_px,height_px,created_at_ms,updated_at_ms)
VALUES('item-0','COVER','EXPLICIT_GAME','cover.png',4,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','DISCOVERED','image/png',1,1,1,1);`,
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
	for _, table := range []string{"blobs", "pegasus_import_item_files", "pegasus_import_item_assets"} {
		result[table] = workflowTable(t, db, table)
	}
	return result
}

func TestMaterializationWritesFenceFrozenSourceAndLiveOwner(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"file", "asset", "warning"} {
		t.Run(operation, func(t *testing.T) {
			fields := []string{
				"job version",
				"parent version",
				"execution",
				"attempt",
				"worker",
				"lease",
				"deadline",
				"parent state",
				"item version",
				"path",
				"size",
				"facts",
				"source state",
			}
			if operation != "file" {
				fields = append(fields, "dimensions")
			}
			for _, field := range fields {
				t.Run(field, func(t *testing.T) { assertMaterialFence(t, operation, field) })
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
	err := NewMaterialization(db).WithMaterialization(t.Context(), func(scope pegasusimportmodel.MaterialScope) error {
		before, err := scope.Read.Source(t.Context(), key)
		if err != nil {
			return err
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
			return scope.Write.Warn(
				t.Context(),
				pegasusimportmodel.MaterialWarning{
					Before:   before,
					State:    "READ_FAILED",
					Code:     "PEGASUS_IMAGE_INVALID",
					Warnings: []map[string]any{{"code": "PEGASUS_IMAGE_INVALID", "field": "cover"}},
					NowMS:    10,
				},
			)
		}
		_, err = scope.Write.Bind(t.Context(), pegasusimportmodel.MaterialBinding{Before: before, Blob: blob, NowMS: 10})
		return err
	})
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
		t.Run(operation, func(t *testing.T) { assertMaterialRollback(t, operation) })
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
	err := NewMaterialization(db).WithMaterialization(t.Context(), func(scope pegasusimportmodel.MaterialScope) error {
		assertMaterialWriteVisible(t, scope, operation, key, blob)
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("%s not written then rejected: %v", operation, err)
	}
	if !reflect.DeepEqual(beforeRows, materialRows(t, db)) {
		t.Fatalf("%s partially committed", operation)
	}
}

func assertMaterialWriteVisible(
	t *testing.T,
	scope pegasusimportmodel.MaterialScope,
	operation string,
	key pegasusimportmodel.MaterialKey,
	blob pegasusimportmodel.VerifiedBlob,
) {
	t.Helper()
	if operation == "phase" {
		assertMaterialPhaseVisible(t, scope)
		return
	}
	before, err := scope.Read.Source(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	id := ""
	if operation == "warning" {
		err = scope.Write.Warn(t.Context(), pegasusimportmodel.MaterialWarning{
			Before: before, State: "READ_FAILED", Code: "PEGASUS_IMAGE_INVALID",
			Warnings: []map[string]any{{"code": "PEGASUS_IMAGE_INVALID", "field": "cover"}}, NowMS: 10,
		})
	} else {
		id, err = scope.Write.Bind(t.Context(), pegasusimportmodel.MaterialBinding{Before: before, Blob: blob, NowMS: 10})
	}
	if err != nil {
		t.Fatal(err)
	}
	current, err := scope.Read.Source(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if operation == "warning" {
		if current.State != "READ_FAILED" || len(current.Warnings) != 1 {
			t.Fatalf("warning not written: %#v", current)
		}
	} else if current.State != "COPIED" || id == "" || current.BlobID != id {
		t.Fatalf("binding not written: %#v", current)
	}
}

func assertMaterialPhaseVisible(t *testing.T, scope pegasusimportmodel.MaterialScope) {
	t.Helper()
	phase, err := scope.Read.Execution(t.Context(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if err := scope.Write.Phase(
		t.Context(),
		pegasusimportmodel.PhaseChange{Before: phase, Phase: "VALIDATING", NowMS: 10},
	); err != nil {
		t.Fatal(
			err,
		)
	}
	current, err := scope.Read.Execution(t.Context(), "work")
	if err != nil || current.Phase != "VALIDATING" {
		t.Fatalf("phase not written: %#v %v", current, err)
	}
}

func TestMaterializationReplayAcceptsHeartbeatButRejectsDifferentCASFacts(t *testing.T) {
	t.Parallel()
	db, key, blob := materialDatabase(t)
	var source pegasusimportmodel.MaterialSource
	if err := NewMaterialization(db).WithMaterialization(t.Context(), func(scope pegasusimportmodel.MaterialScope) error {
		snapshot, err := scope.Read.Source(t.Context(), key)
		source = snapshot.Source
		return err
	}); err != nil {
		t.Fatal(err)
	}
	service := pegasusimportservice.NewMaterialization(NewMaterialization(db), func() time.Time { return time.UnixMilli(10) })
	identity := pegasusimportmodel.ExecutionIdentity{
		JobID:       "work",
		ImportID:    "import-0",
		WorkerID:    "old-worker",
		ExecutionNo: 1,
		Attempt:     1,
	}
	id, err := service.Copy(t.Context(), identity, source, blob)
	if err != nil || id == "" {
		t.Fatalf("copy=%s %v", id, err)
	}
	if _, err := db.ExecContext(
		t.Context(),
		`UPDATE jobs SET version=version+1,leased_until_ms=99 WHERE id='work';UPDATE pegasus_imports SET version=version+1 WHERE id='import-0'`,
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
		t.Context(),
		identity,
		source,
		blob,
	); result != "" || !errors.Is(
		err,
		pegasusimportmodel.ErrVersionConflict,
	) {
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
	err := NewMaterialization(db).WithMaterialization(t.Context(), func(scope pegasusimportmodel.MaterialScope) error {
		_, err := scope.Read.Source(t.Context(), key)
		return err
	})
	var cause *json.UnmarshalTypeError
	if !errors.As(err, &cause) {
		t.Fatalf("warning cause=%v", err)
	}
	if !reflect.DeepEqual(before, materialRows(t, db)) {
		t.Fatal("invalid warnings changed source")
	}
}
