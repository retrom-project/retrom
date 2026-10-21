package scans

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"retrom/internal/model"
	"retrom/internal/storage"

	"github.com/google/uuid"
)

type memoryTemporary struct {
	mutex  sync.Mutex
	values map[string]string
}

func (m *memoryTemporary) Get(_ context.Context, key string) (string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	value, exists := m.values[key]
	if !exists {
		return "", model.ErrContextExpired
	}
	return value, nil
}

func (m *memoryTemporary) Set(_ context.Context, key, value string, _ time.Duration) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.values == nil {
		m.values = make(map[string]string)
	}
	m.values[key] = value
	return nil
}

func (m *memoryTemporary) Delete(_ context.Context, key string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.values, key)
	return nil
}

func uploadService(t *testing.T) (*Service, string) {
	t.Helper()
	directory := t.TempDir()
	uploads, err := storage.OpenUploads(directory)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Uploads: uploads, Redis: &memoryTemporary{}, Now: time.Now}, directory
}

func admin() model.Principal {
	return model.Principal{User: model.User{ID: uuid.NewString(), Role: "admin", Status: "active"}}
}

func TestUploadDeclarationEnforcesSafeTreesAndLimits(t *testing.T) {
	t.Parallel()
	file := func(name string, size int64) model.UploadFileInput {
		return model.UploadFileInput{Path: name, SizeBytes: size}
	}
	for _, test := range []struct {
		name  string
		files []model.UploadFileInput
		valid bool
	}{
		{"nested folders and empty files", []model.UploadFileInput{file("RPG/Game.exe", 10), file("RPG/data/a.dat", 0)}, true},
		{"largest file", []model.UploadFileInput{file("disc.iso", model.UploadMaximumFileBytes)}, true},
		{"nothing", nil, false},
		{"duplicate path", []model.UploadFileInput{file("a.nes", 1), file("a.nes", 1)}, false},
		{"parent traversal", []model.UploadFileInput{file("../a.nes", 1)}, false},
		{"absolute path", []model.UploadFileInput{file("/a.nes", 1)}, false},
		{"unclean path", []model.UploadFileInput{file("a//b.nes", 1)}, false},
		{"file used as directory", []model.UploadFileInput{file("a", 1), file("a/b.nes", 1)}, false},
		{"file above limit", []model.UploadFileInput{file("disc.iso", model.UploadMaximumFileBytes+1)}, false},
		{"session above limit", []model.UploadFileInput{
			file("1.iso", model.UploadMaximumFileBytes), file("2.iso", model.UploadMaximumFileBytes),
			file("3.iso", model.UploadMaximumFileBytes), file("4.iso", model.UploadMaximumFileBytes),
			file("5.iso", model.UploadMaximumFileBytes), file("6.iso", 1),
		}, false},
		{"deeper than a scan walks", []model.UploadFileInput{file(strings.Repeat("d/", 25)+"a.nes", 1)}, false},
		{"negative size", []model.UploadFileInput{file("a.nes", -1)}, false},
	} {
		_, err := validateUploadFiles(test.files)
		if (err == nil) != test.valid {
			t.Errorf("%s: error=%v", test.name, err)
		}
	}
}

func declareUpload(t *testing.T, s *Service, owner model.Principal) model.Upload {
	t.Helper()
	upload, err := s.CreateUpload(t.Context(), owner, model.UploadInput{Files: []model.UploadFileInput{
		{Path: "Super Game.nes", SizeBytes: 4}, {Path: "Super Game.png", SizeBytes: 3}, {Path: "Quest/GAME.EXE", SizeBytes: 2},
	}})
	if err != nil || upload.Completed || len(upload.Files) != 3 || upload.ChunkSizeBytes != model.UploadChunkBytes {
		t.Fatalf("upload=%+v error=%v", upload, err)
	}
	return upload
}

func TestUploadSessionIsPrivateToItsAdministrator(t *testing.T) {
	t.Parallel()
	s, _ := uploadService(t)
	owner, other := admin(), admin()
	upload := declareUpload(t, s, owner)
	if _, err := s.Upload(t.Context(), other, upload.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("another administrator must not see the upload: %v", err)
	}
	if _, err := s.WriteUpload(t.Context(), other, upload.ID, 0, 0, strings.NewReader("NES\x1a")); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("another administrator must not write: %v", err)
	}
	if err := s.DeleteUpload(t.Context(), other, upload.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("another administrator must not cancel: %v", err)
	}
	if _, err := s.Upload(t.Context(), owner, upload.ID); err != nil {
		t.Fatal(err)
	}
}

