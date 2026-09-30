package libraryimport

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"retrom/internal/cleanup"
	launch "retrom/internal/service/launch"
)

// ReviewPreviews supplies current facts to Launch and fences them inside the review-owned transaction.
type ReviewPreviews struct {
	repository  ReviewPreviewRepository
	provider    launch.PreviewProvider
	environment launch.PreviewEnvironment
}

func NewReviewPreviews(
	repository ReviewPreviewRepository, provider launch.PreviewProvider, environment launch.PreviewEnvironment,
) *ReviewPreviews {
	if environment.Now == nil {
		environment.Now = time.Now
	}
	return &ReviewPreviews{repository: repository, provider: provider, environment: environment}
}

func (service *ReviewPreviews) Create(
	ctx context.Context, request launch.ReviewPreviewRequest,
) (launch.ReviewPreviewCreated, error) {
	if request.ImportItemID == "" || request.ActorUserID == "" || request.IdempotencyKey == "" {
		return launch.ReviewPreviewCreated{}, launch.ErrReviewPreviewUnavailable
	}
	sessions := launch.NewPreviewCreator(service.provider, service.environment)
	receipt, found, err := service.repository.Replay(ctx, request.ActorUserID, request.IdempotencyKey)
	if err != nil {
		return launch.ReviewPreviewCreated{}, fmt.Errorf("read review preview replay: %w", err)
	}
	if found {
		result, err := sessions.Replay(request, receipt)
		if err != nil {
			return launch.ReviewPreviewCreated{}, fmt.Errorf("replay review preview: %w", err)
		}
		return result, nil
	}
	snapshot, found, err := service.repository.Snapshot(ctx, request.ImportItemID)
	if err != nil {
		return launch.ReviewPreviewCreated{}, fmt.Errorf("read review preview input: %w", err)
	}
	if !found {
		return launch.ReviewPreviewCreated{}, launch.ErrReviewPreviewUnavailable
	}
	plan, capability, err := sessions.Prepare(request, snapshot)
	if err != nil {
		return launch.ReviewPreviewCreated{}, fmt.Errorf("prepare review preview session: %w", err)
	}
	return service.createPrepared(ctx, sessions, snapshot, plan, capability)
}

func (service *ReviewPreviews) commit(
	ctx context.Context, sessions *launch.PreviewCreator, snapshot launch.PreviewSnapshot,
	plan launch.PreviewCreatePlan, capability string,
) (launch.ReviewPreviewCreated, error) {
	request := plan.Request
	var result launch.ReviewPreviewCreated
	err := service.repository.WithCreation(ctx, func(scope ReviewPreviewScope) error {
		receipt, found, err := scope.Replay(ctx, request.ActorUserID, request.IdempotencyKey)
		if err != nil {
			return fmt.Errorf("read final review preview replay: %w", err)
		}
		if found {
			result, err = sessions.Replay(request, receipt)
			if err != nil {
				return fmt.Errorf("replay final review preview: %w", err)
			}
			return nil
		}
		current, profile, found, err := scope.Current(ctx, request)
		if err != nil {
			return fmt.Errorf("read final review preview input: %w", err)
		}
		if !found || !reflect.DeepEqual(current, snapshot) {
			return launch.ErrReviewPreviewUnavailable
		}
		result, err = sessions.Commit(ctx, scope, profile, plan, capability)
		if err != nil {
			return fmt.Errorf("persist review preview session: %w", err)
		}
		return nil
	})
	if err != nil {
		return launch.ReviewPreviewCreated{}, fmt.Errorf("create review preview: %w", err)
	}
	return result, nil
}

func (service *ReviewPreviews) prepareRestore(ctx context.Context, sessions *launch.PreviewCreator,
	plan launch.PreviewCreatePlan,
) (launch.PreviewCreatePlan, error) {
	if plan.Request.RestoreFromPreviewID == nil {
		return plan, nil
	}
	restore, found, err := service.repository.Restore(ctx, *plan.Request.RestoreFromPreviewID)
	if err != nil {
		return launch.PreviewCreatePlan{}, fmt.Errorf("read prepared restore: %w", err)
	}
	if !found {
		return launch.PreviewCreatePlan{}, launch.ErrSaveIncompatible
	}
	prepared, err := sessions.PrepareRestore(ctx, plan, restore)
	if err != nil {
		return launch.PreviewCreatePlan{}, fmt.Errorf("prepare restore: %w", err)
	}
	return prepared, nil
}

func (service *ReviewPreviews) createPrepared(ctx context.Context, sessions *launch.PreviewCreator,
	snapshot launch.PreviewSnapshot, plan launch.PreviewCreatePlan, capability string,
) (launch.ReviewPreviewCreated, error) {
	prepared, err := service.prepareRestore(ctx, sessions, plan)
	if err != nil {
		return launch.ReviewPreviewCreated{}, err
	}
	committed := false
	defer func() { service.discardPreparedRestore(ctx, prepared, committed) }()
	result, err := service.commit(ctx, sessions, snapshot, prepared, capability)
	committed = err == nil && result.PreviewID == plan.ID
	return result, err
}

func (service *ReviewPreviews) discardPreparedRestore(ctx context.Context, plan launch.PreviewCreatePlan,
	committed bool,
) {
	if committed || plan.RestoreFileRecord == nil || service.environment.DiscardPreviewPayload == nil {
		return
	}
	err := service.environment.DiscardPreviewPayload(context.WithoutCancel(ctx), plan.ID)
	cleanup.Error("discard uncommitted preview restore", err)
}
