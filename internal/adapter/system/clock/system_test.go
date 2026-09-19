package clock_test

import (
	"testing"
	"time"

	systemclock "retrom/internal/adapter/system/clock"
	clockmodel "retrom/internal/model/clock"
)

func TestSystemReportsUnixMilliseconds(t *testing.T) {
	var source clockmodel.Clock = systemclock.System{}
	before := time.Now().UnixMilli()
	actual := source.NowMS()
	after := time.Now().UnixMilli()
	if actual < before || actual > after {
		t.Fatalf("NowMS = %d, want Unix milliseconds between %d and %d", actual, before, after)
	}
}
