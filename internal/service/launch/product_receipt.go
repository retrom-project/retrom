package launch

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
)

func productReceipt(created Created, now int64) (ProductReceipt, error) {
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
		return ProductReceipt{}, fmt.Errorf("encode product receipt: %w", err)
	}
	return ProductReceipt{
		Status:      status,
		Body:        body,
		Created:     created,
		CreatedAtMS: now,
		ExpiresAtMS: now + 86_400_000,
	}, nil
}

func (service *ProductCreator) replay(command ProductCreateCommand, receipt ProductReceipt) (ProductReceipt, error) {
	if subtle.ConstantTimeCompare([]byte(command.Digest), []byte(receipt.Digest)) != 1 {
		return ProductReceipt{}, ErrIdempotencyKeyReused
	}
	var created Created
	if err := json.Unmarshal(receipt.Body, &created); err != nil {
		return ProductReceipt{}, fmt.Errorf("decode product receipt: %w", err)
	}
	switch receipt.Status {
	case 201:
		id, err := checkedProductID(func() (string, error) { return created.LaunchID, nil })
		if err != nil {
			return ProductReceipt{}, err
		}
		capability, _, err := service.environment.SignCapability(id)
		if err != nil {
			return ProductReceipt{}, fmt.Errorf("sign replayed product capability: %w", err)
		}
		created.Capability = capability
	case 202:
		if created.Status != "VALIDATION_PENDING" {
			return ProductReceipt{}, ErrBlocked
		}
		if _, err := checkedProductID(func() (string, error) { return created.JobID, nil }); err != nil {
			return ProductReceipt{}, err
		}
	default:
		return ProductReceipt{}, ErrBlocked
	}
	receipt.Created, receipt.Replayed = created, true
	return receipt, nil
}
