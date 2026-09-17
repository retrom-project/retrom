// Package importdiscard coordinates durable disposition of unpublished import content.
package importdiscard

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

const reason = "丢弃本批次未发布内容"

type Service struct {
	repository Repository
	importer   ImportWorkflow
	sources    SourceWorkflow
	now        func() time.Time
	stop       chan struct{}
	wait       sync.WaitGroup
}

func New(repository Repository, importer ImportWorkflow, sources SourceWorkflow, now func() time.Time) *Service {
	return &Service{repository: repository, importer: importer, sources: sources, now: now, stop: make(chan struct{})}
}

func validKey(key Key) bool {
	if key.Kind != "IMPORT" && key.Kind != "PEGASUS" && key.Kind != "EMULATIONSTATION" {
		return false
	}
	_, err := uuid.Parse(key.ID)
	return err == nil
}

func (service *Service) Get(ctx context.Context, kind, id string) (Status, error) {
	key := Key{Kind: kind, ID: id}
	if !validKey(key) {
		return Status{}, ErrInvalid
	}
	var result Status
	err := service.repository.WithRead(ctx, func(records Reader) error {
		batch, err := records.Batch(ctx, key)
		if err != nil {
			return failure("access discard status", err)
		}
		result = Status{Kind: key.Kind, ImportID: key.ID, State: "UNAVAILABLE"}
		disposition, found, err := records.Disposition(ctx, key)
		if err != nil {
			return failure("access discard status", err)
		}
		if batch.Started && batch.State != "SCANNING" && batch.State != "AWAITING_MAPPING" {
			result.State = "AVAILABLE"
		}
		if found {
			result.State = disposition.State
			result.ErrorCode = disposition.ErrorCode
		}
		return nil
	})
	return result, failure("access discard status", err)
}

func (service *Service) Request(ctx context.Context, kind, id, userID string) (Status, error) {
	key := Key{Kind: kind, ID: id}
	if !validKey(key) {
		return Status{}, ErrInvalid
	}
	result, err := service.repository.CommitRequestDiscard(ctx, RequestDiscardCommand{
		Key: key, UserID: userID, NowMS: service.now().UnixMilli(),
	})
	return result, failure("access discard status", err)
}

func failure(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
