// Package serversource provides the fail-closed filesystem capability shared
// by all imports from deployment-configured read-only roots.
package serversource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"retrom/internal/cleanup"
)

var sourceReaders = make(chan struct{}, 2)

func AcquireReader(ctx context.Context) (func(), error) {
	select {
	case sourceReaders <- struct{}{}:
		return func() { <-sourceReaders }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("serversource/acquire reader: %w", ctx.Err())
	}
}

var (
	ErrRootIDInvalid   = errors.New("SERVER_IMPORT_ROOT_ID_INVALID")
	ErrPathInvalid     = errors.New("SERVER_IMPORT_PATH_INVALID")
	ErrRootNotFound    = errors.New("SERVER_IMPORT_ROOT_NOT_FOUND")
	ErrRootUnavailable = errors.New("SERVER_IMPORT_ROOT_UNAVAILABLE")
	ErrScanLimit       = errors.New("SERVER_IMPORT_SCAN_LIMIT_EXCEEDED")
	ErrSourceChanged   = errors.New("SERVER_IMPORT_SOURCE_CHANGED")
)

var uriSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

type Directory struct {
	Name         string `json:"name"`
	RelativePath string `json:"relativePath"`
}

type File struct {
	RelativePath string
	Basename     string
	SizeBytes    int64
	Parent       *os.File
	Name         string
}

type Limits struct {
	MaxDepth       int
	MaxDirectories int64
	MaxFiles       int64
}

type Counts struct {
	Directories            int64
	Files                  int64
	SkippedSpecial         int64
	SkippedUnrepresentable int64
}

type FileSummary struct {
	Name         string
	RelativePath string
	SizeBytes    int64
	FactsDigest  string
}

func ValidateRootID(value string) error {
	if len(value) < 1 || len(value) > 32 || value[0] < 'a' || value[0] > 'z' {
		return ErrRootIDInvalid
	}
	for _, character := range value[1:] {
		if character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return ErrRootIDInvalid
		}
	}
	return nil
}

func ValidateRelativePath(value string) error {
	if !utf8.ValidString(value) || len([]byte(value)) > 4096 || strings.Contains(value, "\\") ||
		strings.ContainsRune(value, 0) || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") ||
		looksLikeWindowsPath(value) {
		return ErrPathInvalid
	}
	if value == "" {
		return nil
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." || len([]byte(segment)) > 255 || hasControl(segment) {
			return ErrPathInvalid
		}
	}
	return nil
}

// NormalizeDeclaredPath accepts the backslash separator used by real Pegasus
// libraries, while rejecting host-absolute, drive, UNC, URI, and traversal forms.
func NormalizeDeclaredPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "\\") ||
		strings.HasPrefix(value, "//") || looksLikeWindowsPath(value) || hasURIScheme(value) {
		return "", ErrPathInvalid
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	if err := ValidateRelativePath(normalized); err != nil || normalized == "" {
		return "", ErrPathInvalid
	}
	return normalized, nil
}

func ResolveDeclaredPath(metadataRelativePath, declared string) (string, error) {
	normalized, err := NormalizeDeclaredPath(declared)
	if err != nil {
		return "", err
	}
	base := ""
	if slash := strings.LastIndexByte(metadataRelativePath, '/'); slash >= 0 {
		base = metadataRelativePath[:slash]
	}
	if base != "" {
		normalized = base + "/" + normalized
	}
	if err := ValidateRelativePath(normalized); err != nil {
		return "", err
	}
	return normalized, nil
}

func looksLikeWindowsPath(value string) bool {
	return len(value) >= 2 &&
		((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) &&
		value[1] == ':'
}

func hasURIScheme(value string) bool {
	return uriSchemePattern.MatchString(value)
}

func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func OpenRoot(path string) (*os.File, error) { return openDirectoryNoFollow(path) }

func OpenSelectedDirectory(rootPath, relativePath string) (*os.File, error) {
	root, err := openDirectoryNoFollow(rootPath)
	if err != nil {
		return nil, ErrRootUnavailable
	}
	current := root
	for _, segment := range strings.Split(relativePath, "/") {
		if segment == "" {
			continue
		}
		next, openErr := openDirectoryAt(current, segment)
		if current != root {
			cleanup.Error("close", current.Close())
		}
		if openErr != nil {
			cleanup.Error("close", root.Close())
			return nil, ErrPathInvalid
		}
		current = next
	}
	if current != root {
		cleanup.Error("close", root.Close())
	}
	return current, nil
}

func ListDirectories(rootPath, relativePath string) ([]Directory, error) {
	directory, err := OpenSelectedDirectory(rootPath, relativePath)
	if err != nil {
		return nil, err
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, ErrRootUnavailable
	}
	result := make([]Directory, 0)
	for _, entry := range entries {
		name := entry.Name()
		if !utf8.ValidString(name) || hasControl(name) || len([]byte(name)) > 255 || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		child, openErr := openDirectoryAt(directory, name)
		if openErr != nil {
			continue
		}
		cleanup.Error("close", child.Close())
		path := name
		if relativePath != "" {
			path = relativePath + "/" + name
		}
		result = append(result, Directory{Name: name, RelativePath: path})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Name != result[right].Name {
			return result[left].Name < result[right].Name
		}
		return result[left].RelativePath < result[right].RelativePath
	})
	return result, nil
}

