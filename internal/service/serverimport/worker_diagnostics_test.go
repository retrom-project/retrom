package serverimport

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/filestore"
	"retrom/internal/firmware"
	"retrom/internal/importing"
)

func TestEvaluationDiagnosticsPreserveFailureStage(t *testing.T) {
	for _, test := range []struct {
		cause              error
		stage, state, code string
	}{
		{ErrDATUnavailable, "CATALOG", "CATALOG_INVALID", "DAT_UNAVAILABLE"},
		{ErrDATMachineUndefined, "CATALOG", "CATALOG_INVALID", "DAT_MACHINE_UNDEFINED"},
		{ErrDATEntriesUnverifiable, "CATALOG", "CATALOG_INVALID", "DAT_ENTRIES_UNVERIFIABLE"},
		{&os.PathError{Op: "open", Path: "input.zip", Err: os.ErrPermission}, "ARCHIVE", "READ_FAILED", "SERVER_IMPORT_SOURCE_UNREADABLE"},
		{zip.ErrChecksum, "ARCHIVE", "INVALID_ARCHIVE", "BIOS_ARCHIVE_INVALID"},
		{importing.ErrArchiveUnsafe, "ARCHIVE", "ARCHIVE_UNSAFE", "ARCHIVE_UNSAFE"},
		{errors.New("database unavailable"), "CATALOG", "VALIDATION_FAILED", "SERVER_IMPORT_VALIDATION_FAILED"},
	} {
		t.Run(test.code, func(t *testing.T) {
			state, code, stage := evaluationDiagnosis(&evaluationFailure{stage: test.stage, cause: fmt.Errorf("wrapped: %w", test.cause)})
			if state != test.state || code != test.code || stage != test.stage {
				t.Fatalf("%s/%s/%s", state, code, stage)
			}
		})
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errCancelled} {
		memory := &recoveryMemory{err: cause}
		service := &Service{recovery: NewRecovery(memory, memory), archiveScan: make(chan struct{}, 1)}
		version, machine := "dat", "machine"
		task := candidateHashTask{associations: []association{{item: CatalogItem{
			SourceKind: "DAT_MACHINE", DATVersionID: &version, DATMachineName: &machine,
		}}}}
		if _, err := service.evaluateCandidateAssociations(t.Context(), task, filestore.Metadata{}, firmware.FileFacts{}); !errors.Is(err, cause) {
			t.Fatalf("cancellation converted into candidate diagnosis: %v", err)
		}
	}
}

func TestCatalogFailureDoesNotBecomeArchiveFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "psarc95.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create("boot.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("firmware")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	memory := &recoveryMemory{}
	service := &Service{recovery: NewRecovery(memory, memory), archiveScan: make(chan struct{}, 1)}
	version, machine := "dat", "psarc95"
	item := CatalogItem{SourceKind: "DAT_MACHINE", LogicalName: "psarc95.zip", DATVersionID: &version, DATMachineName: &machine}
	task := candidateHashTask{
		file:         discoveredFile{RelativePath: "psarc95.zip", Basename: "psarc95.zip"},
		associations: []association{{item: item, kind: "EXACT_NAME"}},
	}
	candidates, err := service.evaluateCandidateAssociations(t.Context(), task, filestore.Metadata{Path: path}, firmware.FileFacts{})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%v err=%v", candidates, err)
	}
	candidate := candidates[0]
	state, code := rejectedArchiveOutcome(candidates)
	if candidate.State != "CATALOG_INVALID" || state != "CATALOG_INVALID" || code != "DAT_ENTRIES_UNVERIFIABLE" {
		t.Fatalf("candidate=%s details=%v outcome=%s/%s", candidate.State, candidate.Details, state, code)
	}
}