func TestUploadIsScannableOnlyWhenCompleteAndCancelRemovesIt(t *testing.T) {
	t.Parallel()
	s, directory := uploadService(t)
	owner := admin()
	upload := declareUpload(t, s, owner)
	inspect := model.SourceInput{UploadID: upload.ID, Format: "files"}
	if _, err := s.Inspect(t.Context(), owner, inspect); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("an incomplete upload is not a source: %v", err)
	}
	for index, content := range []string{"NES\x1a", "png", "MZ"} {
		progress, err := s.WriteUpload(t.Context(), owner, upload.ID, index, 0, strings.NewReader(content))
		if err != nil || !progress.Completed {
			t.Fatalf("file %d progress=%+v error=%v", index, progress, err)
		}
	}
	items, err := s.Inspect(t.Context(), owner, inspect)
	if err != nil || len(items) != 1 || items[0].GameCount != 2 || items[0].Key != "." {
		t.Fatalf("inspected=%+v error=%v", items, err)
	}
	both := model.SourceInput{Path: "/data", UploadID: upload.ID, Format: "files"}
	if _, err = s.Inspect(t.Context(), owner, both); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("a request naming two sources error=%v", err)
	}
	if err = s.DeleteUpload(t.Context(), owner, upload.ID); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(directory, upload.ID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cancel removes staging: %v", statErr)
	}
}

func TestUploadSessionsAreLimitedToTwoActiveUploads(t *testing.T) {
	t.Parallel()
	s, _ := uploadService(t)
	owner := admin()
	declare := model.UploadInput{Files: []model.UploadFileInput{{Path: "a.nes", SizeBytes: 1}}}
	first, err := s.CreateUpload(t.Context(), owner, declare)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateUpload(t.Context(), owner, declare); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateUpload(t.Context(), owner, declare); !errors.Is(err, model.ErrUploadLimit) {
		t.Fatalf("third upload error=%v", err)
	}
	if err = s.DeleteUpload(t.Context(), owner, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateUpload(t.Context(), owner, declare); err != nil {
		t.Fatalf("a cancelled upload frees its slot: %v", err)
	}
}

func TestUploadCleanupRemovesOnlyAbandonedStaging(t *testing.T) {
	t.Parallel()
	s, directory := uploadService(t)
	owner := admin()
	declare := model.UploadInput{Files: []model.UploadFileInput{{Path: "a.nes", SizeBytes: 1}}}
	live, err := s.CreateUpload(t.Context(), owner, declare)
	if err != nil {
		t.Fatal(err)
	}
	staged := func(id string) bool {
		_, statErr := os.Stat(filepath.Join(directory, id))
		return statErr == nil
	}
	abandoned, recent, scanning := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{abandoned, recent, scanning} {
		if err = s.Uploads.Create(id, declare.Files); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * uploadWindow)
	for _, id := range []string{live.ID, abandoned, scanning} {
		if err = os.Chtimes(filepath.Join(directory, id), old, old); err != nil {
			t.Fatal(err)
		}
	}
	s.uploadState.scanned = map[string]bool{scanning: true}
	if err = s.CleanupUploads(t.Context()); err != nil {
		t.Fatal(err)
	}
	if staged(abandoned) || !staged(live.ID) || !staged(recent) || !staged(scanning) {
		t.Fatalf("abandoned=%v live=%v recent=%v scanning=%v",
			staged(abandoned), staged(live.ID), staged(recent), staged(scanning))
	}
}

func TestLooseFilesSourceMakesEachTopLevelEntryAGameWithSameNameCover(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for name, content := range map[string]string{
		"Super Game (Japan).zip": "zip", "Super Game (Japan).PNG": "png", "Bar.nes": "nes",
		"Lonely.webp": "image without a game", ".DS_Store": "hidden", "Quest/Game.exe": "MZ", "Quest/www/data.json": "{}",
	} {
		target := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	collection, err := parseFiles(root, directory)
	if err != nil {
		t.Fatal(err)
	}
	type game struct{ title, file, cover string }
	got := make([]game, 0, len(collection.Games))
	for _, candidate := range collection.Games {
		got = append(got, game{candidate.Input.Title, candidate.Files[0], candidate.Cover})
	}
	want := []game{
		{"Bar", "Bar.nes", ""},
		{"Lonely", "Lonely.webp", ""},
		{"Quest", "Quest", ""},
		{"Super Game (Japan)", "Super Game (Japan).zip", "Super Game (Japan).PNG"},
	}
	if len(got) != len(want) || collection.Entry.GameCount != 4 || collection.Entry.Path != directory {
		t.Fatalf("games=%+v entry=%+v", got, collection.Entry)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("game %d=%+v want %+v", index, got[index], want[index])
		}
	}
}
