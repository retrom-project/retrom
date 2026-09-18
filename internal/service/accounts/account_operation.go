package accounts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	model "retrom/internal/model/accounts"

	"github.com/google/uuid"
)

func newAccountOperation(operation, principal, key string, value any, now int64) (model.AccountOperation, error) {
	encoded, err := json.Marshal(map[string]any{"operationId": operation, "principalId": principal, "value": value})
	if err != nil {
		return model.AccountOperation{}, fmt.Errorf("encode account operation: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return model.AccountOperation{
		PrincipalID: principal,
		Operation:   operation,
		Key:         key,
		Digest: hex.EncodeToString(
			digest[:],
		),
		Now: now,
	}, nil
}

func accountReceipt(operation model.AccountOperation, status int, body []byte) model.AccountReceipt {
	return model.AccountReceipt{
		Operation: operation,
		Status:    status,
		Body:      body,
		ExpiresAt: operation.Now + int64(
			24*time.Hour/time.Millisecond,
		),
	}
}

func newAccountAudit(
	actorID, action, resourceType, resourceID string,
	before, after any,
	now int64,
) (model.AccountAudit, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.AccountAudit{}, fmt.Errorf("create account audit identity: %w", err)
	}
	result := model.AccountAudit{
		ID:           id.String(),
		ActorID:      actorID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Now:          now,
	}
	if before != nil {
		value, err := auditJSON(before)
		if err != nil {
			return model.AccountAudit{}, err
		}
		result.BeforeJSON = &value
	}
	if after != nil {
		value, err := auditJSON(after)
		if err != nil {
			return model.AccountAudit{}, err
		}
		result.AfterJSON = &value
	}
	return result, nil
}

func auditJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode account audit: %w", err)
	}
	return string(encoded), nil
}
