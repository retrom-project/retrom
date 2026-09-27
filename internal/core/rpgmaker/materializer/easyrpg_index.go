package materializer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	"retrom/internal/importing"
)

const easyRPGCacheVersion = 2

type EasyRPGIndex struct {
	Contents []byte
	SHA256   string
}

type cacheNode map[string]any

// BuildEasyRPGIndex implements the gencache V2 lookup tree without the upstream
// wall-clock date. The result is deterministic canonical compact JSON.
func BuildEasyRPGIndex(files []SourceFile) (EasyRPGIndex, error) {
	if len(files) == 0 {
		return EasyRPGIndex{}, ErrInvalid
	}
	ordered := append([]SourceFile(nil), files...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].Path < ordered[right].Path })
	root := make(cacheNode)
	for _, file := range ordered {
		if _, err := importing.ValidateLogicalPath(file.Path); err != nil {
			return EasyRPGIndex{}, ErrInvalid
		}
		if err := addCachePath(root, strings.Split(file.Path, "/")); err != nil {
			return EasyRPGIndex{}, err
		}
	}
	contents, err := json.Marshal(map[string]any{
		"cache":    root,
		"metadata": map[string]any{"version": easyRPGCacheVersion},
	})
	if err != nil {
		return EasyRPGIndex{}, fmt.Errorf("%w: marshal EasyRPG index: %w", ErrInvalid, err)
	}
	digest := sha256.Sum256(contents)
	return EasyRPGIndex{Contents: contents, SHA256: hex.EncodeToString(digest[:])}, nil
}

func addCachePath(root cacheNode, segments []string) error {
	current := root
	for index, segment := range segments {
		isFile := index == len(segments)-1
		if isFile {
			key := easyRPGFileKey(segment, index == 0)
			if key == "_dirname" {
				return fmt.Errorf("%w: reserved EasyRPG key", ErrInvalid)
			}
			if _, exists := current[key]; exists {
				return fmt.Errorf("%w: duplicate EasyRPG key %s", ErrInvalid, key)
			}
			current[key] = segment
			return nil
		}
		key := easyRPGKey(segment)
		if key == "_dirname" {
			return fmt.Errorf("%w: reserved EasyRPG directory key", ErrInvalid)
		}
		existing, exists := current[key]
		if !exists {
			next := cacheNode{"_dirname": segment}
			current[key] = next
			current = next
			continue
		}
		next, ok := existing.(cacheNode)
		if !ok || next["_dirname"] != segment {
			return fmt.Errorf("%w: EasyRPG directory collision %s", ErrInvalid, key)
		}
		current = next
	}
	return ErrInvalid
}

func easyRPGFileKey(name string, root bool) string {
	key := easyRPGKey(name)
	if root || strings.HasSuffix(key, ".ini") || strings.HasSuffix(key, ".po") {
		if stripLastExtension(key) == "exfont" {
			return "exfont"
		}
		return key
	}
	return stripLastExtension(key)
}

func easyRPGKey(value string) string {
	return strings.ToLower(norm.NFKC.String(value))
}

func stripLastExtension(value string) string {
	extension := path.Ext(value)
	return strings.TrimSuffix(value, extension)
}
