package prepare

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"retrom/internal/filestore"
)

func TestSingleNormalizesAndValidatesWASM4(t *testing.T) {
	cart, err := os.ReadFile("../../../testdata/public-roms/wasm4-controls/controls.wasm")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, filename string
		payload        []byte
		code           string
	}{
		{"raw", "game.wasm", cart, ""},
		{"zip", "game.zip", zipCart(t, "nested/game.wasm", cart), ""},
		{"renamed archive", "game.wasm", zipCart(t, "game.wasm", cart), "WASM4_CART_INVALID"},
		{"truncated", "game.wasm", cart[:len(cart)-1], "WASM4_CART_INVALID"},
		{"wrong import", "game.wasm", bytes.ReplaceAll(cart, []byte("env"), []byte("bad")), "WASM4_CART_INVALID"},
		{"oversize", "game.wasm", append(cart, make([]byte, maxWASM4Bytes)...), "WASM4_CART_INVALID"},
		{"empty", "game.wasm", nil, "CONTENT_EMPTY"},
		{"wrong extension", "game.nes", cart, "UNSUPPORTED_CONTENT_FORMAT"},
		{"wrong entry", "game.zip", zipCart(t, "game.nes", cart), "NO_SUPPORTED_CONTENT"},
		{"bad zipped bytes", "game.zip", zipCart(t, "game.wasm", []byte("not wasm")), "WASM4_CART_INVALID"},
		{"traversal", "game.zip", zipCart(t, "../game.wasm", cart), "ARCHIVE_UNSAFE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := filestore.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := store.Put(bytes.NewReader(test.payload))
			if err != nil {
				t.Fatal(err)
			}
			result, err := New(store).Single(t.Context(), "wasm4", File{
				LogicalName: test.filename,
				Record:      metadata.Record, SHA256: metadata.SHA256, Size: metadata.Size,
			})
			if test.code != "" {
				assertInvalidCode(t, err, test.code)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			reader, err := os.ReadFile(store.Path(result.File.Record))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(reader, cart) || result.File.LogicalName != "game.wasm" {
				t.Fatalf("normalization did not preserve cartridge: %#v", result.File)
			}
		})
	}
}

func TestSingleCancellationIsNotInvalidContent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := New(nil).Single(ctx, "wasm4", File{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func zipCart(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func assertInvalidCode(t *testing.T, err error, code string) {
	t.Helper()
	var invalid *Invalid
	if !errors.As(err, &invalid) || invalid.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}
