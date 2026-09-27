package filestore

import (
	"context"
	"fmt"
	"io"

	"retrom/internal/cleanup"
)

// Copy creates independent bytes and identity for a new owner.
func (store *Store) Copy(ctx context.Context, id string) (Metadata, error) {
	file, err := store.OpenRecord(id)
	if err != nil {
		return Metadata{}, err
	}
	defer func() { cleanup.Error("close copied file", file.Close()) }()
	metadata, err := store.Put(contextReader{ctx: ctx, source: file})
	if err != nil {
		return Metadata{}, fmt.Errorf("copy owned file: %w", err)
	}
	return metadata, nil
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, fmt.Errorf("copy interrupted: %w", err)
	}
	n, err := reader.source.Read(buffer)
	if err == io.EOF {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read owned file: %w", err)
	}
	return n, nil
}
