package packs

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestDeleteRejectsVariantAndCheckpointReferences(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.ExecContext(context.Background(), `
CREATE TABLE runtime_asset_pack_installations(id TEXT PRIMARY KEY,status TEXT,version INTEGER,bundle_blob_id TEXT);
CREATE TABLE game_variant_runtime_packs(game_variant_id TEXT,installation_id TEXT);
CREATE TABLE save_states(id TEXT,source_launch_session_id TEXT,deleted_at_ms INTEGER);
CREATE TABLE launch_content_files(launch_session_id TEXT,logical_name TEXT,blob_id TEXT);
INSERT INTO runtime_asset_pack_installations VALUES('018f47d2-3b83-7a3b-8c8e-7d5688a45001','READY',3,'pack-bundle');
INSERT INTO game_variant_runtime_packs VALUES('variant','018f47d2-3b83-7a3b-8c8e-7d5688a45001');
INSERT INTO save_states VALUES('active','launch-active',NULL),('deleted','launch-deleted',10);
INSERT INTO launch_content_files VALUES
 ('launch-active','__retrom__/pack-0.zip','pack-bundle'),
 ('launch-deleted','__retrom__/pack-0.zip','pack-bundle');
`); err != nil {
		t.Fatal(err)
	}
	service := New(database, nil, nil, time.Now)
	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	references, err := installationReferences(
		context.Background(), transaction, "018f47d2-3b83-7a3b-8c8e-7d5688a45001",
	)
	_ = transaction.Rollback()
	if err != nil || references != (ReferenceCounts{GameCount: 1, CheckpointCount: 1}) {
		t.Fatalf("installation references = %#v, error=%v", references, err)
	}
	err = service.Delete(context.Background(), "018f47d2-3b83-7a3b-8c8e-7d5688a45001", 3)
	if !errors.Is(err, ErrReferenced) {
		t.Fatalf("Delete error = %v", err)
	}
	var status string
	var version int64
	if err := database.QueryRowContext(context.Background(), `SELECT status,version FROM runtime_asset_pack_installations`).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "READY" || version != 3 {
		t.Fatalf("referenced installation changed to %s v%d", status, version)
	}
}

func TestNormalizeCustomDefinitionRequestUsesNFCAndRejectsControls(t *testing.T) {
	generation := "RPGXP"
	name := "  Cafe\u0301  "
	request, err := normalizeCustomDefinitionRequest(InstallRequest{
		Kind: "RGSS_CUSTOM_RTP", Generation: &generation, DeclaredName: &name,
	})
	if err != nil || *request.DeclaredName != "Café" {
		t.Fatalf("normalized custom name = %#v, error=%v", request.DeclaredName, err)
	}
	invalid := "bad\u0007name"
	if _, err := normalizeCustomDefinitionRequest(InstallRequest{
		Kind: "RGSS_CUSTOM_RTP", Generation: &generation, DeclaredName: &invalid,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("control custom name error = %v", err)
	}
}

func TestEasyRTPLayoutUsesPinnedPlayerDecoderExtensions(t *testing.T) {
	files := []FileIdentity{
		{Path: "Backdrop/Dungeon1.bmp"},
		{Path: "Music/Dungeon1.ogg"},
	}
	if err := ValidateEasyRTPLayout("RPG2000", files); err != nil {
		t.Fatalf("decoder-supported layout error = %v", err)
	}
	files[1].Path = "Music/Dungeon1.exe"
	err := ValidateEasyRTPLayout("RPG2000", files)
	var violation *layoutViolation
	if !errors.Is(err, ErrInvalid) || !errors.As(err, &violation) || violation.Path != files[1].Path {
		t.Fatalf("unsupported extension error = %v", err)
	}
}
