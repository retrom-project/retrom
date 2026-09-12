package emulationstationimport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/serversource"
)

type readerCheckpointContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (ctx *readerCheckpointContext) Done() <-chan struct{} {
	ctx.checks++
	if ctx.checks == 2 {
		ctx.cancel()
	}
	return ctx.Context.Done()
}

func TestESFrozenReaderChecksContextAfterAcquiringSlot(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "gamelist.xml")
	if err := os.WriteFile(file, []byte("<gameList/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checkpoint := &readerCheckpointContext{Context: ctx, cancel: cancel}
	value, _, err := readFrozenFile(checkpoint, Root{path: root}, "", discoveredFile{Path: "gamelist.xml", Size: info.Size(), Facts: serversource.FactsDigest(info)}, 1024)
	if !errors.Is(err, context.Canceled) || len(value) != 0 {
		t.Fatalf("reader continued after checkpoint: value=%q error=%v checks=%d", value, err, checkpoint.checks)
	}
}
