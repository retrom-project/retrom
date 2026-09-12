package launch

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/google/uuid"
)

type PreviewCreator struct {
	repository  PreviewCreationRepository
	provider    PreviewProvider
	environment PreviewEnvironment
}

func NewPreviewCreator(
	repository PreviewCreationRepository,
	provider PreviewProvider,
	environment PreviewEnvironment,
) *PreviewCreator {
	if environment.NewID == nil {
		environment.NewID = newPreviewID
	}
	return &PreviewCreator{repository: repository, provider: provider, environment: environment}
}

func newPreviewID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("preview identity: %w", err)
	}
	return id.String(), nil
}

func (service *PreviewCreator) Create(ctx context.Context, request ReviewPreviewRequest) (ReviewPreviewCreated, error) {
	if request.ImportItemID == "" || request.ActorUserID == "" || request.IdempotencyKey == "" {
		return ReviewPreviewCreated{}, ErrReviewPreviewUnavailable
	}
	receipt, found, err := service.repository.Replay(ctx, request.ActorUserID, request.IdempotencyKey)
	if err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("read preview replay: %w", err)
	}
	if found {
		return service.replay(request, receipt)
	}
	snapshot, found, err := service.repository.Snapshot(ctx, request.ImportItemID)
	if err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("read preview snapshot: %w", err)
	}
	if !found {
		return ReviewPreviewCreated{}, ErrReviewPreviewUnavailable
	}
	if err := service.validateSource(snapshot.Source, request.ClientCapabilities); err != nil {
		return ReviewPreviewCreated{}, err
	}
	content, err := previewContent(snapshot)
	if err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("assemble preview content: %w", err)
	}
	plan, capability, err := service.prepare(request, snapshot.Source, content)
	if err != nil {
		return ReviewPreviewCreated{}, err
	}
	var result ReviewPreviewCreated
	err = service.repository.WithCreation(ctx, func(scope PreviewCreationScope) error {
		var createErr error
		result, createErr = service.commit(ctx, scope, plan, capability)
		return createErr
	})
	if err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("create preview: %w", err)
	}
	return result, nil
}

func (service *PreviewCreator) prepare(
	request ReviewPreviewRequest,
	source PreviewSource,
	content PreviewContent,
) (PreviewCreatePlan, string, error) {
	id, err := service.environment.NewID()
	if err != nil {
		return PreviewCreatePlan{}, "", err
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 {
		return PreviewCreatePlan{}, "", ErrReviewPreviewUnavailable
	}
	capability, hash, err := service.environment.SignCapability(id)
	if err != nil {
		return PreviewCreatePlan{}, "", err
	}
	plan := PreviewCreatePlan{Request: request, Source: source, Content: content, ID: id, CredentialHash: hash}
	if source.DeliveryProfile == "ISOLATED_WEB_PROJECT" {
		if service.environment.SignIsolation == nil {
			return PreviewCreatePlan{}, "", ErrBlocked
		}
		ticket, signErr := service.environment.SignIsolation(id)
		if signErr != nil {
			return PreviewCreatePlan{}, "", signErr
		}
		plan.Isolation = &ticket
	}
	// Preparation has no open transaction; time and signing may call external adapters.
	plan.NowMS = service.environment.Now().UnixMilli()
	return plan, capability, nil
}

func (service *PreviewCreator) commit(
	ctx context.Context,
	scope PreviewCreationScope,
	plan PreviewCreatePlan,
	capability string,
) (ReviewPreviewCreated, error) {
	receipt, found, err := scope.Replay(ctx, plan.Request.ActorUserID, plan.Request.IdempotencyKey)
	if err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("read final preview replay: %w", err)
	}
	if found {
		return service.replay(plan.Request, receipt)
	}
	current, profileID, found, err := scope.Current(ctx, plan.Request)
	if err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("read final preview source: %w", err)
	}
	if !found || !reflect.DeepEqual(current, plan.Source) {
		return ReviewPreviewCreated{}, ErrReviewPreviewUnavailable
	}
	restore, err := readPreviewRestore(ctx, scope, plan.Request)
	if err != nil {
		return ReviewPreviewCreated{}, err
	}
	plan.NowMS = service.environment.Now().UnixMilli()
	if plan.NowMS < 0 || plan.NowMS > math.MaxInt64-7_200_000 {
		return ReviewPreviewCreated{}, ErrReviewPreviewUnavailable
	}
	plan.BootstrapEnd, plan.HardEnd = plan.NowMS+300_000, plan.NowMS+7_200_000
	if plan.Request.RestoreFromPreviewID != nil {
		if err := validatePreviewRestore(restore, plan); err != nil {
			return ReviewPreviewCreated{}, err
		}
		plan.RestoreBlobID, plan.RestoreFormat = &restore.BlobID, &restore.Format
	}
	if plan.Isolation != nil && profileID == "" {
		return ReviewPreviewCreated{}, ErrReviewPreviewUnavailable
	}
	plan.ProfileID = profileID
	plan.Source.Title = strings.TrimSpace(plan.Source.Title)
	if plan.Source.Title == "" {
		plan.Source.Title = plan.Content.LogicalName
	}
	if err := scope.Create(ctx, plan); err != nil {
		return ReviewPreviewCreated{}, fmt.Errorf("persist preview plan: %w", err)
	}
	return ReviewPreviewCreated{
		PreviewID: plan.ID, PlayURL: "/admin/review-previews/" + plan.ID, Capability: capability,
	}, nil
}

func (service *PreviewCreator) replay(
	request ReviewPreviewRequest,
	receipt PreviewReceipt,
) (ReviewPreviewCreated, error) {
	if receipt.ImportItemID != request.ImportItemID ||
		!reflect.DeepEqual(receipt.RestoreFromPreviewID, request.RestoreFromPreviewID) {
		return ReviewPreviewCreated{}, ErrReviewPreviewUnavailable
	}
	id, err := uuid.Parse(receipt.ID)
	if err != nil || id.Version() != 7 {
		return ReviewPreviewCreated{}, ErrReviewPreviewUnavailable
	}
	capability, _, err := service.environment.SignCapability(receipt.ID)
	if err != nil {
		return ReviewPreviewCreated{}, err
	}
	return ReviewPreviewCreated{
		PreviewID: receipt.ID, PlayURL: "/admin/review-previews/" + receipt.ID, Capability: capability,
	}, nil
}

func (service *PreviewCreator) validateSource(source PreviewSource, capabilities Capabilities) error {
	if service.provider == nil {
		return ErrReviewPreviewUnavailable
	}
	target, found := service.provider.Target(source.ProviderID, source.TargetID)
	bundle, hasBundle := service.provider.BundleSHA256(source.ProviderID, source.TargetID)
	if !found || !hasBundle || bundle != source.BundleSHA256 {
		return ErrReviewPreviewUnavailable
	}
	if target.Capabilities.RequiresThreads &&
		(!capabilities.SecureContext || !capabilities.CrossOriginIsolated || !capabilities.SharedArrayBuffer) {
		return ErrBlocked
	}
	for _, input := range target.Inputs {
		if input.Role == "game" {
			return nil
		}
	}
	return ErrReviewPreviewUnavailable
}
