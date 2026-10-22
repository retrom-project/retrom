//go:build integration

package files

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/storage"
	"retrom/internal/testsupport"
)

func TestRetiredFilesKeepGraceDespiteOldPhysicalMtime(t *testing.T) {
	store := retirementStore(t)
	now := time.Now().Truncate(time.Millisecond)
	retiredAt := now
	r, retired, current := retiredFixture(t, store, now)
	if selected, err := r.RetiredFiles(t.Context(), now.Add(-24*time.Hour).UnixMilli()); err != nil || len(selected) != 0 {
		t.Fatalf("fresh retirement selected before its grace: files=%v err=%v", selected, err)
	}
	orphan := retirementPayload(t, store, "games", uuid.NewString(), now.Add(-7*24*time.Hour))
	service := &Service{Repository: r, Storage: store, Now: func() time.Time { return now }, Grace: 24 * time.Hour}
	for _, elapsed := range []time.Duration{0, 23 * time.Hour, 25 * time.Hour} {
		now = retiredAt.Add(elapsed)
		if err := service.Sweep(t.Context()); err != nil {
			t.Fatal(err)
		}
		sweepOrphans(t, service)
		for name, file := range retired {
			if got, want := exists(t, store, file.Key), elapsed < 24*time.Hour; got != want {
				t.Errorf("%s exists=%v at elapsed %s: retirement grace must use database time, not old physical mtime", name, got, elapsed)
			}
		}
		if exists(t, store, orphan.Key) {
			t.Error("an old file without a database owner survived cleanup")
		}
		if !exists(t, store, current.Key) {
			t.Error("current replacement bytes were removed")
		}
	}
}

func retiredFixture(t *testing.T, store *storage.Store, now time.Time) (*persistence.Repository, map[string]storage.File, storage.File) {
	t.Helper()
	f := testsupport.Library(t)
	old := now.Add(-7 * 24 * time.Hour)
	content := retirementPayload(t, store, "games", f.Game.ID, old)
	f.Game.Files[0].StorageKey = content.Key
	f.Game.Files[0].SHA256 = content.SHA256
	f.Game.Files[0].SizeBytes = content.Size
	f.Publish(t)
	media := retirementPayload(t, store, "games", f.Game.ID, old)
	mediaID := uuid.NewString()
	bios := retirementPayload(t, store, "bios", uuid.NewString(), old)
	biosID := uuid.NewString()
	saveID := uuid.NewString()
	payload := retirementPayload(t, store, "saves", saveID, old)
	screenshot := retirementPayload(t, store, "saves", saveID, old)
	err := f.Repository.Transaction(t.Context(), func(r *persistence.Repository) error {
		if err := r.InsertMedia(t.Context(), f.Game.ID, model.Media{
			ID: mediaID, Kind: "cover",
			StorageKey: media.Key, SHA256: media.SHA256, SizeBytes: media.Size, MediaType: "image/png",
		}, old.UnixMilli()); err != nil {
			return err
		}
		if err := r.WriteBios(t.Context(), model.BiosFile{
			ID: biosID, RequirementKey: "firmware/test.bin",
			Filename: "test.bin", StorageKey: bios.Key, SHA256: bios.SHA256, SizeBytes: bios.Size,
		}, old.UnixMilli()); err != nil {
			return err
		}
		return r.WriteSave(t.Context(), model.Save{
			ID: saveID, UserID: f.Principal.User.ID, Game: model.Game{ID: f.Game.ID},
			Kind: "checkpoint", Name: "Retired save", LastCommitID: uuid.NewString(), StorageKey: payload.Key,
			PayloadHash: payload.SHA256, SizeBytes: payload.Size, ScreenshotKey: screenshot.Key,
		}, old.UnixMilli(), true)
	})
	if err != nil {
		t.Fatal(err)
	}
	replacement := f.Game
	replacement.Input.Version = 2
	current := retirementPayload(t, store, "games", f.Game.ID, now)
	replacement.Files = []model.GameFile{{
		ID: uuid.NewString(), LogicalKey: "game.nes", Role: "content",
		StorageKey: current.Key, SizeBytes: current.Size, SHA256: current.SHA256,
	}}
	err = f.Repository.Transaction(t.Context(), func(r *persistence.Repository) error {
		if err := r.ReplaceFiles(t.Context(), f.Game.ID, replacement, now.UnixMilli()); err != nil {
			return err
		}
		if err := r.DeleteMedia(t.Context(), f.Game.ID, mediaID, now.UnixMilli()); err != nil {
			return err
		}
		if err := r.DeleteBios(t.Context(), "firmware/test.bin", now.UnixMilli()); err != nil {
			return err
		}
		return r.DeleteSave(t.Context(), f.Principal.User.ID, saveID, 1, now.UnixMilli())
	})
	if err != nil {
		t.Fatal(err)
	}
	return f.Repository, map[string]storage.File{"game": content, "media": media, "bios": bios, "save": payload, "screenshot": screenshot}, current
}

