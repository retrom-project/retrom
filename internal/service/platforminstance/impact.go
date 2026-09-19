package platforminstance

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/platforminstance"

	"github.com/google/uuid"
)

func (service *Service) CoreImpact(
	ctx context.Context, instanceID, coreID string, expected int64,
) (model.CoreImpactResult, error) {
	if instanceID == "" || coreID == "" || expected < 1 {
		return model.CoreImpactResult{}, model.ErrInvalid
	}
	var facts model.CoreImpactFacts
	err := service.repository.WithRead(ctx, func(reader model.Reader) error {
		var err error
		facts, err = reader.CoreImpact(ctx, instanceID, coreID, expected)
		if err != nil {
			return fmt.Errorf("read core impact: %w", err)
		}
		return nil
	})
	if err != nil {
		return model.CoreImpactResult{}, repositoryError("core impact", err)
	}
	return projectCoreImpact(instanceID, coreID, facts), nil
}

func (service *Service) ChangeDefaultCore(
	ctx context.Context,
	instanceID, coreID string,
	expected int64,
	digest string,
	confirmBlocked bool,
	actor model.AuditActor,
) (model.DefaultCoreChangeResult, error) {
	if instanceID == "" || coreID == "" || expected < 1 || digest == "" {
		return model.DefaultCoreChangeResult{}, model.ErrImpactStale
	}
	var change model.DefaultCoreChangeResult
	err := service.repository.WithWrite(ctx, func(scope model.WriteScope) error {
		facts, err := scope.Reader.CoreImpact(ctx, instanceID, coreID, expected)
		if err != nil {
			return fmt.Errorf("read core impact: %w", err)
		}
		result := projectCoreImpact(instanceID, coreID, facts)
		actualDigest := ImpactDigest(result.Impact)
		if subtle.ConstantTimeCompare([]byte(actualDigest), []byte(digest)) != 1 {
			return model.ErrImpactStale
		}
		if result.Counts["blocked"] > 0 && !confirmBlocked {
			return model.ErrDefaultCoreBlocked
		}
		now := service.now().UnixMilli()
		changed, err := scope.Directories.ChangeDefaultCore(ctx, model.DefaultCoreChange{
			ID: instanceID, CoreID: coreID, ExpectedVersion: expected, UpdatedAtMS: now,
		})
		if err != nil {
			return fmt.Errorf("change default core: %w", err)
		}
		if !changed {
			return model.ErrVersionConflict
		}
		auditID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("platforminstance: create audit id: %w", err)
		}
		if err := scope.Directories.RecordAudit(ctx, model.AuditEvent{
			ID: auditID.String(), Action: "PLATFORM_DEFAULT_CORE_CHANGED",
			ResourceType: "PLATFORM_INSTANCE", ResourceID: instanceID,
			Actor:  actor,
			Before: map[string]any{"version": expected},
			After:  map[string]any{"defaultCoreId": coreID, "version": expected + 1, "impactDigest": digest}, CreatedAtMS: now,
		}); err != nil {
			return fmt.Errorf("record default core audit: %w", err)
		}
		change = model.DefaultCoreChangeResult{Version: expected + 1, UpdatedAtMS: now}
		return nil
	})
	return change, repositoryError("change default core", err)
}

func projectCoreImpact(instanceID, coreID string, facts model.CoreImpactFacts) model.CoreImpactResult {
	counts := map[string]int64{"ready": 0, "needsValidation": 0, "blocked": 0}
	items := make([]model.CoreImpactItem, 0, len(facts.Games))
	for _, game := range facts.Games {
		status := "NEEDS_VALIDATION"
		switch {
		case game.VariantStatus != nil && *game.VariantStatus == "READY":
			status = "READY"
			counts["ready"]++
		case game.VariantStatus != nil:
			status = "BLOCKED"
			counts["blocked"]++
		default:
			counts["needsValidation"]++
		}
		items = append(items, model.CoreImpactItem{
			GameID:      game.GameID,
			Status:      status,
			BlockerCode: game.TargetCompatibilityCode,
		})
	}
	return model.CoreImpactResult{
		Impact: model.CoreImpact{
			Action: "CHANGE_DEFAULT_CORE", PlatformInstanceID: instanceID,
			PlatformInstanceVersion: facts.PlatformInstanceVersion, CoreID: coreID,
			ProviderID: facts.ProviderID, TargetID: facts.TargetID, BundleSHA256: facts.BundleSHA256,
			DATVersionID: facts.DATVersionID, Games: facts.Games,
		},
		Counts: counts,
		Items:  items,
	}
}

func ImpactDigest(value model.CoreImpact) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
