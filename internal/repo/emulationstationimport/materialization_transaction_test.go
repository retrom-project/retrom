package emulationstationimport

import (
	"database/sql"
	"reflect"
	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	"strings"
	"testing"
	"time"
)

func materialDatabase(
	t *testing.T,
) (*sql.DB, emulationstationimportmodel.Execution, emulationstationimportmodel.MaterialSource, emulationstationimportmodel.VerifiedBlob) {
	t.Helper()
	db, unit := itemWorkDatabase(t)
	item, found, err := itemWorkService(db).Next(t.Context(), unit)
	if err != nil || !found {
		t.Fatalf("item=%v error=%v", found, err)
	}
	if _, err := db.ExecContext(
		t.Context(),
		`INSERT INTO emulationstation_import_item_assets(item_id,kind,resolution_method,relative_path,size_bytes,source_facts_digest,state,media_type,width_px,height_px,created_at_ms,updated_at_ms) VALUES(?,'COVER','EXPLICIT_IMAGE','cover.png',1,?,'DISCOVERED','image/png',1,1,1,1)`,
		item.ID,
		planDigest,
	); err != nil {
		t.Fatal(err)
	}
	source := emulationstationimportmodel.MaterialSource{
		Key:   emulationstationimportmodel.MaterialKey{ItemID: item.ID},
		Path:  "item.gba",
		Size:  1,
		Facts: planDigest,
	}
	blob := emulationstationimportmodel.VerifiedBlob{
		SHA256: strings.Repeat("c", 64),
		MD5:    strings.Repeat("c", 32),
		SHA1:   strings.Repeat("c", 40),
		CRC32:  "cccccccc",
		Size:   1,
	}
	return db, unit, source, blob
}

func materialAsset(source emulationstationimportmodel.MaterialSource) emulationstationimportmodel.MaterialSource {
	source.Key.Kind = "COVER"
	source.Path = "cover.png"
	source.MediaType = "image/png"
	width, height := int64(1), int64(1)
	source.Width = &width
	source.Height = &height
	return source
}

func materialService(db *sql.DB) *emulationstationimportservice.Materialization {
	return emulationstationimportservice.NewMaterialization(NewMaterialization(db), func() time.Time { return time.UnixMilli(1100) })
}

func TestMaterializationPersistsFrozenBindingAndReplay(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"file", "asset"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			db, unit, source, blob := materialDatabase(t)
			if kind == "asset" {
				source = materialAsset(source)
			}
			service := materialService(db)
			id, err := service.Copy(t.Context(), unit, source, blob)
			if err != nil || id == "" {
				t.Fatalf("blob=%s error=%v", id, err)
			}
			before := planRows(t, db)
			repeated, err := service.Copy(t.Context(), unit, source, blob)
			if err != nil || repeated != id || !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatalf("repeated=%s error=%v", repeated, err)
			}
			blob.MD5 = strings.Repeat("d", 32)
			if _, err := service.Copy(t.Context(), unit, source, blob); err == nil {
				t.Fatal("accepted mismatching copied blob facts")
			}
		})
	}
}

func TestMaterializationPersistsWarningAndPhaseAtomically(t *testing.T) {
	t.Parallel()
	db, unit, source, _ := materialDatabase(t)
	source = materialAsset(source)
	service := materialService(db)
	if err := service.Warning(t.Context(), unit, source, "EMULATIONSTATION_IMAGE_INVALID"); err != nil {
		t.Fatal(err)
	}
	var state, code, field, kind string
	err := db.QueryRowContext(t.Context(), `SELECT asset.state,asset.warning_code,json_extract(item.warnings_json,'$[0].field'),json_extract(item.warnings_json,'$[0].pathKind') FROM emulationstation_import_item_assets asset JOIN emulationstation_import_items item ON item.id=asset.item_id WHERE item.id=?`, source.Key.ItemID).Scan(

		&state,

		&code,

		&field,

		&kind,
	)
	if err != nil || state != "READ_FAILED" || code != "EMULATIONSTATION_IMAGE_INVALID" || field != "image" || kind != "COVER" {
		t.Fatalf("state=%s code=%s field=%s kind=%s error=%v", state, code, field, kind, err)
	}
	before := planRows(t, db)
	if err := service.Warning(t.Context(), unit, source, code); err != nil || !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatalf("warning replay error=%v", err)
	}
	if err := service.SetPhase(t.Context(), unit, "PREPARING_REVIEWS"); err != nil {
		t.Fatal(err)
	}
	summary, err := NewQueries(db).Get(t.Context(), unit.ImportID)
	if err != nil || summary.Phase == nil || *summary.Phase != "PREPARING_REVIEWS" {
		t.Fatalf("summary=%#v error=%v", summary, err)
	}
}
