package emulationstationmeta

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseContextHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ParseContext(ctx, []byte(`<gameList/>`), 2027)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ParseContext() error = %v", err)
	}
}

func TestParseContextChecksCancellationDuringTokenConsumption(t *testing.T) {
	t.Parallel()
	ctx := &cancelOnSecondCheckContext{}
	document := `<gameList>` + strings.Repeat(`<ignored/>`, 200) + `</gameList>`
	_, err := ParseContext(ctx, []byte(document), 2027)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ParseContext() error = %v", err)
	}
}

type cancelOnSecondCheckContext struct{ checks int }

func (*cancelOnSecondCheckContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelOnSecondCheckContext) Done() <-chan struct{}       { return nil }
func (ctx *cancelOnSecondCheckContext) Err() error {
	ctx.checks++
	if ctx.checks >= 2 {
		return context.Canceled
	}
	return nil
}
func (*cancelOnSecondCheckContext) Value(any) any { return nil }

func TestParseProjectsDocumentGamesFieldsAndAssets(t *testing.T) {
	t.Parallel()
	xmlDocument := "\xef\xbb\xbf<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n" +
		`<!-- before --><gameList source="ignored"><provider><name>private scraper</name></provider>` +
		`<folder><path>./subdirectory</path></folder><future><game><path>ignored.gba</path></game></future>` +
		`<game id="42" source="private source"><boxart>./media/box.png</boxart>` +
		`<path>./roms\demo.gba</path><name> Demo &amp; <![CDATA[Test]]> </name>` +
		"<desc> first\r\nsecond </desc><developer> Dev </developer><publisher> Pub </publisher>" +
		`<genre>Action</genre><players>1-4</players><releasedate>20240229T235959</releasedate>` +
		`<hidden>TRUE</hidden><adult>false</adult><kidgame>TrUe</kidgame>` +
		`<image>./media\cover.png</image><thumbnail>lower.png</thumbnail><video>./media/demo.webm</video>` +
		`</game><!-- after game --></gameList><!-- after root -->`
	document, err := Parse([]byte(xmlDocument), 2027)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(document.Games) != 1 {
		t.Fatalf("document summary = %#v", document)
	}
	summary := Document{FolderEntryCount: 1, ProviderPresent: true}
	if document.FolderEntryCount != summary.FolderEntryCount || document.ProviderPresent != summary.ProviderPresent {
		t.Fatalf("document summary = %#v", document)
	}
	game := document.Games[0]
	if game.Ordinal != 1 || game.Path != "roms/demo.gba" || game.BlockedCode != "" {
		t.Fatalf("game identity = %#v", game)
	}
	wantPlayers, wantYear := 4, 2024
	wantMetadata := Metadata{
		SchemaVersion: 1, Title: "Demo & Test", Description: "first\nsecond", Developer: "Dev",
		Publisher: "Pub", Genre: "Action", Players: &wantPlayers, ReleaseYear: &wantYear,
	}
	if !reflect.DeepEqual(game.Metadata, wantMetadata) {
		t.Fatalf("metadata text = %#v", game.Metadata)
	}
	if game.SourceFlags != (SourceFlags{Hidden: true, KidGame: true}) {
		t.Fatalf("source flags = %#v", game.SourceFlags)
	}
	wantAssets := AssetReferences{
		Cover: &AssetReference{RelativePath: "media/cover.png", ResolutionMethod: "EXPLICIT_IMAGE"},
		Video: &AssetReference{RelativePath: "media/demo.webm", ResolutionMethod: "EXPLICIT_VIDEO"},
	}
	if !reflect.DeepEqual(game.Assets, wantAssets) {
		t.Fatalf("assets = %#v", game.Assets)
	}
	assertWarning(t, game.Warnings, WarningPlayerRange, "players")
	assertWarning(t, game.Warnings, WarningFieldIgnored, "game/@id")
	assertWarning(t, game.Warnings, WarningFieldIgnored, "game/@source")
	assertStringsEqual(t, document.IgnoredFields, []string{"future", "game/@id", "game/@source"})
}

