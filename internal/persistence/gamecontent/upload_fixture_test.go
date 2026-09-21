//go:build integration

package gamecontent

import (
	"context"
	"database/sql"

	"retrom/internal/service/gamecontent"
)

func validateUploadFixture(ctx context.Context, transaction *sql.Tx, id, mode, platform string) error {
	upload, err := (records{transaction}).Upload(ctx, id)
	if err != nil {
		return err
	}
	return gamecontent.ValidateUpload(upload, mode, platform)
}
