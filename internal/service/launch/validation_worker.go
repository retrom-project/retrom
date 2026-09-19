package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	model "retrom/internal/model/launch"

	"github.com/google/uuid"
)

// ValidationWorker owns one complete attempt; evaluation uses a read snapshot and
// every write is fenced by the execution, attempt and unique worker identity.
type ValidationWorker struct {
	repository  model.ValidationWorkerRepository
	environment ValidationWorkerEnvironment
}

func NewValidationWorker(
	repository model.ValidationWorkerRepository,
	environment ValidationWorkerEnvironment,
) *ValidationWorker {
	if environment.Now == nil {
		environment.Now = time.Now
	}
	if environment.NewID == nil {
		environment.NewID = newProductID
	}
	if environment.NewTicker == nil {
		environment.NewTicker = func(period time.Duration) model.ValidationTicker {
			return validationRealTicker{time.NewTicker(period)}
		}
	}
	return &ValidationWorker{repository: repository, environment: environment}
}

func checkedValidationWorkerID(next func() (string, error)) (string, error) {
	id, err := next()
	if err != nil {
		return "", fmt.Errorf("generate validation worker: %w", err)
	}
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 || parsed.String() != id {
		return "", ErrValidationInput
	}
	return id, nil
}

func (service *ValidationWorker) Run(ctx context.Context, id string) error {
	claim, claimed, err := service.claim(ctx, id)
	if err != nil || !claimed {
		return validationStageError("validation operation", err)
	}
	if err = decodeWorkerSnapshot(&claim); err != nil {
		return service.fail(ctx, claim, err)
	}
	budget := time.Duration(*claim.Job.DeadlineMS-service.environment.Now().UnixMilli()) * time.Millisecond
	if budget <= 0 {
		return service.fail(ctx, claim, context.DeadlineExceeded)
	}
	evaluation, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	evaluation, stopEvaluation := context.WithCancelCause(evaluation)
	defer stopEvaluation(context.Canceled)
	monitor := service.monitor(evaluation, claim, stopEvaluation)
	facts, err := service.repository.Facts(evaluation, claim.Snapshot.Inputs)
	var outcome model.ValidationOutcome
	if err == nil {
		outcome, err = EvaluateValidation(claim.Snapshot.Inputs, facts)
	}
	err = errors.Join(err, monitor.Close())
	if err == nil {
		err = service.settle(evaluation, claim, facts, outcome)
	}
	if err != nil {
		return service.fail(ctx, claim, err)
	}
	return nil
}

func (service *ValidationWorker) claim(ctx context.Context, id string) (model.ValidationClaim, bool, error) {
	var claim model.ValidationClaim
	claimed := false
	err := service.repository.WithWorker(ctx, func(scope model.ValidationWorkerScope) error {
		work, found, err := scope.Jobs.Read(ctx, id)
		if err != nil {
			return validationStageError("validation operation", err)
		}
		now := service.environment.Now().UnixMilli()
		if !found || work.Kind != "VARIANT_VALIDATE" || work.State != "QUEUED" || work.AvailableMS > now {
			return nil
		}
		if validationExhausted(work, now) {
			return scope.Jobs.Recover(ctx, model.ValidationRecovery{Before: work, NowMS: now, Terminal: true})
		}
		plan, err := service.claimPlan(work, now)
		if err != nil {
			return err
		}
		if err := scope.Jobs.Claim(ctx, plan); err != nil {
			return fmt.Errorf("claim validation attempt: %w", err)
		}

		work.WorkerID, work.State = plan.WorkerID, "RUNNING"
		work.Attempt++
		work.Version++
		work.StartedMS, work.DeadlineMS, work.LeaseMS = &plan.StartedMS, &plan.DeadlineMS, &plan.LeaseMS
		claim.Job = work
		claimed = true
		return nil
	})
	return claim, claimed, validationStageError("claim validation transaction", err)
}

// Decode only after claiming, so corrupt input can settle the owned attempt.
// Preserve the parser's concrete error together with the stable input category.
func decodeWorkerSnapshot(claim *model.ValidationClaim) error {
	if err := json.Unmarshal([]byte(claim.Job.SnapshotJSON), &claim.Snapshot); err != nil {
		return fmt.Errorf("%w: decode validation snapshot: %w", ErrValidationInput, err)
	}
	return validateWorkerSnapshot(*claim)
}

