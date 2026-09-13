package libraryimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"retrom/internal/authn"
	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

type ownedSourceCreation struct {
	intent application.SourceCreationIntent
	before application.SourceCreationSnapshot
	result ServerImportResult
}

// LookupOwnedServerSource uses permanent source bindings, including after payload cleanup.
// It does not authorize subsequent writes by the requesting worker.
func (service *Service) LookupOwnedServerSource(
	ctx context.Context,
	intent application.SourceCreationIntent,
) (ServerImportResult, bool, error) {
	if !intent.Kind.Valid() || intent.ImportID == "" || intent.ItemID == "" {
		return ServerImportResult{}, false, ErrInvalid
	}
	found, present, err := repository.BindOwnedSources(service.database).Lookup(ctx, intent)
	if err != nil {
		return ServerImportResult{}, false, fmt.Errorf("lookup owned server source: %w", err)
	}
	return found.Result, present, nil
}

// CreateOwnedServerSource commits the import and its server source binding together.
func (service *Service) CreateOwnedServerSource(
	ctx context.Context,
	request application.OwnedServerSourceRequest,
) (ServerImportResult, error) {
	request.Intent.PrimaryPaths = slices.Clone(request.Intent.PrimaryPaths)
	request.Files = slices.Clone(request.Files)
	request.TagIDs = slices.Clone(request.TagIDs)
	replayed, found, err := repository.BindOwnedSources(service.database).Lookup(ctx, request.Intent)
	if err != nil {
		return ServerImportResult{}, fmt.Errorf("create owned server source: %w", err)
	}
	if found {
		return validateOwnedSourceReplay(request, replayed)
	}
	ownership := application.NewSourceOwnership(service.now)
	before, err := ownership.Prepare(
		ctx, repository.BindSourceOwnership(service.database), request.Intent, request.TargetPlatformInstanceID,
	)
	if err != nil {
		return ServerImportResult{}, fmt.Errorf("create owned server source: %w", err)
	}
	if err := application.ValidateOwnedSourceRequest(before, request); err != nil {
		return ServerImportResult{}, fmt.Errorf("validate frozen source request: %w", err)
	}
	if err := application.ValidateOwnedSourceFiles(before, request.Files); err != nil {
		return ServerImportResult{}, fmt.Errorf("validate copied source files: %w", err)
	}
	prepared, err := service.prepareServerSource(
		ctx, "SERVER_"+string(request.Intent.Kind)+"_IMPORT:"+request.Intent.ItemID, request.ContentMode, request.Files,
	)
	if err != nil {
		return ServerImportResult{}, fmt.Errorf("create owned server source: %w", err)
	}
	target, err := service.loadCreationTarget(ctx, request.TargetPlatformInstanceID)
	if err != nil {
		return ServerImportResult{}, fmt.Errorf("create owned server source: %w", err)
	}
	prepared.contentMode = normalizeTargetContentMode(target.PlatformID, prepared.contentMode)
	_, exists, err := service.ensureServerSourceUpload(ctx, prepared, request.TargetPlatformInstanceID)
	if err != nil {
		return ServerImportResult{}, fmt.Errorf("create owned server source: %w", err)
	}
	if exists {
		return ServerImportResult{}, ErrVersionConflict
	}
	binding := &ownedSourceCreation{intent: request.Intent, before: before}
	if len(request.TagIDs) > 0 {
		ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: request.AssignedByUserID})
	}
	_, err = service.create(ctx, CreateRequest{
		UploadID: prepared.uploadID, TargetPlatformInstanceID: request.TargetPlatformInstanceID,
		MetadataProvider: "NONE", ContentMode: prepared.contentMode, TagIDs: request.TagIDs,
	}, nil, creationOptions{sourceCreation: binding, reviewHandoffKind: prepared.reviewHandoffKind()})
	if err != nil {
		service.removeUnusedClonedUpload(ctx, prepared.uploadID)
		return ServerImportResult{}, fmt.Errorf("create owned server source: %w", err)
	}
	return binding.result, nil
}

func validateOwnedSourceReplay(
	request application.OwnedServerSourceRequest,
	replayed application.OwnedSourceLookup,
) (ServerImportResult, error) {
	if err := application.ValidateOwnedSourceReplayPaths(
		request.Intent.PrimaryPaths, replayed.PrimaryPaths,
	); err != nil {
		return ServerImportResult{}, fmt.Errorf("validate replay source paths: %w", err)
	}
	mode := request.ContentMode
	if mode == "" {
		mode = "STANDARD"
	}
	if replayed.ContentMode == "RPG_MAKER_PROJECT" && mode == "STANDARD" {
		mode = "RPG_MAKER_PROJECT"
	}
	if request.TargetPlatformInstanceID != replayed.TargetPlatformInstanceID || mode != replayed.ContentMode ||
		len(request.Files) == 0 || len(request.Files) > ServerSourceFileLimit {
		return ServerImportResult{}, ErrInvalid
	}
	files := slices.Clone(request.Files)
	sort.Slice(files, func(a, b int) bool { return files[a].RelativePath < files[b].RelativePath })
	encoded, err := json.Marshal(map[string]any{"schemaVersion": 1, "files": files})
	if err != nil {
		return ServerImportResult{}, fmt.Errorf("encode source replay identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != replayed.ManifestDigest {
		return ServerImportResult{}, ErrInvalid
	}
	return replayed.Result, nil
}