func TestFreshStagedHardlinksSurviveCleanupOfOldSource(t *testing.T) {
	r := testsupport.Database(t)
	store := retirementStore(t)
	now := time.Now().Truncate(time.Millisecond)
	source := retirementPayload(t, store, "bios", uuid.NewString(), now.Add(-7*24*time.Hour))
	if err := r.WriteBios(t.Context(), model.BiosFile{
		ID: uuid.NewString(), RequirementKey: "firmware/staged.bin",
		Filename: "staged.bin", StorageKey: source.Key, SHA256: source.SHA256, SizeBytes: source.Size,
	}, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	tree, _, err := store.StageTree(t.Context(), []model.ContentFile{{
		LogicalKey: "nested/staged.bin",
		StorageKey: source.Key, SHA256: source.SHA256, SizeBytes: source.Size,
	}})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Repository: r, Storage: store, Now: func() time.Time { return now }, Grace: 24 * time.Hour}
	sweepOrphans(t, service)
	if !exists(t, store, tree+"/nested/staged.bin") {
		t.Fatal("fresh staged hardlink was reclaimed using its old source inode mtime")
	}
	if !exists(t, store, source.Key) {
		t.Fatal("active source was removed")
	}
	now = now.Add(25 * time.Hour)
	sweepOrphans(t, service)
	if exists(t, store, tree+"/nested/staged.bin") || !exists(t, store, source.Key) {
		t.Fatal("abandoned staging must expire without changing the active source")
	}
}

func TestOrphanSweepDoesNotBypassRetirementBatchLimit(t *testing.T) {
	r := testsupport.Database(t)
	store := retirementStore(t)
	now := time.Now().Truncate(time.Millisecond)
	old := now.Add(-48 * time.Hour)
	keys := make([]string, 0, 105)
	for i := range 105 {
		file := retirementPayload(t, store, "bios", uuid.NewString(), old)
		requirement := fmt.Sprintf("firmware/%03d.bin", i)
		if err := r.WriteBios(t.Context(), model.BiosFile{
			ID: uuid.NewString(), RequirementKey: requirement,
			Filename: "firmware.bin", StorageKey: file.Key, SHA256: file.SHA256, SizeBytes: file.Size,
		}, old.UnixMilli()); err != nil {
			t.Fatal(err)
		}
		if err := r.DeleteBios(t.Context(), requirement, old.UnixMilli()); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, file.Key)
	}
	service := &Service{Repository: r, Storage: store, Now: func() time.Time { return now }, Grace: 24 * time.Hour}
	for _, expected := range []int64{5, 0} {
		if err := service.Sweep(t.Context()); err != nil {
			t.Fatal(err)
		}
		sweepOrphans(t, service)
		remaining, err := r.Count(t.Context(), "SELECT count(*) FROM bios_file_tab WHERE status='deleted'")
		var physical int64
		for _, key := range keys {
			if exists(t, store, key) {
				physical++
			}
		}
		if err != nil || remaining != expected || physical != expected {
			t.Fatalf("retirement batch bypassed: deleted=%d physical=%d expected=%d err=%v", remaining, physical, expected, err)
		}
	}
}

func retirementStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func retirementPayload(t *testing.T, store *storage.Store, kind, owner string, at time.Time) storage.File {
	t.Helper()
	file, err := store.Write(t.Context(), kind, owner, strings.NewReader("immutable bytes"), 100)
	if err != nil {
		t.Fatal(err)
	}
	age(t, store, file.Key, at)
	return file
}
