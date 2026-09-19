package testsupport

import (
	"context"
	"slices"
	"sync"

	"retrom/internal/model/diagnostics"
)

// DiagnosticRecorder records explicitly injected test diagnostics without global logging.
type DiagnosticRecorder struct {
	mutex  sync.Mutex
	events []diagnostics.DiagnosticEvent
}

func (recorder *DiagnosticRecorder) Report(_ context.Context, event diagnostics.DiagnosticEvent) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.events = append(recorder.events, event)
}

func (recorder *DiagnosticRecorder) Events() []diagnostics.DiagnosticEvent {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return slices.Clone(recorder.events)
}
