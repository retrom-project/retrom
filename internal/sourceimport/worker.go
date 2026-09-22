package sourceimport

import (
	"context"
	"errors"
	"fmt"
	"io"

	application "retrom/internal/service/sourceimport"
)

type (
	executionItem  = application.ExecutionItem
	executionFile  = application.ExecutionFile
	executionAsset = application.ExecutionAsset
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, fmt.Errorf("sourceimport/read cancelled: %w", reader.ctx.Err())
	default:
	}
	if len(buffer) > 8<<20 {
		buffer = buffer[:8<<20]
	}
	count, err := reader.reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		return count, io.EOF
	}
	if err != nil {
		return count, fmt.Errorf("sourceimport/read source: %w", err)
	}
	return count, nil
}
