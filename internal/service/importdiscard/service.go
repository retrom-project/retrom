// Package importdiscard coordinates durable disposition of unpublished import content.
package importdiscard

import (
	"context"
	"fmt"
	model "retrom/internal/model/importdiscard"
	"sync"
	"time"

	"github.com/google/uuid"
)

const reason = "丢弃本批次未发布内容"

type Service struct {
	repository model.Repository
	importer   model.ImportWorkflow
	sources    model.SourceWorkflow
	now        func() time.Time
	stop       chan struct{}
	wait       sync.WaitGroup
}

func New(repository model.Repository, importer model.ImportWorkflow, sources model.SourceWorkflow, now func() time.Time) *Service {
	return &Service{repository: repository, importer: importer, sources: sources, now: now, stop: make(chan struct{})}
}

func validKey(key model.Key) bool {
	if key.Kind != "IMPORT" && key.Kind != "PEGASUS" && key.Kind != "EMULATIONSTATION" {
		return false
	}
	_, err := uuid.Parse(key.ID)
	return err == nil
}

func (service *Service) Get(ctx context.Context, kind, id string) (model.Status, error) {
	key := model.Key{Kind: kind, ID: id}
	if !validKey(key) {
		return model.Status{}, model.ErrInvalid
	}
	var result model.Status
	err := service.repository.WithRead(ctx, func(records model.Reader) error {
		batch, err := records.Batch(ctx, key)
		if err != nil {
			return failure("access discard status", err)
		}
		result = model.Status{Kind: key.Kind, ImportID: key.ID, State: "UNAVAILABLE"}
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

func (service *Service) Request(ctx context.Context, kind, id, userID string) (model.Status, error) {
	key := model.Key{Kind: kind, ID: id}
	if !validKey(key) {
		return model.Status{}, model.ErrInvalid
	}
	result, err := service.repository.CommitRequestDiscard(ctx, model.RequestDiscardCommand{
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
