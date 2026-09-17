package libraryimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/content/multidisc"
)

type ReviewDependencies struct{ reader model.ReviewDependencyReader }

func NewReviewDependencies(reader model.ReviewDependencyReader) *ReviewDependencies {
	return &ReviewDependencies{reader: reader}
}

func (service *ReviewDependencies) Arcade(
	ctx context.Context,
	itemID string,
	head model.ReviewDependencyHead,
) (model.ReviewArcade, bool, error) {
	if head.PlatformID != "arcade" || head.DependencyJSON == nil {
		return model.ReviewArcade{}, false, nil
	}
	snapshot, err := CanonicalArcadeSnapshot(ctx, service.reader, *head.DependencyJSON)
	if err != nil {
		return model.ReviewArcade{}, false, err
	}
	attachments, err := service.reader.ArcadeAttachments(ctx, itemID)
	if err != nil {
		return model.ReviewArcade{}, false, fmt.Errorf("read review arcade attachments: %w", err)
	}
	byMachine, active := indexArcadeAttachments(attachments)
	result := &model.ReviewArcade{
		Machine:           snapshot.Machine,
		Status:            optionalTextValue(head.ValidationStatus),
		CompatibilityCode: optionalTextValue(head.CompatibilityCode),
		Nodes: make([]model.ReviewArcadeNode,
			0,
			len(snapshot.Dependencies)),
		ActiveAttachment: active,
	}
	unsupported := arcadeAttachmentUnsupported(result.CompatibilityCode)
	for _, dependency := range snapshot.Dependencies {
		node := model.ReviewArcadeNode{
			Kind:                dependency.Kind,
			Machine:             dependency.Machine,
			RequiredBy:          dependency.RequiredBy,
			Depth:               dependency.Depth,
			ExpectedLogicalName: dependency.ExpectedLogicalName,
			State:               dependency.State,
			RequiredEntryCount:  dependency.RequiredEntryCount,
			RequiredEntries:     dependency.RequiredEntries,
			Attachment:          byMachine[dependency.Machine],
		}
		node.CanAttach = dependency.Kind == "PARENT" &&
			(dependency.State == "MISSING" ||
				dependency.State == "MISMATCH") &&
			active == nil &&
			!unsupported
		result.Nodes = append(result.Nodes, node)
	}
	return *result, true, nil
}

func optionalTextValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (service *ReviewDependencies) MultiDisc(
	ctx context.Context,
	itemID string,
	head model.ReviewDependencyHead,
) (model.ReviewMultiDisc, bool, error) {
	if head.ContentKind != multidisc.ContentKind {
		return model.ReviewMultiDisc{}, false, nil
	}
	source, err := service.reader.MultiDiscSource(ctx, head.SnapshotID)
	if err != nil {
		return model.ReviewMultiDisc{}, false, fmt.Errorf("read review multi-disc source: %w", err)
	}
	result := projectMultiDiscSource(source)
	attachments, err := service.reader.MultiDiscAttachments(ctx, itemID)
	if err != nil {
		return model.ReviewMultiDisc{}, false, fmt.Errorf("read review multi-disc attachments: %w", err)
	}
	retryRequired := false
	for i := range attachments {
		attachment := &attachments[i]
		attachment.CanRetry = attachment.Retryable()
		if result.LatestAttachment == nil {
			result.LatestAttachment = attachment
		}
		if result.ActiveAttachment == nil && attachment.Active() {
			result.ActiveAttachment = attachment
		}
		if attachment.State == "FAILED_RETRYABLE" {
			retryRequired = true
		}
	}
	result.CanAttachMissingDiscs = result.MissingDiscCount > 0 && result.ActiveAttachment == nil && !retryRequired
	return *result, true, nil
}

func projectMultiDiscSource(source model.MultiDiscSource) *model.ReviewMultiDisc {
	result := &model.ReviewMultiDisc{
		ContentKind:       multidisc.ContentKind,
		Playlist:          source.Playlist,
		DiscCount:         len(source.Entries),
		MaxDiscs:          source.MaxDiscs,
		MaxTotalBytes:     source.MaxTotalBytes,
		Entries:           source.Entries,
		MissingReferences: []string{},
	}
	for i := range result.Entries {
		entry := &result.Entries[i]
		entry.DiscIndex = entry.Index
		entry.Label = fmt.Sprintf("光盘 %d", entry.Index+1)
		if entry.SizeBytes != nil {
			result.TotalPresentBytes += *entry.SizeBytes
			result.PresentDiscCount++
		}
		if entry.State == "MISSING" {
			result.MissingReferences = append(result.MissingReferences, entry.SourceReference)
		}
	}
	result.MissingDiscCount = len(result.MissingReferences)
	return result
}

func indexArcadeAttachments(attachments []model.ArcadeAttachment) (map[string]*model.ArcadeAttachment, *model.ArcadeAttachment) {
	byMachine := make(map[string]*model.ArcadeAttachment)
	var active *model.ArcadeAttachment
	for i := range attachments {
		entry := &attachments[i]
		if _, found := byMachine[entry.Machine]; !found {
			byMachine[entry.Machine] = entry
		}
		if active == nil && (entry.State == "QUEUED" || entry.State == "RUNNING") {
			active = entry
		}
	}
	return byMachine, active
}

func arcadeAttachmentUnsupported(code string) bool {
	switch code {
	case "UNSUPPORTED_MERGED_ROMSET", "UNSUPPORTED_CHD", "ARCADE_DEPENDENCY_CYCLE", "ARCADE_DAT_UNAVAILABLE":
		return true
	default:
		return false
	}
}
