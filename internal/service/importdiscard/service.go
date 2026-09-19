// Package importdiscard coordinates durable disposition of unpublished import content.
package importdiscard

import (
	"context"
	"fmt"
	"sync"
	"time"

	model "retrom/internal/model/importdiscard"

	"github.com/google/uuid"
)

const reason = "丢弃本批次未发布内容"

type Status struct {
	Kind      string  `json:"kind"`
	ImportID  string  `json:"importId"`
	State     string  `json:"state"`
	ErrorCode *string `json:"errorCode"`
}
type Service struct {
	repository model.Repository
	importer   model.ImportWorkflow
	sources    model.SourceWorkflow
	now        func() time.Time
	stop       chan struct{}
	wait       sync.WaitGroup
}

func New(
	repository model.Repository,
	importer model.ImportWorkflow,
	sources model.SourceWorkflow,
	now func() time.Time,
) *Service {
	return &Service{repository: repository, importer: importer, sources: sources, now: now, stop: make(chan struct{})}
}

func validKey(key model.Key) bool {
	if key.Kind != "IMPORT" && key.Kind != "PEGASUS" && key.Kind != "EMULATIONSTATION" {
		return false
	}
	_, err := uuid.Parse(key.ID)
	return err == nil
}

func (service *Service) Get(ctx context.Context, kind, id string) (Status, error) {
	key := model.Key{Kind: kind, ID: id}
	if !validKey(key) {
		return Status{}, model.ErrInvalid
	}
	var result Status
	err := service.repository.WithRead(ctx, func(records model.Reader) error {
		var err error
		result, err = status(ctx, records, key)
		return failure("access discard status", err)
	})
	return result, failure("access discard status", err)
}

func status(ctx context.Context, records model.Reader, key model.Key) (Status, error) {
	batch, err := records.Batch(ctx, key)
	if err != nil {
		return Status{}, failure("access discard status", err)
	}
	result := Status{Kind: key.Kind, ImportID: key.ID, State: "UNAVAILABLE"}
	if available(key.Kind, batch) {
		result.State = "AVAILABLE"
	}
	disposition, found, err := records.Disposition(ctx, key)
	if err != nil {
		return Status{}, failure("access discard status", err)
	}
	if found {
		result.State = disposition.State
		result.ErrorCode = disposition.ErrorCode
	}
	return result, nil
}

func available(kind string, batch model.Batch) bool {
	if !batch.Started || batch.State == "SCANNING" || batch.State == "AWAITING_MAPPING" {
		return false
	}
	if kind == "IMPORT" && retainedImport(batch) {
		return true
	}
	for state, count := range batch.ItemCounts {
		if count > 0 && undecided(kind, state) {
			return true
		}
	}
	return false
}

func retainedImport(batch model.Batch) bool {
	switch batch.State {
	case "QUEUED", "RUNNING", "CANCEL_REQUESTED":
		return true
	default:
		return batch.Rejected > batch.ResolvedRejected && batch.PayloadState != "RELEASED"
	}
}

func undecided(kind, state string) bool {
	if kind == "IMPORT" {
		return state != "PUBLISHED" && state != "DISCARDED"
	}
	return state != "PUBLISHED" && state != "REVIEW_DISCARDED" && state != "SKIPPED_EXISTING"
}

func (service *Service) Request(ctx context.Context, kind, id, userID string) (Status, error) {
	key := model.Key{Kind: kind, ID: id}
	if !validKey(key) {
		return Status{}, model.ErrInvalid
	}
	now := service.now().UnixMilli()
	var result Status
	err := service.repository.WithWrite(ctx, func(scope model.WriteScope) error {
		current, err := status(ctx, scope.Reader, key)
		if err != nil {
			return failure("access discard status", err)
		}
		if current.State == "UNAVAILABLE" {
			return model.ErrInvalid
		}
		if current.State != "AVAILABLE" && current.State != "FAILED" {
			result = current
			return nil
		}
		auditID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("create discard audit identity: %w", err)
		}
		if err := scope.Requests.Request(
			ctx, model.Request{
				Key:     key,
				UserID:  userID,
				AuditID: auditID.String(),
				Now:     now,
			},
		); err != nil {
			return failure("access discard status", err)
		}
		result = Status{Kind: kind, ImportID: id, State: "REQUESTED"}
		return nil
	})
	return result, failure("access discard status", err)
}

func failure(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
