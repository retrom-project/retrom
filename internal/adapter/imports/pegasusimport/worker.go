package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"io"

	pegasusimportmodel "retrom/internal/model/pegasusimport"
)

type (
	executionItem  = pegasusimportmodel.ExecutionItem
	executionFile  = pegasusimportmodel.ExecutionFile
	executionAsset = pegasusimportmodel.ExecutionAsset
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, fmt.Errorf("pegasusimport/read cancelled: %w", reader.ctx.Err())
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
		return count, fmt.Errorf("pegasusimport/read source: %w", err)
	}
	return count, nil
}
