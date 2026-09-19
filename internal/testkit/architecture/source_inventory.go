package architecture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

var (
	ErrEmptySources    = errors.New("architecture: no source files discovered")
	ErrUnsafeSource    = errors.New("architecture: unsafe source path")
	ErrInvalidRegistry = errors.New("architecture: invalid ownership registry")
)

// SourceFile records actual bytes, including uncommitted sources, without host paths.
type SourceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

// SourceSnapshot binds a sorted, exact file set to its bytes.
type SourceSnapshot struct {
	SHA256 string       `json:"sha256"`
	Files  []SourceFile `json:"files"`
}

// DiscoverSources includes tracked and nonignored untracked source files.
func DiscoverSources(ctx context.Context, root string) ([]string, error) {
	output, err := inventoryGit(ctx, root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, name := range strings.Split(output, "\x00") {
		if sourceExtension(name) {
			if !safeRepositoryPath(name) {
				return nil, fmt.Errorf("%w: %q", ErrUnsafeSource, name)
			}
			files = append(files, name)
		}
	}
	slices.Sort(files)
	files = slices.Compact(files)
	if len(files) == 0 {
		return nil, ErrEmptySources
	}
	return files, nil
}

func sourceExtension(name string) bool {
	switch path.Ext(name) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".py", ".sh":
		return true
	default:
		return false
	}
}

func safeRepositoryPath(name string) bool {
	return name != "" && name != "." && path.Clean(name) == name &&
		!strings.ContainsAny(name, "\\\x00\r\n:") && !strings.HasPrefix(name, "/") &&
		name != ".." && !strings.HasPrefix(name, "../")
}

// SnapshotFiles hashes actual files and rejects missing files and symlink traversal.
func SnapshotFiles(root string, sources []string) (SourceSnapshot, error) {
	if len(sources) == 0 {
		return SourceSnapshot{}, ErrEmptySources
	}
	names := slices.Clone(sources)
	slices.Sort(names)
	if len(slices.Compact(slices.Clone(names))) != len(names) {
		return SourceSnapshot{}, ErrInvalidRegistry
	}
	files := make([]SourceFile, 0, len(names))
	for _, name := range names {
		content, err := readInventoryFile(root, name)
		if err != nil {
			return SourceSnapshot{}, err
		}
		sum := sha256.Sum256(content)
		files = append(files, SourceFile{Path: name, SHA256: hex.EncodeToString(sum[:]), Size: len(content)})
	}
	encoded, err := json.Marshal(files)
	if err != nil {
		return SourceSnapshot{}, fmt.Errorf("encode source fingerprint: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return SourceSnapshot{SHA256: hex.EncodeToString(sum[:]), Files: files}, nil
}

func readInventoryFile(root, name string) ([]byte, error) {
	if !safeRepositoryPath(name) {
		return nil, fmt.Errorf("%w: %q", ErrUnsafeSource, name)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve inventory root: %w", err)
	}
	target := filepath.Join(absolute, filepath.FromSlash(name))
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, fmt.Errorf("resolve source %q: %w", name, err)
	}
	if target != resolved {
		return nil, fmt.Errorf("%w: %q", ErrUnsafeSource, name)
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("stat source %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q", ErrUnsafeSource, name)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("read source %q: %w", name, err)
	}
	return content, nil
}
