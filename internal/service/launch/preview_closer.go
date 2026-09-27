package launch

import (
	"context"
	"fmt"
	"math"
	"time"
)

type PreviewCloseSource struct {
	ID      string
	Session SessionRecord
	Version int64
}

type PreviewCloseScope interface {
	Preview(context.Context, string) (PreviewCloseSource, bool, error)
	Finish(context.Context, PreviewCloseSource, int64) error
}

type PreviewCloseRepository interface {
	WithPreviewClose(context.Context, func(PreviewCloseScope) error) error
}

type PreviewCloser struct {
	repository PreviewCloseRepository
	policy     accessPolicy
}

func NewPreviewCloser(repository PreviewCloseRepository, now func() time.Time, matches MatchCapability) *PreviewCloser {
	return &PreviewCloser{repository: repository, policy: accessPolicy{now: now, matches: matches}}
}

// Finish closes only review previews. Product statistics never revoke a launch.
func (service *PreviewCloser) Finish(ctx context.Context, id, capability string) error {
	err := service.repository.WithPreviewClose(ctx, func(scope PreviewCloseScope) error {
		source, found, err := scope.Preview(ctx, id)
		if err != nil {
			return fmt.Errorf("read preview to close: %w", err)
		}
		now := service.policy.now().UnixMilli()
		if !found || source.Session.HardExpiresAtMS <= now || service.policy.matches == nil ||
			!service.policy.matches(capability, source.Session.CredentialHash) {
			return ErrCredential
		}
		if source.Session.State == "FINISHED" {
			return nil
		}
		if (source.Session.State != "CREATED" && source.Session.State != "ACTIVE") ||
			source.Version < 1 || source.Version == math.MaxInt64 {
			return ErrCredential
		}
		if err := scope.Finish(ctx, source, now); err != nil {
			return fmt.Errorf("close preview: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("finish preview transaction: %w", err)
	}
	return nil
}