func TestParsePreservesDirectGameOrderAndFallsBackToSafeBasename(t *testing.T) {
	t.Parallel()
	longName := strings.Repeat("a", MaxTitleRunes+3)
	xmlDocument := `<gameList><game><path>./nested/first.gba</path></game>` +
		`<folder><path>ignored</path></folder><game><path>` + longName + `.gba</path><name/></game>` +
		`<game><name/></game></gameList>`
	document, err := Parse([]byte(xmlDocument), 2027)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(document.Games) != 3 || document.Games[0].Ordinal != 1 || document.Games[1].Ordinal != 2 ||
		document.Games[2].Ordinal != 3 {
		t.Fatalf("game ordinals = %#v", document.Games)
	}
	if document.Games[0].Metadata.Title != "first" || document.Games[2].Metadata.Title != "未命名游戏" ||
		document.Games[2].BlockedCode != CodePathMissing {
		t.Fatalf("fallbacks = %#v", document.Games)
	}
	if got := len([]rune(document.Games[1].Metadata.Title)); got != MaxTitleRunes {
		t.Fatalf("fallback title length = %d", got)
	}
	assertTruncation(t, document.Games[1].Warnings, "name", MaxTitleRunes+3, MaxTitleRunes)
}

func TestParseUsesFirstSingletonAndFixedMediaPriority(t *testing.T) {
	t.Parallel()
	xmlDocument := `<gameList><game><path>first.gba</path><path>second.gba</path>` +
		`<name>First</name><name>Second</name><image/><image>ignored.png</image>` +
		`<boxart>box.png</boxart><mix>mix.png</mix><thumbnail>thumb.png</thumbnail>` +
		`<thumbnails>alias.png</thumbnails><video/><video>ignored.mp4</video></game></gameList>`
	document, err := Parse([]byte(xmlDocument), 2027)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	game := document.Games[0]
	if game.Path != "" || game.BlockedCode != CodePathAmbiguous || game.Metadata.Title != "First" {
		t.Fatalf("singleton projection = %#v", game)
	}
	if game.Assets.Cover == nil || game.Assets.Cover.RelativePath != "box.png" ||
		game.Assets.Cover.ResolutionMethod != "EXPLICIT_BOXART" || game.Assets.Video != nil {
		t.Fatalf("asset priority = %#v", game.Assets)
	}
	for _, field := range []string{"name", "image", "video"} {
		assertWarning(t, game.Warnings, WarningDuplicateField, field)
	}
	if warningCount(game.Warnings, WarningDuplicateField, "path") != 0 {
		t.Fatalf("duplicate path emitted metadata warning: %#v", game.Warnings)
	}
}

func TestParseRejectsNestedKnownFieldsWithoutProjectingNestedText(t *testing.T) {
	t.Parallel()
	xmlDocument := `<gameList><game><path><value>secret.gba</value></path>` +
		`<name>safe<title>secret title</title></name><command><arg>secret command</arg></command>` +
		`<unknown><nested>secret unknown</nested></unknown></game></gameList>`
	document, err := Parse([]byte(xmlDocument), 2027)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	game := document.Games[0]
	if game.BlockedCode != CodePathInvalid || game.Path != "" || game.Metadata.Title != "未命名游戏" {
		t.Fatalf("nested projection = %#v", game)
	}
	for _, field := range []string{"path", "name", "command"} {
		assertWarning(t, game.Warnings, WarningFieldStructure, field)
	}
	if warningCount(game.Warnings, WarningFieldStructure, "unknown") != 0 {
		t.Fatalf("unknown subtree treated as a known structured field: %#v", game.Warnings)
	}
	assertWarning(t, game.Warnings, WarningExecutionField, "command")
	assertWarning(t, game.Warnings, WarningFieldIgnored, "unknown")
}

func TestParseNeverProjectsIgnoredOrExecutionFieldValues(t *testing.T) {
	t.Parallel()
	const secret = "TOP-SECRET-EXECUTION-PAYLOAD"
	xmlDocument := `<gameList><provider>` + secret + `</provider><game id="` + secret + `">` +
		`<path>safe.gba</path><command>` + secret + `</command><emulator>` + secret + `</emulator>` +
		`<core>` + secret + `</core><favorite>` + secret + `</favorite><marquee>` + secret + `</marquee>` +
		`<scrap source="` + secret + `">` + secret + `</scrap><unknown>` + secret + `</unknown></game></gameList>`
	document, err := Parse([]byte(xmlDocument), 2027)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	projection, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(projection), secret) {
		t.Fatalf("ignored field value leaked: %s", projection)
	}
	for _, field := range []string{"command", "emulator", "core"} {
		assertWarning(t, document.Games[0].Warnings, WarningExecutionField, field)
	}
	for _, field := range []string{"favorite", "marquee", "scrap", "scrap/@source", "unknown"} {
		assertWarning(t, document.Games[0].Warnings, WarningFieldIgnored, field)
	}
}

