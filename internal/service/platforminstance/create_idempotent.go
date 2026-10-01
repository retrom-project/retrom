package platforminstance

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	contentcapability "retrom/internal/content/capability"
)

const createOperationID = "platforminstance.create"

// CreateIdempotent commits the directory, audit and complete replay representation together.
func (service *Service) CreateIdempotent(
	ctx context.Context, actor AuditActor, principalID, key, digest string, input CreateInput, multiDiscEnabled bool,
) (IdempotentResponse, error) {
	if principalID == "" || key == "" || digest == "" || !validText(input.Name, 1, 200, false) ||
		!validText(input.Description, 0, 10_000, true) {
		return IdempotentResponse{}, ErrInvalid
	}
	identity := IdempotencyKey{PrincipalID: principalID, Operation: createOperationID, Key: key}
	var response IdempotentResponse
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		now := service.now().UnixMilli()
		stored, found, err := scope.Idempotency.Find(ctx, identity, now)
		if err != nil {
			return repositoryError("find creation receipt", err)
		}
		if found {
			if stored.Digest != digest {
				return ErrIdempotencyReused
			}
			response = stored.Response
			response.Replayed = true
			return nil
		}
		created, err := service.createInstance(ctx, scope, actor, input, "", "PLATFORM_INSTANCE_CREATED", now)
		if err != nil {
			return err
		}
		created.ImportCapabilities = importCapabilities(created, multiDiscEnabled)
		body, err := json.Marshal(struct {
			Instance
			ImportCapabilities contentcapability.ImportCapabilities `json:"importCapabilities"`
		}{created, created.ImportCapabilities})
		if err != nil {
			return fmt.Errorf("platforminstance: encode creation response: %w", err)
		}
		pending := IdempotentResponse{
			Status:  201,
			Headers: map[string]string{"Content-Type": "application/json; charset=utf-8", "ETag": `"v1"`},
			Body:    append(body, '\n'),
		}
		if err := scope.Idempotency.Save(ctx, identity, IdempotencyRecord{
			Digest: digest, Response: pending, CreatedAtMS: now, ExpiresAtMS: now + int64(24*time.Hour/time.Millisecond),
		}); err != nil {
			return repositoryError("store creation receipt", err)
		}
		response = pending
		return nil
	})
	if err != nil {
		return IdempotentResponse{}, repositoryError("create idempotently", err)
	}
	return response, nil
}
