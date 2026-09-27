package architecture

import (
	"path/filepath"
	"runtime"
	"testing"
)

func sourceRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate architecture tests")
	}
	return filepath.Dir(filepath.Dir(filename))
}
