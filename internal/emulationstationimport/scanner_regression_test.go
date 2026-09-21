package emulationstationimport

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"retrom/internal/serversource"

	"github.com/google/uuid"
)

type cancellingScanEntropy struct{ cancel context.CancelFunc }

func (reader cancellingScanEntropy) Read(value []byte) (int, error) {
	for i := range value {
		value[i] = byte(i + 1)
	}
	reader.cancel()
	return len(value), nil
}

func TestESScannerStopsCancellationAfterMetadataBeforeGameProjection(t *testing.T) {
	root := t.TempDir()
	writeScanFile(t, root, "game.nes", []byte("NES"))
	writeScanFile(
		t,
		root,
		"gamelist.xml",
		[]byte(`<gameList><game><path>game.nes</path><name>Game</name></game></gameList>`),
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	uuid.SetRand(cancellingScanEntropy{cancel: cancel})
	defer uuid.SetRand(nil)
	result, err := (&Service{}).scan(ctx, Root{path: root}, "", 2027)
	if !errors.Is(err, context.Canceled) || len(result.Items) != 0 {
		t.Fatalf("cancelled projection escaped: items=%d error=%v", len(result.Items), err)
	}
}

func TestESScannerPreservesUnavailableDirectoryCause(t *testing.T) {
	_, err := (&Service{}).scan(t.Context(), Root{path: t.TempDir()}, "missing", 2027)
	if !errors.Is(err, serversource.ErrRootUnavailable) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("lost selected directory cause: %v", err)
	}
}

func TestESScannerPreservesFrozenFileOpenCause(t *testing.T) {
	_, err := readFrozenFile(
		t.Context(),
		Root{path: t.TempDir()},
		"",
		discoveredFile{Path: "missing", Size: 1, Facts: "frozen"},
		8,
	)
	if !errors.Is(err, ErrSourceChanged) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("lost frozen file cause: %v", err)
	}
}
