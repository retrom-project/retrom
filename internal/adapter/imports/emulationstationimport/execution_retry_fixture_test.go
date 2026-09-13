package emulationstationimport

import (
	"time"

	application "retrom/internal/service/emulationstationimport"
)

func automaticRetryDelay(attempt int64) time.Duration {
	return time.Duration(application.RecoveryDelayMS(attempt)) * time.Millisecond
}
