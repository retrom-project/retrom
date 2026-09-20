package archive

import (
	"context"
	"errors"
	"io"
	"sort"
	"testing"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/format/importing"
	"retrom/internal/testkit/testsupport"
)

func consumeProject(
	ctx context.Context, t *testing.T, format contentprofile.ArchiveFormat, path string, limits importing.ArchiveLimits,
	consume func(importing.ArchiveEntry, io.Reader) (importing.ArchiveContent, error),
) ([]importing.ArchiveEntry, error) {
	t.Helper()
	cursor, err := New(&testsupport.DiagnosticRecorder{}).OpenProject(ctx, path, format, limits)
	if err != nil {
		return nil, err
	}
	closed := false
	defer func() {
		if !closed {
			if err := cursor.Close(); err != nil {
				t.Errorf("close cursor: %v", err)
			}
		}
	}()
	entries := make([]importing.ArchiveEntry, 0)
	for {
		header, err := cursor.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		content, err := consume(header.Entry, cursor)
		if err != nil {
			closeErr := cursor.Close()
			closed = true
			return nil, importing.ProjectArchiveStageError(header, err, closeErr)
		}
		entry, err := cursor.Complete(content)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Ordinal < entries[right].Ordinal })
	return entries, nil
}
