package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/filestore"
	validation "retrom/internal/service/corevalidation"
)

func PreparedGroupContentKind(group PreparedGroup) string {
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

func (run *creationCommit) prepareDependencies(ctx context.Context, scope ImportCreationScope) error {
	groups := make([]PreparedGroup, len(run.groups))
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
	reader validation.Repository,
	target ImportTarget,
	groups []PreparedGroup,
) error {
	if skipsCreationStaticBIOS(target.PlatformID) {
		return nil
	}
	for index := range groups {
		group := &groups[index]
		name := creationBIOSContentName(*group)
		if name == "" {
			return ErrInvalid
		}
		snapshot, status, code, err := validation.New(reader).ResolveBIOS(ctx, target.ProviderID, target.TargetID, name)
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
			if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.FileRecord != nil {
				group.ValidationFiles = append(
					group.ValidationFiles,
					PreparedValidationFile{
						Role:        "BIOS_BUNDLE",
						LogicalName: dependency.LogicalName,
						FileRecord:  *dependency.FileRecord,
						SortOrder:   len(group.ValidationFiles),
					},
				)
			}
		}
	}
	return nil
}

// Missing disc bytes do not erase the validated playlist's content kind.
func creationBIOSContentName(group PreparedGroup) string {
	if len(group.MultiEntries) > 0 {
		return group.MultiEntries[0].SourceReference
	}
	for _, source := range group.Sources {
		if source.Role == "CONTENT" || source.Role == "DISC" {
			return source.LogicalName
		}
	}
	return group.DefaultDOSEntry
}

func skipsCreationStaticBIOS(platform string) bool {
	switch platform {
	case "cavestory", "arcade", "rpgmaker", "ons", "kirikiri", "butterscotch", "tyranoscript", "scummvm", "daphne":
		return true
	default:
		return false
	}
}

func (run *creationCommit) persistValidation(
	ctx context.Context,
	scope ImportCreationScope,
	record *creationGroup,
) error {
	if err := run.registerValidationArtifacts(ctx, scope, record); err != nil {
		return creationError("persist validation", err)
	}
	group := &record.group
	if profile := group.RPGProfile; profile != nil {
		analysis := RPGReviewAnalysis{SelfContained: profile.SelfContained}
		analysis.Requirements.RTP = profile.RTPDependencies
		state := ResolveRPGResourcePolicy(string(profile.ExpectedGeneration), false, analysis)
		group.ValidationStatus, group.CompatibilityCode = state.Status, state.Code
		group.DependencySnapshot = state.SnapshotJSON
	}
	if group.ValidationStatus == "" {
		group.ValidationStatus, group.CompatibilityCode, group.DependencySnapshot = "READY", "READY", "{}"
	}
	if err := run.resolveArcade(ctx, scope, group); err != nil {
		return creationError("persist validation", err)
	}
	return creationError("persist validation", scope.Reviews.Validation(
		ctx,
		CreationValidation{
			ItemID:         record.itemID,
			SnapshotID:     record.snapshotID,
			ManifestDigest: record.manifestDigest,
			Status:         group.ValidationStatus,
			Code:           group.CompatibilityCode,
			DependencyJSON: group.DependencySnapshot,
			DATID:          run.plan.DATVersionID,
			DefaultDOS:     group.DefaultDOSEntry,
			Target:         run.plan.Target,
			DOSEntries:     group.DOSEntries,
			Files:          group.ValidationFiles,
			NowMS:          run.header.NowMS,
		},
	))
}

func (run *creationCommit) resolveArcade(
	ctx context.Context,
	scope ImportCreationScope,
	group *PreparedGroup,
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
		if dependency.DeliveryKind == "BIOS_BUNDLE" && dependency.FileRecord != nil {
			group.ValidationFiles = append(
				group.ValidationFiles,
				PreparedValidationFile{
					Role:        "BIOS_BUNDLE",
					LogicalName: dependency.LogicalName,
					FileRecord:  *dependency.FileRecord,
					SortOrder:   len(group.ValidationFiles),
				},
			)
		}
	}
	return nil
}

func (run *creationCommit) registerValidationArtifacts(
	ctx context.Context,
	scope ImportCreationScope,
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
		file.FileRecord = id
	}
	if group.CanonicalPlaylist != nil {
		id, err := run.registerArtifact(ctx, scope, *group.CanonicalPlaylist, "application/vnd.retrom.m3u")
		if err != nil {
			return creationError("register validation artifacts", err)
		}
		group.ValidationFiles = append(
			group.ValidationFiles,
			PreparedValidationFile{Role: "MULTI_DISC_PLAYLIST", LogicalName: "playlist.m3u", FileRecord: id, SortOrder: 0},
		)
	}
	bundle := group.BundleFileRecord
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
			PreparedValidationFile{Role: "DOS_LAUNCH_BUNDLE", LogicalName: "game.zip", FileRecord: bundle, SortOrder: 0},
		)
	}
	return nil
}

func (run *creationCommit) registerArtifact(
	ctx context.Context,
	scope ImportCreationScope,
	metadata filestore.Metadata,
	kind string,
) (string, error) {
	id, err := scope.Sources.Artifact(
		ctx,
		CreationArtifact{Metadata: metadata, MediaType: kind, NowMS: run.header.NowMS},
	)
	if err != nil {
		return "", fmt.Errorf("register creation validation artifact: %w", err)
	}
	return id, nil
}
