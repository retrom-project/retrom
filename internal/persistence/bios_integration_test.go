//go:build integration

package persistence_test

import (
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/testsupport"
)

func TestBiosKeepsOneActiveFileAndAnyRetiredFiles(t *testing.T) {
	t.Parallel()
	repository := testsupport.Database(t)
	ctx := t.Context()
	file := func() model.BiosFile {
		id := uuid.NewString()
		return model.BiosFile{
			ID: id, RequirementKey: "psx/scph5501.bin", Filename: "scph5501.bin",
			StorageKey: "bios/" + id, SizeBytes: 3, SHA256: "hash",
		}
	}
	for at := range int64(3) {
		if err := repository.WriteBios(ctx, file(), 1000+at); err != nil {
			t.Fatal(err)
		}
	}
	installed, err := repository.InstallBios(ctx, file(), 2000)
	if err != nil || installed {
		t.Fatalf("scan replaced the active BIOS: installed=%v err=%v", installed, err)
	}
	active, err := repository.Count(ctx, `SELECT count(*) FROM bios_file_tab WHERE status='active' AND active_slot=1`)
	if err != nil || active != 1 {
		t.Fatalf("active=%d err=%v", active, err)
	}
	retired, err := repository.Count(ctx, `SELECT count(*) FROM bios_file_tab WHERE status='deleted' AND active_slot IS NULL`)
	if err != nil || retired != 2 {
		t.Fatalf("retired=%d err=%v", retired, err)
	}
}
