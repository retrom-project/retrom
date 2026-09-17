package platforminstance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"retrom/internal/capability/runtime/platformcatalog"
)

func (service *Service) Apply(
	ctx context.Context, actor AuditActor, principalID, key string,
) (IdempotentResponse, error) {
	if principalID == "" || key == "" {
		return IdempotentResponse{}, ErrInvalid
	}
	digestBytes := sha256.Sum256([]byte(applyOperationID + "\x00" + principalID + "\x00{}"))
	digest := hex.EncodeToString(digestBytes[:])
	identity := IdempotencyKey{PrincipalID: principalID, Operation: applyOperationID, Key: key}
	var response IdempotentResponse
	err := service.repository.CommitWrite(ctx, func(scope WriteScope) error {
		now := service.now().UnixMilli()
		stored, found, err := scope.Idempotency.Find(ctx, identity, now)
		if err != nil {
			return repositoryError("find idempotency", err)
		}
		if found {
			if stored.Digest != digest {
				return ErrIdempotencyReused
			}
			response = stored.Response
			response.Replayed = true
			return nil
		}
		result, err := service.apply(ctx, scope, actor, now)
		if err != nil {
			return err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("platforminstance: encode result: %w", err)
		}
		pending := IdempotentResponse{
			Status: 200, Headers: map[string]string{"Content-Type": "application/json; charset=utf-8"}, Body: append(body, '\n'),
		}
		err = scope.Idempotency.Save(ctx, identity, IdempotencyRecord{
			Digest: digest, Response: pending, CreatedAtMS: now, ExpiresAtMS: now + int64(24*time.Hour/time.Millisecond),
		})
		if err != nil {
			return repositoryError("store idempotency", err)
		}
		response = pending
		return nil
	})
	return response, repositoryError("apply", err)
}

func (service *Service) apply(
	ctx context.Context,
	scope WriteScope,
	actor AuditActor,
	now int64,
) (ApplyResult, error) {
	catalog := platformcatalog.Current()
	if err := platformcatalog.Validate(catalog); err != nil {
		return ApplyResult{}, fmt.Errorf("%w: %w", ErrCatalogInvalid, err)
	}
	references, err := scope.Reader.CatalogReferences(ctx, catalog)
	if err != nil {
		return ApplyResult{}, repositoryError("apply catalog", err)
	}
	rows, err := scope.Reader.Directories(ctx)
	if err != nil {
		return ApplyResult{}, repositoryError("apply catalog", err)
	}
	before := projectRecommendations(catalog, references, rows)
	coveredBefore := before.Summary.ActiveCount + before.Summary.CustomizedCount + before.Summary.CoveredByEquivalentCount
	maxSortOrder := int64(0)
	for _, row := range rows {
		if !row.Deleted && row.SortOrder > maxSortOrder {
			maxSortOrder = row.SortOrder
		}
	}
	nextSortOrder := int64(100)
	if maxSortOrder > 0 {
		nextSortOrder = (maxSortOrder/100 + 1) * 100
	}
	created := make([]Instance, 0, before.Summary.MissingCount)
	createdKeys := make([]string, 0, before.Summary.MissingCount)
	for _, recommendation := range before.Items {
		if recommendation.State != StateMissing {
			continue
		}
		template := catalogTemplate(catalog, recommendation.TemplateKey)
		instance, err := service.createInstance(ctx, scope, actor, CreateInput{
			PlatformID: template.PlatformID, DefaultCoreID: template.DefaultCoreID,
			Name: template.Name, Description: template.Description, SortOrder: nextSortOrder,
		}, template.Key, "PLATFORM_INSTANCE_RECOMMENDED_CREATED", now)
		if err != nil {
			return ApplyResult{}, repositoryError("apply catalog", err)
		}
		created = append(created, instance)
		createdKeys = append(createdKeys, template.Key)
		nextSortOrder += 100
	}
	rows, err = scope.Reader.Directories(ctx)
	if err != nil {
		return ApplyResult{}, repositoryError("apply catalog", err)
	}
	after := projectRecommendations(catalog, references, rows)
	return ApplyResult{
		CatalogVersion: catalog.Version, CreatedTemplateKeys: createdKeys, Created: created,
		Summary: ApplySummary{
			CreatedCount: len(created), CoveredCount: coveredBefore,
			SuppressedCount: before.Summary.SuppressedCount, RemainingMissingCount: after.Summary.MissingCount,
		},
		Items: after.Items,
	}, nil
}
