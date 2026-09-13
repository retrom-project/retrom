package launch

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/runtimebundle"
	"retrom/internal/runtimelaunch"
)

type configTestRepository struct {
	snapshot                  ConfigSnapshot
	current                   ConfigAuthority
	loadErr, commitErr        error
	transactions, activations int
}

func (repository *configTestRepository) Load(_ context.Context, _ SessionRef, authorize ConfigAuthorization) (ConfigSnapshot, bool, error) {
	if repository.loadErr != nil {
		return ConfigSnapshot{}, false, repository.loadErr
	}
	if err := authorize(repository.snapshot.Authority.Source); err != nil {
		return ConfigSnapshot{}, false, err
	}
	return repository.snapshot, true, nil
}

func (repository *configTestRepository) WithActivation(_ context.Context, work func(ConfigActivation) error) error {
	repository.transactions++
	if err := work(repository); err != nil {
		return err
	}
	return repository.commitErr
}

func (repository *configTestRepository) Current(context.Context, SessionRef) (ConfigAuthority, bool, error) {
	return repository.current, true, nil
}

func (repository *configTestRepository) Activate(context.Context, ConfigActivationPlan) error {
	repository.activations++
	return nil
}

type configTestBuilder struct {
	cause      error
	afterBuild func()
	inputs     []runtimebundle.Input
}

func (builder *configTestBuilder) Target(string, string) (runtimebundle.Target, bool) {
	return runtimebundle.Target{Inputs: builder.inputs, TargetOptionsSchema: runtimebundle.TargetOptionsSchema{
		"type": "object", "additionalProperties": false, "properties": map[string]any{}, "required": []any{},
	}}, true
}
func (*configTestBuilder) BundleSHA256(string, string) (string, bool) { return "bundle", true }
func (builder *configTestBuilder) Build(runtimelaunch.Input) ([]byte, error) {
	if builder.afterBuild != nil {
		builder.afterBuild()
	}
	return []byte(`{}`), builder.cause
}

func configTestFixture() (*ConfigIssuer, *configTestRepository, *configTestBuilder) {
	source := ConfigSource{
		State: "CREATED", Version: 2, BootstrapEnd: 2000, HardEnd: 3000,
		ProviderID: "provider", TargetID: "target", BundleDigest: "bundle", DetectorProfile: "RPG2000",
		Purpose: "PRODUCT",
	}
	repository := &configTestRepository{
		snapshot: ConfigSnapshot{Authority: ConfigAuthority{Source: source}},
		current:  ConfigAuthority{Source: source},
	}
	builder := &configTestBuilder{}
	issuer := NewConfigIssuer(repository, builder, ConfigEnvironment{
		Now:     func() time.Time { return time.UnixMilli(1000) },
		Matches: func(capability string, _ []byte) bool { return capability == "valid" },
	})
	return issuer, repository, builder
}

func TestConfigBuildFailureCannotActivate(t *testing.T) {
	t.Parallel()
	issuer, repository, builder := configTestFixture()
	cause := errors.New("provider unavailable")
	builder.cause = cause
	configuration, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "valid")
	if !errors.Is(err, cause) || repository.transactions != 0 {
		t.Fatalf("build failure entered activation: transactions=%d error=%v", repository.transactions, err)
	}
	if _, err := configuration.MarshalJSON(); !errors.Is(err, runtimelaunch.ErrEnvelopeInvalid) {
		t.Fatalf("failed config published bytes: %v", err)
	}
}
