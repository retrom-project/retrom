package launch

import (
	"context"
	"fmt"
	"time"
)

type NetplayCreator struct {
	repository  NetplayCreationRepository
	provider    PreviewProvider
	blobs       ProductBlobVerifier
	environment NetplayCreationEnvironment
}

func NewNetplayCreator(
	repository NetplayCreationRepository,
	provider PreviewProvider,
	blobs ProductBlobVerifier,
	environment NetplayCreationEnvironment,
) *NetplayCreator {
	if environment.NewID == nil {
		environment.NewID = newProductID
	}
	if environment.Now == nil {
		environment.Now = time.Now
	}
	return &NetplayCreator{repository: repository, provider: provider, blobs: blobs, environment: environment}
}

type netplayCreationPrepared struct {
	snapshot   NetplayCreationSnapshot
	plan       NetplayCreationPlan
	capability string
}

type netplayCreationOutcome struct {
	result   Created
	existing *NetplayExistingLaunch
}

func (service *NetplayCreator) CreateNetplay(ctx context.Context, request NetplayCreateRequest) (Created, error) {
	if !validNetplayCreationRequest(request) {
		return Created{}, ErrBlocked
	}
	prepared, err := service.prepare(ctx, request)
	if err != nil {
		return Created{}, err
	}
	var outcome netplayCreationOutcome
	err = service.repository.WithCreation(ctx, func(scope NetplayCreationScope) error {
		var commitErr error
		outcome, commitErr = service.commit(ctx, scope, request, prepared)
		return commitErr
	})
	if err != nil {
		return Created{}, fmt.Errorf("commit netplay creation: %w", err)
	}
	if outcome.existing != nil {
		return service.existingResult(*outcome.existing)
	}
	outcome.result.Capability = prepared.capability
	return outcome.result, nil
}

func (service *NetplayCreator) commit(
	ctx context.Context,
	scope NetplayCreationScope,
	request NetplayCreateRequest,
	prepared netplayCreationPrepared,
) (netplayCreationOutcome, error) {
	current, err := scope.Snapshot(ctx, request)
	if err != nil {
		return netplayCreationOutcome{}, fmt.Errorf("read final netplay snapshot: %w", err)
	}
	if err := validateNetplayCreation(request, current); err != nil {
		return netplayCreationOutcome{}, err
	}
	if !sameProductInputs(prepared.snapshot.Product, current.Product, false) {
		return netplayCreationOutcome{}, ErrBlocked
	}
	now := service.environment.Now().UnixMilli()
	if current.Existing != nil {
		if current.Existing.HardEnd <= now {
			return netplayCreationOutcome{}, ErrBlocked
		}
		return netplayCreationOutcome{existing: current.Existing}, nil
	}
	if prepared.snapshot.Existing != nil ||
		current.Authority.ParticipantVersion != prepared.snapshot.Authority.ParticipantVersion {
		return netplayCreationOutcome{}, ErrBlocked
	}
	plan := prepared.plan
	plan.Before = current.Authority
	plan.NowMS = now
	plan.BootstrapEnd = now + int64(5*time.Minute/time.Millisecond)
	plan.HardEnd = now + int64(8*time.Hour/time.Millisecond)
	if err := scope.Create(ctx, plan); err != nil {
		return netplayCreationOutcome{}, fmt.Errorf("persist netplay creation: %w", err)
	}
	return netplayCreationOutcome{result: netplayCreated(plan.ID, plan.BootstrapEnd, plan.HardEnd)}, nil
}

func netplayCreated(id string, bootstrap, hard int64) Created {
	return Created{
		LaunchID:             id,
		PlayURL:              "/play/" + id,
		Warnings:             []string{},
		BootstrapExpiresAtMS: bootstrap,
		HardExpiresAtMS:      hard,
	}
}
