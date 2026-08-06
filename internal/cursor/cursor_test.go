package cursor

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCodecBindsOperationFilterSortAndExpiry(t *testing.T) {
	t.Parallel()
	now := time.UnixMilli(1_786_000_000_000)
	codec := New([32]byte{1, 2, 3}, func() time.Time { return now })
	filter := FilterDigest(map[string]any{"q": "mario"})
	token, err := codec.Encode(
		Payload{
			OperationID:  "getGames",
			FilterDigest: filter,
			SortCode:     "TITLE_ASC",
			SortValues:   []string{"Mario"},
			ID:           "01980000-0000-7000-8000-000000000001",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(token, "getGames", filter, "TITLE_ASC"); err != nil {
		t.Fatal(err)
	}
	for name, candidate := range map[string]string{"operation": "getSaves", "filter": strings.Repeat("0", 64), "sort": "UPDATED_DESC"} {
		t.Run(name, func(t *testing.T) {
			operation, digest, sortCode := "getGames", filter, "TITLE_ASC"
			switch name {
			case "operation":
				operation = candidate
			case "filter":
				digest = candidate
			case "sort":
				sortCode = candidate
			}
			if _, err := codec.Decode(token, operation, digest, sortCode); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	now = now.Add(25 * time.Hour)
	if _, err := codec.Decode(token, "getGames", filter, "TITLE_ASC"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expiry error = %v", err)
	}
}
