package scans

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"retrom/internal/model"
)

type sourceSearch struct {
	Extensions  []string `json:"extensions"`
	HeaderBytes int      `json:"headerBytes"`
}

type sourceCandidate struct {
	SourcePath   string `json:"sourcePath"`
	SizeBytes    int64  `json:"sizeBytes"`
	HeaderBase64 string `json:"headerBase64"`
}

// One inventory belongs to one metadata file's directory, and is discarded with its scan.
type sourceInventory struct {
	names []string
	cache map[string][]sourceCandidate
}

func (inventory *sourceInventory) search(ctx context.Context, root *os.Root, query sourceSearch,
) ([]sourceCandidate, error) {
	if !query.valid() {
		return nil, model.ErrInvalid
	}
	slices.Sort(query.Extensions)
	key := strconv.Itoa(query.HeaderBytes) + ":" + strings.Join(query.Extensions, ",")
	if cached, exists := inventory.cache[key]; exists {
		return cached, nil
	}
	if inventory.names == nil {
		names, err := sourceFiles(root, ".", 0)
		if err != nil {
			return nil, err
		}
		slices.Sort(names)
		inventory.names = names
	}
	result := make([]sourceCandidate, 0)
	evidenceBytes := 0
	for _, name := range inventory.names {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("source search cancelled: %w", err)
		}
		if !slices.Contains(query.Extensions, strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")) {
			continue
		}
		candidate, err := sourceHeader(root, name, query.HeaderBytes)
		if err != nil {
			continue
		}
		evidenceBytes += len(candidate.SourcePath) + len(candidate.HeaderBase64) + 128
		if evidenceBytes > 32*1024*1024 {
			return nil, model.ErrInvalid
		}
		result = append(result, candidate)
	}
	if inventory.cache == nil {
		inventory.cache = make(map[string][]sourceCandidate)
	}
	inventory.cache[key] = result
	return result, nil
}

func sourceHeader(root *os.Root, name string, limit int) (sourceCandidate, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return sourceCandidate{}, fmt.Errorf("stat source candidate: %w", err)
	}
	if !info.Mode().IsRegular() {
		return sourceCandidate{}, model.ErrInvalid
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return sourceCandidate{}, fmt.Errorf("open source candidate: %w", err)
	}
	defer closeFile(file)
	info, err = file.Stat()
	if err != nil {
		return sourceCandidate{}, fmt.Errorf("stat source candidate: %w", err)
	}
	if !info.Mode().IsRegular() {
		return sourceCandidate{}, model.ErrInvalid
	}
	header, err := io.ReadAll(io.LimitReader(file, int64(limit)))
	if err != nil {
		return sourceCandidate{}, fmt.Errorf("read source candidate: %w", err)
	}
	return sourceCandidate{
		SourcePath: name, SizeBytes: info.Size(), HeaderBase64: base64.StdEncoding.EncodeToString(header),
	}, nil
}

func (query sourceSearch) valid() bool {
	if query.HeaderBytes < 1 || query.HeaderBytes > 4096 || len(query.Extensions) == 0 || len(query.Extensions) > 16 {
		return false
	}
	for _, extension := range query.Extensions {
		if extension == "" || len(extension) > 16 || strings.IndexFunc(extension, func(r rune) bool {
			return r < 'a' || r > 'z'
		}) >= 0 {
			return false
		}
	}
	return true
}
