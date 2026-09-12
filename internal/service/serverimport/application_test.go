package serverimport

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplicationSanitizesRootDescriptions(t *testing.T) {
	path := t.TempDir()
	service := New(Repositories{}, Options{Sources: []SourceRoot{{ID: "z", Label: "Unavailable", Path: filepath.Join(path, "missing"), Digest: "secret-digest"}, {ID: "a", Label: "Available", Path: path, Digest: "secret-digest"}}, Now: time.Now})
	roots := service.Roots()
	if len(roots) != 2 || roots[0].ID != "a" || roots[0].Status != "AVAILABLE" || roots[1].ID != "z" || roots[1].Status != "UNAVAILABLE" {
		t.Fatalf("root descriptions: %+v", roots)
	}
	encoded, err := json.Marshal(roots)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), path) || strings.Contains(string(encoded), "secret-digest") || roots[0].path != "" || roots[0].digest != "" {
		t.Fatal("source configuration leaked into root descriptions")
	}
	if service.roots["a"].path != path || service.roots["a"].digest != "secret-digest" {
		t.Fatal("describing roots changed configured source")
	}
}

func TestApplicationSignalsOnlyCommittedCreation(t *testing.T) {
	_, memory := creationFixture()
	service := New(Repositories{Creation: memory}, Options{Sources: []SourceRoot{{ID: "root", Label: "Source", Path: t.TempDir(), Digest: "root-digest"}}, Now: time.Now})
	memory.lateErr = context.Canceled
	request := CreateRequest{Kind: "BIOS_DIRECTORY", RootID: "root"}
	if _, err := service.Create(t.Context(), request, "actor"); !errors.Is(err, context.Canceled) {
		t.Fatalf("creation failure: %v", err)
	}
	if len(service.wake) != 0 {
		t.Fatal("failed creation woke worker")
	}
	memory.lateErr = nil
	result, err := service.Create(t.Context(), request, "actor")
	if err != nil || result.ID == "" || len(service.wake) != 1 {
		t.Fatalf("committed creation was not published: %+v %v", result, err)
	}
}
