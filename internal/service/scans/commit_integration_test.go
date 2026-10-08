//go:build integration

package scans

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"
	"retrom/internal/testsupport"
)

func TestGameCommitRollsBackProgressAndConfirmsLostAcknowledgment(t *testing.T) {
	f := testsupport.Library(t)
	s := &Service{Repository: f.Repository, Now: time.Now}
	scan := model.Scan{ID: uuid.NewString(), ScanType: "game", Status: "running", TotalKnown: true, TotalCount: 1, CreatedAtMs: 1000}
	if err := f.Repository.CreateScan(t.Context(), scan, f.Principal.User.ID); err != nil {
		t.Fatal(err)
	}
	// A removed mapping tag is discovered after the game/files INSERTs. The whole candidate must roll back.
	f.Game.Input.TagIDs = []string{uuid.NewString()}
	if err := s.commitGame(t.Context(), f.Principal, f.Game, &scan); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("error=%v", err)
	}
	assertCounts(t, f.Repository, scan.ID, 0, 0)
	if count, err := f.Repository.Count(t.Context(), "SELECT count(*) FROM game_tab"); err != nil || count != 0 {
		t.Fatalf("games=%d error=%v", count, err)
	}
	if count, err := f.Repository.Count(t.Context(), "SELECT count(*) FROM game_file_tab"); err != nil || count != 0 {
		t.Fatalf("files=%d error=%v", count, err)
	}
	f.Game.Input.TagIDs = []string{}
	before := scan
	if err := s.commitGame(t.Context(), f.Principal, f.Game, &scan); err != nil {
		t.Fatal(err)
	}
	assertCounts(t, f.Repository, scan.ID, 1, 1)
	// The database committed, but the transport lost the COMMIT acknowledgment.
	next := scan
	if err := s.confirmCommit(t.Context(), &before, next, f.Game.ID, "game_tab", persistence.ErrCommitUncertain); err != nil {
		t.Fatal(err)
	}
	if before.ProcessedCount != 1 || before.ImportedCount != 1 {
		t.Fatalf("reconciled=%+v", before)
	}
	missing := before
	missing.ProcessedCount++
	if err := s.confirmCommit(t.Context(), &before, missing, uuid.NewString(), "game_tab", persistence.ErrCommitUncertain); !errors.Is(err, persistence.ErrCommitUncertain) {
		t.Fatalf("unconfirmed error=%v", err)
	}
	if err := s.commitGame(t.Context(), f.Principal, f.Game, &scan); err != nil {
		t.Fatal(err)
	}
	assertCounts(t, f.Repository, scan.ID, 2, 1)
}

func TestBiosScanDoesNotOverwriteConcurrentManualInstallation(t *testing.T) {
	f := testsupport.Library(t)
	s := &Service{Repository: f.Repository, Now: time.Now}
	scan := model.Scan{ID: uuid.NewString(), ScanType: "bios", Status: "running", TotalKnown: true, TotalCount: 1, CreatedAtMs: 1000}
	if err := f.Repository.CreateScan(t.Context(), scan, f.Principal.User.ID); err != nil {
		t.Fatal(err)
	}
	automatic := model.BiosFile{ID: uuid.NewString(), RequirementKey: "firmware/gba_bios.bin", Filename: "scanned.bin", StorageKey: "bios/automatic", SHA256: "automatic", SizeBytes: 16}
	manual := automatic
	manual.ID, manual.StorageKey, manual.SHA256 = uuid.NewString(), "bios/manual", "manual"
	// Preparation happened first; a manual replacement committed before the automatic candidate transaction.
	installed := make(chan struct{})
	release := make(chan struct{})
	manualResult := make(chan error, 1)
	go func() {
		manualResult <- f.Repository.Transaction(t.Context(), func(tx *persistence.Repository) error {
			if err := tx.WriteBios(t.Context(), manual, 2000); err != nil {
				close(installed)
				return err
			}
			close(installed)
			<-release
			return nil
		})
	}()
	<-installed
	automaticResult := make(chan error, 1)
	go func() { automaticResult <- s.commitBios(t.Context(), &scan, automatic, nil) }()
	close(release)
	if err := <-manualResult; err != nil {
		t.Fatal(err)
	}
	if err := <-automaticResult; err != nil {
		t.Fatal(err)
	}
	current, err := f.Repository.Bios(t.Context(), manual.RequirementKey)
	if err != nil || current.ID != manual.ID || current.SHA256 != "manual" {
		t.Fatalf("current=%+v error=%v", current, err)
	}
	if scan.ImportedCount != 0 || scan.SkippedCount != 1 || scan.ProcessedCount != 1 {
		t.Fatalf("scan=%+v", scan)
	}
}

func assertCounts(t *testing.T, r *persistence.Repository, id string, processed, imported int64) {
	t.Helper()
	scan, err := r.Scan(t.Context(), id)
	if err != nil || scan.ProcessedCount != processed || scan.ImportedCount != imported {
		t.Fatalf("scan=%+v error=%v", scan, err)
	}
	if scan.ProcessedCount != scan.ImportedCount+scan.SkippedCount+scan.FailedCount {
		t.Fatalf("broken invariant: %+v", scan)
	}
}

func TestBiosDiscoveryCancellationKeepsProgressAndStopsWithoutSourceFailure(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "user cancel", true: "process shutdown"}[shutdown], func(t *testing.T) {
			f := testsupport.Library(t)
			source := t.TempDir()
			if err := os.WriteFile(filepath.Join(source, "unrelated.bin"), []byte("unrelated"), 0o600); err != nil {
				t.Fatal(err)
			}
			serviceContext, stop := context.WithCancel(t.Context())
			defer stop()
			if shutdown {
				stop()
			}
			s := &Service{
				Repository: f.Repository, Now: time.Now, Context: serviceContext,
				Sources: storage.Sources{},
			}
			scan := model.Scan{ID: uuid.NewString(), ScanType: "bios", Status: "running", CreatedAtMs: 1000}
			if err := f.Repository.CreateScan(t.Context(), scan, f.Principal.User.ID); err != nil {
				t.Fatal(err)
			}
			worker, cancel := context.WithCancel(t.Context())
			cancel()
			s.runBios(worker, scan, model.BiosScanInput{Path: source},
				[]runtimeclient.BiosRequirement{{RequirementKey: "firmware/gba_bios.bin", CoreID: "mgba"}})
			current, err := f.Repository.Scan(t.Context(), scan.ID)
			expected := "cancelled"
			if shutdown {
				expected = "interrupted"
			}
			if err != nil || current.Status != expected || current.Error != nil || current.ProcessedCount != 0 || current.ImportedCount != 0 {
				t.Fatalf("scan=%+v error=%v", current, err)
			}
		})
	}
}
