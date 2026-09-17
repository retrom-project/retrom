package launch

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	model "retrom/internal/model/launch"
)

func productReceipt(created model.Created, now int64) (model.ProductReceipt, error) {
	var body []byte
	var err error
	status := 201
	if created.Status == "VALIDATION_PENDING" {
		status = 202
		body, err = json.Marshal(struct {
			JobID        string `json:"jobId"`
			RetryAfterMS int64  `json:"retryAfterMs"`
			Status       string `json:"status"`
		}{created.JobID, created.RetryAfterMS, created.Status})
	} else {
		body, err = json.Marshal(created)
	}
	if err != nil {
		return model.ProductReceipt{}, fmt.Errorf("encode product receipt: %w", err)
	}
	return model.ProductReceipt{
		Status:        status,
		Body:          body,
		Created: created,
		CreatedAtMS:   now,
		ExpiresAtMS:   now + 86_400_000,
	}, nil
}

func (service *ProductCreator) replay(command model.ProductCreateCommand, receipt model.ProductReceipt) (model.ProductReceipt, error) {
	if subtle.ConstantTimeCompare([]byte(command.Digest), []byte(receipt.Digest)) != 1 {
		return model.ProductReceipt{}, model.ErrIdempotencyKeyReused
	}
	var created model.Created
	if err := json.Unmarshal(receipt.Body, &created); err != nil {
		return model.ProductReceipt{}, fmt.Errorf("decode product receipt: %w", err)
	}
	switch receipt.Status {
	case 201:
		id, err := checkedProductID(func() (string, error) { return created.LaunchID, nil })
		if err != nil {
			return model.ProductReceipt{}, err
		}
		capability, _, err := service.environment.SignCapability(id)
		if err != nil {
			return model.ProductReceipt{}, fmt.Errorf("sign replayed product capability: %w", err)
		}
		created.Capability = capability
	case 202:
		if created.Status != "VALIDATION_PENDING" {
			return model.ProductReceipt{}, model.ErrBlocked
		}
		if _, err := checkedProductID(func() (string, error) { return created.JobID, nil }); err != nil {
			return model.ProductReceipt{}, err
		}
	default:
		return model.ProductReceipt{}, model.ErrBlocked
	}
	receipt.Created, receipt.Replayed = created, true
	return receipt, nil
}
