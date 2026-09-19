package processlock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	model "retrom/internal/model/maintenance"
)

type lockObserver struct {
	root    string
	records []map[string]any
}

func (observer *lockObserver) record(name string, err error, facts map[string]any) {
	text, kind := "", ""
	if err != nil {
		text, kind = strings.ReplaceAll(err.Error(), observer.root, "<root>"), fmt.Sprintf("%T", err)
	}
	observer.records = append(observer.records, map[string]any{
		"name": name, "error": text, "type": kind, "locked": errors.Is(err, model.ErrDataRootLocked),
		"descriptor": errors.Is(err, errDescriptorInvalid), "exists": errors.Is(err, os.ErrExist),
		"notExist": errors.Is(err, os.ErrNotExist), "facts": facts,
	})
}

func TestProcessLockPreservesOldGoObservations(t *testing.T) {
	expected, err := os.ReadFile("testdata/lock-old-go-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(expected)
	if hex.EncodeToString(sum[:]) != "98eabfa43a63bd74c3883cfc227599ea80c1e103812c12bf33f9e5bf36364895" {
		t.Fatal("old-Go process-lock characterization changed")
	}
	observer := &lockObserver{root: t.TempDir(), records: []map[string]any{}}
	var missing *Lock
	observer.record("nil-close", missing.Close(), nil)
	observer.record("zero-close", (&Lock{}).Close(), nil)
	recordInvalidRoots(t, observer)
	recordLockLifetime(t, observer)
	recordLockDirectory(t, observer)
	if len(observer.records) != 11 {
		t.Fatalf("case count: %d", len(observer.records))
	}
	actual, err := json.MarshalIndent(observer.records, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(append(actual, '\n')) != string(expected) {
		t.Fatalf("old-Go process-lock behavior differs:\n%s", actual)
	}
}

func recordInvalidRoots(t *testing.T, observer *lockObserver) {
	t.Helper()
	_, err := (Locker{}).Acquire("")
	observer.record("empty-root", err, nil)
	plain := filepath.Join(observer.root, "plain")
	if err := os.WriteFile(plain, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = (Locker{}).Acquire(plain)
	observer.record("file-root", err, nil)
}

func recordLockLifetime(t *testing.T, observer *lockObserver) {
	t.Helper()
	data := filepath.Join(observer.root, "data")
	first, err := (Locker{}).Acquire(data)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(data, "retrom.lock"))
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.Stat(data)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(data, "retrom.lock"))
	if err != nil {
		t.Fatal(err)
	}
	observer.record("acquire", nil, map[string]any{
		"fileMode":      fmt.Sprintf("%04o", info.Mode().Perm()),
		"directoryMode": fmt.Sprintf("%04o", directory.Mode().Perm()),
		"pid":           string(content) == fmt.Sprintf("%d\n", os.Getpid()),
	})
	_, err = (Locker{}).Acquire(data)
	observer.record("held", err, nil)
	observer.record("close", first.Close(), nil)
	observer.record("double-close", first.Close(), nil)
	second, err := (Locker{}).Acquire(data)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filepath.Join(data, "retrom.lock"))
	if err != nil {
		t.Fatal(err)
	}
	observer.record("reacquire", nil, map[string]any{"sameInode": os.SameFile(info, after)})
	observer.record("close-reacquired", second.Close(), nil)
}

func recordLockDirectory(t *testing.T, observer *lockObserver) {
	t.Helper()
	blocked := filepath.Join(observer.root, "blocked")
	if err := os.MkdirAll(filepath.Join(blocked, "retrom.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := (Locker{}).Acquire(blocked)
	observer.record("lock-path-directory", err, nil)
}
