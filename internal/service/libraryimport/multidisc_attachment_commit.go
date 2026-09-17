package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	model "retrom/internal/model/libraryimport"
	"time"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/content/multidisc"
	validationservice "retrom/internal/service/corevalidation"

	"github.com/google/uuid"
)

func NewMultiDiscAttachmentCommits(
	repository model.MultiDiscAttachmentCommitRepository, now func() time.Time,
) *MultiDiscAttachmentCommits {
	if now == nil {
		now = time.Now
	}
	return &MultiDiscAttachmentCommits{repository: repository, now: now, newID: newMultiDiscAttachmentID}
}

func newMultiDiscAttachmentID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("allocate multi-disc attachment evidence ID: %w", err)
	}
	return id.String(), nil
}

func (service *MultiDiscAttachmentCommits) CommitAccepted(
	ctx context.Context, request model.MultiDiscAttachmentCommitRequest,
) error {
	if !validMultiDiscAttachmentCommitRequest(request) {
		return model.ErrInvalid
	}
	write := model.MultiDiscAttachmentCommitWrite{MultiDiscAttachmentCommitRequest: request, NowMS: service.now().UnixMilli()}
	for _, target := range []*string{
		&write.SourceSnapshotID, &write.ValidationID, &write.ConsumptionID, &write.EventID,
	} {
		id, err := service.newID()
		if err != nil {
			return err
		}
		*target = id
	}
	if err := service.repository.WithCommit(ctx, func(scope model.MultiDiscAttachmentCommitScope) error {
		validation, err := service.resolveValidation(ctx, scope, request)
		if err != nil {
			return err
		}
		write.Validation = validation
		return scope.CommitAccepted(ctx, write)
	}); err != nil {
		return fmt.Errorf("commit accepted multi-disc attachment: %w", err)
	}
	return nil
}

func validMultiDiscAttachmentCommitRequest(request model.MultiDiscAttachmentCommitRequest) bool {
	return model.ValidMultiDiscAttachmentInput(request.Input) && request.JobID != "" && request.WorkerID != "" &&
		len(request.BaseFiles) > 0 && len(request.ResultEntries) >= multidisc.MinDiscs &&
		request.ResultManifestJSON != "" && len(request.ResultManifestDigest) == 64 &&
		request.CanonicalPlaylist.SHA256 != ""
}

func (service *MultiDiscAttachmentCommits) resolveValidation(
	ctx context.Context, scope model.MultiDiscAttachmentCommitScope, request model.MultiDiscAttachmentCommitRequest,
) (model.MultiDiscAttachmentValidation, error) {
	if len(request.ResultEntries) < multidisc.MinDiscs {
		return model.MultiDiscAttachmentValidation{}, model.ErrInvalid
	}
	first := request.ResultEntries[0].File.LogicalName
	records, err := scope.BIOS(ctx, request.Input.ProviderID, request.Input.TargetID)
	if err != nil {
		return model.MultiDiscAttachmentValidation{}, fmt.Errorf("resolve multi-disc BIOS: %w", err)
	}
	snapshot, status, code, err := validationservice.ResolveBIOSRecords(records, first)
	if err != nil {
		return model.MultiDiscAttachmentValidation{}, fmt.Errorf("resolve multi-disc BIOS records: %w", err)
	}
	snapshot.MultiDisc = &corevalidation.MultiDiscSnapshot{
		ContentKind: corevalidation.MultiDiscContentKind, ParserVersion: corevalidation.MultiDiscParserVersion,
		DiscCount: len(request.ResultEntries), MissingEntries: []corevalidation.MultiDiscMissingEntry{},
		OrderedDiscSHA256:       make([]string, 0, len(request.ResultEntries)),
		CanonicalPlaylistSHA256: request.CanonicalPlaylist.SHA256, Delivery: corevalidation.MultiDiscDelivery,
	}
	for _, entry := range request.ResultEntries {
		snapshot.MultiDisc.OrderedDiscSHA256 = append(snapshot.MultiDisc.OrderedDiscSHA256, entry.File.BlobSHA256)
	}
	encoded, err := snapshot.JSON()
	if err != nil {
		return model.MultiDiscAttachmentValidation{}, fmt.Errorf("encode multi-disc dependency snapshot: %w", err)
	}
	files := make([]model.PreparedValidationFile, 0, len(snapshot.BIOS))
	for _, dependency := range snapshot.BIOS {
		if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.BlobID != nil {
			files = append(files, model.PreparedValidationFile{
				Role: "BIOS_BUNDLE", LogicalName: dependency.LogicalName,
				BlobID: *dependency.BlobID, SortOrder: len(files),
			})
		}
	}
	return model.MultiDiscAttachmentValidation{
		Status: status, CompatibilityCode: code,
		DependencySnapshotJSON: string(encoded), Files: files,
	}, nil
}

func multiDiscReviewEventJSON(fields map[string]any) string {
	fields["schemaVersion"] = 2
	encoded, _ := json.Marshal(fields)
	return string(encoded)
}
