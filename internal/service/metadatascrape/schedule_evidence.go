package metadatascrape

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	model "retrom/internal/model/metadatascrape"
)

func scheduleEvidence(ctx context.Context, scope model.ScheduleScope, plan model.SchedulePlan, platform string) error {
	var evidence []model.HashEvidence
	var err error
	if platform == "arcade" {
		evidence, err = arcadeEvidence(ctx, scope.Sources, plan)
	} else {
		evidence, err = contentEvidence(ctx, scope.Sources, plan)
	}
	if err != nil {
		return err
	}
	if err := scope.Writes.Evidence(ctx, evidence); err != nil {
		return fmt.Errorf("persist scrape evidence: %w", err)
	}
	return nil
}

func contentEvidence(
	ctx context.Context,
	reader model.ScheduleEvidenceReader,
	plan model.SchedulePlan,
) ([]model.HashEvidence, error) {
	files, err := reader.Files(ctx, plan.Subject)
	if err != nil {
		return nil, fmt.Errorf("read content hash evidence: %w", err)
	}
	evidence := make([]model.HashEvidence, 0, len(files))
	for _, file := range files {
		if strings.EqualFold(filepath.Ext(file.Name), ".zip") && file.ArchiveBlobID == nil {
			continue
		}
		id, err := scheduleID()
		if err != nil {
			return nil, err
		}
		item := model.HashEvidence{
			ID:      id,
			RunID:   plan.RunID,
			Profile: "RAW_FILE",
			BlobID:  &file.BlobID,
			Hashes:  file.Hashes,
			Order: len(
				evidence,
			),
			Now: plan.Now,
		}
		if file.ArchiveBlobID != nil && file.ArchiveOrdinal != nil {
			item.Profile = "SINGLE_ARCHIVE_MEMBER"
			item.BlobID = nil
			item.ArchiveBlobID = file.ArchiveBlobID
			item.ArchiveOrdinal = file.ArchiveOrdinal
		}
		evidence = append(evidence, item)
	}
	return evidence, nil
}

func arcadeEvidence(
	ctx context.Context,
	reader model.ScheduleEvidenceReader,
	plan model.SchedulePlan,
) ([]model.HashEvidence, error) {
	binding, found, err := reader.DAT(ctx, plan.Subject)
	if err != nil {
		return nil, fmt.Errorf("read arcade scrape DAT: %w", err)
	}
	if !found {
		return nil, nil
	}
	var snapshot struct {
		Machine string `json:"machine"`
	}
	if err := json.Unmarshal([]byte(binding.SnapshotJSON), &snapshot); err != nil || snapshot.Machine == "" {
		return nil, model.ErrArcadeSnapshotInvalid
	}
	entries, err := reader.Arcade(ctx, plan.Subject, binding.ID, snapshot.Machine)
	if err != nil {
		return nil, fmt.Errorf("read arcade hash evidence: %w", err)
	}
	return selectArcadeEvidence(entries, plan)
}

func selectArcadeEvidence(entries []model.ArcadeEvidence, plan model.SchedulePlan) ([]model.HashEvidence, error) {
	sort.Slice(entries, func(left, right int) bool {
		if (entries[left].SHA1 != nil) != (entries[right].SHA1 != nil) {
			return entries[left].SHA1 != nil
		}
		if entries[left].Size != entries[right].Size {
			return entries[left].Size > entries[right].Size
		}
		return entries[left].Name < entries[right].Name
	})
	seen := make(map[string]struct{}, len(entries))
	evidence := make([]model.HashEvidence, 0, 8)
	for _, entry := range entries {
		key := textValue(entry.CRC32) + "\x00" + textValue(entry.SHA1)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		id, err := scheduleID()
		if err != nil {
			return nil, err
		}
		evidence = append(evidence, model.HashEvidence{
			ID: id, RunID: plan.RunID, Profile: "ARCADE_DAT_ENTRIES", ArchiveBlobID: &entry.ArchiveBlobID,
			ArchiveOrdinal: &entry.Ordinal, Hashes: model.Hashes{
				CRC32: entry.CRC32,
				SHA1:  entry.SHA1,
			}, Order: len(
				evidence,
			), Now: plan.Now,
		})
		if len(evidence) == 8 {
			break
		}
	}
	return evidence, nil
}

func textValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
