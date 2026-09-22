package sourceimport

import (
	"errors"
	"strings"
	"testing"

	"retrom/internal/blobstore"
)

func materialFixture(t *testing.T) (*Service, work, executionFile, executionAsset, blobstore.Metadata) {
	t.Helper()
	service, unit, _ := handoffFixture(t)
	mustExecSourceTest(
		t.Context(),
		t,
		service.database,
		`UPDATE source_import_items SET execution_state='COPYING',library_import_job_id=NULL,library_import_item_id=NULL;
DELETE FROM source_import_item_files;
INSERT INTO source_import_item_files(item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,state,created_at_ms,updated_at_ms)
VALUES('item',0,'FILE','game.gba',4,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','DISCOVERED',1,1);
INSERT INTO source_import_item_assets(item_id,kind,resolution_method,relative_path,size_bytes,source_facts_digest,state,media_type,width_px,height_px,created_at_ms,updated_at_ms)
VALUES('item','COVER','EXPLICIT_GAME','cover.png',4,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','DISCOVERED','image/png',1,1,1,1);`,
	)
	one := int64(1)
	return service, unit, executionFile{
			Ordinal: 0,
			Path:    "game.gba",
			Size:    4,
			Facts:   fixedHandoffDigest,
		}, executionAsset{
			Kind:      "COVER",
			Path:      "cover.png",
			Size:      4,
			Facts:     fixedHandoffDigest,
			MediaType: "image/png",
			Width:     &one,
			Height:    &one,
		}, blobstore.Metadata{
			SHA256: strings.Repeat("b", 64),
			MD5:    strings.Repeat("b", 32),
			SHA1:   strings.Repeat("b", 40),
			CRC32:  "bbbbbbbb",
			Size:   4,
		}
}

func TestMaterializationRejectsReplacedOwner(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"file", "asset", "phase", "warning"} {
		t.Run(kind, func(t *testing.T) {
			service, unit, file, asset, metadata := materialFixture(t)
			mustExecSourceTest(t.Context(), t, service.database, `UPDATE jobs SET worker_id='new-worker' WHERE id='work'`)
			switch kind {
			case "file":
				_, _ = service.recordCopiedFile(t.Context(), unit, "item", file, metadata)
			case "asset":
				_, _ = service.recordCopiedAsset(t.Context(), unit, "item", asset, metadata)
			case "phase":
				_ = service.updateExecutionPhase(t.Context(), unit, "VALIDATING")
			case "warning":
				_ = service.closeAssetWarning(t.Context(), unit, "item", asset, "PEGASUS_IMAGE_INVALID")
			}
			var changed int
			err := service.database.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM blobs WHERE sha256=?) +
(SELECT count(*) FROM source_import_item_assets WHERE item_id='item' AND state<>'DISCOVERED')+
(SELECT count(*) FROM source_imports WHERE phase='VALIDATING')`, metadata.SHA256).Scan(&changed)
			if err != nil {
				t.Fatal(err)
			}
			if changed != 0 {
				t.Fatalf("replaced worker persisted %s: changed=%d", kind, changed)
			}
		})
	}
}

func TestMaterializationRejectsMissingFileBeforeCatalogRegistration(t *testing.T) {
	t.Parallel()
	service, unit, file, _, metadata := materialFixture(t)
	file.Ordinal = 63
	id, err := service.recordCopiedFile(t.Context(), unit, "item", file, metadata)
	if !errors.Is(err, ErrVersionConflict) || id != "" {
		t.Fatalf("missing source accepted blob=%q err=%v", id, err)
	}
}

func TestMaterializationWarningIsIdempotent(t *testing.T) {
	t.Parallel()
	service, unit, _, asset, _ := materialFixture(t)
	for range 2 {
		_ = service.closeAssetWarning(t.Context(), unit, "item", asset, "PEGASUS_IMAGE_INVALID")
	}
	var count int
	if err := service.database.QueryRowContext(t.Context(), `SELECT json_array_length(warnings_json) FROM source_import_items WHERE id='item'`).Scan(

		&count,
	); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("warning count=%d", count)
	}
}
