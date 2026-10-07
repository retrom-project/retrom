package scans

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/model"
	"retrom/internal/storage"
)

func TestSourceContentRejectsSocketBeforeOpening(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	listen := net.ListenConfig{}
	listener, err := listen.Listen(t.Context(), "unix", filepath.Join(directory, "project", "game.nes"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := listener.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	s := &Service{}
	for _, name := range []string{"project/game.nes", "project"} {
		if _, _, err = s.contentFile(t.Context(), root, "unused", name, model.Directory{}); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("nonregular source content %q accepted: %v", name, err)
		}
	}
}

func TestContentReferencesCannotChangeSourceOrLogicalBoundary(t *testing.T) {
	t.Parallel()
	locators := map[string]string{"disc/game.cue": "/managed/immutable-file"}
	valid := contentReference{LogicalKey: "disc/audio/track.wav", RelativeTo: "disc/game.cue", RelativePath: "audio/track.wav"}
	if !validContentReference(valid, locators) {
		t.Fatal("safe relative reference rejected")
	}
	for _, value := range []contentReference{
		{LogicalKey: "elsewhere.wav", RelativeTo: "disc/game.cue", RelativePath: "audio/track.wav"},
		{LogicalKey: "disc/track.wav", RelativeTo: "missing.cue", RelativePath: "track.wav"},
		{LogicalKey: "disc/track.wav", RelativeTo: "disc/game.cue", RelativePath: "../track.wav"},
		{LogicalKey: "disc/track.wav", RelativeTo: "disc/game.cue", RelativePath: "/track.wav"},
		{LogicalKey: "disc/track.wav", RelativeTo: "disc/game.cue", RelativePath: `audio\track.wav`},
		{LogicalKey: "disc/track.wav", RelativeTo: "disc/game.cue", RelativePath: "track\x00.wav"},
	} {
		if validContentReference(value, locators) {
			t.Fatalf("unsafe reference accepted: %+v", value)
		}
	}
}

func TestReferencedSourceSymlinkCannotEscapeRoot(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "disc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "disc", "track.bin")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	s := &Service{Storage: store}
	if _, err = s.copyReference(t.Context(), root, "11111111-1111-4111-8111-111111111111",
		contentReference{LogicalKey: "track.bin", RelativePath: "track.bin"}, "disc/game.cue"); err == nil {
		t.Fatal("referenced symlink escaped source root")
	}
}
