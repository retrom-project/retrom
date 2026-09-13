package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"io"

	application "retrom/internal/service/emulationstationimport"
)

var errImportCancelled = application.ErrExecutionCancelled

type (
	executionItem  = application.ExecutionItem
	executionFile  = application.ExecutionFile
	executionAsset = application.ExecutionAsset
)

type contextReader struct {
	ctx                         context.Context
	reader                      io.Reader
	check                       func() error
	bytesUntilCancellationCheck int
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, fmt.Errorf("emulationstationimport/read cancelled: %w", reader.ctx.Err())
	default:
	}
	if reader.bytesUntilCancellationCheck <= 0 {
		if reader.check != nil {
			if err := reader.check(); err != nil {
				return 0, fmt.Errorf("check EmulationStation source execution: %w", err)
			}
		}
		reader.bytesUntilCancellationCheck = 8 << 20
	}
	if len(buffer) > reader.bytesUntilCancellationCheck {
		buffer = buffer[:reader.bytesUntilCancellationCheck]
	}
	count, err := reader.reader.Read(buffer)
	reader.bytesUntilCancellationCheck -= count
	if errors.Is(err, io.EOF) {
		return count, io.EOF
	}
	if err != nil {
		return count, fmt.Errorf("emulationstationimport/read source: %w", err)
	}
	return count, nil
}

func (service *Service) updateExecutionPhase(ctx context.Context, unit work, phase string) error {
	if err := service.materialization().SetPhase(ctx, unit, phase); err != nil {
		return fmt.Errorf("update EmulationStation phase: %w", err)
	}
	return nil
}
