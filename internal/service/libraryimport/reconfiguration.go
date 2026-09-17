package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	model "retrom/internal/model/libraryimport"

	"github.com/google/uuid"
)

type ReconfigurationRequest struct {
	SourceImportJobID      string
	ExpectedVersion        int64
	TargetPlatformInstance string
	MetadataProvider       string
	TagIDs                 []string
}

type ReconfigurationCreate func(context.Context, model.ImportRequest, model.ImportCreationOptions) (model.ImportCreationResult, error)

// Reconfigurations coordinates reusing rejected files with the normal import
// creation workflow. It contains no database implementation details.
type Reconfigurations struct {
	repository model.ReconfigurationRepository
	create     ReconfigurationCreate
	now        func() time.Time
	newID      func() (string, error)
}

func NewReconfigurations(
	repository model.ReconfigurationRepository,
	create ReconfigurationCreate,
	now func() time.Time,
) *Reconfigurations {
	if now == nil {
		now = time.Now
	}
	return &Reconfigurations{
		repository: repository,
		create:     create,
		now:        now,
		newID:      newReconfigurationID,
	}
}

func newReconfigurationID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("allocate reconfiguration upload ID: %w", err)
	}
	return id.String(), nil
}

func (service *Reconfigurations) Reconfigure(
	ctx context.Context,
	request ReconfigurationRequest,
) (model.ImportCreationResult, error) {
	if request.SourceImportJobID == "" || request.ExpectedVersion < 1 ||
		service.repository == nil || service.create == nil {
		return model.ImportCreationResult{}, model.ErrInvalid
	}
	source, found, err := service.repository.Source(
		ctx, request.SourceImportJobID, request.ExpectedVersion,
	)
	if err != nil {
		return model.ImportCreationResult{}, fmt.Errorf("read reconfiguration source: %w", err)
	}
	if !found || len(source.Files) == 0 {
		return model.ImportCreationResult{}, model.ErrInvalid
	}
	uploadID, err := service.newID()
	if err != nil {
		return model.ImportCreationResult{}, err
	}
	clone := model.ReconfigurationClone{
		UploadID:          uploadID,
		SourceImportJobID: request.SourceImportJobID,
		ExpectedVersion:   request.ExpectedVersion,
		SourceType:        source.SourceType,
		Files:             source.Files,
		ManifestDigest: ReconfigurationManifestDigest(
			request.SourceImportJobID, request.ExpectedVersion, source.Files,
		),
		NowMS: service.now().UnixMilli(),
	}
	if err := service.repository.Clone(ctx, clone); err != nil {
		return model.ImportCreationResult{}, fmt.Errorf("clone reconfiguration upload: %w", err)
	}
	created, err := service.create(ctx, model.ImportRequest{
		UploadID:                 uploadID,
		TargetPlatformInstanceID: request.TargetPlatformInstance,
		MetadataProvider:         request.MetadataProvider,
		TagIDs:                   request.TagIDs,
	}, model.ImportCreationOptions{Reconfiguration: &model.ImportReconfiguration{
		ImportID: request.SourceImportJobID,
		Version:  request.ExpectedVersion,
		FileIDs:  reconfigurationFileIDs(source.Files),
	}})
	if err != nil {
		_ = service.repository.RemoveUnused(context.WithoutCancel(ctx), uploadID)
		return model.ImportCreationResult{}, err
	}
	return created, nil
}

func reconfigurationFileIDs(files []model.PreparedReusableUploadFile) []string {
	result := make([]string, 0, len(files))
	for _, file := range files {
		result = append(result, file.ID)
	}
	return result
}

func ReconfigurationManifestDigest(
	sourceImportJobID string,
	sourceVersion int64,
	files []model.PreparedReusableUploadFile,
) string {
	manifestFiles := make([]map[string]any, 0, len(files))
	for _, file := range files {
		manifestFiles = append(manifestFiles, map[string]any{
			"sourceUploadFileId": file.ID,
			"relativePath":       file.Path,
			"sizeBytes":          file.Size,
			"blobId":             file.BlobID,
		})
	}
	manifest, _ := json.Marshal(map[string]any{
		"schemaVersion":     1,
		"sourceImportJobId": sourceImportJobID,
		"sourceVersion":     sourceVersion,
		"files":             manifestFiles,
	})
	digest := sha256.Sum256(manifest)
	return hex.EncodeToString(digest[:])
}
