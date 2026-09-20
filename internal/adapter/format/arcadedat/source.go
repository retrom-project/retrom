// Package arcadedat owns acquisition of the locally installed DAT source.
package arcadedat

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	model "retrom/internal/model/dependencies"
)

type Source struct{}

var _ model.DATCatalogSource = Source{}

// OpenBuiltIn retains the installed manifest's root/path resolution. Acquisition
// does not read ahead or add cancellation checks before the existing parser.
func (Source) OpenBuiltIn(root, relativePath string) (io.ReadCloser, error) {
	file, err := os.Open(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		return nil, fmt.Errorf("open built-in DAT: %w", err)
	}
	return file, nil
}
