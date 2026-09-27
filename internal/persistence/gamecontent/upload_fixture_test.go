//go:build integration

package gamecontent

import (
	"context"

	dbapi "retrom/internal/database"

	"retrom/internal/service/gamecontent"
)

func validateUploadFixture(ctx context.Context, transaction dbapi.Tx, id, mode, platform string) error {
	upload, err := (records{transaction}).Upload(ctx, id)
	if err != nil {
		return err
	}
	return gamecontent.ValidateUpload(upload, mode, platform)
}
