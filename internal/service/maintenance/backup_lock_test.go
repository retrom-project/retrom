package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"retrom/internal/testkit/testsupport"

	"retrom/internal/bootstrap/config"
	"retrom/internal/model/diagnostics"
	model "retrom/internal/model/maintenance"
)

func TestBackupLockFailurePreventsCheckpoint(t *testing.T) {
	failure := errors.New("lock unavailable")
	for _, test := range []struct {
		name              string
		acquire, expected error
	}{
		{"held", model.ErrDataRootLocked, ErrBackupOffline},
		{"failure", failure, failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			configuration, output := lockBackupFixture(t)
			locker := &backupLocker{failure: test.acquire}
			repository := &backupLockRepository{}
			_, err := New(repository, time.Now, locker, &testsupport.DiagnosticRecorder{}).Backup(t.Context(), configuration, output)
			if !errors.Is(err, test.expected) || locker.acquires != 1 || repository.calls != 0 {
				t.Fatalf("acquisition boundary: err=%v acquire=%d checkpoint=%d", err, locker.acquires, repository.calls)
			}
			if locker.dataRoot != configuration.DataDir {
				t.Fatal("lease used another data root")
			}
		})
	}
}

func TestBackupCheckpointFailureReleasesLeaseAndRetainsCause(t *testing.T) {
	checkpointFailure := errors.New("checkpoint unavailable")
	closeFailure := errors.New("close unavailable")
	for _, releaseErr := range []error{nil, closeFailure} {
		configuration, output := lockBackupFixture(t)
		events := []string{}
		lease := &backupLease{failure: releaseErr, events: &events}
		locker := &backupLocker{lease: lease, events: &events}
		repository := &backupLockRepository{failure: checkpointFailure, events: &events}
		reporter := &testsupport.DiagnosticRecorder{}
		_, err := New(repository, time.Now, locker, reporter).Backup(t.Context(), configuration, output)
		if !errors.Is(err, checkpointFailure) || errors.Is(err, closeFailure) ||
			locker.acquires != 1 || repository.calls != 1 || lease.closes != 1 {
			t.Fatalf("release boundary: err=%v acquire=%d checkpoint=%d close=%d",
				err, locker.acquires, repository.calls, lease.closes)
		}
		if repository.path != configuration.DBPath {
			t.Fatal("checkpoint used another database")
		}
		if !slices.Equal(events, []string{"acquire", "checkpoint", "close"}) {
			t.Fatalf("lease must enclose checkpoint: events=%v", events)
		}
		reports := reporter.Events()
		if releaseErr == nil {
			if len(reports) != 0 {
				t.Fatal("successful close reported a failure")
			}
		} else if len(reports) != 1 || reports[0] != diagnostics.CleanupFailure("close", "", "*errors.errorString") {
			t.Fatalf("secondary failure must be reported exactly once: %+v", reports)
		}
	}
}

func TestInvalidBackupDoesNotAcquireLease(t *testing.T) {
	locker := &backupLocker{failure: errors.New("must not acquire")}
	repository := &backupLockRepository{}
	_, err := New(repository, time.Now, locker, &testsupport.DiagnosticRecorder{}).Backup(t.Context(), config.Maintenance{}, "relative")
	if !errors.Is(err, model.ErrInvalidBundle) || locker.acquires != 0 || repository.calls != 0 {
		t.Fatalf("validation order: err=%v acquire=%d checkpoint=%d", err, locker.acquires, repository.calls)
	}
}

type backupLocker struct {
	lease    model.DataRootLease
	failure  error
	acquires int
	dataRoot string
	events   *[]string
}

func (locker *backupLocker) Acquire(_ context.Context, dataRoot string) (model.DataRootLease, error) {
	locker.acquires++
	if locker.events != nil {
		*locker.events = append(*locker.events, "acquire")
	}
	locker.dataRoot = dataRoot
	return locker.lease, locker.failure
}

type backupLease struct {
	closes  int
	failure error
	events  *[]string
}

func (lease *backupLease) Close() error {
	lease.closes++
	if lease.events != nil {
		*lease.events = append(*lease.events, "close")
	}
	return lease.failure
}

type backupLockRepository struct {
	model.Repository
	calls   int
	path    string
	failure error
	events  *[]string
}

func (repository *backupLockRepository) Checkpoint(_ context.Context, path string) error {
	repository.calls++
	if repository.events != nil {
		*repository.events = append(*repository.events, "checkpoint")
	}
	repository.path = path
	return repository.failure
}

func lockBackupFixture(t *testing.T) (config.Maintenance, string) {
	t.Helper()
	root := t.TempDir()
	dependencies := filepath.Join(root, "dependencies")
	manifest := filepath.Join(dependencies, "dat", "emulatorjs", "4.2.3", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		t.Fatal(err)
	}
	contents := []byte("{\"schema_version\":8,\"emulatorjs\":{\"version\":\"4.2.3\"},\"cores\":[]}")
	if err := os.WriteFile(manifest, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := os.ReadFile("../../../data/runtime-target-bindings/v1/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dependencies, "runtime-target-bindings", "v1", "catalog.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, catalog, 0o600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "data")
	return config.Maintenance{
		DataDir: data, DBPath: filepath.Join(data, "retrom.db"), DependencyRoot: dependencies,
		DependencyVersions: []string{"4.2.3"}, ActiveEJSVersion: "4.2.3",
	}, filepath.Join(root, "backup")
}

func TestMaintenanceRequiresAnExplicitDiagnosticReporter(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("missing reporter was deferred until resource cleanup")
		}
	}()
	New(nil, nil, nil, nil)
}