func TestParseTruncatesTextByUnicodeCodePointAndNormalizesLineEndings(t *testing.T) {
	t.Parallel()
	description := strings.Repeat("段", MaxDescriptionRunes+2)
	developer := strings.Repeat("界", MaxMetadataRunes+1)
	xmlDocument := `<gameList><game><path>safe.gba</path><name> title </name><desc>` + description +
		"\rline\r\nlast</desc><developer>" + developer + `</developer></game></gameList>`
	document, err := Parse([]byte(xmlDocument), 2027)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	game := document.Games[0]
	if game.Metadata.Title != "title" || len([]rune(game.Metadata.Description)) != MaxDescriptionRunes ||
		len([]rune(game.Metadata.Developer)) != MaxMetadataRunes {
		t.Fatalf("truncated metadata = %#v", game.Metadata)
	}
	assertTruncation(t, game.Warnings, "desc", MaxDescriptionRunes+12, MaxDescriptionRunes)
	assertTruncation(t, game.Warnings, "developer", MaxMetadataRunes+1, MaxMetadataRunes)
}

func TestParseCapsWarningsWithoutLeakingOmittedFields(t *testing.T) {
	t.Parallel()
	var xmlDocument strings.Builder
	xmlDocument.WriteString("<gameList><game>")
	for index := 0; index < MaxGameFields; index++ {
		xmlDocument.WriteString("<unknown")
		xmlDocument.WriteString(strconvThreeDigits(index))
		xmlDocument.WriteString("/>")
	}
	xmlDocument.WriteString("</game></gameList>")
	document, err := Parse([]byte(xmlDocument.String()), 2027)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	warnings := document.Games[0].Warnings
	if len(warnings) != MaxWarnings || warnings[MaxWarnings-1].Code != WarningLimitReached ||
		warnings[MaxWarnings-1].OmittedCount != MaxGameFields-(MaxWarnings-1) {
		t.Fatalf("warning cap = %#v", warnings)
	}
	if len(document.IgnoredFields) != MaxIgnoredFieldNames ||
		document.IgnoredFieldOtherCount != MaxGameFields-MaxIgnoredFieldNames {
		t.Fatalf("ignored field cap = %#v", document)
	}
}

func assertWarning(t *testing.T, warnings []Warning, code, field string) {
	t.Helper()
	if warningCount(warnings, code, field) == 0 {
		t.Fatalf("missing warning %s/%s in %#v", code, field, warnings)
	}
}

func warningCount(warnings []Warning, code, field string) int {
	count := 0
	for _, warning := range warnings {
		if warning.Code == code && warning.Field == field {
			count++
		}
	}
	return count
}

func assertTruncation(t *testing.T, warnings []Warning, field string, original, retained int) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Code == WarningFieldTruncated && warning.Field == field &&
			warning.OriginalLength == original && warning.RetainedLength == retained {
			return
		}
	}
	t.Fatalf("missing truncation %s/%d/%d in %#v", field, original, retained, warnings)
}

func assertStringsEqual(t *testing.T, actual, expected []string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("strings = %#v, want %#v", actual, expected)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("strings = %#v, want %#v", actual, expected)
		}
	}
}

func strconvThreeDigits(value int) string {
	if value < 10 {
		return "00" + string(rune('0'+value))
	}
	if value < 100 {
		return "0" + string(rune('0'+value/10)) + string(rune('0'+value%10))
	}
	return string(rune('0'+value/100)) + string(rune('0'+value/10%10)) + string(rune('0'+value%10))
}

func TestParseErrorsAreStableSentinels(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte(`<gameList><game><path>&undeclared;</path></game></gameList>`), 2027)
	if !errors.Is(err, ErrInvalidXML) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidXML)
	}
}
