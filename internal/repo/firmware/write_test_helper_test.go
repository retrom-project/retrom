package firmware

import (
	"context"
	"fmt"

	"retrom/internal/model/firmware"
	"retrom/internal/repo/dbexec"
)

func testWithWrite(ctx context.Context, repo *Repository, work func(firmware.WriteScope) error) error {
	tx, err := repo.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin BIOS write: %w", err)
	}
	defer dbexec.Rollback(tx)
	if err := work(repo.writeScope(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit BIOS write: %w", err)
	}
	return nil
}
