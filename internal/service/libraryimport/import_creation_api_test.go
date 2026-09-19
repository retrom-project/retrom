package libraryimport

import (
	"context"
	"errors"
	"testing"

	model "retrom/internal/model/libraryimport"
)

type creationRepositoryProbe struct{ writes int }

func (repository *creationRepositoryProbe) WithCreation(
	_ context.Context,
	_ func(model.ImportCreationScope) error,
) error {
	repository.writes++
	return nil
}

func TestImportCreationIdentityFailurePrecedesWrite(t *testing.T) {
	cause := errors.New("creation identity entropy unavailable")
	for failure := 1; failure <= 7; failure++ {
		repository := &creationRepositoryProbe{}
		service := NewImportCreations(repository, nil, nil, nil, ImportCreationSettings{})
		calls := 0
		service.newID = func() (string, error) {
			calls++
			if calls == failure {
				return "", cause
			}
			return "identity", nil
		}
		result, err := service.CommitPrepared(t.Context(), creationPreparedInput(), model.ImportCreationOptions{})
		if !errors.Is(err, cause) || result.Created != (model.ServerCreated{}) || result.Owned.Items != nil ||
			calls != failure || repository.writes != 0 {
			t.Fatalf(
				"identity %d failure: result=%+v calls=%d writes=%d error=%v",
				failure,
				result,
				calls,
				repository.writes,
				err,
			)
		}
	}
}

func creationPreparedInput() model.PreparedImport {
	file := model.ImportFile{
		ID:     "file",
		Path:   "game.gba",
		BlobID: "blob",
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Size:   16,
	}
	return model.PreparedImport{
		Request: model.ImportRequest{
			UploadID:                 "upload",
			TargetPlatformInstanceID: "platform",
			MetadataProvider:         "NONE",
			ContentMode:              "STANDARD",
		},
		Upload: model.ImportUpload{
			ID:             "upload",
			Purpose:        "GENERAL",
			SourceType:     "FILES",
			State:          "COMPLETE",
			Version:        1,
			ManifestDigest: file.SHA256,
			FileCount:      1,
		},
		Target: model.ImportTarget{
			ID:            "platform",
			PlatformID:    "gba",
			DefaultCoreID: "mgba",
			CoreID:        "mgba",
			BindingID:     "mgba",
			ProviderID:    "provider",
			TargetID:      "target",
			Version:       1,
		},
		ContentMode:  "STANDARD",
		SourceType:   "FILES",
		Files:        []model.ImportFile{file},
		Groups:       []model.PreparedGroup{{Sources: []model.PreparedSource{{File: file, Role: "CONTENT", LogicalName: file.Path}}}},
		Dispositions: []model.PreparedDisposition{{File: file, Disposition: "ACCEPTED"}},
	}
}
