package gamelist

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"sort"
	"strings"
)

func snapshotDigest(gamelists []scannedGamelist) string {
	type gamelistSnapshot struct {
		Path          string  `json:"path"`
		SizeBytes     int64   `json:"sizeBytes"`
		ContentDigest *string `json:"contentDigest"`
		FactsDigest   string  `json:"factsDigest"`
		ParseState    string  `json:"parseState"`
	}
	type sourceSnapshot struct {
		SchemaVersion int                `json:"schemaVersion"`
		Gamelists     []gamelistSnapshot `json:"gamelists"`
	}
	values := make([]gamelistSnapshot, 0, len(gamelists))
	for _, gamelist := range gamelists {
		var contentDigest *string
		if gamelist.Digest != "" {
			contentDigest = &gamelist.Digest
		}
		values = append(values, gamelistSnapshot{
			Path: gamelist.Path, SizeBytes: gamelist.Size,
			ContentDigest: contentDigest, FactsDigest: gamelist.Facts,
			ParseState: gamelist.State,
		})
	}
	encoded := compactJSON(sourceSnapshot{SchemaVersion: 1, Gamelists: values})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func compactJSON(value any) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
}

func extensionName(value string) string {
	extension := strings.ToLower(path.Ext(path.Base(value)))
	if extension == "" {
		return "(none)"
	}
	return extension
}

func extensionSummary(values map[string]int64) (string, int64) {
	type pair struct {
		Extension string `json:"extension"`
		Count     int64  `json:"count"`
	}
	items := make([]pair, 0, len(values))
	for extension, count := range values {
		items = append(items, pair{Extension: extension, Count: count})
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].Count != items[right].Count {
			return items[left].Count > items[right].Count
		}
		return items[left].Extension < items[right].Extension
	})
	var other int64
	if len(items) > 32 {
		for _, item := range items[32:] {
			other += item.Count
		}
		items = items[:32]
	}
	return string(compactJSON(items)), other
}
