package gamecontent

import (
	"context"
	"errors"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/contentcapability"
	"retrom/internal/service/payloadrelease"
)

var (
	ErrInvalid              = errors.New("GAME_CONTENT_INVALID")
	ErrIdempotencyKeyReused = errors.New("IDEMPOTENCY_KEY_REUSED")
	ErrExecutionLost        = errors.New("GAME_CONTENT_EXECUTION_LOST")
)

type Scheduled struct {
	GameID  string `json:"gameId"`
	JobID   string `json:"jobId"`
	State   string `json:"state"`
	Version int64  `json:"version"`
}
type Service struct {
	repository             Repository
	blobs                  *blobstore.Store
	payloadReleases        ReleaseSignal
	gc                     payloadrelease.GCStager
	multiDiscImportEnabled bool
	now                    func() time.Time
}

func New(repository Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

func (service *Service) WithPayloadRelease(signal ReleaseSignal) *Service {
	service.payloadReleases = signal
	return service
}

func (service *Service) WithMultiDiscImportEnabled(enabled bool) *Service {
	service.multiDiscImportEnabled = enabled
	return service
}

type replacementValidationError struct{ code string }

func (err *replacementValidationError) Error() string { return err.code }

type inputEnvelope struct {
	SchemaVersion int         `json:"schemaVersion"`
	Kind          string      `json:"kind"`
	Scope         inputScope  `json:"scope"`
	ExecutionID   string      `json:"executionId"`
	Inputs        JobSnapshot `json:"inputs"`
}
type inputScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func (service *Service) Schedule(
	ctx context.Context,
	gameID, uploadID string,
	expectedVersion int64,
) (Scheduled, error) {
	result, _, err := service.schedule(ctx, gameID, uploadID, expectedVersion, contentcapability.ModeStandard, "", "")
	return result, err
}

func (service *Service) ScheduleMode(
	ctx context.Context,
	gameID, uploadID, mode string,
	expectedVersion int64,
) (Scheduled, error) {
	result, _, err := service.schedule(ctx, gameID, uploadID, expectedVersion, mode, "", "")
	return result, err
}

func (service *Service) ScheduleIdempotent(
	ctx context.Context,
	gameID, uploadID string,
	expectedVersion int64,
	key, digest string,
) (Scheduled, bool, error) {
	return service.ScheduleIdempotentMode(
		ctx,
		gameID,
		uploadID,
		contentcapability.ModeStandard,
		expectedVersion,
		key,
		digest,
	)
}

func (service *Service) ScheduleIdempotentMode(
	ctx context.Context,
	gameID, uploadID, mode string,
	expectedVersion int64,
	key, digest string,
) (Scheduled, bool, error) {
	if key == "" || len(digest) != 64 {
		return Scheduled{}, false, ErrInvalid
	}
	return service.schedule(ctx, gameID, uploadID, expectedVersion, mode, key, digest)
}

func pointerText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (service *Service) WithGCStager(gc payloadrelease.GCStager) *Service {
	service.gc = gc
	return service
}
