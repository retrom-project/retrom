//go:build linux

package blobstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	blobmodel "retrom/internal/model/blob"
)

func newTestDigestCoordinator(
	t *testing.T, root string, wait func(context.Context) error,
) *DigestCoordinator {
	t.Helper()
	coordinator, err := OpenDigestCoordinator(root, DigestCoordinatorOptions{Wait: wait})
	if err != nil {
		t.Fatal(err)
	}
	return coordinator
}

func acquireTestDigests(t *testing.T, coordinator *DigestCoordinator, digests ...string) blobmodel.DigestLease {
	t.Helper()
	lease, err := coordinator.Acquire(t.Context(), digests)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lease.Release(); err != nil {
			t.Error(err)
		}
	})
	return lease
}

func testDigest(prefix string) string { return prefix + strings.Repeat("0", 62) }

func TestDigestCoordinatorCanonicalStripeFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	coordinator := newTestDigestCoordinator(t, root, nil)
	digests := []string{testDigest("ff"), testDigest("00"), testDigest("ff"), strings.Repeat("0", 63) + "1"}
	before := slices.Clone(digests)
	lease := acquireTestDigests(t, coordinator, digests...)
	if !slices.Equal(digests, before) {
		t.Fatal("Acquire mutated the caller's digest set")
	}
	entries, err := os.ReadDir(filepath.Join(root, ".locks", "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("unsafe stripe file: %v, %v", info, err)
		}
	}
	if !slices.Equal(names, []string{"00.lock", "ff.lock"}) {
		t.Fatalf("stripe files = %v", names)
	}
	for _, path := range []string{".locks", ".locks/blobs"} {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("unsafe lock directory: %v, %v", info, err)
		}
	}
	path := filepath.Join(root, ".locks", "blobs", "00.lock")
	beforeRelease, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	acquireTestDigests(t, coordinator, testDigest("00"))
	afterRelease, err := os.Stat(path)
	if err != nil || !os.SameFile(beforeRelease, afterRelease) {
		t.Fatalf("Release replaced or removed the shared lock inode: %v", err)
	}
}

func TestDigestCoordinatorRejectsNoncanonicalSetsBeforeOpeningFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	coordinator := newTestDigestCoordinator(t, root, nil)
	invalid := []string{"", "00", strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("/", 64)}
	for _, digest := range invalid {
		lease, err := coordinator.Acquire(t.Context(), []string{testDigest("00"), digest})
		if lease != nil || !errors.Is(err, errInvalidBlobDigest) {
			t.Fatalf("digest %q: lease=%v err=%v", digest, lease, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, ".locks", "blobs"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid set created stripe files: %v, %v", entries, err)
	}
	acquireTestDigests(t, coordinator)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	lease, err := coordinator.Acquire(ctx, []string{testDigest("00")})
	if lease != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquire: lease=%v err=%v", lease, err)
	}
	if err := waitForDigestLock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("technical wait ignored cancellation: %v", err)
	}
}

func TestDigestCoordinatorSameProcessOrderAndCancellation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	holder := newTestDigestCoordinator(t, root, nil)
	held := acquireTestDigests(t, holder, testDigest("10"))
	waiting := make(chan struct{})
	waiter := newTestDigestCoordinator(t, root, func(ctx context.Context) error {
		close(waiting)
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		lease, err := waiter.Acquire(ctx, []string{testDigest("ff"), testDigest("10"), testDigest("00")})
		if lease != nil {
			err = errors.Join(err, lease.Release(), errors.New("contending acquisition unexpectedly succeeded"))
		}
		result <- err
	}()
	awaitDigestSignal(t, waiting)
	entries, err := os.ReadDir(filepath.Join(root, ".locks", "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !slices.Equal(names, []string{"00.lock", "10.lock"}) {
		t.Fatalf("locks were not opened in ascending order: %v", names)
	}
	heldEarlier := errors.New("earlier stripe is held while the later stripe waits")
	probeWhileWaiting := newTestDigestCoordinator(t, root, func(context.Context) error {
		return heldEarlier
	})
	if lease, err := probeWhileWaiting.Acquire(t.Context(), []string{testDigest("00")}); lease != nil || !errors.Is(err, heldEarlier) {
		t.Fatalf("first stripe was not held during the later wait: lease=%v err=%v", lease, err)
	}
	cancel()
	if err := awaitDigestResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting acquisition error = %v", err)
	}
	probe := newTestDigestCoordinator(t, root, func(context.Context) error {
		return errors.New("canceled acquisition leaked its first stripe")
	})
	acquireTestDigests(t, probe, testDigest("00"))
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	acquireTestDigests(t, probe, testDigest("10"))
}

func TestDigestCoordinatorOpenFailureUnwindsEarlierStripes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	coordinator := newTestDigestCoordinator(t, root, nil)
	target := filepath.Join(root, "not-a-lock")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".locks", "blobs", "10.lock")); err != nil {
		t.Fatal(err)
	}
	lease, err := coordinator.Acquire(t.Context(), []string{testDigest("00"), testDigest("10")})
	if lease != nil || err == nil {
		t.Fatalf("symlink stripe accepted: lease=%v err=%v", lease, err)
	}
	probe := newTestDigestCoordinator(t, root, func(context.Context) error {
		return errors.New("failed acquisition leaked an earlier stripe")
	})
	acquireTestDigests(t, probe, testDigest("00"))
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "unchanged" {
		t.Fatalf("symlink target changed: %q, %v", content, err)
	}
}

