package arcadedat

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceOpensInstalledDATWithoutReading(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	directory := filepath.Join(root, "nested")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "catalog.dat")
	const content = "<datafile><game name=\"owned-fixture\"/></datafile>"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stream, err := (Source{}).OpenBuiltIn(root, "nested/catalog.dat")
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			if err := stream.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	file, ok := stream.(*os.File)
	if !ok {
		t.Fatalf("source replaced the actual file resource: %T", stream)
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 0 {
		t.Fatalf("source read ahead: position=%d error=%v", position, err)
	}
	data, err := io.ReadAll(stream)
	if err != nil || string(data) != content {
		t.Fatalf("installed bytes changed: %q, %v", data, err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	var buffer [1]byte
	if _, err := stream.Read(buffer[:]); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("caller does not own the file lifetime: %v", err)
	}
}

func TestSourceRetainsOpenErrorObject(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "missing.dat")
	stream, err := (Source{}).OpenBuiltIn(root, "missing.dat")
	if stream != nil {
		if closeErr := stream.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal("failed open returned a resource")
	}
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing path cause changed: %T %v", err, err)
	}
	// Check immediate node types before matching causes: an additional wrapper
	// must not pass merely because errors.Is can still find the underlying cause.
	if fmt.Sprintf("%T", err) != "*fmt.wrapError" || fmt.Sprintf("%T", errors.Unwrap(err)) != "*fs.PathError" {
		t.Fatalf("open error gained an intervening node: %T -> %T", err, errors.Unwrap(err))
	}
	if !errors.Is(errors.Unwrap(err), pathErr) || pathErr.Op != "open" || pathErr.Path != path {
		t.Fatalf("open wrapper or path identity changed: %#v", pathErr)
	}
	if err.Error() != "open built-in DAT: "+pathErr.Error() {
		t.Fatalf("open context changed: %v", err)
	}
}
