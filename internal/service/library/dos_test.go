package library

import (
	"encoding/json"
	"errors"
	"testing"

	"retrom/internal/model"
)

func TestDOSCandidateInspectionIgnoresObsoleteProgramAndAllowsCurrentArchive(t *testing.T) {
	t.Parallel()
	config, err := dosCandidateConfig(json.RawMessage(`{"content":{"kind":"DOS_BUNDLE","entryFile":"old.zip","entryPath":"missing/START.EXE"}}`), "new.zip")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"content":{"entryFile":"new.zip","kind":"DOS_BUNDLE"}}` {
		t.Fatalf("unexpected projection: %s", raw)
	}
	if _, err = dosCandidateConfig(json.RawMessage(`{"content":{"kind":"SINGLE_FILE","entryFile":"game.nes"}}`), ""); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("accepted non-DOS config: %v", err)
	}
}

func TestDOSCandidatePermissionPrecedesLibraryReads(t *testing.T) {
	t.Parallel()
	service := &Service{}
	if _, err := service.DOSEntryCandidates(t.Context(), model.Principal{}, "game", ""); err == nil {
		t.Fatal("anonymous inspection accepted")
	}
}
