package detector

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type catalogFile struct {
	path string
	size int64
}

type catalog struct {
	source FileIndex
	files  map[string]catalogFile
}

func newCatalog(source FileIndex) (*catalog, error) {
	if source == nil {
		return nil, newError(CodeProjectNotFound, "nil file index", nil)
	}
	result := &catalog{source: source, files: make(map[string]catalogFile)}
	for _, file := range source.Files() {
		if !validIndexedPath(file.Path) || file.Size < 0 {
			return nil, newError(CodePathCollision, "invalid normalized file index entry", nil)
		}
		key := lookupKey(file.Path)
		if prior, exists := result.files[key]; exists {
			return nil, newError(CodePathCollision, fmt.Sprintf("%q conflicts with %q", prior.path, file.Path), nil)
		}
		result.files[key] = catalogFile{path: file.Path, size: file.Size}
	}
	return result, nil
}

func (files *catalog) exists(path string) bool {
	_, exists := files.files[lookupKey(path)]
	return exists
}

func (files *catalog) original(path string) string {
	return files.files[lookupKey(path)].path
}

func (files *catalog) paths() []catalogFile {
	result := make([]catalogFile, 0, len(files.files))
	for _, file := range files.files {
		result = append(result, file)
	}
	return result
}

func (files *catalog) read(path string, limit int64, code Code) ([]byte, error) {
	file, exists := files.files[lookupKey(path)]
	if !exists {
		return nil, newError(code, fmt.Sprintf("required file %q is missing", path), nil)
	}
	if file.size > limit {
		return nil, newError(code, fmt.Sprintf("file %q exceeds %d bytes", file.path, limit), nil)
	}
	reader, err := files.source.Open(file.path)
	if err != nil {
		return nil, newError(code, fmt.Sprintf("open %q", file.path), err)
	}
	if reader == nil {
		return nil, newError(code, fmt.Sprintf("open %q returned no reader", file.path), nil)
	}
	contents, readErr := io.ReadAll(io.LimitReader(reader, limit+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, newError(code, fmt.Sprintf("read %q", file.path), readErr)
	}
	if closeErr != nil {
		return nil, newError(code, fmt.Sprintf("close %q", file.path), closeErr)
	}
	if int64(len(contents)) != file.size || int64(len(contents)) > limit {
		return nil, newError(code, fmt.Sprintf("size of %q changed during detection", file.path), nil)
	}
	return contents, nil
}

func lookupKey(path string) string {
	return cases.Fold().String(norm.NFKC.String(path))
}

func validIndexedPath(path string) bool {
	if invalidWholeIndexedPath(path) {
		return false
	}
	for index, part := range strings.Split(path, "/") {
		if invalidIndexedPathPart(part) || index == 0 && windowsDrivePart(part) {
			return false
		}
	}
	return norm.NFC.IsNormalString(path)
}

func invalidWholeIndexedPath(path string) bool {
	return path == "" || len(path) > 1024 || !utf8.ValidString(path) ||
		strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.Contains(path, "\\")
}

func invalidIndexedPathPart(part string) bool {
	return part == "" || part == "." || part == ".." || len(part) > 255 || hasControl(part)
}

func windowsDrivePart(part string) bool {
	return len(part) >= 2 && isASCIIAlpha(part[0]) && part[1] == ':'
}

func hasControl(value string) bool {
	return bytes.IndexFunc([]byte(value), func(character rune) bool {
		return character == 0x7f || character >= 0 && character <= 0x1f
	}) >= 0
}

func isASCIIAlpha(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}
