package scans

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"retrom/internal/model"
)

// coverExtensions are the images a loose source may place beside a game as its cover.
var coverExtensions = []string{".png", ".jpg", ".jpeg", ".webp"}

// parseFiles reads a loose source: every top-level file or directory is one game titled by
// its name, and a top-level image sharing a game's name is that game's cover. Entry points,
// archives and multi-file layouts stay runtime decisions, as for metadata sources.
func parseFiles(root *os.Root, absolute string) (Collection, error) {
	entries, err := readDir(root, ".")
	if err != nil {
		return Collection{}, err
	}
	entries = slices.DeleteFunc(entries, func(entry os.DirEntry) bool {
		return strings.HasPrefix(entry.Name(), ".") || (!entry.IsDir() && !entry.Type().IsRegular())
	})
	// Directory order is unspecified; name order keeps candidates and cover choice stable.
	slices.SortFunc(entries, func(left, right os.DirEntry) int { return strings.Compare(left.Name(), right.Name()) })
	covers := coverNames(entries)
	collection := Collection{
		Directory: ".",
		Entry:     model.SourceEntry{Key: ".", Name: filepath.Base(absolute), Path: absolute},
	}
	for _, entry := range entries {
		if covers[title(entry)] == entry.Name() {
			continue
		}
		collection.Games = append(collection.Games, Candidate{
			Input: metadata(title(entry), "", "", "", "", nil, nil),
			Files: []string{entry.Name()},
			Cover: covers[title(entry)],
		})
	}
	if len(collection.Games) == 0 {
		return Collection{}, model.ErrInvalid
	}
	collection.Entry.GameCount = int64(len(collection.Games))
	return collection, nil
}

// coverNames maps a game title to the top-level image of the same name, when one exists.
func coverNames(entries []os.DirEntry) map[string]string {
	titles := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !cover(entry.Name()) {
			titles[title(entry)] = true
		}
	}
	covers := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() && cover(entry.Name()) && titles[title(entry)] && covers[title(entry)] == "" {
			covers[title(entry)] = entry.Name()
		}
	}
	return covers
}

func title(entry os.DirEntry) string {
	if entry.IsDir() {
		return entry.Name()
	}
	name := entry.Name()
	if stem := strings.TrimSuffix(name, path.Ext(name)); stem != "" {
		return stem
	}
	return name
}

func cover(name string) bool {
	return slices.Contains(coverExtensions, strings.ToLower(path.Ext(name)))
}
