package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	model "retrom/internal/model/launch"

	"github.com/google/uuid"
)

// ValidationScheduler operates within its caller's creation transaction. The caller
// publishes resume signals only after that transaction and its receipt commit.
type ValidationScheduler struct {
	repository  model.ValidationJobRepository
	environment model.ValidationEnvironment
}

func NewValidationScheduler(
	repository model.ValidationJobRepository,
	environment model.ValidationEnvironment,
) *ValidationScheduler {
	if environment.NewID == nil {
		environment.NewID = newProductID
	}
	return &ValidationScheduler{repository: repository, environment: environment}
}

func newProductID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("product identity: %w", err)
	}
	return id.String(), nil
}

func checkedProductID(next func() (string, error)) (string, error) {
	id, err := next()
	if err != nil {
		return "", fmt.Errorf("generate product identity: %w", err)
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 || parsed.String() != id {
		return "", model.ErrBlocked
	}
	return id, nil
}

func ValidationDedupeKey(variantID, digest string) string {
	canonical, _ := json.Marshal(map[string]string{"gameVariantId": variantID, "validationInputDigest": digest})
	value := sha256.New()
	_, _ = value.Write([]byte("retrom-job-dedupe-v1\x00VARIANT_VALIDATE\x00"))
	_, _ = value.Write(canonical)
	return hex.EncodeToString(value.Sum(nil))
}

func BindCurrentGameStateDigest(baseDigest string, gameVersion int64, sourceManifestDigest string) string {
	canonical, _ := json.Marshal(struct {
		BaseDigest           string `json:"baseDigest"`
		GameVersion          int64  `json:"gameVersion"`
		SourceManifestDigest string `json:"sourceManifestDigest"`
	}{baseDigest, gameVersion, sourceManifestDigest})
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func (service *ValidationScheduler) Queue(ctx context.Context, inputs model.ValidationInputs) (model.ValidationQueued, error) {
	dedupe := ValidationDedupeKey(inputs.GameVariantID, inputs.ValidationInputDigest)
	previous, found, err := service.repository.Find(ctx, dedupe)
	if err != nil {
		return model.ValidationQueued{}, fmt.Errorf("read validation job: %w", err)
	}
	if found && previous.State != "FAILED" && previous.State != "CANCELLED" {
		return model.ValidationQueued{JobID: previous.ID}, nil
	}
	if found && (previous.State != "FAILED" || !previous.Retryable) {
		return model.ValidationQueued{}, model.ErrBlocked
	}
	plan, err := service.prepare(inputs, dedupe, previous, found)
	if err != nil {
		return model.ValidationQueued{}, err
	}
	if err := service.repository.Write(ctx, plan); err != nil {
		return model.ValidationQueued{}, fmt.Errorf("write validation job: %w", err)
	}
	return model.ValidationQueued{JobID: plan.JobID, Queued: true}, nil
}

func (service *ValidationScheduler) prepare(
	inputs model.ValidationInputs,
	dedupe string,
	previous model.ValidationJob,
	retry bool,
) (model.ValidationJobWrite, error) {
	snapshot := model.ValidationSnapshot{
		SchemaVersion: 1, Kind: "VARIANT_VALIDATE",
		Scope: model.ValidationScope{Type: "GAME_VARIANT", ID: inputs.GameVariantID}, Inputs: inputs,
	}
	plan := model.ValidationJobWrite{VariantID: inputs.GameVariantID, DedupeKey: dedupe, ExecutionNo: 1, Retry: retry}
	var err error
	if retry {
		snapshot, err = retryValidationSnapshot(previous, inputs.GameVariantID)
		plan.JobID, plan.ExecutionNo, plan.PreviousVersion = previous.ID, previous.ExecutionNo+1, previous.Version
	} else {
		plan.JobID, err = checkedProductID(service.environment.NewID)
	}
	if err != nil {
		return model.ValidationJobWrite{}, err
	}
	snapshot.ExecutionID, err = checkedProductID(service.environment.NewID)
	if err != nil {
		return model.ValidationJobWrite{}, err
	}
	plan.NowMS = service.environment.Now().UnixMilli()
	if plan.NowMS < 0 {
		return model.ValidationJobWrite{}, model.ErrBlocked
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return model.ValidationJobWrite{}, fmt.Errorf("encode validation input: %w", err)
	}
	hash := sha256.Sum256(encoded)
	plan.SnapshotJSON, plan.InputDigest = string(encoded), hex.EncodeToString(hash[:])
	payload, err := json.Marshal(struct {
		SchemaVersion    int   `json:"schemaVersion"`
		InputExecutionNo int64 `json:"inputExecutionNo"`
	}{1, plan.ExecutionNo})
	if err != nil {
		return model.ValidationJobWrite{}, fmt.Errorf("encode validation payload: %w", err)
	}
	plan.PayloadJSON = string(payload)
	return plan, nil
}

func retryValidationSnapshot(previous model.ValidationJob, variantID string) (model.ValidationSnapshot, error) {
	var snapshot model.ValidationSnapshot
	if err := json.Unmarshal([]byte(previous.SnapshotJSON), &snapshot); err != nil {
		return model.ValidationSnapshot{}, fmt.Errorf("read retried validation input: %w", err)
	}
	if snapshot.SchemaVersion != 1 || snapshot.Kind != "VARIANT_VALIDATE" ||
		snapshot.Scope.Type != "GAME_VARIANT" || snapshot.Scope.ID != variantID ||
		previous.ExecutionNo < 1 || previous.ExecutionNo == math.MaxInt64 || previous.Version == math.MaxInt64 {
		return model.ValidationSnapshot{}, model.ErrBlocked
	}
	return snapshot, nil
}
