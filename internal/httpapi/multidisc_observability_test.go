package httpapi

import (
	"testing"

	"retrom/internal/httpapi/generated"
	"retrom/internal/launch"
)

func TestDiscCountBucket(t *testing.T) {
	t.Parallel()
	tests := map[int]string{-1: "unknown", 0: "0-1", 2: "2", 3: "3-4", 4: "3-4", 5: "5-8", 8: "5-8", 9: "9+"}
	for count, expected := range tests {
		if actual := discCountBucket(count); actual != expected {
			t.Errorf("discCountBucket(%d) = %q, want %q", count, actual, expected)
		}
	}
}

func TestValidMultiDiscPlayerEvent(t *testing.T) {
	t.Parallel()
	dimensions := launch.MultiDiscTelemetryDimensions{DiscCount: 3}
	three, two := 3, 2
	tests := []struct {
		name  string
		body  generated.MultiDiscPlayerEventRequest
		valid bool
	}{
		{name: "start", body: playerEvent("START", "OK", 3, &three), valid: true},
		{name: "mismatch", body: playerEvent("DISK_COUNT_MISMATCH", "PLAYER_DISC_SET_INVALID", 3, &two), valid: true},
		{name: "switch failure", body: playerEvent("SWITCH_FAILURE", "PLAYER_DISC_SWITCH_FAILED", 3, nil), valid: true},
		{name: "wrong locked count", body: playerEvent("START", "OK", 2, &two)},
		{name: "success with error", body: playerEvent("SWITCH_SUCCESS", "PLAYER_DISC_SWITCH_FAILED", 3, &three)},
		{name: "false mismatch", body: playerEvent("DISK_COUNT_MISMATCH", "PLAYER_DISC_SET_INVALID", 3, &three)},
		{name: "failure marked ok", body: playerEvent("SAVE_RESTORE_FAILURE", "OK", 3, &three)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := validMultiDiscPlayerEvent(test.body, dimensions); actual != test.valid {
				t.Fatalf("validMultiDiscPlayerEvent() = %t, want %t", actual, test.valid)
			}
		})
	}
}

func playerEvent(eventType, resultCode string, discCount int, observed *int) generated.MultiDiscPlayerEventRequest {
	return generated.MultiDiscPlayerEventRequest{
		EventType:  generated.MultiDiscPlayerEventRequestEventType(eventType),
		ResultCode: generated.MultiDiscPlayerEventRequestResultCode(resultCode),
		DiscCount:  discCount, ObservedDiscCount: observed,
	}
}
