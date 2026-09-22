package libraryimport

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"retrom/internal/authn"
)

type metadataMemory struct {
	current                     MetadataDraft
	change                      MetadataChange
	readErr, saveErr, commitErr error
	reads, writes               int
}

func (m *metadataMemory) CurrentMetadata(context.Context, string) (MetadataDraft, error) {
	m.reads++
	return m.current, m.readErr
}

func (m *metadataMemory) SaveMetadata(_ context.Context, change MetadataChange) error {
	m.writes++
	m.change = change
	return m.saveErr
}

func (m *metadataMemory) WithMetadata(_ context.Context, work func(MetadataScope) error) error {
	if err := work(m); err != nil {
		return err
	}
	return m.commitErr
}
func metadataClock() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func TestMetadataSeederWritesCurrentMetadataAndSupportsIdempotency(t *testing.T) {
	t.Parallel()
	m := &metadataMemory{current: MetadataDraft{MetadataJSON: `{"title":"Before"}`, Version: 7}}
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "actor"})
	version, warnings, err := NewMetadataSeeder(m, metadataClock).Seed(ctx, "item", ServerMetadata{Title: "After"}, 2027)
	if err != nil || version != 8 || warnings == nil || len(warnings) != 0 || m.writes != 1 {
		t.Fatalf("seed result %d %#v %v, writes=%d", version, warnings, err, m.writes)
	}
	change := m.change
	assertMetadataChange(t, change, m.current)
	m.current = MetadataDraft{MetadataJSON: change.MetadataJSON, Version: 8}
	version, _, err = NewMetadataSeeder(m, metadataClock).Seed(ctx, "item", ServerMetadata{Title: "After"}, 2027)
	if err != nil || version != 8 || m.writes != 1 {
		t.Fatalf("idempotency %d %v writes=%d", version, err, m.writes)
	}
}

func assertMetadataChange(t *testing.T, change MetadataChange, before MetadataDraft) {
	t.Helper()
	if change.ItemID != "item" || change.Before != before || change.SearchText != "after" ||
		change.NowMS != metadataClock().UnixMilli() {
		t.Fatalf("change=%#v", change)
	}
}

func TestMetadataSeederPropagatesFailuresWithoutResult(t *testing.T) {
	t.Parallel()
	cause := errors.New("metadata persistence failed")
	for _, phase := range []string{"read", "save", "commit"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			m := &metadataMemory{current: MetadataDraft{MetadataJSON: `{}`, Version: 1}}
			switch phase {
			case "read":
				m.readErr = cause
			case "save":
				m.saveErr = cause
			case "commit":
				m.commitErr = cause
			}
			version, warnings, err := NewMetadataSeeder(m, metadataClock).Seed(t.Context(), "item", ServerMetadata{Title: "Changed"}, 2027)
			if !errors.Is(err, cause) || version != 0 || warnings != nil {
				t.Fatalf("%s failure=%d %#v %v", phase, version, warnings, err)
			}
		})
	}
}

func TestMetadataSeederRejectsInvalidInputAndDraft(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, item, before string
		version            int64
		title              string
	}{
		{"empty item", "", `{}`, 1, "Valid"},
		{"empty title", "item", `{}`, 1, ""},
		{"invalid json", "item", `{`, 1, "Valid"},
		{"array json", "item", `[]`, 1, "Valid"},
		{"null json", "item", `null`, 1, "Valid"},
		{"invalid version", "item", `{}`, 0, "Valid"},
		{"overflow", "item", `{}`, math.MaxInt64, "Valid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &metadataMemory{current: MetadataDraft{MetadataJSON: tc.before, Version: tc.version}}
			version, _, err := NewMetadataSeeder(m, metadataClock).Seed(t.Context(), tc.item, ServerMetadata{Title: tc.title}, 2027)
			if err == nil || version != 0 || m.writes != 0 {
				t.Fatalf("invalid draft accepted: %d %v writes=%d", version, err, m.writes)
			}
		})
	}
}

func TestMetadataNormalizationPreservesRuneLimitsAndWarningOrder(t *testing.T) {
	t.Parallel()
	year := 1949
	normalized, warnings, err := NormalizeServerReviewMetadata(ServerMetadata{Title: "Fixture", Description: strings.Repeat("界", 10001), Developer: strings.Repeat("开", 201), Publisher: strings.Repeat("发", 201), Genre: strings.Repeat("类", 201), ReleaseYear: &year}, 2027)
	if err != nil || len([]rune(normalized.Description)) != 10000 || len([]rune(normalized.Developer)) != 200 || len([]rune(normalized.Publisher)) != 200 || len([]rune(normalized.Genre)) != 200 || normalized.ReleaseYear != nil {
		t.Fatalf("normalization=%#v %v", normalized, err)
	}
	expected := []ServerMetadataWarning{{"FIELD_TRUNCATED", "description"}, {"FIELD_TRUNCATED", "developer"}, {"FIELD_TRUNCATED", "publisher"}, {"FIELD_TRUNCATED", "genre"}, {"FIELD_VALUE_INVALID", "releaseYear"}}
	if !reflect.DeepEqual(warnings, expected) {
		t.Fatalf("warnings=%#v", warnings)
	}
}

func TestMetadataNormalizationRejectsMalformedSourceFields(t *testing.T) {
	t.Parallel()
	zero, largePlayers, oldYear, futureYear := 0, 65, 999, 10000
	for _, tc := range []struct {
		name  string
		value ServerMetadata
	}{
		{"blank", ServerMetadata{Title: " "}},
		{"trailing space", ServerMetadata{Title: "Title "}},
		{"utf8", ServerMetadata{Title: string([]byte{255})}},
		{"title length", ServerMetadata{Title: strings.Repeat("界", 201)}},
		{"title control", ServerMetadata{Title: "Title\nnew"}},
		{"description", ServerMetadata{Title: "Title", Description: strings.Repeat("界", 20001)}},
		{"description control", ServerMetadata{Title: "Title", Description: "a\x00b"}},
		{"developer", ServerMetadata{Title: "Title", Developer: strings.Repeat("界", 501)}},
		{"publisher control", ServerMetadata{Title: "Title", Publisher: "a\nb"}},
		{"genre space", ServerMetadata{Title: "Title", Genre: " genre"}},
		{"zero players", ServerMetadata{Title: "Title", Players: &zero}},
		{"too many players", ServerMetadata{Title: "Title", Players: &largePlayers}},
		{"old year", ServerMetadata{Title: "Title", ReleaseYear: &oldYear}},
		{"future year", ServerMetadata{Title: "Title", ReleaseYear: &futureYear}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := NormalizeServerReviewMetadata(tc.value, 2027)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted %#v: %v", tc.value, err)
			}
		})
	}
}

func TestMetadataNormalizationAcceptsInclusiveLimits(t *testing.T) {
	t.Parallel()
	for _, year := range []int{1950, 2028} {
		for _, players := range []int{1, 64} {
			value := ServerMetadata{Title: strings.Repeat("界", 200), Description: "First\nSecond\r\nThird\tline", Developer: strings.Repeat("界", 200), Players: &players, ReleaseYear: &year}
			normalized, warnings, err := NormalizeServerReviewMetadata(value, 2028)
			if err != nil || !reflect.DeepEqual(normalized, value) || warnings == nil || len(warnings) != 0 {
				t.Fatalf("boundary=%#v %#v %v", normalized, warnings, err)
			}
		}
	}
}