func ListRegularFiles(rootPath, selectedPath, relativeDirectory string) ([]FileSummary, error) {
	combined := relativeDirectory
	if selectedPath != "" && relativeDirectory != "" {
		combined = selectedPath + "/" + relativeDirectory
	} else if selectedPath != "" {
		combined = selectedPath
	}
	if err := ValidateRelativePath(combined); err != nil {
		return nil, err
	}
	directory, err := OpenSelectedDirectory(rootPath, combined)
	if err != nil {
		return nil, err
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, ErrRootUnavailable
	}
	result := make([]FileSummary, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !utf8.ValidString(name) || hasControl(name) || len([]byte(name)) > 255 {
			continue
		}
		info, infoErr := inspectEntryAt(directory, name)
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		relative := name
		if relativeDirectory != "" {
			relative = relativeDirectory + "/" + name
		}
		result = append(result, FileSummary{
			Name: name, RelativePath: relative, SizeBytes: info.Size(), FactsDigest: FactsDigest(info),
		})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result, nil
}

// WalkFiles visits only regular files beneath an already opened selected
// directory. File.Parent is borrowed for the duration of the callback.
func WalkFiles(root *os.File, limits Limits, visit func(File) error) (Counts, error) {
	state := fileWalker{limits: limits, visit: visit}
	err := state.walk(root, "", 0)
	return state.counts, err
}

type fileWalker struct {
	limits Limits
	visit  func(File) error
	counts Counts
}

func (state *fileWalker) walk(directory *os.File, prefix string, depth int) error {
	if depth > state.limits.MaxDepth {
		return ErrScanLimit
	}
	state.counts.Directories++
	if state.counts.Directories > state.limits.MaxDirectories {
		return ErrScanLimit
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return ErrRootUnavailable
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	for _, entry := range entries {
		if err := state.entry(directory, prefix, depth, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (state *fileWalker) entry(directory *os.File, prefix string, depth int, name string) error {
	relative := name
	if prefix != "" {
		relative = prefix + "/" + name
	}
	if !utf8.ValidString(name) || hasControl(name) || len([]byte(name)) > 255 || len([]byte(relative)) > 4096 {
		state.counts.SkippedUnrepresentable++
		return nil
	}
	info, ok := safeEntryInfo(directory, name)
	if !ok || info.Mode()&os.ModeSymlink != 0 {
		state.counts.SkippedSpecial++
		return nil
	}
	if info.IsDir() {
		return state.directory(directory, relative, depth, name)
	}
	if !info.Mode().IsRegular() {
		state.counts.SkippedSpecial++
		return nil
	}
	state.counts.Files++
	if state.counts.Files > state.limits.MaxFiles {
		return ErrScanLimit
	}
	if name == ".DS_Store" || name == "Thumbs.db" || strings.HasPrefix(name, "._") {
		return nil
	}
	file := File{RelativePath: relative, Basename: name, SizeBytes: info.Size(), Parent: directory, Name: name}
	if err := state.visit(file); err != nil {
		return fmt.Errorf("serversource/visit file: %w", err)
	}
	return nil
}

func safeEntryInfo(directory *os.File, name string) (fs.FileInfo, bool) {
	info, err := inspectEntryAt(directory, name)
	return info, err == nil
}

func (state *fileWalker) directory(parent *os.File, relative string, depth int, name string) error {
	child, err := openDirectoryAt(parent, name)
	if err != nil {
		state.counts.SkippedSpecial++
		return nil
	}
	defer func() { cleanup.Error("close", child.Close()) }()
	return state.walk(child, relative, depth+1)
}

func OpenFile(file File) (*os.File, fs.FileInfo, error) {
	handle, err := openRegularFileAt(file.Parent, file.Name)
	if err != nil {
		return nil, nil, err
	}
	info, err := handle.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != file.SizeBytes {
		cleanup.Error("close", handle.Close())
		return nil, nil, ErrSourceChanged
	}
	return handle, info, nil
}

func OpenRelativeFile(rootPath, selectedPath, candidatePath string) (*os.File, fs.FileInfo, error) {
	combined := candidatePath
	if selectedPath != "" {
		combined = selectedPath + "/" + candidatePath
	}
	if err := ValidateRelativePath(combined); err != nil || combined == "" {
		return nil, nil, ErrPathInvalid
	}
	segments := strings.Split(combined, "/")
	directory, err := OpenSelectedDirectory(rootPath, strings.Join(segments[:len(segments)-1], "/"))
	if err != nil {
		return nil, nil, err
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	handle, err := openRegularFileAt(directory, segments[len(segments)-1])
	if err != nil {
		return nil, nil, err
	}
	before, err := handle.Stat()
	if err != nil || !before.Mode().IsRegular() {
		cleanup.Error("close", handle.Close())
		return nil, nil, ErrSourceChanged
	}
	return handle, before, nil
}

func SameFileFacts(before, after fs.FileInfo) bool {
	return before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && os.SameFile(before, after)
}

// FactsDigest preserves mutation evidence without storing the host device,
// inode, mode, or timestamp values themselves.
func FactsDigest(info fs.FileInfo) string {
	encoded, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "sizeBytes": info.Size(), "mode": uint32(info.Mode()),
		"modifiedAtNs": info.ModTime().UnixNano(), "system": systemFacts(info.Sys()),
	})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func DuplicateDirectory(parent *os.File) (*os.File, error) { return duplicateDirectory(parent) }