func validateWorkerSnapshot(claim model.ValidationClaim) error {
	work, snapshot := claim.Job, claim.Snapshot
	digest := sha256.Sum256([]byte(work.SnapshotJSON))
	if snapshot.SchemaVersion != 1 || snapshot.Kind != "VARIANT_VALIDATE" || work.ScopeType != "GAME_VARIANT" ||
		snapshot.Scope.Type != work.ScopeType ||
		snapshot.Scope.ID != work.ScopeID ||
		snapshot.Inputs.GameVariantID != work.ScopeID ||
		snapshot.Inputs.GameID == "" || snapshot.Inputs.ProviderID == "" || snapshot.Inputs.TargetID == "" ||
		snapshot.Inputs.ValidationInputDigest == "" || snapshot.Inputs.BIOSDependencyDigest == "" ||
		work.ExecutionNo < 1 || hex.EncodeToString(digest[:]) != work.InputDigest {
		return ErrValidationInput
	}
	_, err := checkedValidationWorkerID(func() (string, error) { return snapshot.ExecutionID, nil })
	return validationStageError("validation operation", err)
}

func ownsValidationWork(current model.ValidationWork, claim model.ValidationClaim, now int64) bool {
	return validationOwnerError(current, claim, now) == nil
}

func validationOwnerError(current model.ValidationWork, claim model.ValidationClaim, now int64) error {
	if !sameValidationAttempt(current, claim.Job) {
		return model.ErrValidationOwnership
	}
	if current.LeaseMS == nil || current.DeadlineMS == nil || claim.Job.DeadlineMS == nil {
		return ErrValidationInput
	}
	if !equalValidationTime(current.StartedMS, claim.Job.StartedMS) || *current.DeadlineMS != *claim.Job.DeadlineMS {
		return model.ErrValidationOwnership
	}
	if *current.DeadlineMS <= now {
		return context.DeadlineExceeded
	}
	if *current.LeaseMS <= now {
		return errValidationLeaseExpired
	}
	return nil
}

func (service *ValidationWorker) settle(
	ctx context.Context,
	claim model.ValidationClaim,
	before model.ValidationFacts,
	outcome model.ValidationOutcome,
) error {
	err := service.repository.WithWorker(ctx, func(scope model.ValidationWorkerScope) error {
		current, found, err := scope.Jobs.Read(ctx, claim.Job.ID)
		if err != nil {
			return validationStageError("validation operation", err)
		}
		now := service.environment.Now().UnixMilli()
		if !found {
			return model.ErrValidationOwnership
		}
		if err := validationOwnerError(current, claim, now); err != nil {
			return err
		}
		after, err := scope.Facts.Facts(ctx, claim.Snapshot.Inputs)
		if err != nil {
			return validationStageError("validation operation", err)
		}
		if err = validateFinalValidation(claim.Snapshot.Inputs, before, after); err != nil {
			return validationStageError("validation operation", err)
		}
		if err = scope.Variants.Apply(
			ctx, model.ValidationVariantWrite{Inputs: claim.Snapshot.Inputs, Outcome: outcome, NowMS: now},
		); err != nil {
			return validationStageError("validation operation", err)
		}
		state, code := "FAILED", outcome.Code
		if outcome.Status == "READY" {
			state = "SUCCEEDED"
		}
		return scope.Jobs.Finish(ctx, model.ValidationTerminal{
			Claim:     claim,
			State:     state,
			Code:      code,
			NowMS:     now,
			Evaluated: true,
		})
	})
	return validationStageError("settle validation transaction", err)
}

