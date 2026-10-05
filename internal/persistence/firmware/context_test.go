package firmware

import (
	"context"
	"errors"
	"testing"
	"time"

	dbpostgres "retrom/internal/database/postgres"
	firmwareservice "retrom/internal/service/firmware"
	"retrom/internal/testsupport/testpostgres"
)

func TestBIOSPreparationPreservesCancelledRead(t *testing.T) {
	database, err := dbpostgres.Open(testpostgres.DSN(t), dbpostgres.Options{})
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
	_, err = firmwareservice.New(firmwareservice.Dependencies{Repository: New(database), Files: constructorFiles(t)}, time.Now).Install(ctx, "requirement", 1, firmwareservice.InstallRequest{UploadFileID: "upload"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled catalog read was lost: %v", err)
	}
}
