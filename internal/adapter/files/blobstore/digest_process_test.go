//go:build linux

package blobstore

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const digestProcessRootEnv = "RETROM_TEST_DIGEST_PROCESS_ROOT"

func TestDigestCoordinatorProcessIsolationAndCrashRelease(t *testing.T) {
	if root := os.Getenv(digestProcessRootEnv); root != "" {
		runDigestCrashWorker(t, root)
		return
	}
	t.Parallel()
	root := t.TempDir()
	coordinator := newTestDigestCoordinator(t, root, nil)
	lease := acquireTestDigests(t, coordinator, testDigest("42"))
	lockPath := filepath.Join(root, ".locks", "blobs", "42.lock")
	original, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	child, input, lines, complete := startDigestCrashWorker(t, root)
	if line := awaitDigestProcessLine(t, lines); line != "WAIT" {
		t.Fatalf("child bypassed parent's held stripe: %q", line)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	writeDigestProcessCommand(t, input, "retry")
	if line := awaitDigestProcessLine(t, lines); line != "HELD" {
		t.Fatalf("child did not acquire the released stripe: %q", line)
	}
	// The child intentionally exits without Release. Its kernel descriptor,
	// not an in-memory registry, must keep this parent's next attempt blocked.
	heldByChild := errors.New("child currently owns the stripe")
	probe := newTestDigestCoordinator(t, root, func(context.Context) error {
		return heldByChild
	})
	blocked, err := probe.Acquire(t.Context(), []string{testDigest("42")})
	if blocked != nil || !errors.Is(err, heldByChild) {
		t.Fatalf("parent bypassed child's held stripe: lease=%v err=%v", blocked, err)
	}
	writeDigestProcessCommand(t, input, "crash")
	err = child.Wait()
	complete()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("crash worker exit = %v, want 23", err)
	}
	acquireTestDigests(t, probe, testDigest("42"))
	current, err := os.Stat(lockPath)
	if err != nil || !os.SameFile(original, current) {
		t.Fatalf("process exit removed/replaced lock inode: %v", err)
	}
}

func startDigestCrashWorker(
	t *testing.T, root string,
) (*exec.Cmd, io.WriteCloser, <-chan string, func()) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	child := exec.CommandContext(
		ctx, binary, "-test.run=^TestDigestCoordinatorProcessIsolationAndCrashRelease$", "-test.timeout=5s",
	)
	child.Env = append(os.Environ(), digestProcessRootEnv+"="+root)
	child.Stderr = os.Stderr
	input, err := child.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	output, err := child.StdoutPipe()
	if err != nil {
		cancel()
		if closeErr := input.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	finished := false
	t.Cleanup(func() {
		cancel()
		if !finished {
			// A failed assertion still kills and reaps the isolated child.
			if err := child.Wait(); err != nil && ctx.Err() == nil {
				t.Error(err)
			}
		}
		if err := input.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	})
	lines := make(chan string, 4)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			lines <- "read error: " + err.Error()
		}
	}()
	return child, input, lines, func() { finished = true }
}

func runDigestCrashWorker(t *testing.T, root string) {
	t.Helper()
	commands := digestProcessCommands(t.Context())
	coordinator := newTestDigestCoordinator(t, root, func(ctx context.Context) error {
		if _, err := fmt.Fprintln(os.Stdout, "WAIT"); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case command, ok := <-commands:
			if !ok || command != "retry" {
				return errors.New("missing explicit retry signal")
			}
			return nil
		}
	})
	lease, err := coordinator.Acquire(t.Context(), []string{testDigest("42")})
	if err != nil || lease == nil {
		t.Fatalf("worker acquire: lease=%v, err=%v", lease, err)
	}
	if _, err := fmt.Fprintln(os.Stdout, "HELD"); err != nil {
		t.Fatal(err)
	}
	select {
	case command, ok := <-commands:
		if !ok || command != "crash" {
			t.Fatal("missing explicit crash signal")
		}
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	// Deliberately bypass Go cleanup to exercise OS release after a crash.
	os.Exit(23)
}

func digestProcessCommands(ctx context.Context) <-chan string {
	commands := make(chan string)
	go func() {
		defer close(commands)
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			select {
			case commands <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	return commands
}

func writeDigestProcessCommand(t *testing.T, input io.Writer, command string) {
	t.Helper()
	if _, err := fmt.Fprintln(input, command); err != nil {
		t.Fatal(err)
	}
}

func awaitDigestProcessLine(t *testing.T, lines <-chan string) string {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case line, ok := <-lines:
		if !ok {
			t.Fatal("digest worker exited before its synchronization signal")
		}
		return line
	case <-timer.C:
		t.Fatal("digest worker synchronization did not complete")
		return ""
	}
}
