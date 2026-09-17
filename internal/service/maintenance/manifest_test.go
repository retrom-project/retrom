package maintenance

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	model "retrom/internal/model/maintenance"
)

func TestBundleManifestRejectsTrailingJSON(t *testing.T) {
	lineage := model.Lineage{Version: 13, Digest: strings.Repeat("a", 64)}
	manifest := Manifest{SchemaVersion: 2, DatabaseSchemaVersion: lineage.Version, MigrationLineageDigest: lineage.Digest}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"\n{}\n", "\ntrue", "\ninvalid", "\n \t"} {
		t.Run(suffix, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "backup.json"), append(append([]byte{}, data...), []byte(suffix)...), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := loadBundleManifest(root, lineage)
			if suffix == "\n \t" {
				if err != nil {
					t.Fatalf("valid trailing whitespace: %v", err)
				}
			} else if !errors.Is(err, model.ErrInvalidBundle) {
				t.Fatalf("trailing JSON accepted: %v", err)
			}
		})
	}
}

func TestReferencedFilesRejectInvalidDigestsAndEscapingKeys(t *testing.T) {
	for _, snapshot := range []model.Snapshot{
		{Blobs: []model.Blob{{SHA256: "short", SizeBytes: 1}}},
		{Blobs: []model.Blob{{SHA256: strings.Repeat("X", 64), SizeBytes: 1}}},
		{Blobs: []model.Blob{{SHA256: strings.Repeat("a", 64), SizeBytes: -1}}},
		{Parts: []model.UploadPart{{StorageKey: "../private", SHA256: strings.Repeat("a", 64), SizeBytes: 1}}},
		{Parts: []model.UploadPart{{StorageKey: "tmp/uploads/nested", SHA256: strings.Repeat("a", 64), SizeBytes: 1}}},
	} {
		if _, err := referencedFiles(snapshot); !errors.Is(err, model.ErrInvalidBundle) {
			t.Fatalf("invalid references accepted: %+v %v", snapshot, err)
		}
	}
}