func TestDigestLeaseReleaseContinuesAfterCloseFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	coordinator := newTestDigestCoordinator(t, root, nil)
	resource, err := coordinator.Acquire(t.Context(), []string{testDigest("00")})
	if err != nil {
		t.Fatal(err)
	}
	lease, ok := resource.(*digestLease)
	if !ok {
		t.Fatalf("unexpected lease implementation %T", resource)
	}
	// Inject an invalid final descriptor: reverse release must continue to
	// close the real held descriptor even after EBADF.
	lease.descriptors = append(lease.descriptors, -1)
	first := lease.Release()
	if !errors.Is(first, unix.EBADF) || !errors.Is(lease.Release(), first) {
		t.Fatalf("release did not retain its idempotent error: %v", first)
	}
	probe := newTestDigestCoordinator(t, root, func(context.Context) error {
		return errors.New("close failure leaked another held stripe")
	})
	acquireTestDigests(t, probe, testDigest("00"))
}

func TestDigestLeaseConcurrentRelease(t *testing.T) {
	t.Parallel()
	coordinator := newTestDigestCoordinator(t, t.TempDir(), nil)
	lease := acquireTestDigests(t, coordinator, testDigest("01"))
	var workers sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		workers.Go(func() { failures <- lease.Release() })
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	acquireTestDigests(t, coordinator, testDigest("01"))
}

func TestDigestCoordinatorRejectsUnsafeDirectories(t *testing.T) {
	t.Parallel()
	for _, position := range []string{"root", "ancestor", ".locks", ".locks/blobs", "directory-mode"} {
		t.Run(position, func(t *testing.T) {
			t.Parallel()
			root, other := t.TempDir(), t.TempDir()
			configured := unsafeDigestDirectory(t, root, other, position)
			if coordinator, err := OpenDigestCoordinator(configured, DigestCoordinatorOptions{}); coordinator != nil || err == nil {
				t.Fatalf("unsafe path %s accepted: %v, %v", position, coordinator, err)
			}
		})
	}
}

func unsafeDigestDirectory(t *testing.T, root, other, position string) string {
	t.Helper()
	configured := root
	switch position {
	case "root":
		configured = filepath.Join(root, "link")
		if err := os.Symlink(other, configured); err != nil {
			t.Fatal(err)
		}
	case "ancestor":
		if err := os.Mkdir(filepath.Join(other, "data"), 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(other, link); err != nil {
			t.Fatal(err)
		}
		configured = filepath.Join(link, "data")
	case ".locks", ".locks/blobs":
		path := filepath.Join(root, position)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(other, path); err != nil {
			t.Fatal(err)
		}
	case "directory-mode":
		if err := os.Mkdir(filepath.Join(root, ".locks"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return configured
}

func TestDigestCoordinatorRejectsUnsafeLockFiles(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"mode", "hardlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			coordinator := newTestDigestCoordinator(t, root, nil)
			path := filepath.Join(root, ".locks", "blobs", "00.lock")
			switch kind {
			case "mode":
				if err := os.WriteFile(path, nil, 0o640); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				target := filepath.Join(root, "another-name")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(target, path); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			lease, err := coordinator.Acquire(t.Context(), []string{testDigest("00")})
			if lease != nil || !errors.Is(err, errUnsafeDigestPath) {
				t.Fatalf("unsafe %s accepted: lease=%v err=%v", kind, lease, err)
			}
		})
	}
}

func TestDigestCoordinatorRevalidatesPathAndRejectsUnsupportedFilesystem(t *testing.T) {
	t.Parallel()
	root, other := t.TempDir(), t.TempDir()
	coordinator := newTestDigestCoordinator(t, root, nil)
	lockRoot := filepath.Join(root, ".locks")
	if err := os.Rename(lockRoot, filepath.Join(root, "retired-locks")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, lockRoot); err != nil {
		t.Fatal(err)
	}
	if lease, err := coordinator.Acquire(t.Context(), []string{testDigest("00")}); lease != nil || err == nil {
		t.Fatalf("Acquire followed a replaced lock directory: %v, %v", lease, err)
	}
	// procfs is real but cannot offer the required persistent lock files.
	unsupported := filepath.Join("/proc", strconv.Itoa(os.Getpid()), "fd")
	if coordinator, err := OpenDigestCoordinator(unsupported, DigestCoordinatorOptions{}); coordinator != nil || !errors.Is(err, errDigestFilesystem) {
		t.Fatalf("unsupported filesystem: coordinator=%v, err=%v", coordinator, err)
	}
}

func awaitDigestSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	// This is a deadlock watchdog only. All interleavings use explicit signals.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatal("digest lock synchronization did not complete")
	}
}

func awaitDigestResult(t *testing.T, result <-chan error) error {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		t.Fatal("digest lock result did not complete")
		return errors.New("digest lock test watchdog")
	}
}
