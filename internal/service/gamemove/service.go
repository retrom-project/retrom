package gamemove

import (
	"context"
	"fmt"

	"retrom/internal/capability/content/corevalidation"
	model "retrom/internal/model/gamemove"

	"github.com/google/uuid"
)

type Service struct {
	repository model.Repository
	validation model.ValidationResolver
}

func New(repository model.Repository, validation model.ValidationResolver) *Service {
	return &Service{repository: repository, validation: validation}
}

func (service *Service) Preview(ctx context.Context, request model.PreviewRequest) (model.Impact, error) {
	if request.GameID == "" || request.TargetPlatformInstanceID == "" || request.ExpectedVersion < 1 {
		return model.Impact{}, model.ErrInvalid
	}
	subject, err := service.repository.ImpactSubject(ctx, request.GameID, request.TargetPlatformInstanceID)
	if err != nil {
		return model.Impact{}, fmt.Errorf("read game move impact: %w", err)
	}
	if subject.GameID == "" {
		subject.GameID = request.GameID
	}
	if subject.GameVersion != request.ExpectedVersion ||
		subject.SourcePlatformInstanceID == subject.TargetPlatformInstanceID ||
		subject.SourcePlatformID != subject.TargetPlatformID {
		return model.Impact{}, model.ErrImpactStale
	}
	inputDigest, err := service.validationDigest(ctx, request.GameID, subject)
	if err != nil {
		return model.Impact{}, err
	}
	status, code, err := service.variantStatus(ctx, request.GameID, subject)
	if err != nil {
		return model.Impact{}, err
	}
	blockers := variantBlockers(status, code)
	return model.Impact{
		Action:                   "MOVE_GAME",
		GameID:                   request.GameID,
		GameVersion:              subject.GameVersion,
		SourcePlatformInstanceID: subject.SourcePlatformInstanceID,
		TargetPlatformInstanceID: subject.TargetPlatformInstanceID,
		TargetPlatformVersion:    subject.TargetPlatformVersion,
		TargetCoreID:             subject.TargetCoreID,
		TargetProviderID:         subject.TargetProviderID,
		TargetID:                 subject.TargetID,
		TargetDATVersionID:       subject.TargetDATVersionID,
		ValidationInputDigest:    inputDigest,
		VariantStatus:            status,
		BlockerCodes:             blockers,
	}, nil
}

func (service *Service) validationDigest(
	ctx context.Context, gameID string, subject model.ImpactSubject,
) (string, error) {
	if service.validation == nil {
		return "", model.ErrValidationUnavailable
	}
	biosSnapshot, _, _, err := service.validation.ResolveBIOS(
		ctx, subject.TargetProviderID, subject.TargetID, subject.ContentLogicalName,
	)
	if err != nil {
		return "", fmt.Errorf("resolve game move BIOS: %w", err)
	}
	digest, err := corevalidation.ProviderValidationInputDigest(
		subject.TargetProviderID, subject.TargetID, gameID, subject.TargetDATVersionID, biosSnapshot,
	)
	if err != nil {
		return "", fmt.Errorf("create game move validation digest: %w", err)
	}
	return digest, nil
}

func (service *Service) variantStatus(
	ctx context.Context, gameID string, subject model.ImpactSubject,
) (string, string, error) {
	variant, found, err := service.repository.Variant(ctx, model.VariantQuery{
		GameID: gameID, CoreID: subject.TargetCoreID, ProviderID: subject.TargetProviderID,
		TargetID: subject.TargetID, DATVersionID: subject.TargetDATVersionID,
	})
	if err != nil {
		return "", "", fmt.Errorf("read game move variant: %w", err)
	}
	if !found || (variant.Status == "BLOCKED" && variant.CompatibilityCode == "VALIDATION_PENDING") {
		return "NEEDS_VALIDATION", "VARIANT_VALIDATION_REQUIRED", nil
	}
	return variant.Status, variant.CompatibilityCode, nil
}

func variantBlockers(status, code string) []string {
	if status == "READY" {
		return []string{}
	}
	return []string{code}
}

func (service *Service) Move(ctx context.Context, request model.MoveRequest) (model.MoveResult, error) {
	if request.GameID == "" || request.TargetPlatformInstanceID == "" || request.ExpectedVersion < 1 ||
		request.NowMS < 0 || !validImpact(request) {
		return model.MoveResult{}, model.ErrInvalid
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return model.MoveResult{}, fmt.Errorf("create game move audit ID: %w", err)
	}
	err = service.repository.WithMove(ctx, func(scope model.MoveScope) error {
		changed, err := scope.UpdateGame(
			ctx,
			request.GameID,
			request.TargetPlatformInstanceID,
			request.ExpectedVersion,
			request.NowMS,
		)
		if err != nil {
			return fmt.Errorf("update moved game: %w", err)
		}
		if !changed {
			return model.ErrVersionConflict
		}
		if err := scope.Audit(ctx, model.AuditEvent{
			ID: auditID.String(), Action: "GAME_MOVED", ResourceType: "GAME", ResourceID: request.GameID,
			Before: map[string]any{"platformInstanceId": request.Impact.SourcePlatformInstanceID},
			After: map[string]any{
				"platformInstanceId": request.TargetPlatformInstanceID,
				"targetCoreId":       request.Impact.TargetCoreID,
				"variantStatus":      request.Impact.VariantStatus,
			},
			Actor: request.Actor, CreatedAtMS: request.NowMS,
		}); err != nil {
			return fmt.Errorf("write game move audit: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.MoveResult{}, fmt.Errorf("move game: %w", err)
	}
	return model.MoveResult{
		GameID: request.GameID, PlatformInstanceID: request.TargetPlatformInstanceID,
		Version: request.ExpectedVersion + 1, UpdatedAtMS: request.NowMS,
	}, nil
}

func validImpact(request model.MoveRequest) bool {
	impact := request.Impact
	return impact.Action == "MOVE_GAME" && impact.GameID == request.GameID &&
		impact.GameVersion == request.ExpectedVersion &&
		impact.TargetPlatformInstanceID == request.TargetPlatformInstanceID &&
		impact.TargetCoreID != "" && impact.TargetProviderID != "" && impact.TargetID != ""
}

func (service *Service) QueuedJobState(ctx context.Context, jobID string) (string, error) {
	if jobID == "" {
		return "", model.ErrInvalid
	}
	state, err := service.repository.QueuedJobState(ctx, jobID)
	if err != nil {
		return "", fmt.Errorf("read game move validation job: %w", err)
	}
	return state, nil
}

func (service *Service) ScrapeCandidates(ctx context.Context, gameID string) (model.ScrapeCandidatesResult, error) {
	if gameID == "" {
		return model.ScrapeCandidatesResult{}, model.ErrInvalid
	}
	runID, found, err := service.repository.LatestScrapeRun(ctx, gameID)
	if err != nil {
		return model.ScrapeCandidatesResult{}, fmt.Errorf("read game scrape run: %w", err)
	}
	if !found {
		return model.ScrapeCandidatesResult{Items: []model.CandidateRecord{}}, nil
	}
	records, err := service.repository.ScrapeCandidates(ctx, runID)
	if err != nil {
		return model.ScrapeCandidatesResult{}, fmt.Errorf("read game scrape candidates: %w", err)
	}
	if records == nil {
		records = []model.CandidateRecord{}
	}
	return model.ScrapeCandidatesResult{RunID: &runID, Items: records}, nil
}
