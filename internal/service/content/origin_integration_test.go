//go:build integration

package content

import (
	"reflect"
	"strings"
	"testing"

	"retrom/internal/model"
	"retrom/internal/storage"
	"retrom/internal/testsupport"

	"github.com/google/uuid"
)

func TestPackExistingCopiesOriginalIntoNewOwnerAndPreservesIdentity(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "copy", true: "missing original"}[missing], func(t *testing.T) {
			s, f := packFixture(t)
			previous := setPackOriginal(t, s, f, missing)
			before, err := s.Repository.ContentSnapshot(t.Context(), f.Game.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, packErr := s.PackExisting(t.Context(), f.Game.ID, true)
			after, err := s.Repository.ContentSnapshot(t.Context(), f.Game.ID)
			if err != nil {
				t.Fatal(err)
			}
			if missing {
				if packErr == nil || !reflect.DeepEqual(before, after) {
					t.Fatal("missing original switched current storage")
				}
				return
			}
			if packErr != nil || len(after.Files) != 1 || after.ContentHash != before.ContentHash {
				t.Fatalf("storage conversion=%+v err=%v", after, packErr)
			}
			assertCopiedOriginal(t, s.Storage, f.Game.ID, before.Files[0].ID, previous, after.Files[0])
		})
	}
}

func packTestOriginal(t *testing.T, store *storage.Store, gameID, fileID string) *model.GameFileConversionInfo {
	t.Helper()
	operation := uuid.NewString()
	staged, err := store.Stage(t.Context(), operation, strings.NewReader("unconverted original"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.PublishOrigin(t.Context(), gameID, fileID, "raw.zip", staged)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.RemoveTemporary("temporary/" + operation); err != nil {
		t.Fatal(err)
	}
	return &model.GameFileConversionInfo{OriginInfo: info}
}

func setPackOriginal(t *testing.T, s *Service, f testsupport.Fixture, missing bool) *model.OriginGameInfo {
	t.Helper()
	old := f.Game.Files[0]
	old.ExtInfo.ConversionInfo = packTestOriginal(t, s.Storage, f.Game.ID, old.ID)
	if err := s.Repository.Execute(t.Context(), "UPDATE game_file_tab SET ext_info=$2 WHERE id=$1", old.ID, old.ExtInfo); err != nil {
		t.Fatal(err)
	}
	info := old.ExtInfo.ConversionInfo.OriginInfo
	if missing {
		if err := s.Storage.Remove(info.StorageKey); err != nil {
			t.Fatal(err)
		}
	}
	return info
}

func assertCopiedOriginal(t *testing.T, store *storage.Store, gameID, previousFileID string,
	previous *model.OriginGameInfo, file model.GameFile,
) {
	t.Helper()
	origin := file.ExtInfo.ConversionInfo.OriginInfo
	if origin.StorageKey == previous.StorageKey || origin.SHA256 != previous.SHA256 || origin.SizeBytes != previous.SizeBytes {
		t.Fatalf("original attachment shared or changed: %+v", origin)
	}
	if err := store.RemoveOrigin(gameID, previousFileID); err != nil {
		t.Fatal(err)
	}
	reader, err := store.Read(origin.StorageKey)
	if err != nil {
		t.Fatalf("new original lost with previous owner: %v", err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
}
