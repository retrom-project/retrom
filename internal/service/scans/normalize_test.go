package scans

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"
)

func normalizedFacts(sizes ...int64) []normalizedFile {
	files := make([]normalizedFile, len(sizes))
	for index, size := range sizes {
		files[index] = normalizedFile{File: runtimeclient.File{
			LogicalKey: fmt.Sprintf("assets/%d.bin", index), Name: fmt.Sprintf("assets/%d.bin", index),
			SHA256: strings.Repeat("a", 64), SizeBytes: size,
		}}
	}
	return files
}

func TestNormalizedProjectExceedsSingleFileLimit(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	if err := validateNormalizedFiles(normalizedFacts(4*gib, 5*gib)); err != nil {
		t.Fatalf("9 GiB project with valid individual files rejected: %v", err)
	}
	if err := validateNormalizedFiles(normalizedFacts(8*gib, 8*gib, 8*gib, 8*gib,
		8*gib, 8*gib, 8*gib, 8*gib)); err != nil {
		t.Fatalf("project at the aggregate limit rejected: %v", err)
	}
}

func TestNormalizedProjectKeepsFileAndProjectBounds(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	cases := map[string][]normalizedFile{
		"single file": normalizedFacts(storage.MaximumFileSize + 1),
		"negative":    normalizedFacts(-1),
		"project": normalizedFacts(8*gib, 8*gib, 8*gib, 8*gib,
			8*gib, 8*gib, 8*gib, 8*gib, 1),
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateNormalizedFiles(files); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("expected invalid oversized/negative project, got %v", err)
			}
		})
	}
}
