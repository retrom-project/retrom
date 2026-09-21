package scummvm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectorUsesOnlyJSONStdoutAndPropagatesToolFailure(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(t.TempDir(), "detector")
	data := detectorJSON(t, []DetectedGame{detectedGame("", "en")})
	body := "#!/bin/sh\nprintf 'diagnostic on stderr\\n' >&2\nprintf '%s' '" + string(data) + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	detector := New(func(context.Context) (Tool, error) {
		return Tool{Path: script, UpstreamCommit: testCommit, Engines: []string{"sky"}}, nil
	})
	result, err := detector.Detect(t.Context(), root, strings.Repeat("a", 64))
	if err != nil || result.AutomaticSelection == "" {
		t.Fatalf("valid stdout rejected: %+v %v", result, err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = detector.Detect(t.Context(), root, strings.Repeat("a", 64))
	if !errors.Is(err, ErrToolFailed) {
		t.Fatalf("tool failure became empty success: %v", err)
	}
}

func TestDetectorRejectsSymlinksRegularRootsAndCanceledRequests(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "regular")
	if err := os.WriteFile(regular, []byte("game"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkTree(t.Context(), regular); !errors.Is(err, ErrInputInvalid) {
		t.Fatalf("regular file root accepted: %v", err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := checkTree(t.Context(), root); !errors.Is(err, ErrInputInvalid) {
		t.Fatalf("symlink accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := checkTree(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was ignored: %v", err)
	}
}

func TestDetectorOutputIsBounded(t *testing.T) {
	stdout := &limitedOutput{limit: 4}
	if _, err := stdout.Write([]byte("oversized")); !errors.Is(err, ErrLimit) || len(stdout.data) != 4 {
		t.Fatalf("stdout limit not enforced: %v", err)
	}
	stderr := &limitedOutput{limit: 4, discardOverflow: true}
	for range 3 {
		if size, err := stderr.Write([]byte("diagnostic")); err != nil || size != 10 || len(stderr.data) > 4 {
			t.Fatalf("stderr truncation failed: %v", err)
		}
	}
}
