package archive

import (
	"context"
	"errors"
	"fmt"
	"io"

	"retrom/internal/capability/format/importing"
	"retrom/internal/model/diagnostics"
)

type zipCursor struct {
	ctx      context.Context
	reporter diagnostics.ErrorReporter
	archive  *zipArchive
	limits   importing.ArchiveLimits
	index    int
	active   io.ReadCloser
	monitor  *archiveReadMonitor
	failure  error
	done     bool
	closed   bool
}

func (factory *Factory) openZIP(ctx context.Context, path string, limits importing.ArchiveLimits) (*zipCursor, error) {
	archive, err := factory.loadZIP(ctx, path, limits)
	if err != nil {
		return nil, err
	}
	return &zipCursor{ctx: ctx, reporter: factory.reporter, archive: archive, limits: limits}, nil
}

func (cursor *zipCursor) Next() (importing.ArchiveMemberHeader, error) {
	if cursor.closed || cursor.active != nil {
		return importing.ArchiveMemberHeader{}, errCursorState
	}
	if cursor.failure != nil {
		return importing.ArchiveMemberHeader{}, cursor.failure
	}
	if cursor.done || cursor.index == len(cursor.archive.members) {
		cursor.done = true
		return importing.ArchiveMemberHeader{}, io.EOF
	}
	member := cursor.archive.members[cursor.index]
	reader, err := cursor.archive.reader.File[member.Ordinal].Open()
	if err != nil {
		cursor.failure = fmt.Errorf("scan archive entry %q: %w", member.Path,
			fmt.Errorf("%w: open entry", importing.ErrArchiveUnsafe))
		return importing.ArchiveMemberHeader{}, cursor.failure
	}
	cursor.active = reader
	cursor.monitor = &archiveReadMonitor{ctx: cursor.ctx, reader: reader, limit: cursor.limits.MaxEntryBytes}
	return importing.ArchiveMemberHeader{Entry: member.Entry()}, nil
}

func (cursor *zipCursor) Read(buffer []byte) (int, error) {
	if cursor.closed || cursor.active == nil {
		return 0, errCursorState
	}
	if cursor.failure != nil {
		return 0, cursor.failure
	}
	count, err := cursor.monitor.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		cursor.failure = err
	}
	return count, err
}

func (cursor *zipCursor) Complete(content importing.ArchiveContent) (importing.ArchiveEntry, error) {
	if cursor.closed || cursor.active == nil {
		return importing.ArchiveEntry{}, errCursorState
	}
	member := cursor.archive.members[cursor.index]
	var entry importing.ArchiveEntry
	err := cursor.failure
	if err == nil {
		entry, err = member.Complete(
			content, cursor.monitor.written, cursor.monitor.observedPrefix(), cursor.limits.AllowNestedArchives,
		)
	}
	// ZIP data descriptors are checked only when the member reader observes EOF.
	if err == nil && !cursor.monitor.eof {
		err = cursor.completeMemberEOF()
	}
	closeResource(cursor.ctx, cursor.reporter, cursor.active)
	cursor.active = nil
	cursor.monitor = nil
	if err != nil {
		cursor.failure = fmt.Errorf("scan archive entry %q: %w", member.Path, err)
		return importing.ArchiveEntry{}, cursor.failure
	}
	cursor.index++
	return entry, nil
}

func (cursor *zipCursor) completeMemberEOF() error {
	var single [1]byte
	count, err := cursor.monitor.Read(single[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if count != 0 || !errors.Is(err, io.EOF) {
		return importing.ErrArchiveUnsafe
	}
	return nil
}

func (cursor *zipCursor) Close() error {
	if cursor.closed {
		return nil
	}
	cursor.closed = true
	if cursor.active != nil {
		closeResource(cursor.ctx, cursor.reporter, cursor.active)
		cursor.active = nil
	}
	cursor.monitor = nil
	closeZIP(cursor.ctx, cursor.reporter, cursor.archive)
	return nil
}
