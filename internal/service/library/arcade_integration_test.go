//go:build integration

package library

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"
	"retrom/internal/testsupport"
)

func TestParentAttachmentAndPublishedReplacementPreserveGameAndOtherCore(t *testing.T) {
	t.Parallel()
	s, f, directory := parentFixture(t)
	before, err := s.Detail(t.Context(), f.Principal, f.Game.ID, "pending_review")
	if err != nil {
		t.Fatal(err)
	}
	projection, err := s.ArcadeParentOptions(t.Context(), f.Principal, f.Game.ID, "fbneo")
	if err != nil || !reflect.DeepEqual(projection.MissingParents, []string{"1941.zip"}) {
		t.Fatalf("projection=%+v error=%v", projection, err)
	}
	for _, name := range []string{"unrelated.zip", "1941j.zip"} {
		if _, err = s.UploadParent(t.Context(), f.Principal, f.Game.ID, "fbneo", name, 1,
			bytes.NewReader(parentZIP(t, "wrong"))); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("invalid attachment=%v", err)
		}
		assertParentFiles(t, directory, f.Game.ID, 2)
	}
	// A structurally valid but incomplete parent is saved without a launch-readiness gate.
	first, err := s.UploadParent(t.Context(), f.Principal, f.Game.ID, "fbneo", "1941.zip", 1,
		bytes.NewReader(parentZIP(t, "first replacement")))
	if err != nil {
		t.Fatal(err)
	}
	assertParentChange(t, before, first, "pending_review")
	projection, err = s.ArcadeParentOptions(t.Context(), f.Principal, f.Game.ID, "fbneo")
	if err != nil || !reflect.DeepEqual(projection.MissingParents, []string{"1941.zip"}) {
		t.Fatalf("incomplete parent must remain explicit: %+v error=%v", projection, err)
	}
	published, err := s.Approve(t.Context(), f.Principal, f.Game.ID, first.Game.Version)
	if err != nil {
		t.Fatal(err)
	}
	replaced, err := s.UploadParent(t.Context(), f.Principal, f.Game.ID, "fbneo", "1941.zip", published.Game.Version,
		bytes.NewReader(parentZIP(t, "second replacement")))
	if err != nil {
		t.Fatal(err)
	}
	assertParentChange(t, published, replaced, "published")
	assertParentFiles(t, directory, f.Game.ID, 4)
	committed, err := s.confirmParent(t.Context(), f.Game.ID, parentFile(t, replaced, "1941.zip").ID, persistence.ErrCommitUncertain)
	if err != nil || !committed {
		t.Fatalf("allocated file commit confirmation=%t error=%v", committed, err)
	}
	committed, err = s.confirmParent(t.Context(), f.Game.ID, uuid.NewString(), persistence.ErrCommitUncertain)
	if committed || !errors.Is(err, persistence.ErrCommitUncertain) {
		t.Fatalf("uncommitted allocation=%t error=%v", committed, err)
	}
}

func TestParentAttachmentCASConflictCleansOnlyNewFile(t *testing.T) {
	t.Parallel()
	s, f, directory := parentFixture(t)
	before, err := s.Detail(t.Context(), f.Principal, f.Game.ID, "pending_review")
	if err != nil {
		t.Fatal(err)
	}
	reader := &parentRaceReader{Reader: bytes.NewReader(parentZIP(t, "racing replacement")), bump: func() error {
		return f.Repository.BumpGame(t.Context(), f.Game.ID, 1, 2000)
	}}
	if _, err := s.UploadParent(t.Context(), f.Principal, f.Game.ID, "fbneo", "1941.zip", 1, reader); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("concurrent version change=%v", err)
	}
	assertParentFiles(t, directory, f.Game.ID, 2)
	old, err := s.Detail(t.Context(), f.Principal, f.Game.ID, "pending_review")
	if err != nil || old.Game.Version != 2 || old.Game.ContentHash != f.Game.ContentHash ||
		!reflect.DeepEqual(old.Files, before.Files) || !bytes.Equal(old.RuntimeConfig, before.RuntimeConfig) {
		t.Fatalf("CAS failure changed existing content: %+v error=%v", old, err)
	}
}

