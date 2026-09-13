package libraryimport

import (
	"context"
	"fmt"
	"sort"
	"strings"

	librarypersistence "retrom/internal/repo/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"

	"retrom/internal/capability/security/authn"
)

type ServerSourceFile = libraryservice.ServerSourceFile

const ServerSourceFileLimit = libraryservice.ServerSourceFileLimit

const (
	reviewHandoffDirect           = "DIRECT"
	reviewHandoffEmulationStation = "EMULATIONSTATION"
)

type (
	ServerImportItem     = libraryservice.ServerImportItem
	ServerDuplicateMatch = libraryservice.ServerDuplicateMatch
	ServerImportResult   = libraryservice.ServerImportResult
)

type (
	ServerMetadata        = libraryservice.ServerMetadata
	ServerMetadataWarning = libraryservice.ServerMetadataWarning
)

// CreateServerSource adopts already verified CAS blobs into the established
// import/content-profile pipeline. It creates an internal COMPLETE upload
// envelope so all archive, DAT, BIOS, multi-disc, and duplicate invariants stay
// identical to browser imports.
func (service *Service) CreateServerSource(
	ctx context.Context,
	targetPlatformInstanceID, contentMode string,
	files []ServerSourceFile,
	tagIDs []string,
	assignedByUserID string,
) (ServerImportResult, error) {
	return service.createServerSource(
		ctx, "", targetPlatformInstanceID, contentMode, files, tagIDs, assignedByUserID,
	)
}

// CreateServerSourceOnce gives a server-owned source item an idempotent handoff
// into the ordinary import pipeline. Repeating the same key and frozen inputs
// returns the original import instead of creating a second hidden review.
func (service *Service) CreateServerSourceOnce(
	ctx context.Context,
	idempotencyKey, targetPlatformInstanceID, contentMode string,
	files []ServerSourceFile,
	tagIDs []string,
	assignedByUserID string,
) (ServerImportResult, error) {
	if idempotencyKey == "" {
		return ServerImportResult{}, ErrInvalid
	}
	return service.createServerSource(
		ctx, idempotencyKey, targetPlatformInstanceID, contentMode, files, tagIDs, assignedByUserID,
	)
}

func (service *Service) createServerSource(
	ctx context.Context,
	idempotencyKey, targetPlatformInstanceID, contentMode string,
	files []ServerSourceFile,
	tagIDs []string,
	assignedByUserID string,
) (ServerImportResult, error) {
	prepared, err := service.prepareServerSource(ctx, idempotencyKey, contentMode, files)
	if err != nil {
		return ServerImportResult{}, err
	}
	target, err := service.loadCreationTarget(ctx, targetPlatformInstanceID)
	if err != nil {
		return ServerImportResult{}, err
	}
	prepared.contentMode = normalizeTargetContentMode(target.PlatformID, prepared.contentMode)
	created, found, err := service.ensureServerSourceUpload(ctx, prepared, targetPlatformInstanceID)
	if err != nil {
		return ServerImportResult{}, err
	}
	if found {
		return service.serverImportResult(ctx, created)
	}
	return service.createPreparedServerSource(
		ctx, prepared, targetPlatformInstanceID, tagIDs, assignedByUserID,
	)
}

func (service *Service) createPreparedServerSource(
	ctx context.Context,
	prepared preparedServerSource,
	targetPlatformInstanceID string,
	tagIDs []string,
	assignedByUserID string,
) (ServerImportResult, error) {
	if len(tagIDs) > 0 {
		ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: assignedByUserID})
	}
	created, err := service.create(ctx, CreateRequest{
		UploadID: prepared.uploadID, TargetPlatformInstanceID: targetPlatformInstanceID,
		MetadataProvider: "NONE", ContentMode: prepared.contentMode, TagIDs: tagIDs,
	}, nil, creationOptions{reviewHandoffKind: prepared.reviewHandoffKind()})
	if err != nil {
		created, found, lookupErr := service.serverSourceCreation(
			ctx, prepared.uploadID, targetPlatformInstanceID, prepared.contentMode,
		)
		if prepared.idempotent && lookupErr == nil && found {
			return service.serverImportResult(ctx, created)
		}
		service.removeUnusedClonedUpload(ctx, prepared.uploadID)
		return ServerImportResult{}, err
	}
	return service.serverImportResult(ctx, created)
}

func (service *Service) validateServerFiles(
	ctx context.Context,
	files []ServerSourceFile,
) ([]ServerSourceFile, []reusableUploadFile, int64, error) {
	sorted := append([]ServerSourceFile(nil), files...)
	sort.SliceStable(sorted, func(left, right int) bool {
		return sorted[left].RelativePath < sorted[right].RelativePath
	})
	reusable := make([]reusableUploadFile, 0, len(sorted))
	seen := make(map[string]struct{}, len(sorted))
	var totalBytes int64
	for index, file := range sorted {
		folded := strings.ToLower(file.RelativePath)
		if file.RelativePath == "" || file.BlobID == "" || file.SizeBytes < 0 {
			return nil, nil, 0, ErrInvalid
		}
		if _, exists := seen[folded]; exists {
			return nil, nil, 0, ErrInvalid
		}
		size, found, err := librarypersistence.NewServerSourceUploads(service.database).BlobSize(ctx, file.BlobID)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("read reusable upload blob: %w", err)
		}
		if !found {
			return nil, nil, 0, ErrInvalid
		}
		if size != file.SizeBytes {
			return nil, nil, 0, ErrInvalid
		}
		seen[folded] = struct{}{}
		totalBytes += file.SizeBytes
		reusable = append(reusable, reusableUploadFile{
			ID: fmt.Sprintf("server-%d", index), Path: file.RelativePath, BlobID: file.BlobID, Size: file.SizeBytes,
		})
	}
	return sorted, reusable, totalBytes, nil
}

func (service *Service) insertServerUpload(
	ctx context.Context,
	uploadID, sourceType string,
	files []reusableUploadFile,
	digest string,
	now, _ int64,
	ownerKind, ownerItemID string,
) error {
	if err := librarypersistence.NewServerSourceUploads(service.database).Insert(
		ctx, uploadID, sourceType, files, digest, now, ownerKind, ownerItemID,
	); err != nil {
		return fmt.Errorf("insert reusable server upload: %w", err)
	}
	return nil
}

func (service *Service) serverImportResult(ctx context.Context, created Created) (ServerImportResult, error) {
	result, err := librarypersistence.BindSourceResults(service.database).Read(ctx, created)
	if err != nil {
		return ServerImportResult{}, fmt.Errorf("read server source result: %w", err)
	}
	return result, nil
}

func (service *Service) SeedServerReviewMetadata(
	ctx context.Context,
	importItemID string,
	metadata ServerMetadata,
) (int64, []ServerMetadataWarning, error) {
	return service.SeedServerReviewMetadataAtYear(
		ctx, importItemID, metadata, service.now().UTC().Year()+1,
	)
}

func (service *Service) SeedServerReviewMetadataAtYear(
	ctx context.Context,
	importItemID string,
	metadata ServerMetadata,
	maximumYear int,
) (int64, []ServerMetadataWarning, error) {
	version, warnings, err := libraryservice.NewMetadataSeeder(
		librarypersistence.NewMetadata(service.database), service.now,
	).Seed(ctx, importItemID, metadata, maximumYear)
	if err != nil {
		return 0, nil, fmt.Errorf("libraryimport/server metadata: %w", err)
	}
	return version, warnings, nil
}
