// Package pegasusmeta parses and projects Pegasus metadata without accessing
// the filesystem, database, network, or task scheduler.
package pegasusmeta

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxMetadataBytes    = 8 << 20
	MaxPhysicalLine     = 64 << 10
	MaxEntryValues      = 2048
	MaxGameFileValues   = 64
	MaxCollectionRunes  = 200
	MaxTitleRunes       = 200
	MaxDescriptionRunes = 20_000
	MaxJoinedRunes      = 500
)

var (
	ErrTooLarge     = errors.New("PEGASUS_METADATA_TOO_LARGE")
	ErrInvalidUTF8  = errors.New("PEGASUS_METADATA_INVALID_UTF8")
	ErrSyntax       = errors.New("PEGASUS_METADATA_SYNTAX_INVALID")
	errControlValue = errors.New("field contains a control character")
)

type Warning struct {
	Code  string `json:"code"`
	Field string `json:"field,omitempty"`
}

type AssetReferences struct {
	Covers []string `json:"covers,omitempty"`
	Videos []string `json:"videos,omitempty"`
}

type Metadata struct {
	SchemaVersion int    `json:"schemaVersion"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Developer     string `json:"developer"`
	Publisher     string `json:"publisher"`
	Genre         string `json:"genre"`
	Players       *int   `json:"players"`
	ReleaseYear   *int   `json:"releaseYear"`
}

type Game struct {
	Ordinal       int             `json:"ordinal"`
	Metadata      Metadata        `json:"metadata"`
	Files         []string        `json:"files"`
	Assets        AssetReferences `json:"assets"`
	Warnings      []Warning       `json:"warnings,omitempty"`
	UnknownFields []string        `json:"unknownFields,omitempty"`
	BlockedCode   string          `json:"blockedCode,omitempty"`
}

type Collection struct {
	SegmentOrdinal int             `json:"segmentOrdinal"`
	Name           string          `json:"name"`
	ShortName      string          `json:"shortName,omitempty"`
	Description    string          `json:"description,omitempty"`
	Assets         AssetReferences `json:"assets"`
	Games          []Game          `json:"games"`
	Warnings       []Warning       `json:"warnings,omitempty"`
	UnknownFields  []string        `json:"unknownFields,omitempty"`
	IgnoredRules   []string        `json:"ignoredRules,omitempty"`
}

type Document struct {
	Collections   []Collection `json:"collections"`
	OrphanGames   []Game       `json:"orphanGames,omitempty"`
	Warnings      []Warning    `json:"warnings,omitempty"`
	UnknownFields []string     `json:"unknownFields,omitempty"`
}

type field struct {
	key    string
	values []string
}

type rawGame struct {
	ordinal int
	fields  []field
}

type rawCollection struct {
	ordinal int
	fields  []field
	games   []rawGame
}

// Parse validates the bounded Pegasus syntax and returns its canonical Retrom
// projection. A syntax error invalidates the entire metadata file.
func Parse(contents []byte) (Document, error) {
	if len(contents) > MaxMetadataBytes {
		return Document{}, ErrTooLarge
	}
	if bytes.HasPrefix(contents, []byte{0xef, 0xbb, 0xbf}) {
		contents = contents[3:]
	}
	if !utf8.Valid(contents) || bytes.Contains(contents, []byte{0xef, 0xbb, 0xbf}) {
		return Document{}, ErrInvalidUTF8
	}
	fields, err := parseFields(contents)
	if err != nil {
		return Document{}, err
	}
	rawCollections, orphanGames := groupFields(fields)
	document := Document{Collections: make([]Collection, 0, len(rawCollections))}
	for _, raw := range rawCollections {
		document.Collections = append(document.Collections, projectCollection(raw))
	}
	for _, raw := range orphanGames {
		game := projectGame(raw)
		game.BlockedCode = "PEGASUS_GAME_WITHOUT_COLLECTION"
		document.OrphanGames = append(document.OrphanGames, game)
	}
	return document, nil
}

func parseFields(contents []byte) ([]field, error) {
	lines := bytes.Split(contents, []byte("\n"))
	fields := make([]field, 0, len(lines))
	valueCount := 0
	for index, raw := range lines {
		if len(raw) > 0 && raw[len(raw)-1] == '\r' {
			raw = raw[:len(raw)-1]
		}
		if len(raw) > MaxPhysicalLine {
			return nil, syntaxError(index+1, "physical line exceeds limit")
		}
		if len(raw) == 0 || raw[0] == '#' {
			continue
		}
		var err error
		fields, valueCount, err = parseLine(fields, valueCount, raw, index+1)
		if err != nil {
			return nil, err
		}
		if valueCount > MaxEntryValues {
			return nil, syntaxError(index+1, "metadata value count exceeds limit")
		}
	}
	return fields, nil
}

func parseLine(fields []field, valueCount int, raw []byte, line int) ([]field, int, error) {
	if raw[0] == ' ' || raw[0] == '\t' {
		if len(fields) == 0 {
			return nil, 0, syntaxError(line, "continuation without field")
		}
		value := strings.TrimLeft(string(raw), " \t")
		if err := validateValue(value); err != nil {
			return nil, 0, syntaxError(line, err.Error())
		}
		fields[len(fields)-1].values = append(fields[len(fields)-1].values, value)
		return fields, valueCount + 1, nil
	}
	colon := bytes.IndexByte(raw, ':')
	if colon < 1 {
		return nil, 0, syntaxError(line, "field must contain a non-empty key")
	}
	key := strings.TrimSpace(string(raw[:colon]))
	if key == "" || strings.IndexFunc(key, unicode.IsSpace) >= 0 || hasControl(key) {
		return nil, 0, syntaxError(line, "invalid field key")
	}
	value := strings.TrimSpace(string(raw[colon+1:]))
	if err := validateValue(value); err != nil {
		return nil, 0, syntaxError(line, err.Error())
	}
	key = asciiLower(key)
	if key == "collection" || key == "game" {
		valueCount = 0
	}
	return append(fields, field{key: key, values: []string{value}}), valueCount + 1, nil
}

func validateValue(value string) error {
	if strings.ContainsRune(value, '\ufeff') || hasControl(value) {
		return errControlValue
	}
	return nil
}

func syntaxError(line int, detail string) error {
	return fmt.Errorf("%w: line %d: %s", ErrSyntax, line, detail)
}

func hasControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func asciiLower(value string) string {
	return strings.Map(func(character rune) rune {
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		return character
	}, value)
}

func groupFields(fields []field) ([]rawCollection, []rawGame) {
	collections := make([]rawCollection, 0)
	orphans := make([]rawGame, 0)
	var currentCollection *rawCollection
	var currentGame *rawGame
	gameOrdinal := 0
	for _, value := range fields {
		switch value.key {
		case "collection":
			collections = append(collections, rawCollection{ordinal: len(collections), fields: []field{value}})
			currentCollection = &collections[len(collections)-1]
			currentGame = nil
		case "game":
			game := rawGame{ordinal: gameOrdinal, fields: []field{value}}
			gameOrdinal++
			if currentCollection == nil {
				orphans = append(orphans, game)
				currentGame = &orphans[len(orphans)-1]
			} else {
				currentCollection.games = append(currentCollection.games, game)
				currentGame = &currentCollection.games[len(currentCollection.games)-1]
			}
		default:
			if currentGame != nil {
				currentGame.fields = append(currentGame.fields, value)
			} else if currentCollection != nil {
				currentCollection.fields = append(currentCollection.fields, value)
			}
		}
	}
	return collections, orphans
}

func projectCollection(raw rawCollection) Collection {
	result := Collection{SegmentOrdinal: raw.ordinal}
	known := map[string]bool{
		"collection": true, "shortname": true, "summary": true, "description": true,
		"assets.box_front": true, "assets.boxfront": true, "assets.boxart2d": true,
		"assets.video": true, "launch": true, "command": true, "workdir": true, "cwd": true,
		"sort-by": true,
	}
	values, duplicates := scalar(raw.fields, "collection")
	result.Name = values
	if duplicates {
		result.Warnings = append(result.Warnings, Warning{Code: "DUPLICATE_SINGLETON_FIELD", Field: "collection"})
	}
	if !validBoundedText(result.Name, MaxCollectionRunes, false) {
		result.Warnings = append(result.Warnings, Warning{Code: "PEGASUS_COLLECTION_NAME_INVALID", Field: "collection"})
	}
	result.ShortName, duplicates = scalar(raw.fields, "shortname")
	if duplicates {
		result.Warnings = append(result.Warnings, Warning{Code: "DUPLICATE_SINGLETON_FIELD", Field: "shortname"})
	}
	result.Description = firstNonEmpty(flowing(raw.fields, "description"), flowing(raw.fields, "summary"))
	result.Assets.Covers = list(raw.fields, "assets.box_front", "assets.boxfront", "assets.boxart2d")
	result.Assets.Videos = list(raw.fields, "assets.video")
	for _, value := range raw.fields {
		if ignoredDiscoveryRule(value.key) {
			result.IgnoredRules = appendUnique(result.IgnoredRules, value.key)
			continue
		}
		if !known[value.key] && !strings.HasPrefix(value.key, "ignore-") {
			result.UnknownFields = appendUnique(result.UnknownFields, value.key)
		}
	}
	for _, game := range raw.games {
		result.Games = append(result.Games, projectGame(game))
	}
	return result
}

func projectGame(raw rawGame) Game {
	result := Game{Ordinal: raw.ordinal}
	projectGameMetadata(raw.fields, &result)
	projectGameFiles(raw.fields, &result)
	projectGameFields(raw.fields, &result)
	return result
}

func projectGameMetadata(fields []field, result *Game) {
	result.Metadata.SchemaVersion = 1
	result.Metadata.Title, _ = scalar(fields, "game")
	if !validBoundedText(result.Metadata.Title, MaxTitleRunes, false) {
		result.BlockedCode = "PEGASUS_GAME_TITLE_INVALID"
	}
	result.Metadata.Description = firstNonEmpty(flowing(fields, "description"), flowing(fields, "summary"))
	if utf8.RuneCountInString(result.Metadata.Description) > MaxDescriptionRunes {
		result.Metadata.Description = truncateRunes(result.Metadata.Description, MaxDescriptionRunes)
		result.Warnings = append(result.Warnings, Warning{Code: "FIELD_TRUNCATED", Field: "description"})
	}
	result.Metadata.Developer = joined(fields, MaxJoinedRunes, &result.Warnings, "developer", "developers")
	result.Metadata.Publisher = joined(fields, MaxJoinedRunes, &result.Warnings, "publisher", "publishers")
	result.Metadata.Genre = joined(fields, MaxJoinedRunes, &result.Warnings, "genre", "genres")
	projectPlayers(fields, result)
	projectRelease(fields, result)
}

func projectPlayers(fields []field, result *Game) {
	players, duplicatePlayers := scalar(fields, "players")
	if duplicatePlayers {
		result.Warnings = append(result.Warnings, Warning{Code: "DUPLICATE_SINGLETON_FIELD", Field: "players"})
	}
	if players == "" {
		return
	}
	parsed, err := strconv.Atoi(players)
	if err == nil && parsed >= 1 && parsed <= 64 && strconv.Itoa(parsed) == players {
		result.Metadata.Players = &parsed
		return
	}
	result.Warnings = append(result.Warnings, Warning{Code: "FIELD_VALUE_INVALID", Field: "players"})
}

func projectRelease(fields []field, result *Game) {
	release, duplicateRelease := scalar(fields, "release")
	if duplicateRelease {
		result.Warnings = append(result.Warnings, Warning{Code: "DUPLICATE_SINGLETON_FIELD", Field: "release"})
	}
	if release == "" {
		return
	}
	if year, ok := releaseYear(release); ok {
		result.Metadata.ReleaseYear = &year
		return
	}
	result.Warnings = append(result.Warnings, Warning{Code: "FIELD_VALUE_INVALID", Field: "release"})
}

func projectGameFiles(fields []field, result *Game) {
	result.Files = list(fields, "file", "files")
	if len(result.Files) == 0 && result.BlockedCode == "" {
		result.BlockedCode = "PEGASUS_GAME_WITHOUT_FILE"
	} else if len(result.Files) > MaxGameFileValues && result.BlockedCode == "" {
		result.BlockedCode = "PEGASUS_MULTIPLE_LAUNCH_FILES_UNSUPPORTED"
	}
	result.Assets.Covers = list(fields, "assets.box_front", "assets.boxfront", "assets.boxart2d")
	result.Assets.Videos = list(fields, "assets.video")
}

func projectGameFields(fields []field, result *Game) {
	known := map[string]bool{
		"game": true, "description": true, "summary": true, "developer": true, "developers": true,
		"publisher": true, "publishers": true, "genre": true, "genres": true, "players": true,
		"release": true, "file": true, "files": true, "assets.box_front": true,
		"assets.boxfront": true, "assets.boxart2d": true, "assets.video": true, "assets.logo": true,
		"sort-by": true, "sort_title": true, "sort_name": true, "rating": true, "tag": true,
		"tags": true, "launch": true, "command": true, "workdir": true, "cwd": true,
	}
	for _, value := range fields {
		if !known[value.key] {
			result.UnknownFields = appendUnique(result.UnknownFields, value.key)
		}
	}
	for _, value := range fields {
		if singletonField(value.key) && len(value.values) > 1 {
			result.Warnings = append(result.Warnings, Warning{Code: "DUPLICATE_SINGLETON_FIELD", Field: value.key})
		}
	}
}

func scalar(fields []field, keys ...string) (string, bool) {
	values := list(fields, keys...)
	nonEmpty := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			nonEmpty = append(nonEmpty, value)
		}
	}
	if len(nonEmpty) == 0 {
		return "", false
	}
	return nonEmpty[len(nonEmpty)-1], len(nonEmpty) > 1
}

func flowing(fields []field, key string) string {
	var occurrences []string
	for _, value := range fields {
		if value.key != key {
			continue
		}
		parts := make([]string, 0, len(value.values))
		for _, line := range value.values {
			line = strings.ReplaceAll(strings.TrimSpace(line), `\n`, "\n")
			if line == "." {
				parts = append(parts, "\n\n")
			} else if line != "" {
				parts = append(parts, line)
			}
		}
		joined := ""
		for _, part := range parts {
			if part == "\n\n" {
				joined = strings.TrimSpace(joined) + "\n\n"
				continue
			}
			if joined != "" && !strings.HasSuffix(joined, "\n") {
				joined += " "
			}
			joined += part
		}
		if strings.TrimSpace(joined) != "" {
			occurrences = append(occurrences, strings.TrimSpace(joined))
		}
	}
	if len(occurrences) == 0 {
		return ""
	}
	return occurrences[len(occurrences)-1]
}

func list(fields []field, keys ...string) []string {
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	values := make([]string, 0)
	for _, value := range fields {
		if _, ok := keySet[value.key]; !ok {
			continue
		}
		for _, item := range value.values {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				values = append(values, trimmed)
			}
		}
	}
	return values
}

func joined(fields []field, maximum int, warnings *[]Warning, keys ...string) string {
	seen := map[string]struct{}{}
	values := make([]string, 0)
	for _, value := range list(fields, keys...) {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	result := strings.Join(values, " / ")
	if utf8.RuneCountInString(result) > maximum {
		result = truncateRunes(result, maximum)
		*warnings = append(*warnings, Warning{Code: "FIELD_TRUNCATED", Field: keys[0]})
	}
	return result
}

func validBoundedText(value string, maximum int, multiline bool) bool {
	if value == "" || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) && (!multiline || character != '\n' && character != '\r' && character != '\t') {
			return false
		}
	}
	return true
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

func releaseYear(value string) (int, bool) {
	parts := strings.Split(value, "-")
	if len(parts) < 1 || len(parts) > 3 || len(parts[0]) != 4 {
		return 0, false
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil || year < 1000 || year > 9999 {
		return 0, false
	}
	if len(parts) >= 2 && !validDatePart(parts[1], 1, 12) {
		return 0, false
	}
	if len(parts) == 3 && !validDatePart(parts[2], 1, 31) {
		return 0, false
	}
	return year, true
}

func validDatePart(value string, minimum, maximum int) bool {
	parsed, err := strconv.Atoi(value)
	return err == nil && len(value) == 2 && parsed >= minimum && parsed <= maximum
}

func ignoredDiscoveryRule(key string) bool {
	if strings.HasPrefix(key, "ignore-") {
		return true
	}
	switch key {
	case "extension", "extensions", "file", "files", "regex", "directory", "directories":
		return true
	default:
		return false
	}
}

func singletonField(key string) bool {
	switch key {
	case "game", "description", "summary", "players", "release":
		return true
	default:
		return false
	}
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
