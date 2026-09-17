//go:build integration

package launch

import (
	"context"
	"errors"
	"testing"

	application "retrom/internal/model/launch"
	persistence "retrom/internal/repo/launch"

	"modernc.org/sqlite"
)

func TestConfigRelatedQueryFailurePreservesStorageCause(t *testing.T) {
	t.Parallel()
	fixture, created := newPlaySourceFixture(t, false, false)
	before := playRows(t, fixture.database)
	mustRPGLaunchSQL(t, fixture.database, `ALTER TABLE launch_external_files RENAME TO unavailable_launch_external_files`)
	configuration, err := fixture.launcher.Config(t.Context(), created.LaunchID, created.Capability)
	var storageError *sqlite.Error
	if !errors.As(err, &storageError) || errors.Is(err, ErrCredential) {
		t.Fatalf("related resource failure classified as missing/credential: %v", err)
	}
	assertNoConfig(t, configuration, err, storageError)
	configDraftUnchanged(t, fixture, before)
}

func TestConfigFinalAuthorityReadPreservesStorageCause(t *testing.T) {
	t.Parallel()
	fixture, created := newPlaySourceFixture(t, false, false)
	builder := configBuildHook{ConfigBuilder: fixture.launcher.runtimeBuilder, after: func() {
		mustRPGLaunchSQL(t, fixture.database, `ALTER TABLE launch_sessions RENAME TO unavailable_launch_sessions`)
	}}
	issuer := fixtureConfigIssuer(fixture, persistence.NewConfig(fixture.database), builder)
	configuration, err := issuer.Issue(t.Context(), application.SessionRef{ID: created.LaunchID}, created.Capability)
	var storageError *sqlite.Error
	if !errors.As(err, &storageError) || errors.Is(err, ErrCredential) {
		t.Fatalf("final authority cause lost: %v", err)
	}
	assertNoConfig(t, configuration, err, storageError)
	var state string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT state FROM unavailable_launch_sessions WHERE id=?`, created.LaunchID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "CREATED" {
		t.Fatalf("failed final read activated source: %s", state)
	}
}

func TestProjectIdentityReadPreservesCancellation(t *testing.T) {
	t.Parallel()
	fixture, created := newPlaySourceFixture(t, false, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	identity, err := fixture.launcher.ProjectContentIdentity(ctx, created.LaunchID, created.Capability)
	if !errors.Is(err, context.Canceled) || identity != "" {
		t.Fatalf("project cancellation cause lost: %v", err)
	}
}

func TestConfigRejectsCredentialBeforeLoadingResources(t *testing.T) {
	t.Parallel()
	for _, preview := range []bool{false, true} {
		t.Run(configDraftSourceTable(preview), func(t *testing.T) {
			fixture, created := newPlaySourceFixture(t, preview, false)
			query := `ALTER TABLE launch_external_files RENAME TO unavailable_launch_external_files`
			if preview {
				query = `ALTER TABLE review_preview_files RENAME TO unavailable_review_preview_files`
			}
			mustRPGLaunchSQL(t, fixture.database, query)
			var configuration Config
			var err error
			if preview {
				configuration, err = fixture.launcher.ReviewPreviewConfig(t.Context(), created.LaunchID, "wrong")
			} else {
				configuration, err = fixture.launcher.Config(t.Context(), created.LaunchID, "wrong")
			}
			assertNoConfig(t, configuration, err, ErrCredential)
		})
	}
}

func TestProjectRejectsCredentialBeforeLoadingFiles(t *testing.T) {
	t.Parallel()
	fixture, created := newPlaySourceFixture(t, false, false)
	mustRPGLaunchSQL(t, fixture.database, `ALTER TABLE launch_content_files RENAME TO unavailable_launch_content_files`)
	identity, err := fixture.launcher.ProjectContentIdentity(t.Context(), created.LaunchID, "wrong")
	if identity != "" || !errors.Is(err, ErrCredential) {
		t.Fatalf("unauthorized project read files: %v", err)
	}
}
