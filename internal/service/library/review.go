package library

import (
	"context"

	"retrom/internal/model"
)

// File/CLI inspection stays outside the publication transaction. Approve locks and
// rechecks this same version before committing, so a concurrent edit cannot bypass it.
func (s *Service) approvalReadiness(ctx context.Context, p model.Principal, id string,
	version int64, coreID string,
) error {
	result, err := s.Readiness(ctx, p, []string{id}, coreID)
	if err != nil {
		return err
	}
	item := result.Items[0]
	if item.Version == nil || *item.Version != version {
		return model.ErrConflict
	}
	if item.Error != nil {
		if item.Error.Code == "REVIEW_READINESS_UNAVAILABLE" {
			return model.ErrUnavailable
		}
		return &model.RuntimeContentError{Code: item.Error.Code, Message: item.Error.Message}
	}
	if item.BIOSSatisfied == nil {
		return model.ErrUnavailable
	}
	if len(item.MissingROMMembers) > 0 {
		return &model.RuntimeContentError{
			Code:    "REVIEW_REQUIRED_FILES_MISSING",
			Message: "缺少 DAT 要求的必需文件或文件大小不符，请查看运行条件检查，补齐后再发布。",
		}
	}
	if !*item.BIOSSatisfied {
		return model.ErrBIOSMissing
	}
	return nil
}

// Withdraw changes only publication state. All content and private relations remain owned by the same Game.
func (s *Service) Withdraw(ctx context.Context, p model.Principal, id string, version int64) (model.GameDetail, error) {
	if err := p.Admin(); err != nil {
		return model.GameDetail{}, wrap(err)
	}
	if err := s.Repository.TransitionGame(ctx, id, "published", "pending_review",
		version, s.Now().UnixMilli()); err != nil {
		return model.GameDetail{}, wrap(err)
	}
	return s.Detail(ctx, p, id, "pending_review")
}
