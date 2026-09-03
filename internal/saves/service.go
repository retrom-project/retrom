package saves

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"retrom/internal/blobstore"
	retromruntime "retrom/internal/runtime"
)

const (
	MaxRequestBytes    = int64(270 << 20)
	maxScreenshotBytes = int64(10 << 20)
	maxPixels          = int64(40_000_000)
)

var (
	ErrCredential             = errors.New("LAUNCH_CREDENTIAL_INVALID")
	ErrInvalid                = errors.New("SAVE_INVALID")
	ErrTooLarge               = errors.New("SAVE_TOO_LARGE")
	ErrSequenceReused         = errors.New("SAVE_SEQUENCE_REUSED")
	ErrCheckpointInvalid      = errors.New("RPG_CHECKPOINT_INVALID")
	ErrCheckpointIncompatible = errors.New("RPG_CHECKPOINT_INCOMPATIBLE")
	ErrCheckpointUnavailable  = errors.New("RPG_CHECKPOINT_UNAVAILABLE")
)

type Service struct {
	database    *sql.DB
	blobs       *blobstore.Store
	credentials *retromruntime.Credentials
	now         func() time.Time
}

func New(
	database *sql.DB,
	blobs *blobstore.Store,
	credentials *retromruntime.Credentials,
	now func() time.Time,
) *Service {
	return &Service{database: database, blobs: blobs, credentials: credentials, now: now}
}

type ManualResult struct {
	ResourceKind     string
	SaveStateID      string
	ValidationID     string
	CheckpointFormat string
	ScreenshotURL    *string
	SizeBytes        int64
	PayloadSHA256    string
	CreatedAtMS      int64
	Name             string
	DiscIndex        *int
	Version          int64
	ActiveDurationMS int64
}

func (result ManualResult) MarshalJSON() ([]byte, error) {
	if result.ResourceKind == "RPG_RUNTIME_VALIDATION_CHECKPOINT" {
		contents, err := json.Marshal(struct {
			ResourceKind     string `json:"resourceKind"`
			ValidationID     string `json:"validationId"`
			CheckpointFormat string `json:"checkpointFormat"`
			SizeBytes        int64  `json:"sizeBytes"`
			PayloadSHA256    string `json:"sha256"`
			CreatedAtMS      int64  `json:"createdAtMs"`
		}{
			result.ResourceKind, result.ValidationID, result.CheckpointFormat,
			result.SizeBytes, result.PayloadSHA256, result.CreatedAtMS,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal validation checkpoint result: %w", err)
		}
		return contents, nil
	}
	contents, err := json.Marshal(struct {
		ResourceKind     string  `json:"resourceKind"`
		SaveStateID      string  `json:"saveStateId"`
		CheckpointFormat string  `json:"checkpointFormat"`
		ScreenshotURL    *string `json:"screenshotUrl"`
		CreatedAtMS      int64   `json:"createdAtMs"`
	}{
		result.ResourceKind, result.SaveStateID, result.CheckpointFormat,
		result.ScreenshotURL, result.CreatedAtMS,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal save checkpoint result: %w", err)
	}
	return contents, nil
}

func (result *ManualResult) UnmarshalJSON(contents []byte) error {
	var wire struct {
		ResourceKind     string  `json:"resourceKind"`
		SaveStateID      string  `json:"saveStateId"`
		ValidationID     string  `json:"validationId"`
		CheckpointFormat string  `json:"checkpointFormat"`
		ScreenshotURL    *string `json:"screenshotUrl"`
		SizeBytes        int64   `json:"sizeBytes"`
		PayloadSHA256    string  `json:"sha256"`
		CreatedAtMS      int64   `json:"createdAtMs"`
	}
	if err := json.Unmarshal(contents, &wire); err != nil {
		return fmt.Errorf("unmarshal checkpoint result: %w", err)
	}
	*result = ManualResult{
		ResourceKind: wire.ResourceKind, SaveStateID: wire.SaveStateID, ValidationID: wire.ValidationID,
		CheckpointFormat: wire.CheckpointFormat,
		ScreenshotURL:    wire.ScreenshotURL, SizeBytes: wire.SizeBytes, PayloadSHA256: wire.PayloadSHA256,
		CreatedAtMS: wire.CreatedAtMS,
	}
	return nil
}

type CheckpointStatus struct {
	CheckpointFormat string                 `json:"checkpointFormat"`
	Availability     CheckpointAvailability `json:"availability"`
}

type CheckpointAvailability struct {
	Available bool    `json:"available"`
	Reason    *string `json:"reason"`
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
