package launch

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"retrom/internal/capability/runtime/runtimelaunch"
)

func assertConfigRejected(t *testing.T, configuration Config, err error, cause error) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error=%v want cause=%v", err, cause)
	}
	if _, err := configuration.MarshalJSON(); !errors.Is(err, runtimelaunch.ErrEnvelopeInvalid) {
		t.Fatalf("rejected config contains a publishable envelope: %v", err)
	}
}

func TestConfigFinalAuthorityRejectsChangedSession(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*ConfigSource){
		"finished":   func(source *ConfigSource) { source.State = "FINISHED" },
		"revoked":    func(source *ConfigSource) { source.State = "REVOKED" },
		"expired":    func(source *ConfigSource) { source.State = "EXPIRED" },
		"unknown":    func(source *ConfigSource) { source.State = "UNKNOWN" },
		"bootstrap":  func(source *ConfigSource) { source.BootstrapEnd = 1000 },
		"hard":       func(source *ConfigSource) { source.HardEnd = 1000 },
		"idle":       func(source *ConfigSource) { source.State = "ACTIVE"; value := int64(1000); source.IdleEnd = &value },
		"version":    func(source *ConfigSource) { source.Version++ },
		"overflow":   func(source *ConfigSource) { source.Version = math.MaxInt64 },
		"credential": func(source *ConfigSource) { source.CredentialHash = []byte("changed") },
		"target":     func(source *ConfigSource) { source.TargetID = "other" },
		"bundle":     func(source *ConfigSource) { source.BundleDigest = "other" },
		"purpose":    func(source *ConfigSource) { source.Purpose = "REVIEW_PREVIEW" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			issuer, repository, builder := configTestFixture()
			builder.afterBuild = func() { change(&repository.current.Source) }
			configuration, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "valid")
			assertConfigRejected(t, configuration, err, ErrCredential)
			if repository.activations != 0 {
				t.Fatal("changed session activated")
			}
		})
	}
}

func TestConfigUsesFinalClockAndPreservesCommitCause(t *testing.T) {
	t.Parallel()
	for _, boundary := range []int64{2000, 3000} {
		issuer, repository, builder := configTestFixture()
		builder.afterBuild = func() { issuer.environment.Now = func() time.Time { return time.UnixMilli(boundary) } }
		configuration, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "valid")
		assertConfigRejected(t, configuration, err, ErrCredential)
		if repository.activations != 0 {
			t.Fatal("expired session activated")
		}
	}
	issuer, repository, _ := configTestFixture()
	repository.commitErr = context.Canceled
	configuration, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "valid")
	assertConfigRejected(t, configuration, err, context.Canceled)
}

func TestConfigAcceptsConcurrentActivationAndActiveHeartbeat(t *testing.T) {
	t.Parallel()
	for _, active := range []bool{false, true} {
		issuer, repository, builder := configTestFixture()
		if active {
			repository.snapshot.Authority.Source.State = "ACTIVE"
			repository.current.Source.State = "ACTIVE"
		}
		builder.afterBuild = func() {
			repository.current.Source.State = "ACTIVE"
			repository.current.Source.Version++
			if active {
				deadline := int64(1800)
				repository.current.Source.IdleEnd = &deadline
			}
		}
		configuration, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "valid")
		if err != nil || repository.activations != 0 {
			t.Fatalf("valid active retry: error=%v writes=%d", err, repository.activations)
		}
		if _, err := configuration.MarshalJSON(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConfigRejectsRestoreChangesDuringBuild(t *testing.T) {
	t.Parallel()
	issuer, repository, builder := configTestFixture()
	builder.afterBuild = func() { repository.current.Restore.Required = true }
	configuration, err := issuer.Issue(t.Context(), SessionRef{ID: "launch"}, "valid")
	assertConfigRejected(t, configuration, err, ErrCredential)
	if repository.activations != 0 {
		t.Fatal("changed restore activated")
	}
}
