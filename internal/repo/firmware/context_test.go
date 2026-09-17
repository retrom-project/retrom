package firmware

import (
	"context"
	"database/sql"
	"errors"
	firmwareservice "retrom/internal/service/firmware"
	"testing"
	"time"

	firmwaremodel "retrom/internal/model/firmware"

	_ "modernc.org/sqlite"
)

func TestBIOSPreparationPreservesCancelledRead(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = firmwareservice.New(New(database), time.Now).Install(ctx, "requirement", 1, firmwaremodel.InstallRequest{UploadFileID: "upload"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled catalog read was lost: %v", err)
	}
}
