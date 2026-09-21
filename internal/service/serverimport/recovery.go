package serverimport

import (
	"context"
	"fmt"

	"retrom/internal/blobstore"
	"retrom/internal/firmware"
	"retrom/internal/serversource"
)

type CandidateEvidence struct {
	ID, RequirementID, Association, State string
	Facts                                 firmware.FileFacts
	Static                                *firmware.StaticEvaluation
	DAT                                   *firmware.DATEvaluation
	Details                               map[string]any
}
type RecoveryRepository interface {
	Items(context.Context, string) ([]CatalogItem, error)
	Phase(context.Context, string) (string, error)
	Candidates(context.Context, string) ([]CandidateEvidence, error)
	DATEntries(context.Context, string, string) ([]firmware.ExpectedDATEntry, error)
}
type (
	BlobLocator interface{ Path(string) string }
	Recovery    struct {
		repository RecoveryRepository
		blobs      BlobLocator
	}
)

func NewRecovery(repository RecoveryRepository, blobs BlobLocator) *Recovery {
	return &Recovery{repository, blobs}
}

func (service *Recovery) Items(ctx context.Context, id string) ([]CatalogItem, error) {
	items, err := service.repository.Items(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read frozen import catalog: %w", err)
	}
	return items, nil
}

func (service *Recovery) DiscoveryWasPersisted(ctx context.Context, id string) (bool, error) {
	phase, err := service.repository.Phase(ctx, id)
	if err != nil {
		return false, fmt.Errorf("read import discovery phase: %w", err)
	}
	switch phase {
	case "DISCOVERY_COMPLETED", "RANKING", "INSTALLING", "QUEUEING_REVALIDATION":
		return true, nil
	default:
		return false, nil
	}
}

func (service *Recovery) ExpectedDATEntries(
	ctx context.Context,
	item CatalogItem,
) ([]firmware.ExpectedDATEntry, error) {
	if item.ArchiveMembersJSON != nil {
		members, err := firmware.StaticArchiveExpectations(*item.ArchiveMembersJSON)
		if err != nil {
			return nil, fmt.Errorf("load archive requirements: %w", err)
		}
		return members, nil
	}
	if item.DATVersionID == nil || item.DATMachineName == nil {
		return nil, ErrCatalogInvalid
	}
	entries, err := service.repository.DATEntries(ctx, *item.DATVersionID, *item.DATMachineName)
	if err != nil {
		return nil, fmt.Errorf("read expected DAT entries: %w", err)
	}
	if len(entries) == 0 {
		return nil, ErrCatalogInvalid
	}
	return entries, nil
}

func (service *Recovery) Candidates(
	ctx context.Context,
	id string,
	items []CatalogItem,
) (map[string][]*EvaluatedCandidate, error) {
	itemByID := make(map[string]CatalogItem, len(items))
	for _, item := range items {
		itemByID[item.RequirementID] = item
	}
	records, err := service.repository.Candidates(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read recovered candidates: %w", err)
	}
	result := make(map[string][]*EvaluatedCandidate)
	expected := make(map[string][]firmware.ExpectedDATEntry)
	for _, record := range records {
		item, ok := itemByID[record.RequirementID]
		if !ok {
			return nil, ErrCatalogInvalid
		}
		candidate, err := restoreCandidate(record, item)
		if err != nil {
			return nil, err
		}
		if candidate.Metadata.SHA256 != "" {
			candidate.Metadata.Path = service.blobs.Path(candidate.Metadata.SHA256)
		}
		if candidate.DAT != nil {
			entries, ok := expected[item.RequirementID]
			if !ok {
				entries, err = service.ExpectedDATEntries(ctx, item)
				if err != nil {
					return nil, err
				}
				expected[item.RequirementID] = entries
			}
			candidate.ExpectedDATEntries = entries
		}
		result[item.RequirementID] = append(result[item.RequirementID], candidate)
	}
	return result, nil
}

func restoreCandidate(record CandidateEvidence, item CatalogItem) (*EvaluatedCandidate, error) {
	facts := record.Facts
	candidate := &EvaluatedCandidate{
		ID: record.ID, Item: item, Association: record.Association, State: record.State, Details: record.Details,
		File: serversource.File{
			RelativePath: facts.RelativePath,
			Basename:     facts.Basename,
			Name:         facts.Basename,
			SizeBytes:    facts.SizeBytes,
		},
		Metadata: blobstore.Metadata{
			Size:   facts.SizeBytes,
			MD5:    facts.MD5,
			SHA1:   facts.SHA1,
			SHA256: facts.SHA256,
			CRC32:  facts.CRC32,
		},
	}
	switch {
	case !item.IsArchive() && record.Static != nil:
		value := *record.Static
		value.Status, value.Method = StaticStatusMethod(value)
		candidate.Static = &value
	case item.IsArchive() && record.DAT != nil:
		value := *record.DAT
		value.Status, value.Method = DATStatusMethod(value)
		item.ApplyArchivePolicy(&value)
		candidate.DAT = &value
	}
	if record.State == "ELIGIBLE" && candidate.Static == nil && candidate.DAT == nil {
		return nil, ErrCatalogInvalid
	}
	return candidate, nil
}
