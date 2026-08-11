package serverimport

import (
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"retrom/internal/cleanup"
)

var (
	ErrRootIDInvalid   = errors.New("SERVER_IMPORT_ROOT_ID_INVALID")
	ErrPathInvalid     = errors.New("SERVER_IMPORT_PATH_INVALID")
	ErrRootNotFound    = errors.New("SERVER_IMPORT_ROOT_NOT_FOUND")
	ErrRootUnavailable = errors.New("SERVER_IMPORT_ROOT_UNAVAILABLE")
)

type Directory struct {
	Name         string `json:"name"`
	RelativePath string `json:"relativePath"`
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

func looksLikeWindowsPath(value string) bool {
	return len(value) >= 2 &&
		((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) &&
		value[1] == ':' ||
		strings.HasPrefix(value, "//")
}

func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func openSelectedDirectory(rootPath, relativePath string) (*os.File, error) {
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

func listDirectories(rootPath, relativePath string) ([]Directory, error) {
	directory, err := openSelectedDirectory(rootPath, relativePath)
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

type discoveredFile struct {
	RelativePath string
	Basename     string
	SizeBytes    int64
	Parent       *os.File
	Name         string
}

type walkCounts struct {
	Directories            int64
	Files                  int64
	SkippedSpecial         int64
	SkippedUnrepresentable int64
}

//nolint:gocognit,gocyclo // The no-follow walker keeps every filesystem type and resource gate in one traversal.
func walkFiles(root *os.File, limits scanLimits, visit func(discoveredFile) error) (walkCounts, error) {
	counts := walkCounts{}
	var walk func(*os.File, string, int) error
	walk = func(directory *os.File, prefix string, depth int) error {
		if depth > limits.maxDepth {
			return ErrScanLimit
		}
		counts.Directories++
		if counts.Directories > limits.maxDirectories {
			return ErrScanLimit
		}
		entries, err := directory.ReadDir(-1)
		if err != nil {
			return ErrRootUnavailable
		}
		sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
		for _, entry := range entries {
			name := entry.Name()
			relative := name
			if prefix != "" {
				relative = prefix + "/" + name
			}
			if !utf8.ValidString(name) || hasControl(name) || len([]byte(name)) > 255 || len([]byte(relative)) > 4096 {
				counts.SkippedUnrepresentable++
				continue
			}
			info, infoErr := inspectEntryAt(directory, name)
			if infoErr != nil || info.Mode()&os.ModeSymlink != 0 {
				counts.SkippedSpecial++
				continue
			}
			if info.IsDir() {
				child, openErr := openDirectoryAt(directory, name)
				if openErr != nil {
					counts.SkippedSpecial++
					continue
				}
				walkErr := walk(child, relative, depth+1)
				cleanup.Error("close", child.Close())
				if walkErr != nil {
					return walkErr
				}
				continue
			}
			if !info.Mode().IsRegular() {
				counts.SkippedSpecial++
				continue
			}
			counts.Files++
			if counts.Files > limits.maxFiles {
				return ErrScanLimit
			}
			if name == ".DS_Store" || name == "Thumbs.db" || strings.HasPrefix(name, "._") {
				continue
			}
			file := discoveredFile{
				RelativePath: relative,
				Basename:     name,
				SizeBytes:    info.Size(),
				Parent:       directory,
				Name:         name,
			}
			if err := visit(file); err != nil {
				return err
			}
		}
		return nil
	}
	return counts, walk(root, "", 0)
}

func openCandidate(file discoveredFile) (*os.File, fs.FileInfo, error) {
	handle, err := openRegularFileAt(file.Parent, file.Name)
	if err != nil {
		return nil, nil, err
	}
	info, err := handle.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != file.SizeBytes {
		cleanup.Error("close", handle.Close())
		return nil, nil, errSourceChanged
	}
	return handle, info, nil
}

// openRelativeCandidate resolves every component from a configured root with
// no-follow directory handles. Neither the selected directory nor the file
// path is ever rejoined into a host absolute path for opening.
func openRelativeCandidate(rootPath, selectedPath, candidatePath string) (*os.File, fs.FileInfo, error) {
	combined := candidatePath
	if selectedPath != "" {
		combined = selectedPath + "/" + candidatePath
	}
	if err := ValidateRelativePath(combined); err != nil || combined == "" {
		return nil, nil, ErrPathInvalid
	}
	segments := strings.Split(combined, "/")
	directory, err := openSelectedDirectory(rootPath, strings.Join(segments[:len(segments)-1], "/"))
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
		return nil, nil, errSourceChanged
	}
	return handle, before, nil
}

func sameFileFacts(before, after fs.FileInfo) bool {
	return before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && os.SameFile(before, after)
}
