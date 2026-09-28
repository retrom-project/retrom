package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShutdownDeadlineDoesNotReleaseResourcesUsedByBlockedWork(t *testing.T) {
	release, released := make(chan struct{}), make(chan struct{})
	progress := &shutdownProgress{phase: "blocked DAT", pending: func() []string { return []string{"indexing"} }}
	err := superviseShutdown(t.Context(), 0, progress, func(_ context.Context, begin func()) error {
		defer close(released)
		begin()
		<-release
		return nil
	})
	if !errors.Is(err, errShutdownDeadline) || !strings.Contains(err.Error(), "indexing") {
		t.Fatalf("missing deadline/diagnostics: %v", err)
	}
	select {
	case <-released:
		t.Fatal("resource released while task still running")
	default:
	}
	close(release)
	<-released
}

func TestSignalAndStartupFailureUseTheSameShutdownBudget(t *testing.T) {
	for _, failure := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		cause := errors.New("startup recovery failed")
		err := superviseShutdown(ctx, time.Second, &shutdownProgress{}, func(ctx context.Context, begin func()) error {
			if failure {
				begin()
			} else {
				cancel()
			}
			<-ctx.Done()
			return cause
		})
		cancel()
		if !errors.Is(err, cause) {
			t.Fatalf("lost shutdown result: %v", err)
		}
	}
}

func TestProcessDeadlineExitsNonzeroWithoutResourceDefers(t *testing.T) {
	if path := os.Getenv("RETROM_TEST_SHUTDOWN_MARKER"); path != "" {
		err := superviseShutdown(context.Background(), 0, &shutdownProgress{phase: "blocked worker"},
			func(_ context.Context, begin func()) error {
				defer func() {
					if err := os.WriteFile(path, []byte("unsafe cleanup"), 0o600); err != nil {
						panic(err)
					}
				}()
				begin()
				select {}
			})
		exitOnFailure(err)
		return
	}
	marker := filepath.Join(t.TempDir(), "released")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessDeadlineExitsNonzeroWithoutResourceDefers$")
	command.Env = append(os.Environ(), "RETROM_TEST_SHUTDOWN_MARKER="+marker)
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "blocked worker") {
		t.Fatalf("exit=%v output=%s", err, output)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resource defer ran: %v", err)
	}
}
