package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	model "retrom/internal/model/libraryimport"

	"retrom/internal/adapter/files/blobstore"
	corevalidationmodel "retrom/internal/model/corevalidation"
	corevalidationservice "retrom/internal/service/corevalidation"
)

func PreparedGroupContentKind(group model.PreparedGroup) string {
	if group.ContentKind != "" {
		return group.ContentKind
	}
	for _, source := range group.Sources {
		if source.Role == "DOS_SOURCE" {
			return "DOS_BUNDLE"
		}
	}
	return "SINGLE_FILE"
}

func (run *creationCommit) prepareDependencies(ctx context.Context, scope model.ImportCreationScope) error {
	groups := make([]model.PreparedGroup, len(run.groups))
	for index := range run.groups {
		groups[index] = run.groups[index].group
	}
	if err := PrepareCreationStaticBIOS(ctx, scope.BIOS, run.plan.Target, groups); err != nil {
		return creationError("prepare dependencies", err)
	}
	for index := range groups {
		run.groups[index].group = groups[index]
	}
	return nil
}

func PrepareCreationStaticBIOS(
	ctx context.Context,
	reader corevalidationmodel.Repository,
	target model.ImportTarget,
	groups []model.PreparedGroup,
) error {
	if skipsCreationStaticBIOS(target.PlatformID) {
		return nil
	}
	for index := range groups {
		group := &groups[index]
		name := ""
		for _, source := range group.Sources {
			if source.Role == "CONTENT" || source.Role == "DISC" {
				name = source.LogicalName
				break
			}
		}
		if name == "" && target.PlatformID == "dos" {
			name = group.DefaultDOSEntry
		}
		if name == "" {
			return model.ErrInvalid
		}
		snapshot, status, code, err := corevalidationservice.New(reader).ResolveBIOS(ctx, target.ProviderID, target.TargetID, name)
		if err != nil {
			return creationError("prepare creation static b i o s", err)
		}
		snapshot.MultiDisc = group.MultiDependency
		encoded, err := snapshot.JSON()
		if err != nil {
			return fmt.Errorf("encode creation BIOS snapshot: %w", err)
		}
		if group.CompatibilityCode != "MULTI_DISC_FILE_MISSING" {
			group.ValidationStatus, group.CompatibilityCode = status, code
		}
		group.DependencySnapshot = string(encoded)
		for _, dependency := range snapshot.BIOS {
			if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.BlobID != nil {
				group.ValidationFiles = append(
					group.ValidationFiles,
					model.PreparedValidationFile{
						Role:        "BIOS_BUNDLE",
						LogicalName: dependency.LogicalName,
						BlobID:      *dependency.BlobID,
						SortOrder:   len(group.ValidationFiles),
					},
				)
			}
		}
	}
	return nil
}

func skipsCreationStaticBIOS(platform string) bool {
	switch platform {
	case "cavestory", "arcade", "rpgmaker", "ons", "kirikiri", "butterscotch", "tyranoscript", "scummvm":
		return true
	default:
		return false
	}
}

func (run *creationCommit) persistValidation(
	ctx context.Context,
	scope model.ImportCreationScope,
	record *creationGroup,
) error {
	if err := run.registerValidationArtifacts(ctx, scope, record); err != nil {
		return creationError("persist validation", err)
	}
	group := &record.group
	if profile := group.RPGProfile; profile != nil {
		analysis := model.RPGReviewAnalysis{SelfContained: profile.SelfContained}
		analysis.Requirements.RTP = profile.RTPDependencies
		state := model.ResolveRPGResourcePolicy(string(profile.ExpectedGeneration), false, analysis)
		group.ValidationStatus, group.CompatibilityCode = state.Status, state.Code
		group.DependencySnapshot = state.SnapshotJSON
	}
	if group.ValidationStatus == "" {
		group.ValidationStatus, group.CompatibilityCode, group.DependencySnapshot = "READY", "READY", "{}"
	}
	if err := run.resolveArcade(ctx, scope, group); err != nil {
		return creationError("persist validation", err)
	}
	target := run.plan.Target
	digest := model.PrepublishDigest(
		model.PrepublishDigestInput{
			SchemaVersion:            1,
			SourceSnapshotID:         record.snapshotID,
			SourceManifestDigest:     record.manifestDigest,
			ContentKind:              record.kind,
			TargetPlatformInstanceID: target.ID,
			ProviderID:               target.ProviderID,
			TargetID:                 target.TargetID,
			ContentPolicyDigest:      target.Policy.DigestFor(record.kind),
			DATVersionID:             creationOptional(run.plan.DATVersionID),
			DefaultDOSEntry:          creationOptional(group.DefaultDOSEntry),
			DependencySnapshot:       json.RawMessage(group.DependencySnapshot),
			Status:                   group.ValidationStatus,
			CompatibilityCode:        group.CompatibilityCode,
		},
	)
	if digest == "" {
		return model.ErrInvalid
	}
	return creationError("persist validation", scope.Reviews.Validation(
		ctx,
		model.CreationValidation{
			ID:             record.validationID,
			ItemID:         record.itemID,
			SnapshotID:     record.snapshotID,
			ManifestDigest: record.manifestDigest,
			InputDigest:    digest,
			Status:         group.ValidationStatus,
			Code:           group.CompatibilityCode,
			DependencyJSON: group.DependencySnapshot,
			DATID:          run.plan.DATVersionID,
			DefaultDOS:     group.DefaultDOSEntry,
			Target:         target,
			DOSEntries:     group.DOSEntries,
			Files:          group.ValidationFiles,
			NowMS:          run.header.NowMS,
		},
	))
}

