package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
)

func TestStageTreeLinksStandaloneFilesAndCopiesPackageMembers(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := s.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	id := uuid.NewString()
	staged, err := s.Stage(t.Context(), id, strings.NewReader("standalone"))
	if err != nil {
		t.Fatal(err)
	}
	standalone := model.ContentFile{LogicalKey: "standalone.bin", StorageKey: staged.Key, SHA256: staged.SHA256, SizeBytes: staged.Size}
	packed, err := s.BuildPackage(t.Context(), id, []model.ContentFile{{
		LogicalKey: "member.bin", StorageKey: staged.Key, SHA256: staged.SHA256, SizeBytes: staged.Size,
	}})
	if err != nil {
		t.Fatal(err)
	}
	member := model.ContentFile{
		LogicalKey: "dir/member.bin", StorageKey: packed.StorageKey, Offset: packed.Members[0].DataOffset,
		SHA256: staged.SHA256, SizeBytes: staged.Size, Package: &packed,
	}
	key, tree, err := s.StageTree(t.Context(), []model.ContentFile{standalone, member})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if removeErr := s.RemoveTemporary(key); removeErr != nil {
			t.Error(removeErr)
		}
	}()
	source, err := s.Absolute(staged.Key)
	if err != nil {
		t.Fatal(err)
	}
	if !sameFile(t, source, filepath.Join(tree, "standalone.bin")) {
		t.Fatal("standalone file was copied instead of linked")
	}
	copied := filepath.Join(tree, "dir", "member.bin")
	bytes, err := os.ReadFile(copied)
	if err != nil || string(bytes) != "standalone" || sameFile(t, source, copied) {
		t.Fatalf("package member was not copied as its logical bytes: %q %v", bytes, err)
	}
}

func sameFile(t *testing.T, left, right string) bool {
	t.Helper()
	a, err := os.Stat(left)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(right)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(a, b)
}