func TestParentInsertFailureRollsBackVersionConfigAndRetirement(t *testing.T) {
	t.Parallel()
	s, f, _ := parentFixture(t)
	before, err := s.Detail(t.Context(), f.Principal, f.Game.ID, "pending_review")
	if err != nil {
		t.Fatal(err)
	}
	file := before.Files[0]
	file.ID = before.Files[1].ID
	err = f.Repository.Transaction(t.Context(), func(r *persistence.Repository) error {
		return r.ChangeParent(t.Context(), f.Game.ID, "pending_review", 1, file,
			json.RawMessage(`{"content":{"kind":"ARCADE","entryFile":"1941j.zip"}}`), "changed", 2000)
	})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("file identity collision=%v", err)
	}
	after, err := s.Detail(t.Context(), f.Principal, f.Game.ID, "pending_review")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("failed INSERT left partial game/retirement: %+v error=%v", after, err)
	}
}

type parentRaceReader struct {
	io.Reader
	bump func() error
}

func (r *parentRaceReader) Read(p []byte) (int, error) {
	if r.bump != nil {
		bump := r.bump
		r.bump = nil
		if err := bump(); err != nil {
			return 0, err
		}
	}
	return r.Reader.Read(p)
}

func parentFixture(t *testing.T) (*Service, testsupport.Fixture, string) {
	t.Helper()
	f := testsupport.Library(t)
	f.Directory.PlatformID, f.Directory.DefaultCoreID = "arcade", "fbneo"
	f.Directory.CoreIDs = []string{"fbneo", "mame2003"}
	f.Directory.Version = 1
	if err := f.Repository.WriteDirectory(t.Context(), f.DirectoryID, f.Directory, 1000, false); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	store, err := storage.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	f.Game.Files = []model.GameFile{}
	for _, name := range []string{"1941.zip", "1941j.zip"} {
		file, writeErr := store.Write(t.Context(), "games", f.Game.ID, bytes.NewReader(parentZIP(t, name)), storage.MaximumFileSize)
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		f.Game.Files = append(f.Game.Files, model.GameFile{
			ID: uuid.NewString(), LogicalKey: name, Role: "content",
			StorageKey: file.Key, SizeBytes: file.Size, SHA256: file.SHA256,
		})
	}
	f.Game.Input.RuntimeConfig = json.RawMessage(`{"content":{"kind":"ARCADE","entryFile":"1941j.zip"},"cores":{"mame2003":{"parentFiles":["1941.zip"],"options":{}}}}`)
	if err = f.Repository.CreateGame(t.Context(), f.Principal.User.ID, f.Game, 1000); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(os.Getenv("NODE_HOME"), "bin", "node")
	if os.Getenv("NODE_HOME") == "" {
		node = "node"
	}
	client, err := runtimeclient.Open(t.Context(), os.Getenv("RETROM_RUNTIME_TOOL_INPUT"), node, os.Getenv("RETROM_PROVIDER_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	client.Locate = store.Absolute
	return &Service{Repository: f.Repository, Runtime: client, Storage: store, Now: time.Now}, f, directory
}

func parentZIP(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	file, err := w.Create("fixture.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assertParentFiles(t *testing.T, directory, id string, count int) {
	t.Helper()
	files, err := os.ReadDir(filepath.Join(directory, "games", id))
	if err != nil || len(files) != count {
		t.Fatalf("owned files=%d want=%d error=%v", len(files), count, err)
	}
}

func assertParentChange(t *testing.T, before, after model.GameDetail, status string) {
	t.Helper()
	if after.Game.Version != before.Game.Version+1 || after.Game.Status != status || len(after.Files) != 2 ||
		after.Game.ContentHash == before.Game.ContentHash ||
		parentFile(t, after, "1941j.zip") != parentFile(t, before, "1941j.zip") ||
		parentFile(t, after, "1941.zip").ID == parentFile(t, before, "1941.zip").ID {
		t.Fatalf("parent change replaced unrelated facts: %+v", after)
	}
	var previous, current map[string]any
	if err := json.Unmarshal(before.RuntimeConfig, &previous); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after.RuntimeConfig, &current); err != nil {
		t.Fatal(err)
	}
	previousCores, previousOK := previous["cores"].(map[string]any)
	currentCores, currentOK := current["cores"].(map[string]any)
	if !previousOK || !currentOK {
		t.Fatal("expected explicit core configuration")
	}
	if !reflect.DeepEqual(previous["content"], current["content"]) ||
		!reflect.DeepEqual(previousCores["mame2003"], currentCores["mame2003"]) ||
		!reflect.DeepEqual(currentCores["fbneo"], map[string]any{"parentFiles": []any{"1941.zip"}}) {
		t.Fatalf("core configuration changed: %s", after.RuntimeConfig)
	}
}

func parentFile(t *testing.T, detail model.GameDetail, key string) model.GameFile {
	t.Helper()
	for _, file := range detail.Files {
		if file.LogicalKey == key {
			return file
		}
	}
	t.Fatalf("game file %q missing", key)
	return model.GameFile{}
}
