//go:build integration

package refactor

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/runtime/runtimecatalog"
	modelidempotency "retrom/internal/model/idempotency"
	"retrom/internal/model/saves"
	repohome "retrom/internal/repo/home"
	repoidempotency "retrom/internal/repo/idempotency"
	reporuntimecatalog "retrom/internal/repo/runtimecatalog"
	"retrom/internal/repo/store"
	servicehome "retrom/internal/service/home"
)

const characterizationTimeMS int64 = 1786000000000

type (
	schemaObject struct {
		Kind, Name, Table string
		SQL               *string
	}
	lineageEntry struct {
		Version        int64
		Name, Checksum string
		AppliedAtMS    int64
	}
	blobEvidence struct {
		SHA256, MD5, SHA1, CRC32, RelativePath string
		Size                                   int64
		FirstExisting, SecondExisting          bool
	}
)

type characterization struct {
	Schema          []schemaObject
	Migrations      []lineageEntry
	HomeJSON        string
	Blob            blobEvidence
	ProductSaveJSON string
	PreviewSaveJSON string
	Receipt         modelidempotency.Receipt
}

func captureCharacterization(t *testing.T) characterization {
	t.Helper()
	root := fixtureRepositoryRoot(t)
	data := t.TempDir()
	database, err := store.Open(t.Context(), filepath.Join(data, "retrom.db"),
		func() time.Time { return time.UnixMilli(characterizationTimeMS) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	seedCharacterizationCatalog(t, database.SQL, root)
	snapshot := characterization{
		Schema:     readCharacterizationSchema(t, database.SQL),
		Migrations: readCharacterizationLineage(t, database.SQL),
		Blob:       captureCharacterizationBlob(t, root, data),
	}
	dashboard, err := servicehome.New(repohome.New(database.ReadOnly), nil).
		Dashboard(t.Context(), "0198b58e-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.HomeJSON = characterizationJSON(t, dashboard)
	screenshot := "/content/save-states/0198b58e-0000-7000-8000-000000000002/screenshot"
	disc := 1
	snapshot.ProductSaveJSON = characterizationJSON(t, saves.ManualResult{
		ResourceKind: "SAVE_STATE", SaveStateID: "0198b58e-0000-7000-8000-000000000002",
		CheckpointFormat: "emulatorjs-state-v1", ScreenshotURL: &screenshot,
		CreatedAtMS: characterizationTimeMS, Name: "公开回归存档", DiscIndex: &disc, Version: 3, ActiveDurationMS: 2500,
	})
	snapshot.PreviewSaveJSON = characterizationJSON(t, saves.ManualResult{
		ResourceKind: "REVIEW_PREVIEW_CHECKPOINT", PreviewID: "0198b58e-0000-7000-8000-000000000003",
		CheckpointFormat: "emulatorjs-state-v1", CreatedAtMS: characterizationTimeMS,
	})
	snapshot.Receipt = captureCharacterizationReceipt(t, database.SQL, snapshot.ProductSaveJSON, snapshot.Blob.SHA256)
	return snapshot
}

func seedCharacterizationCatalog(t *testing.T, database *sql.DB, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "data/runtime-target-bindings/v1/catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := runtimecatalog.ParseCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reporuntimecatalog.SynchronizeDefinitions(t.Context(), transaction, catalog, characterizationTimeMS); err != nil {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func captureCharacterizationBlob(t *testing.T, root, data string) blobEvidence {
	t.Helper()
	publicROM, err := os.ReadFile(filepath.Join(root, "testdata/public-roms/gba-smoke/gba-smoke.gba"))
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	first, err := blobs.Put(bytes.NewReader(publicROM))
	if err != nil {
		t.Fatal(err)
	}
	second, err := blobs.Put(bytes.NewReader(publicROM))
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(data, first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != second.SHA256 || first.Path != second.Path || first.Size != second.Size {
		t.Fatal("identical public input did not converge on one CAS object")
	}
	return blobEvidence{
		SHA256: first.SHA256, MD5: first.MD5, SHA1: first.SHA1, CRC32: first.CRC32,
		RelativePath: filepath.ToSlash(relative), Size: first.Size,
		FirstExisting: first.Existing, SecondExisting: second.Existing,
	}
}

func captureCharacterizationReceipt(t *testing.T, database *sql.DB, body, digest string) modelidempotency.Receipt {
	t.Helper()
	repository := repoidempotency.New(database)
	receipt := modelidempotency.Receipt{
		RequestDigest: digest, HTTPStatus: 201, HeadersJSON: "{}",
		Body: append([]byte(" \n"), []byte(body)...),
	}
	if err := repository.Save(t.Context(), "postRuntimeSaveState", "baseline-key", "fixture-user",
		receipt, characterizationTimeMS, characterizationTimeMS+86400000); err != nil {
		t.Fatal(err)
	}
	stored, found, err := repository.Find(t.Context(), "postRuntimeSaveState", "baseline-key", "fixture-user")
	if err != nil || !found {
		t.Fatalf("read stored receipt: found=%t error=%v", found, err)
	}
	if !bytes.Equal(stored.Body, receipt.Body) {
		t.Fatal("historical receipt was reencoded")
	}
	return stored
}

func characterizationJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
