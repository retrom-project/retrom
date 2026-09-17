package dependencies

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	model "retrom/internal/model/dependencies"

	"retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/capability/format/arcadedat"
	"retrom/internal/capability/runtime/runtimecatalog"
)

func TestTargetRequiresUniqueProviderBinding(t *testing.T) {
	for _, bindings := range [][]runtimecatalog.Binding{
		nil,
		{{CoreID: "core", ProviderID: "a", TargetID: "target"}, {CoreID: "core", ProviderID: "b", TargetID: "target"}},
	} {
		_, err := targetForCore(runtimecatalog.Catalog{Bindings: bindings}, "core")
		if !errors.Is(err, dependencies.ErrInvalid) {
			t.Fatalf("ambiguous or absent target accepted: %v", err)
		}
	}
	target, err := targetForCore(runtimecatalog.Catalog{Bindings: []runtimecatalog.Binding{
		{CoreID: "core", ProviderID: "provider", TargetID: "target"},
		{CoreID: "core", ProviderID: "provider", TargetID: "target"},
	}}, "core")
	if err != nil || target != (model.RuntimeTarget{ProviderID: "provider", TargetID: "target"}) {
		t.Fatalf("duplicate identical binding must be accepted: %+v %v", target, err)
	}
}

func TestClaimWritesStateThroughOneScope(t *testing.T) {
	repository := &workflowRepository{}
	err := claimBuiltInDATJob(t.Context(), repository, "dat", "job", time.UnixMilli(1000))
	if err != nil || repository.transactions != 1 || !reflect.DeepEqual(repository.calls, []string{"claim", "parsing"}) {
		t.Fatalf("claim workflow: calls=%v transactions=%d error=%v", repository.calls, repository.transactions, err)
	}
	if repository.claim.DeadlineMS != 1_801_000 || repository.claim.LeaseUntilMS != 61_000 {
		t.Fatalf("claim lost deadline/lease: %+v", repository.claim)
	}
}

func TestClaimFailureStopsDirectoryTransition(t *testing.T) {
	failure := errors.New("event unavailable")
	repository := &workflowRepository{claimError: failure}
	err := claimBuiltInDATJob(t.Context(), repository, "dat", "job", time.UnixMilli(1000))
	if !errors.Is(err, failure) || !reflect.DeepEqual(repository.calls, []string{"claim"}) {
		t.Fatalf("claim failure not preserved: calls=%v error=%v", repository.calls, err)
	}
}

func TestRetainedFailedJobIsNotRecreated(t *testing.T) {
	for _, state := range []string{"FAILED", "CANCELLED"} {
		repository := &workflowRepository{job: model.Job{ID: "existing", State: state}, found: true}
		_, err := ensureBuiltInDATJob(t.Context(), repository, "dat", "sha", "parser", time.UnixMilli(1000))
		if !errors.Is(err, ErrDATParseFailed) || len(repository.calls) != 0 {
			t.Fatalf("retained %s evidence overwritten: calls=%v error=%v", state, repository.calls, err)
		}
	}
}

func TestPublicationStopsBeforeActivationAfterWriteFailure(t *testing.T) {
	failure := errors.New("index unavailable")
	repository := &workflowRepository{publicationError: failure}
	err := publishBuiltInDATCatalog(t.Context(), repository, "dat", "job", 0, 1, arcadedat.Catalog{}, time.UnixMilli(1000))
	if !errors.Is(err, failure) || !reflect.DeepEqual(repository.calls, []string{"publish"}) {
		t.Fatalf("publication advanced after failure: calls=%v error=%v", repository.calls, err)
	}
}

type workflowRepository struct {
	transactions                 int
	calls                        []string
	claim                        model.JobClaim
	claimError, publicationError error
	job                          model.Job
	found                        bool
}

func (*workflowRepository) TargetExists(context.Context, model.RuntimeTarget) (bool, error) {
	panic("unexpected target read")
}

func (*workflowRepository) FindDAT(context.Context, model.DATLookup) (model.DATState, error) {
	panic("unexpected DAT read")
}

func (*workflowRepository) CommitBootstrapDefinitions(context.Context, model.BootstrapCommand) error {
	panic("unexpected bootstrap")
}

func (repository *workflowRepository) CommitEnsureDATJob(
	_ context.Context, cmd model.EnsureDATJobCommand,
) (string, error) {
	repository.transactions++
	if repository.found {
		if repository.job.State == "FAILED" || repository.job.State == "CANCELLED" {
			return "", ErrDATParseFailed
		}
		return repository.job.ID, nil
	}
	return cmd.JobID, nil
}

func (repository *workflowRepository) CommitClaimDAT(
	_ context.Context, cmd model.ClaimDATCommand,
) error {
	repository.transactions++
	repository.calls = append(repository.calls, "claim")
	repository.claim = cmd.Claim
	if repository.claimError != nil {
		return repository.claimError
	}
	repository.calls = append(repository.calls, "parsing")
	return nil
}

func (*workflowRepository) CommitFailDAT(context.Context, model.FailDATCommand) error {
	panic("unexpected failure write")
}

func (*workflowRepository) CommitActivateDAT(context.Context, model.ActivateDATCommand) error {
	panic("unexpected activation")
}

func (repository *workflowRepository) CommitPublishDAT(
	_ context.Context, _ model.PublishDATCommand,
) error {
	repository.transactions++
	repository.calls = append(repository.calls, "publish")
	if repository.publicationError != nil {
		return repository.publicationError
	}
	repository.calls = append(repository.calls, "activate", "finish")
	return nil
}
