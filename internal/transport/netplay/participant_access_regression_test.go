package netplay

import (
	"errors"
	"testing"
	"time"
)

func TestParticipantCapabilityPreservesDatabaseFailure(t *testing.T) {
	t.Parallel()
	database := openNetplayTestDatabase(t.Context(), t, time.Now)
	service := NewService(database.SQL, nil, nil, Options{}, time.Now)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ParticipantCapability(t.Context(), "session", "profile"); err == nil || errors.Is(err, ErrForbidden) {
		t.Fatalf("storage failure collapsed to forbidden: %v", err)
	}
}
