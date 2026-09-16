package tagging

import (
	"context"
	"fmt"

	model "retrom/internal/model/tagging"
)

// EnsureCommonTags atomically creates missing starter tags and preserves existing ones.
func (service *Service) EnsureCommonTags(ctx context.Context, actorUserID string) (model.CommonTagsResult, error) {
	if !model.ValidID(actorUserID) {
		return model.CommonTagsResult{}, model.ErrInvalid
	}
	candidates, err := service.buildCommonCandidates()
	if err != nil {
		return model.CommonTagsResult{}, err
	}
	result, err := service.commands.CommitEnsureCommonTags(ctx, model.EnsureCommonTagsCommand{
		ActorUserID: actorUserID,
		NowMS:       service.now().UnixMilli(),
		Candidates:  candidates,
	})
	return result, repositoryError("ensure common tags", err)
}

func (service *Service) buildCommonCandidates() ([]model.CommonTagCandidate, error) {
	names := model.CommonTagNames()
	candidates := make([]model.CommonTagCandidate, 0, len(names))
	for _, rawName := range names {
		name, key, search, err := model.NormalizeName(rawName)
		if err != nil {
			return nil, fmt.Errorf("tagging: normalize common tag %q: %w", rawName, err)
		}
		tagID, err := service.newID()
		if err != nil {
			return nil, fmt.Errorf("tagging: create common tag id: %w", err)
		}
		auditID, err := service.newID()
		if err != nil {
			return nil, fmt.Errorf("tagging: create common tag audit id: %w", err)
		}
		candidates = append(candidates, model.CommonTagCandidate{
			TagID: tagID, AuditID: auditID,
			Name: name, NameKey: key, SearchText: search,
		})
	}
	return candidates, nil
}
