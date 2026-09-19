package launch

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/launch"
)

type ProjectQueries struct {
	repository model.ProjectIdentityReader
	policy     accessPolicy
}

func NewProjectQueries(
	repository model.ProjectIdentityReader,
	now func() time.Time,
	matches model.MatchCapability,
) *ProjectQueries {
	return &ProjectQueries{repository: repository, policy: accessPolicy{now: now, matches: matches}}
}

func (service *ProjectQueries) Identity(ctx context.Context, id, capability string) (string, error) {
	snapshot, found, err := service.repository.Project(ctx, id, func(source model.ConfigSource) error {
		if !validProjectAuthority(service.policy, source, capability) {
			return model.ErrCredential
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("read project identity: %w", err)
	}
	if !found {
		return "", model.ErrCredential
	}
	return ProjectIdentity(snapshot.Files)
}

func validProjectAuthority(policy accessPolicy, source model.ConfigSource, capability string) bool {
	if policy.matches == nil || !policy.matches(capability, source.CredentialHash) {
		return false
	}
	now := policy.now().UnixMilli()
	if source.Purpose == "REVIEW_PREVIEW" {
		return source.State == "ACTIVE" && source.HardEnd > now
	}
	return source.Purpose == "PRODUCT" && validConfigLifetime(source, now) &&
		(source.Delivery == "FILE_TREE_PROJECT" || source.Delivery == "SEEKABLE_PROJECT_ARCHIVE" ||
			source.Delivery == "ISOLATED_WEB_PROJECT")
}