func (service *ValidationWorker) fail(parent context.Context, claim model.ValidationClaim, cause error) error {
	if errors.Is(cause, model.ErrValidationOwnership) {
		return cause
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	err := service.repository.WithWorker(ctx, func(scope model.ValidationWorkerScope) error {
		current, found, err := scope.Jobs.Read(ctx, claim.Job.ID)
		if err != nil {
			return validationStageError("validation operation", err)
		}
		// Cleanup may close its own expired attempt, but never a replacement owner.
		if !found || !sameValidationAttempt(current, claim.Job) {
			return nil
		}
		state, code, retryable := "FAILED", validationUnavailable, true
		if errors.Is(cause, ErrValidationGameChanged) {
			state, code, retryable = "CANCELLED", "GAME_STATE_CHANGED", false
		}
		if errors.Is(cause, ErrValidationInput) {
			retryable = false
		}
		return scope.Jobs.Finish(
			ctx, model.ValidationTerminal{
				Claim:     claim,
				State:     state,
				Code:      code,
				Retryable: retryable,
				NowMS:     service.environment.Now().UnixMilli(),
			},
		)
	})
	return errors.Join(cause, err)
}

func sameValidationAttempt(current, expected model.ValidationWork) bool {
	return current.Kind == expected.Kind &&
		current.State == "RUNNING" &&
		current.ID == expected.ID &&
		current.ExecutionNo == expected.ExecutionNo &&
		current.Attempt == expected.Attempt &&
		current.WorkerID == expected.WorkerID &&
		current.InputDigest == expected.InputDigest && current.SnapshotJSON == expected.SnapshotJSON &&
		current.ScopeType == expected.ScopeType && current.ScopeID == expected.ScopeID
}

func (service *ValidationWorker) Recover(ctx context.Context) ([]string, error) {
	candidates, err := service.repository.Candidates(ctx, service.environment.Now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read validation recovery candidates: %w", err)
	}
	ready := make([]string, 0, len(candidates))
	for _, id := range candidates {
		queued, err := service.recoverOne(ctx, id)
		if err != nil {
			return nil, err
		}
		if queued {
			ready = append(ready, id)
		}
	}
	return ready, nil
}

func (service *ValidationWorker) recoverOne(ctx context.Context, id string) (bool, error) {
	queued := false
	err := service.repository.WithWorker(ctx, func(scope model.ValidationWorkerScope) error {
		current, found, err := scope.Jobs.Read(ctx, id)
		if err != nil {
			return fmt.Errorf("read recovery execution: %w", err)
		}
		if !found || current.Kind != "VARIANT_VALIDATE" {
			return nil
		}
		now := service.environment.Now().UnixMilli()
		stale := current.State == "RUNNING" && current.LeaseMS != nil && *current.LeaseMS <= now
		if current.State != "QUEUED" && !stale {
			return nil
		}
		exhausted := validationExhausted(current, now)
		if stale || exhausted {
			if err := scope.Jobs.Recover(ctx, model.ValidationRecovery{
				Before:   current,
				NowMS:    now,
				Terminal: exhausted,
			}); err != nil {
				return fmt.Errorf("recover validation execution: %w", err)
			}
		}
		queued = !exhausted && (stale || current.AvailableMS <= now)
		return nil
	})
	return queued, validationStageError("recover validation transaction", err)
}

func validationExhausted(work model.ValidationWork, now int64) bool {
	return work.Attempt >= work.MaxAttempts || work.DeadlineMS != nil && *work.DeadlineMS <= now
}

func (service *ValidationWorker) claimPlan(work model.ValidationWork, now int64) (model.ValidationClaimWrite, error) {
	if work.Version == math.MaxInt64 || now < 0 || now > math.MaxInt64-int64(validationBudget/time.Millisecond) {
		return model.ValidationClaimWrite{}, ErrValidationInput
	}
	worker, err := checkedValidationWorkerID(service.environment.NewID)
	if err != nil {
		return model.ValidationClaimWrite{}, err
	}
	started, deadline := now, now+int64(validationBudget/time.Millisecond)
	if work.StartedMS != nil {
		started = *work.StartedMS
	}
	if work.DeadlineMS != nil {
		deadline = *work.DeadlineMS
	}
	return model.ValidationClaimWrite{
		Before: work, WorkerID: worker, NowMS: now, StartedMS: started, DeadlineMS: deadline,
		LeaseMS: min(now+int64(time.Minute/time.Millisecond), deadline),
	}, nil
}

func validationStageError(stage string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", stage, err)
}

func equalValidationTime(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
