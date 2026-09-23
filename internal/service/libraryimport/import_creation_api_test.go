package libraryimport

import (
	"context"
	"errors"
	"testing"
)

type creationRepositoryProbe struct{ writes int }

func (repository *creationRepositoryProbe) WithCreation(
	_ context.Context,
	_ func(ImportCreationScope) error,
) error {
	repository.writes++
	return nil
}

func TestImportCreationIdentityFailurePrecedesWrite(t *testing.T) {
	cause := errors.New("creation identity entropy unavailable")
	for failure := 1; failure <= 6; failure++ {
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
		result, err := service.CommitPrepared(t.Context(), creationPreparedInput(), ImportCreationOptions{})
		if !errors.Is(err, cause) || result.Created != (ServerCreated{}) || result.Owned.Items != nil ||
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

func creationPreparedInput() PreparedImport {
	file := ImportFile{
		ID:     "file",
		Path:   "game.gba",
		BlobID: "blob",
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Size:   16,
	}
	return PreparedImport{
		Request: ImportRequest{
			UploadID:                 "upload",
			TargetPlatformInstanceID: "platform",
			MetadataProvider:         "NONE",
			ContentMode:              "STANDARD",
		},
		Upload: ImportUpload{
			ID:             "upload",
			Purpose:        "GENERAL",
			SourceType:     "FILES",
			State:          "COMPLETE",
			Version:        1,
			ManifestDigest: file.SHA256,
			FileCount:      1,
		},
		Target: ImportTarget{
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
		Files:        []ImportFile{file},
		Groups:       []PreparedGroup{{Sources: []PreparedSource{{File: file, Role: "CONTENT", LogicalName: file.Path}}}},
		Dispositions: []PreparedDisposition{{File: file, Disposition: "ACCEPTED"}},
	}
}
