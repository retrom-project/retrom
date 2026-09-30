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
	provider    PreviewProvider
	environment PreviewEnvironment
}

func NewPreviewCreator(
	provider PreviewProvider,
	environment PreviewEnvironment,
) *PreviewCreator {
	if environment.NewID == nil {
		environment.NewID = newPreviewID
	}
	return &PreviewCreator{provider: provider, environment: environment}
}

func newPreviewID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("preview identity: %w", err)
	}
	return id.String(), nil
}

// Prepare consumes current resources supplied by the source owner. It does not query review state.
func (service *PreviewCreator) Prepare(
	request ReviewPreviewRequest, snapshot PreviewSnapshot,
) (PreviewCreatePlan, string, error) {
	if err := service.validateSource(snapshot.Source, request.ClientCapabilities); err != nil {
		return PreviewCreatePlan{}, "", err
	}
	content, err := previewContent(snapshot)
	if err != nil {
		return PreviewCreatePlan{}, "", fmt.Errorf("assemble preview content: %w", err)
	}
	return service.prepare(request, snapshot.Source, content)
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

func (service *PreviewCreator) Commit(
	ctx context.Context,
	scope PreviewSessionScope,
	profileID string,
	plan PreviewCreatePlan,
	capability string,
) (ReviewPreviewCreated, error) {
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
		if plan.RestoreSourceFileRecord != restore.FileRecord || plan.RestoreFileRecord == nil ||
			plan.RestoreFormat == nil || *plan.RestoreFormat != restore.Format {
			return ReviewPreviewCreated{}, ErrSaveIncompatible
		}
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

func (service *PreviewCreator) Replay(
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
