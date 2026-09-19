package httpapi

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"retrom/internal/foundation/cursor"
)

func TestCursorFilterDigestMatchesLegacyV1Encoding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		filter      any
		wantEncoded string
		wantDigest  string
	}{
		{
			name: "null", filter: nil, wantEncoded: "null",
			wantDigest: "74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b",
		},
		{
			name: "empty", filter: map[string]any{}, wantEncoded: `{}`,
			wantDigest: "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
		},
		{
			name: "unicode", filter: map[string]any{"q": "雪"}, wantEncoded: `{"q":"雪"}`,
			wantDigest: "e0a0674ea8858272ef3b87b8ac85f17ccd2c7dfcb039709c5e8af962a902fd85",
		},
		{
			name: "html", filter: map[string]any{"q": "<&>"}, wantEncoded: `{"q":"\u003c\u0026\u003e"}`,
			wantDigest: "517c8e1a7e5c1711935cd57c042d78b7bd3c92f3241dcfc79cd4d05b69c54553",
		},
		{
			name: "unicode-html-null",
			filter: map[string]any{
				"q": "雪<&>", "tagId": nil,
			},
			wantEncoded: `{"q":"雪\u003c\u0026\u003e","tagId":null}`,
			wantDigest:  "19cb33ca222b8e768398cc5a994217f11e0d490e514f3c7ec96f085a5ed143b1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.filter)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != test.wantEncoded {
				t.Fatalf("encoded = %q, want %q", encoded, test.wantEncoded)
			}
			if got := cursorFilterDigest(test.filter); got != test.wantDigest {
				t.Fatalf("digest = %q, want %q", got, test.wantDigest)
			}
		})
	}
}

func TestCursorFilterDigestPreservesMarshalFailureSemantics(t *testing.T) {
	t.Parallel()
	if got, want := cursorFilterDigest(failingCursorFilter{}), cursor.FilterDigest(nil); got != want {
		t.Fatalf("digest = %q, want nil-byte digest %q", got, want)
	}
}

func TestCursorBoundarySamplesClockOncePerOperation(t *testing.T) {
	t.Parallel()
	const nowMS = int64(1_786_000_000_000)
	calls := 0
	server := &Server{
		cursors: cursor.New([32]byte{1, 2, 3}),
		now: func() time.Time {
			calls++
			return time.UnixMilli(nowMS)
		},
	}
	filter := cursorFilterDigest(map[string]any{"q": "雪<&>"})
	token, err := server.encodeCursor(cursor.Payload{
		OperationID:  "getGames",
		FilterDigest: filter,
		SortCode:     "TITLE_ASC",
		SortValues:   []string{"雪<&>"},
		ID:           "01980000-0000-7000-8000-000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("encode clock calls = %d, want 1", calls)
	}
	if _, err := server.decodeCursor(token, "getGames", filter, "TITLE_ASC"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("decode clock calls = %d, want one additional call", calls)
	}
}

type failingCursorFilter struct{}

func (failingCursorFilter) MarshalJSON() ([]byte, error) {
	return nil, errors.New("fixture marshal failure")
}
