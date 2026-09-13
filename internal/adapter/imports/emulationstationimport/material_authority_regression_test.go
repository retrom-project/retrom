package emulationstationimport

import (
	"testing"

	"retrom/internal/adapter/files/blobstore"
)

func TestESMaterialBindingRejectsReplacedWorker(t *testing.T) {
	fixture := newLifecycleFixture(t)
	_, unit := startLifecycleImport(t, fixture, "", "nes")
	item, found, err := fixture.service.nextItem(fixture.context, unit)
	if err != nil || !found || len(item.Files) == 0 {
		t.Fatalf("item=%#v found=%v error=%v", item, found, err)
	}
	file := item.Files[0]
	metadata, err := fixture.service.copySource(
		fixture.context,
		fixture.service.roots[unit.RootID],
		unit,
		unit.RelativePath,
		file.Path,
		file.Size,
		file.Facts,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.ExecContext(
		fixture.context,
		`UPDATE jobs SET worker_id='replacement',version=version+1 WHERE id=?`,
		unit.JobID,
	); err != nil {
		t.Fatal(err)
	}
	before := materialAuthoritySnapshot(t, fixture, item.ID)
	blobID, err := bindMaterialRegression(fixture, unit, item, file, metadata)
	after := materialAuthoritySnapshot(t, fixture, item.ID)
	if before != after || err == nil || blobID != "" {
		t.Fatalf("stale material binding: blob=%s error=%v before=%s after=%s", blobID, err, before, after)
	}
}

func bindMaterialRegression(
	fixture lifecycleFixture,
	unit work,
	item executionItem,
	file executionFile,
	metadata blobstore.Metadata,
) (string, error) {
	return fixture.service.recordCopiedFile(fixture.context, unit, item.ID, file, metadata)
}

func materialAuthoritySnapshot(t *testing.T, fixture lifecycleFixture, itemID string) string {
	t.Helper()
	var snapshot string
	err := fixture.database.QueryRowContext(fixture.context, `SELECT json_object('blobs',(SELECT count(*) FROM blobs),'files',(SELECT json_group_array(json_object('state',state,'blob',blob_id)) FROM emulationstation_import_item_files WHERE item_id=?),'item',(SELECT json_object('state',execution_state,'version',version) FROM emulationstation_import_items WHERE id=?))`, itemID, itemID).Scan(

		&snapshot,
	)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
