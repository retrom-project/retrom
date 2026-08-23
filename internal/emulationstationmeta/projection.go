package emulationstationmeta

import (
	"encoding/xml"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type gameBuilder struct {
	game            Game
	releaseYearMax  int
	seen            map[string]int
	duplicateWarned map[string]bool
	media           map[string]mediaCandidate
	warnings        warningCollector
	ignored         *ignoredCollector
	pathAmbiguous   bool
}

type mediaCandidate struct {
	present          bool
	relativePath     string
	resolutionMethod string
}

func newGameBuilder(
	ordinal, releaseYearMax int,
	attributes []xml.Attr,
	ignored *ignoredCollector,
) *gameBuilder {
	builder := &gameBuilder{
		game: Game{
			Ordinal:  ordinal,
			Metadata: Metadata{SchemaVersion: 1},
		},
		releaseYearMax:  releaseYearMax,
		seen:            make(map[string]int),
		duplicateWarned: make(map[string]bool),
		media:           make(map[string]mediaCandidate),
		ignored:         ignored,
	}
	for _, attribute := range attributes {
		field := "game/@" + reportedName(attribute.Name.Local)
		builder.ignored.add(field)
		builder.warnings.add(Warning{Code: WarningFieldIgnored, Field: field})
	}
	return builder
}

func (builder *gameBuilder) consume(field rawField) {
	name := field.name
	prior := builder.seen[name]
	builder.seen[name] = prior + 1
	if prior > 0 {
		builder.consumeDuplicate(name)
		return
	}
	if builder.consumeStructured(field) {
		return
	}
	builder.consumeProjected(name, field.text)
}

func (builder *gameBuilder) consumeStructured(field rawField) bool {
	name := field.name
	if name == "scrap" {
		builder.recordScrapAttributes(field.attributes)
	}
	if !field.structured || !knownField(name) {
		return false
	}
	builder.warnings.add(Warning{Code: WarningFieldStructure, Field: name})
	if name == "path" {
		builder.game.BlockedCode = CodePathInvalid
	}
	builder.consumeIgnored(name)
	return true
}

func (builder *gameBuilder) consumeProjected(name, value string) {
	switch name {
	case "path":
		builder.consumePath(value)
	case "name":
		builder.game.Metadata.Title = builder.projectText(name, value, MaxTitleRunes)
	case "desc":
		builder.game.Metadata.Description = builder.projectText(name, value, MaxDescriptionRunes)
	case "developer":
		builder.game.Metadata.Developer = builder.projectText(name, value, MaxMetadataRunes)
	case "publisher":
		builder.game.Metadata.Publisher = builder.projectText(name, value, MaxMetadataRunes)
	case "genre":
		builder.game.Metadata.Genre = builder.projectText(name, value, MaxMetadataRunes)
	case "players":
		builder.consumePlayers(value)
	case "releasedate":
		builder.consumeReleaseDate(value)
	case "hidden":
		builder.game.SourceFlags.Hidden = builder.sourceFlag(name, value)
	case "adult":
		builder.game.SourceFlags.Adult = builder.sourceFlag(name, value)
	case "kidgame":
		builder.game.SourceFlags.KidGame = builder.sourceFlag(name, value)
	case "image", "boxart", "mix", "thumbnail", "thumbnails", "video":
		builder.consumeMedia(name, value)
	default:
		builder.consumeIgnored(name)
	}
}

func (builder *gameBuilder) consumeDuplicate(name string) {
	if name == "path" {
		builder.pathAmbiguous = true
		builder.game.Path = ""
		builder.game.BlockedCode = CodePathAmbiguous
		return
	}
	if !builder.duplicateWarned[name] {
		builder.duplicateWarned[name] = true
		builder.warnings.add(Warning{Code: WarningDuplicateField, Field: reportedName(name)})
	}
}

func (builder *gameBuilder) consumePath(value string) {
	if value == "" {
		return
	}
	normalized, err := NormalizeDeclaredPath(value)
	if err != nil {
		builder.game.BlockedCode = CodePathInvalid
		builder.warnings.add(Warning{Code: CodePathInvalid, Field: "path", PathKind: "CONTENT"})
		return
	}
	builder.game.Path = normalized
}

func (builder *gameBuilder) projectText(field, value string, maximum int) string {
	value = normalizeText(value)
	originalLength := utf8.RuneCountInString(value)
	if originalLength <= maximum {
		return value
	}
	builder.warnings.add(Warning{
		Code: WarningFieldTruncated, Field: field,
		OriginalLength: originalLength, RetainedLength: maximum,
	})
	return truncateRunes(value, maximum)
}

func (builder *gameBuilder) consumePlayers(value string) {
	value = normalizeText(value)
	if value == "" {
		return
	}
	if players, ok := parsePlayerCount(value); ok {
		builder.game.Metadata.Players = &players
		return
	}
	minimum, maximum, ok := parsePlayerRange(value)
	if ok && minimum <= maximum {
		builder.game.Metadata.Players = &maximum
		builder.warnings.add(Warning{Code: WarningPlayerRange, Field: "players"})
		return
	}
	builder.warnings.add(Warning{Code: WarningFieldInvalid, Field: "players"})
}

func (builder *gameBuilder) consumeReleaseDate(value string) {
	value = normalizeText(value)
	if value == "" {
		return
	}
	year, ok := releaseYear(value)
	if !ok || year < 1950 || year > builder.releaseYearMax {
		builder.warnings.add(Warning{Code: WarningFieldInvalid, Field: "releasedate"})
		return
	}
	builder.game.Metadata.ReleaseYear = &year
}

func (builder *gameBuilder) sourceFlag(field, value string) bool {
	value = normalizeText(value)
	if asciiEqualFold(value, "true") {
		return true
	}
	if asciiEqualFold(value, "false") {
		return false
	}
	if value != "" {
		builder.warnings.add(Warning{Code: WarningFieldInvalid, Field: field})
	}
	return false
}

func (builder *gameBuilder) consumeMedia(field, value string) {
	if strings.TrimSpace(value) == "" {
		builder.media[field] = mediaCandidate{}
		return
	}
	kind := "COVER"
	if field == "video" {
		kind = "VIDEO"
	}
	candidate := mediaCandidate{present: true, resolutionMethod: mediaResolutionMethod(field)}
	normalized, err := NormalizeDeclaredPath(value)
	if err != nil {
		builder.warnings.add(Warning{Code: CodePathInvalid, Field: field, PathKind: kind})
	} else {
		candidate.relativePath = normalized
	}
	builder.media[field] = candidate
}

func (builder *gameBuilder) consumeIgnored(field string) {
	if executionField(field) {
		builder.ignored.add(field)
		builder.warnings.add(Warning{Code: WarningExecutionField, Field: field})
		return
	}
	if ignoredField(field) || !knownField(field) {
		field = reportedName(field)
		builder.ignored.add(field)
		builder.warnings.add(Warning{Code: WarningFieldIgnored, Field: field})
	}
}

func (builder *gameBuilder) recordScrapAttributes(attributes []xml.Attr) {
	for _, attribute := range attributes {
		field := "scrap/@" + reportedName(attribute.Name.Local)
		builder.ignored.add(field)
		builder.warnings.add(Warning{Code: WarningFieldIgnored, Field: field})
	}
}

func (builder *gameBuilder) finish() Game {
	if builder.pathAmbiguous {
		builder.game.BlockedCode = CodePathAmbiguous
	} else if builder.seen["path"] == 0 || builder.game.Path == "" && builder.game.BlockedCode == "" {
		builder.game.BlockedCode = CodePathMissing
	}
	if builder.game.Metadata.Title == "" {
		builder.game.Metadata.Title = builder.projectText("name", fallbackTitle(builder.game.Path), MaxTitleRunes)
	}
	if builder.game.Metadata.Title == "" {
		builder.game.Metadata.Title = "未命名游戏"
	}
	builder.game.Assets.Cover = builder.selectedCover()
	if candidate := builder.media["video"]; candidate.present {
		builder.game.Assets.Video = assetReference(candidate)
	}
	builder.game.Warnings = builder.warnings.values
	return builder.game
}

func (builder *gameBuilder) selectedCover() *AssetReference {
	for _, field := range []string{"image", "boxart", "mix", "thumbnail", "thumbnails"} {
		if candidate := builder.media[field]; candidate.present {
			return assetReference(candidate)
		}
	}
	return nil
}

func assetReference(candidate mediaCandidate) *AssetReference {
	return &AssetReference{
		RelativePath: candidate.relativePath, ResolutionMethod: candidate.resolutionMethod,
	}
}

func mediaResolutionMethod(field string) string {
	switch field {
	case "image":
		return "EXPLICIT_IMAGE"
	case "boxart":
		return "EXPLICIT_BOXART"
	case "mix":
		return "EXPLICIT_MIX"
	case "thumbnail":
		return "EXPLICIT_THUMBNAIL"
	case "thumbnails":
		return "EXPLICIT_THUMBNAIL_ALIAS"
	case "video":
		return "EXPLICIT_VIDEO"
	default:
		return ""
	}
}

func fallbackTitle(relativePath string) string {
	if relativePath == "" {
		return ""
	}
	basename := path.Base(relativePath)
	if extension := path.Ext(basename); extension != "" {
		basename = strings.TrimSuffix(basename, extension)
	}
	return basename
}

func normalizeText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.TrimSpace(value)
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

func parsePlayerCount(value string) (int, bool) {
	if len(value) == 0 || len(value) > 2 {
		return 0, false
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.Atoi(value)
	return parsed, err == nil && parsed >= 1 && parsed <= 64
}

func parsePlayerRange(value string) (int, int, bool) {
	if strings.Count(value, "-") != 1 {
		return 0, 0, false
	}
	parts := strings.SplitN(value, "-", 2)
	minimum, validMinimum := parsePlayerCount(parts[0])
	maximum, validMaximum := parsePlayerCount(parts[1])
	return minimum, maximum, validMinimum && validMaximum
}

func releaseYear(value string) (int, bool) {
	formats := []string{"20060102T150405", "20060102", "02/01/2006"}
	for _, format := range formats {
		parsed, err := time.Parse(format, value)
		if err == nil {
			return parsed.Year(), true
		}
	}
	return 0, false
}

func asciiEqualFold(value, expected string) bool {
	if len(value) != len(expected) {
		return false
	}
	for index := range value {
		character := value[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		if character != expected[index] {
			return false
		}
	}
	return true
}

func knownField(field string) bool {
	switch field {
	case "path", "name", "desc", "developer", "publisher", "genre", "players", "releasedate",
		"image", "boxart", "mix", "thumbnail", "thumbnails", "video", "hidden", "adult", "kidgame",
		"rating", "sortname", "family", "lang", "region", "genreid", "arcadesystemname", "favorite",
		"playcount", "lastplayed", "gametime", "md5", "crc32", "hash", "cheevosHash", "cheevosId",
		"command", "emulator", "core", "marquee", "wheel", "fanart", "manual", "scrap":
		return true
	default:
		return false
	}
}

func ignoredField(field string) bool {
	return knownField(field) && !projectedField(field) && !executionField(field)
}

func projectedField(field string) bool {
	switch field {
	case "path", "name", "desc", "developer", "publisher", "genre", "players", "releasedate",
		"image", "boxart", "mix", "thumbnail", "thumbnails", "video", "hidden", "adult", "kidgame":
		return true
	default:
		return false
	}
}

func executionField(field string) bool {
	return field == "command" || field == "emulator" || field == "core"
}

type warningCollector struct {
	values []Warning
}

func (collector *warningCollector) add(warning Warning) {
	if len(collector.values) < MaxWarnings-1 {
		collector.values = append(collector.values, warning)
		return
	}
	if len(collector.values) == MaxWarnings-1 {
		collector.values = append(collector.values, Warning{Code: WarningLimitReached, OmittedCount: 1})
		return
	}
	collector.values[MaxWarnings-1].OmittedCount++
}