func (run *creationCommit) resolveArcade(
	ctx context.Context,
	scope model.ImportCreationScope,
	group *model.PreparedGroup,
) error {
	if run.plan.Target.PlatformID != "arcade" {
		return nil
	}
	state, err := ResolveCreationArcade(ctx, scope.Arcade, run.plan.Target.ProviderID, run.plan.Target.TargetID,
		group.DependencySnapshot, group.ValidationStatus, group.CompatibilityCode)
	if err != nil || !state.Tracked {
		return creationError("resolve arcade", err)
	}
	group.ValidationStatus, group.CompatibilityCode = state.Status, state.Code
	group.DependencySnapshot = state.SnapshotJSON
	for _, dependency := range state.Dependencies {
		if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.BlobID != nil {
			group.ValidationFiles = append(
				group.ValidationFiles,
				model.PreparedValidationFile{
					Role:        "BIOS_BUNDLE",
					LogicalName: dependency.LogicalName,
					BlobID:      *dependency.BlobID,
					SortOrder:   len(group.ValidationFiles),
				},
			)
		}
	}
	return nil
}

func (run *creationCommit) registerValidationArtifacts(
	ctx context.Context,
	scope model.ImportCreationScope,
	record *creationGroup,
) error {
	group := &record.group
	for index := range group.ValidationFiles {
		file := &group.ValidationFiles[index]
		if file.Artifact == nil {
			continue
		}
		id, err := run.registerArtifact(ctx, scope, *file.Artifact, "application/octet-stream")
		if err != nil {
			return creationError("register validation artifacts", err)
		}
		file.BlobID = id
	}
	if group.CanonicalPlaylist != nil {
		id, err := run.registerArtifact(ctx, scope, *group.CanonicalPlaylist, "application/vnd.retrom.m3u")
		if err != nil {
			return creationError("register validation artifacts", err)
		}
		group.ValidationFiles = append(
			group.ValidationFiles,
			model.PreparedValidationFile{Role: "MULTI_DISC_PLAYLIST", LogicalName: "playlist.m3u", BlobID: id, SortOrder: 0},
		)
	}
	bundle := group.BundleBlobID
	if group.Bundle != nil {
		id, err := run.registerArtifact(ctx, scope, *group.Bundle, "application/zip")
		if err != nil {
			return creationError("register validation artifacts", err)
		}
		bundle = id
	}
	if bundle != "" {
		group.ValidationFiles = append(
			group.ValidationFiles,
			model.PreparedValidationFile{Role: "DOS_LAUNCH_BUNDLE", LogicalName: "game.zip", BlobID: bundle, SortOrder: 0},
		)
	}
	return nil
}

func (run *creationCommit) registerArtifact(
	ctx context.Context,
	scope model.ImportCreationScope,
	metadata blobstore.Metadata,
	kind string,
) (string, error) {
	id, err := scope.Sources.Artifact(ctx, model.CreationArtifact{Metadata: metadata, MediaType: kind, NowMS: run.header.NowMS})
	if err != nil {
		return "", fmt.Errorf("register creation validation artifact: %w", err)
	}
	return id, nil
}
