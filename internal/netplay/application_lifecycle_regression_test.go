package netplay

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestNetplayCloseBeforeMaintenanceStartDoesNotBlock(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		service := NewService(nil, nil, nil, Options{}, time.Now)
		closed := make(chan struct{})
		go func() { service.Close(); close(closed) }()
		synctest.Wait()
		select {
		case <-closed:
		default:
			t.Error("Close blocked waiting for maintenance that never started")
		}
		service.StartMaintenance()
		synctest.Wait()
	})
}

// This test is sequential because it captures the process-wide default logger.
func TestNetplayMaintenanceReportsStorageFailures(t *testing.T) {
	database := openNetplayTestDatabase(t.Context(), t, time.Now)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	synctest.Test(t, func(t *testing.T) {
		service := NewService(database.SQL, nil, nil, Options{}, time.Now)
		service.StartMaintenance()
		time.Sleep(31 * time.Second)
		service.Close()
		if !strings.Contains(output.String(), "netplay maintenance") {
			t.Error("maintenance storage failure was silently ignored")
		}
	})
}
