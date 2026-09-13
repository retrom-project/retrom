package serverimport

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"retrom/internal/adapter/files/serversource"
	"retrom/internal/capability/content/firmware"
)

type CandidateWrite struct {
	Evidence      CandidateEvidence
	ExactBasename bool
	Rank          *int64
	NotSelected   *string
	Details       []byte
}
type DiscoveryGroup struct {
	RequirementID string
	Candidates    []CandidateWrite
}
type DiscoveryPlan struct {
	Unit                 Work
	Groups               []DiscoveryGroup
	Total, Multiple, Now int64
	Counts               serversource.Counts
}
type DiscoveryRecords interface {
	Reset(context.Context, Work, int64) error
	Persist(context.Context, DiscoveryPlan) error
}
type DiscoveryRepository interface {
	WithWrite(context.Context, func(DiscoveryRecords) error) error
}
type Discovery struct {
	repository DiscoveryRepository
	now        func() time.Time
}

func NewDiscovery(repository DiscoveryRepository, now func() time.Time) *Discovery {
	return &Discovery{repository, now}
}

func (service *Discovery) Reset(ctx context.Context, unit Work) error {
	err := service.repository.WithWrite(ctx, func(records DiscoveryRecords) error {
		if err := records.Reset(ctx, unit, service.now().UnixMilli()); err != nil {
			return fmt.Errorf("reset import discovery: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reset import discovery transaction: %w", err)
	}
	return nil
}

func (service *Discovery) Persist(
	ctx context.Context,
	unit Work,
	groups map[string][]*EvaluatedCandidate,
	counts serversource.Counts,
) error {
	plan, err := discoveryPlan(unit, groups, counts, service.now().UnixMilli())
	if err != nil {
		return err
	}
	err = service.repository.WithWrite(ctx, func(records DiscoveryRecords) error {
		if err := records.Persist(ctx, plan); err != nil {
			return fmt.Errorf("persist import discovery: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("commit import discovery: %w", err)
	}
	return nil
}

func discoveryPlan(
	unit Work,
	groups map[string][]*EvaluatedCandidate,
	counts serversource.Counts,
	now int64,
) (DiscoveryPlan, error) {
	plan := DiscoveryPlan{Unit: unit, Counts: counts, Now: now}
	keys := make([]string, 0, len(groups))
	for id := range groups {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	for _, id := range keys {
		group, err := discoveryGroup(id, groups[id])
		if err != nil {
			return DiscoveryPlan{}, err
		}
		plan.Groups = append(plan.Groups, group)
		plan.Total += int64(len(group.Candidates))
		if len(group.Candidates) > 1 {
			plan.Multiple++
		}
	}
	return plan, nil
}

func discoveryGroup(id string, candidates []*EvaluatedCandidate) (DiscoveryGroup, error) {
	for _, candidate := range candidates {
		if candidate == nil || candidate.Item.RequirementID != id {
			return DiscoveryGroup{}, ErrCatalogInvalid
		}
		if candidate.State == "ELIGIBLE" &&
			((!candidate.Item.IsArchive() && candidate.Static == nil) ||
				(candidate.Item.IsArchive() && candidate.DAT == nil)) {
			return DiscoveryGroup{}, ErrCatalogInvalid
		}
	}
	ranks := make(map[string]int64)
	for index, candidate := range RankCandidates(candidates) {
		ranks[candidate.ID] = int64(index + 1)
	}
	result := DiscoveryGroup{RequirementID: id}
	for _, candidate := range candidates {
		details, err := json.Marshal(candidate.Details)
		if err != nil {
			return DiscoveryGroup{}, fmt.Errorf("encode candidate evidence: %w", err)
		}
		facts := firmware.FileFacts{
			RelativePath: candidate.File.RelativePath,
			Basename:     candidate.File.Basename,
			SizeBytes:    candidate.File.SizeBytes,
			MD5:          candidate.Metadata.MD5,
			SHA1:         candidate.Metadata.SHA1,
			SHA256:       candidate.Metadata.SHA256,
			CRC32:        candidate.Metadata.CRC32,
		}
		value := CandidateWrite{
			Evidence: CandidateEvidence{
				ID:            candidate.ID,
				RequirementID: id,
				Association:   candidate.Association,
				State:         candidate.State,
				Facts:         facts,
				Static:        candidate.Static,
				DAT:           candidate.DAT,
			},
			Details:       details,
			ExactBasename: candidate.File.Basename == candidate.Item.LogicalName,
		}
		rank := ranks[candidate.ID]
		if rank > 0 {
			value.Rank = &rank
		}
		value.NotSelected = notSelectedReason(candidate, rank)
		result.Candidates = append(result.Candidates, value)
	}
	return result, nil
}

func notSelectedReason(candidate *EvaluatedCandidate, rank int64) *string {
	var reason string
	switch {
	case candidate.State == "DUPLICATE_BYTES":
		reason = "DUPLICATE_BYTES"
	case rank > 1:
		reason = "LOWER_RANK"
	case candidate.State != "ELIGIBLE":
		if code, ok := candidate.Details["code"].(string); ok {
			reason = code
		} else {
			reason = "INELIGIBLE"
		}
	default:
		return nil
	}
	return &reason
}
